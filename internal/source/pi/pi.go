package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

type Source struct {
	Harness string
	Root    string
}

func New(harness, root string) *Source {
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, "."+harness, "agent", "sessions")
	}
	return &Source{Harness: harness, Root: root}
}

func (s *Source) Name() string                  { return s.Harness }
func (s *Source) Roots() []string               { return []string{s.Root} }
func (s *Source) Prepare(context.Context) error { return nil }

func (s *Source) Files(ctx context.Context) ([]string, error) {
	var out []string
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	return out, nil
}

func (s *Source) Collect(ctx context.Context, w source.Window) ([]model.Turn, error) {
	return source.CollectAll(ctx, s, w)
}

type usageCost struct {
	Total float64 `json:"total"`
}

type usage struct {
	Input       int64      `json:"input"`
	Output      int64      `json:"output"`
	CacheRead   int64      `json:"cacheRead"`
	CacheWrite  int64      `json:"cacheWrite"`
	Reasoning   int64      `json:"reasoning"`
	TotalTokens *int64     `json:"totalTokens"`
	Cost        *usageCost `json:"cost"`
}

type entry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ParentID  string `json:"parentId"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
	Provider  string `json:"provider"`
	ModelID   string `json:"modelId"`
	// ThinkingLevel is only meaningful on thinking_level_change. It is a
	// pointer because the event distinguishes "no thinking level" (explicit
	// null) from a level string, and both are evidence.
	ThinkingLevel *string `json:"thinkingLevel"`
	Usage         *usage  `json:"usage"`
	Message       *struct {
		Role       string          `json:"role"`
		Model      string          `json:"model"`
		Provider   string          `json:"provider"`
		ResponseID string          `json:"responseId"`
		Usage      *usage          `json:"usage"`
		IsError    bool            `json:"isError"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

// parseState carries the session identity and the active model/provider across
// an incremental resume. Pi declares them once in the session header and then
// only on change, so a resumed scan without this would lose attribution.
type parseState struct {
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Effort    string `json:"effort,omitempty"`
}

func (s *Source) ParseFile(path string, st source.FileState, window source.Window) ([]model.Turn, source.FileState, error) {
	var ps parseState
	if len(st.Meta) > 0 {
		_ = json.Unmarshal(st.Meta, &ps)
	}
	if ps.SessionID == "" {
		ps.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	var out []model.Turn
	next, err := source.ScanLines(path, st, func(line []byte, lineNo int) error {
		var e entry
		if json.Unmarshal(line, &e) != nil {
			return nil
		}
		switch e.Type {
		case "session":
			if e.ID != "" {
				ps.SessionID = e.ID
			}
			if e.CWD != "" {
				ps.CWD = e.CWD
			}
			return nil
		case "model_change":
			if e.Provider != "" {
				ps.Provider = e.Provider
			}
			if e.ModelID != "" {
				ps.Model = e.ModelID
			}
			return nil
		case "thinking_level_change":
			// The event is the authority in both directions: a level applies to
			// every later usage event until the next change, and an explicit
			// null clears it back to unknown rather than leaving a stale level
			// attached to calls that no longer used it.
			if e.ThinkingLevel == nil {
				ps.Effort = ""
			} else {
				ps.Effort = strings.TrimSpace(*e.ThinkingLevel)
			}
			return nil
		}

		if e.Type == "message" && e.Message != nil {
			msg := e.Message
			if msg.Provider != "" {
				ps.Provider = msg.Provider
			}
			if msg.Model != "" {
				ps.Model = msg.Model
			}
			switch msg.Role {
			case "assistant":
				if msg.Usage == nil {
					return nil
				}
				if t := s.makeTurn(path, lineNo, ps, msg.ResponseID, e.ID, e.ParentID, "assistant", msg.Usage, countToolCalls(msg.Content), 0, e.Timestamp, window); t != nil {
					out = append(out, *t)
				}
			case "toolResult":
				// Tool results can carry their own model usage (nested LLM
				// calls). Dropping them would silently under-report a harness.
				if msg.Usage == nil {
					return nil
				}
				toolErrors := 0
				if msg.IsError {
					toolErrors = 1
				}
				if t := s.makeTurn(path, lineNo, ps, "", e.ID, e.ParentID, "tool-llm", msg.Usage, 0, toolErrors, e.Timestamp, window); t != nil {
					out = append(out, *t)
				}
			}
			return nil
		}

		if (e.Type == "compaction" || e.Type == "branch_summary") && e.Usage != nil {
			kind := strings.ReplaceAll(e.Type, "_", "-")
			if t := s.makeTurn(path, lineNo, ps, "", e.ID, e.ParentID, kind, e.Usage, 0, 0, e.Timestamp, window); t != nil {
				out = append(out, *t)
			}
		}
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

func (s *Source) makeTurn(path string, lineNo int, ps parseState, responseID, entryID, logicalTurnID, kind string, raw *usage, toolCalls, toolErrors int, timestamp string, window source.Window) *model.Turn {
	if raw == nil {
		return nil
	}
	if raw.Input == 0 && raw.Output == 0 && raw.CacheRead == 0 && raw.CacheWrite == 0 && (raw.TotalTokens == nil || *raw.TotalTokens == 0) {
		return nil
	}
	ts := parseTime(timestamp)
	if !window.Contains(ts) {
		return nil
	}
	u := model.NewSplitUsage(raw.Input, raw.Output, raw.CacheRead, raw.CacheWrite, raw.Reasoning, raw.TotalTokens, model.UsageObserved)
	var costUSD *float64
	if raw.Cost != nil {
		v := raw.Cost.Total
		costUSD = &v
	}
	return &model.Turn{
		Timestamp:       ts,
		Harness:         s.Harness,
		Client:          s.Harness,
		Provider:        ps.Provider,
		Model:           ps.Model,
		ReasoningEffort: ps.Effort,
		Project:         projectName(ps.CWD),
		CWD:             ps.CWD,
		SessionID:       ps.SessionID,
		RecordID:        s.recordID(ps.SessionID, kind, responseID, entryID, timestamp, lineNo),
		TurnID:          logicalTurnID,
		EventKind:       kind,
		ToolCalls:       toolCalls,
		ToolErrors:      toolErrors,
		Usage:           u,
		CostUSD:         costUSD,
		SourceFile:      path,
		SourceLine:      lineNo,
	}
}

// recordID builds a globally stable identity for one turn across session forks
// and continuations.
//
// Pi/OMP copies parent session history into child session files when branching,
// compacting or continuing a session. If the record identity included the child's
// session ID, each historical turn would receive a fresh identity in every child
// file and be counted multiple times.
//
// We anchor identity on the authoritative underlying evidence:
//  1. The model provider's completion response ID (msg.responseId) when present,
//     which is globally unique per model call.
//  2. The entry ID + precise timestamp (e.id@timestamp) for tool-calls, compactions,
//     or providers omitting response IDs. In Pi, entry IDs and timestamps are
//     immutable and preserved verbatim when copied across sessions.
//  3. A file-line fallback only when neither is available.
func (s *Source) recordID(sessionID, kind, responseID, entryID, timestamp string, lineNo int) string {
	if respid := strings.TrimSpace(responseID); respid != "" {
		return s.Harness + ":" + kind + ":resp:" + respid
	}
	if eid := strings.TrimSpace(entryID); eid != "" {
		if ts := strings.TrimSpace(timestamp); ts != "" {
			return s.Harness + ":" + kind + ":entry:" + eid + "@" + ts
		}
		return s.Harness + ":" + kind + ":entry:" + eid
	}
	return fmt.Sprintf("%s:%s:%s:line:%d", s.Harness, sessionID, kind, lineNo)
}

func countToolCalls(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) != nil {
		return 0
	}
	n := 0
	for _, b := range blocks {
		if t, _ := b["type"].(string); t == "toolCall" || t == "tool_use" {
			n++
		}
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
