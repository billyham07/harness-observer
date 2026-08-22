package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

type Source struct{ Root string }

func New(root string) *Source {
	if root == "" {
		h, _ := os.UserHomeDir()
		root = filepath.Join(h, ".codex")
	}
	return &Source{Root: root}
}

func (s *Source) Name() string { return "codex" }

func (s *Source) Roots() []string {
	return []string{filepath.Join(s.Root, "sessions"), filepath.Join(s.Root, "archived_sessions")}
}

func (s *Source) Prepare(context.Context) error { return nil }

// Files lists rollout transcripts. Archiving a session moves the same rollout
// file under archived_sessions, so the basename is the identity that must not
// be visited twice; sessions/ wins because it is the live copy.
func (s *Source) Files(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string
	for _, root := range s.Roots() {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
				return nil
			}
			if _, dup := seen[d.Name()]; dup {
				return nil
			}
			seen[d.Name()] = struct{}{}
			out = append(out, path)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return out, err
		}
	}
	return out, nil
}

func (s *Source) Collect(ctx context.Context, w source.Window) ([]model.Turn, error) {
	return source.CollectAll(ctx, s, w)
}

type tokenUsage struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Reasoning  int64 `json:"reasoning_output_tokens"`
	Total      int64 `json:"total_tokens"`
}

type entry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type          string `json:"type"`
		ID            string `json:"id"`
		TurnID        string `json:"turn_id"`
		SessionID     string `json:"session_id"`
		CWD           string `json:"cwd"`
		Model         string `json:"model"`
		ModelProvider string `json:"model_provider"`
		Originator    string `json:"originator"`
		// Effort is the reasoning effort Codex persists per turn_context.
		// collaboration_mode.settings.reasoning_effort carries the same value
		// and is only used when the flat field is absent.
		Effort            string `json:"effort"`
		CollaborationMode *struct {
			Settings *struct {
				ReasoningEffort string `json:"reasoning_effort"`
			} `json:"settings"`
		} `json:"collaboration_mode"`
		Info *struct {
			Model     string      `json:"model"`
			ModelName string      `json:"model_name"`
			Last      *tokenUsage `json:"last_token_usage"`
			Total     *tokenUsage `json:"total_token_usage"`
		} `json:"info"`
		Name string `json:"name"`
	} `json:"payload"`
}

// parseState is everything a resumed scan needs that was declared earlier in
// the file: session identity, the current model/provider, the cumulative token
// baseline used for derived deltas, and tool calls seen since the last usage
// record. It is persisted as opaque JSON in the checkpoint table, so it must
// stay observability metadata only.
type parseState struct {
	SessionID      string      `json:"session_id,omitempty"`
	CWD            string      `json:"cwd,omitempty"`
	Model          string      `json:"model,omitempty"`
	Provider       string      `json:"provider,omitempty"`
	Originator     string      `json:"originator,omitempty"`
	Effort         string      `json:"effort,omitempty"`
	PendingTools   int         `json:"pending_tools,omitempty"`
	TokenEvents    int         `json:"token_events,omitempty"`
	PreviousTotal  *tokenUsage `json:"previous_total,omitempty"`
	LastCumulative *int64      `json:"last_cumulative,omitempty"`
}

func (s *Source) ParseFile(path string, st source.FileState, window source.Window) ([]model.Turn, source.FileState, error) {
	var ps parseState
	if len(st.Meta) > 0 {
		_ = json.Unmarshal(st.Meta, &ps)
	}
	if ps.SessionID == "" {
		ps.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if ps.Provider == "" {
		ps.Provider = "openai"
	}

	var out []model.Turn
	next, err := source.ScanLines(path, st, func(line []byte, lineNo int) error {
		var row entry
		if json.Unmarshal(line, &row) != nil {
			return nil
		}
		if row.Type == "session_meta" {
			if row.Payload.ID != "" {
				ps.SessionID = row.Payload.ID
			}
			if row.Payload.SessionID != "" {
				ps.SessionID = row.Payload.SessionID
			}
			if row.Payload.CWD != "" {
				ps.CWD = row.Payload.CWD
			}
			if row.Payload.ModelProvider != "" {
				ps.Provider = row.Payload.ModelProvider
			}
			if row.Payload.Model != "" {
				ps.Model = row.Payload.Model
			}
			if row.Payload.Originator != "" {
				ps.Originator = row.Payload.Originator
			}
			return nil
		}
		if row.Type == "turn_context" {
			// Effort is read only from turn_context. session_meta also contains
			// the string "reasoningEffort", but only inside a tool's input
			// schema description, which says nothing about this session.
			if e := turnContextEffort(row); e != "" {
				ps.Effort = e
			}
		}
		if row.Payload.Model != "" {
			ps.Model = row.Payload.Model
		}
		if isToolCall(row) {
			ps.PendingTools++
			return nil
		}
		if row.Type != "event_msg" || row.Payload.Type != "token_count" || row.Payload.Info == nil {
			return nil
		}
		info := row.Payload.Info
		if info.Model != "" {
			ps.Model = info.Model
		}
		if info.ModelName != "" {
			ps.Model = info.ModelName
		}

		var native tokenUsage
		basis := model.UsageObserved
		switch {
		case info.Last != nil:
			native = *info.Last
		case info.Total != nil:
			if ps.PreviousTotal != nil {
				native = subtractUsage(*info.Total, *ps.PreviousTotal)
			} else {
				native = *info.Total
			}
			basis = model.UsageDerivedDelta
		default:
			return nil
		}
		if info.Total != nil {
			// Codex re-emits token_count on rate-limit refreshes without new
			// usage; an unchanged cumulative total means nothing was consumed.
			if ps.LastCumulative != nil && info.Total.Total == *ps.LastCumulative {
				return nil
			}
			cumulative := info.Total.Total
			ps.LastCumulative = &cumulative
			snapshot := *info.Total
			ps.PreviousTotal = &snapshot
		}
		if native == (tokenUsage{}) {
			return nil
		}
		ts := parseTime(row.Timestamp)
		if !window.Contains(ts) {
			// The ordinal is part of the record identity, so it must advance
			// for filtered-out events too; otherwise the same event would get
			// different IDs depending on the requested window.
			ps.TokenEvents++
			ps.PendingTools = 0
			return nil
		}
		var nativeTotal *int64
		if native.Total > 0 {
			v := native.Total
			nativeTotal = &v
		}
		ps.TokenEvents++
		u := model.NewInputIncludesCacheReadUsage(native.Input, native.Output, native.Cached, native.CacheWrite, native.Reasoning, nativeTotal, basis)
		out = append(out, model.Turn{
			Timestamp:       ts,
			Harness:         classifyHarness(ps.Originator),
			Client:          ps.Originator,
			Provider:        ps.Provider,
			Model:           ps.Model,
			ReasoningEffort: ps.Effort,
			Project:         projectName(ps.CWD),
			CWD:             ps.CWD,
			SessionID:       ps.SessionID,
			RecordID:        recordID(ps.SessionID, ps.TokenEvents, native),
			TurnID:          row.Payload.TurnID,
			EventKind:       "assistant",
			ToolCalls:       ps.PendingTools,
			Usage:           u,
			SourceFile:      path,
			SourceLine:      lineNo,
		})
		ps.PendingTools = 0
		return nil
	})
	if err != nil {
		return out, next, err
	}
	if meta, marshalErr := json.Marshal(ps); marshalErr == nil {
		next.Meta = meta
	}
	return out, next, nil
}

// recordID identifies one usage event inside a session.
//
// Resuming or forking a Codex conversation writes a NEW rollout file that
// replays the earlier token_count events. Verified on real rollouts: the replay
// keeps the original usage numbers but gets a fresh timestamp and a different
// line number, and several files share one session_id. So neither the
// timestamp nor the line number can be part of the identity.
//
// What is stable is the position of the event in the session (its ordinal) plus
// the usage numbers themselves. Two forks that diverge at the same ordinal
// produce different numbers and stay separate records; a replayed prefix
// collapses onto the original, which is the direction that must not lose data.
func recordID(sessionID string, ordinal int, u tokenUsage) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d|%d|%d|%d|%d|%d", u.Input, u.Cached, u.CacheWrite, u.Output, u.Reasoning, u.Total)
	return fmt.Sprintf("%s:token_count:%d:%x", sessionID, ordinal, h.Sum64())
}

// turnContextEffort reads the effort Codex persisted for one turn. Both
// observed shapes agree in real rollouts; the flat field is authoritative and
// collaboration_mode is the fallback for rollouts that only carry the nested
// form. Nothing is inferred: an absent field leaves the previous value alone.
func turnContextEffort(row entry) string {
	if v := strings.TrimSpace(row.Payload.Effort); v != "" {
		return v
	}
	if cm := row.Payload.CollaborationMode; cm != nil && cm.Settings != nil {
		return strings.TrimSpace(cm.Settings.ReasoningEffort)
	}
	return ""
}

func isToolCall(row entry) bool {
	if row.Type == "response_item" {
		switch row.Payload.Type {
		case "function_call", "custom_tool_call", "mcp_tool_call":
			return true
		}
	}
	return row.Type == "event_msg" && row.Payload.Type == "mcp_tool_call_end"
}

func subtractUsage(current, previous tokenUsage) tokenUsage {
	return tokenUsage{
		Input:      clampDelta(current.Input - previous.Input),
		Cached:     clampDelta(current.Cached - previous.Cached),
		CacheWrite: clampDelta(current.CacheWrite - previous.CacheWrite),
		Output:     clampDelta(current.Output - previous.Output),
		Reasoning:  clampDelta(current.Reasoning - previous.Reasoning),
		Total:      clampDelta(current.Total - previous.Total),
	}
}

func clampDelta(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// classifyHarness keeps the frontend identity separate from the model. The same
// ~/.codex/sessions tree is shared by Desktop, the TUI and IDE/app-server
// clients, so originator is evidence, not decoration.
func classifyHarness(originator string) string {
	v := strings.ToLower(strings.TrimSpace(originator))
	switch {
	case v == "":
		return "codex"
	case strings.Contains(v, "desktop"):
		return "codex-desktop"
	case strings.Contains(v, "tui"), strings.Contains(v, "cli"):
		return "codex-cli"
	case strings.Contains(v, "vscode"), strings.Contains(v, "ide"), strings.Contains(v, "app-server"):
		return "codex-ide"
	default:
		return "codex"
	}
}

func projectName(cwd string) string {
	v := strings.TrimRight(strings.ReplaceAll(cwd, "\\", "/"), "/")
	if v == "" {
		return ""
	}
	if i := strings.LastIndexByte(v, '/'); i >= 0 {
		return v[i+1:]
	}
	return v
}

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}
