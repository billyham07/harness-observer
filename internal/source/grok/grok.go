// Package grok reads Grok Build local session evidence under
// $GROK_HOME/sessions or ~/.grok/sessions.
//
// Authoritative usage comes from turn_completed.usage records. Legacy sessions
// that only expose a running context curve are reported as uncovered rather
// than estimated. Undocumented costUsdTicks is ignored.
package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

type Source struct {
	// Root is the sessions directory. Empty means GROK_HOME/sessions or ~/.grok/sessions.
	Root string

	notes []string
}

func New(root string) *Source {
	if root == "" {
		root = defaultSessionsDir()
	}
	return &Source{Root: root}
}

func (s *Source) Name() string                  { return "grok" }
func (s *Source) Roots() []string               { return []string{s.Root} }
func (s *Source) Prepare(context.Context) error { return nil }

func (s *Source) Report() []string {
	return append([]string(nil), s.notes...)
}

func (s *Source) Files(ctx context.Context) ([]string, error) {
	var out []string
	var covered, uncovered int
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() || !strings.EqualFold(d.Name(), "updates.jsonl") {
			return nil
		}
		dir := filepath.Dir(path)
		if _, statErr := os.Stat(filepath.Join(dir, "summary.json")); statErr != nil {
			return nil
		}
		out = append(out, path)
		if hasTurnCompleted(path) {
			covered++
		} else {
			uncovered++
		}
		return nil
	})
	s.notes = []string{
		"authoritative turn_completed sessions: " + strconv.Itoa(covered),
		"legacy/uncovered sessions: " + strconv.Itoa(uncovered),
	}
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	return out, nil
}

func (s *Source) Collect(ctx context.Context, w source.Window) ([]model.Turn, error) {
	return source.CollectAll(ctx, s, w)
}

type summaryFile struct {
	Info *struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	} `json:"info"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	LastActiveAt    string `json:"last_active_at"`
	CurrentModelID  string `json:"current_model_id"`
	AgentName       string `json:"agent_name"`
	ReasoningEffort string `json:"reasoning_effort"`
}

type signalsFile struct {
	PrimaryModelID string   `json:"primaryModelId"`
	ModelsUsed     []string `json:"modelsUsed"`
}

type grokUsage struct {
	InputTokens         int64                      `json:"inputTokens"`
	OutputTokens        int64                      `json:"outputTokens"`
	TotalTokens         int64                      `json:"totalTokens"`
	CachedReadTokens    int64                      `json:"cachedReadTokens"`
	CacheCreationTokens int64                      `json:"cacheCreationTokens"`
	ReasoningTokens     int64                      `json:"reasoningTokens"`
	ModelUsage          map[string]json.RawMessage `json:"modelUsage"`
}

type grokLine struct {
	Timestamp json.RawMessage `json:"timestamp"`
	Params    *struct {
		Update *struct {
			SessionUpdate string          `json:"sessionUpdate"`
			PromptID      string          `json:"prompt_id"`
			Usage         json.RawMessage `json:"usage"`
			Title         string          `json:"title"`
			Status        string          `json:"status"`
		} `json:"update"`
	} `json:"params"`
}

type parseState struct {
	SessionID    string           `json:"session_id,omitempty"`
	CWD          string           `json:"cwd,omitempty"`
	Model        string           `json:"model,omitempty"`
	Client       string           `json:"client,omitempty"`
	Effort       string           `json:"effort,omitempty"`
	PendingTools int              `json:"pending_tools,omitempty"`
	PendingErrs  int              `json:"pending_errors,omitempty"`
	Anon         int              `json:"anon,omitempty"`
	Turns        map[string]first `json:"turns,omitempty"`
}

type first struct {
	TS   string `json:"ts"`
	Line int    `json:"line"`
}

func (s *Source) ParseFile(path string, st source.FileState, window source.Window) ([]model.Turn, source.FileState, error) {
	var ps parseState
	if len(st.Meta) > 0 {
		_ = json.Unmarshal(st.Meta, &ps)
	}
	if ps.Turns == nil {
		ps.Turns = map[string]first{}
	}
	dir := filepath.Dir(path)
	if ps.SessionID == "" || ps.CWD == "" || ps.Model == "" || ps.Effort == "" || ps.Client == "" {
		sum := readSummary(filepath.Join(dir, "summary.json"))
		sig := readSignals(filepath.Join(dir, "signals.json"))
		if ps.SessionID == "" {
			if sum.Info != nil && sum.Info.ID != "" {
				ps.SessionID = sum.Info.ID
			} else {
				ps.SessionID = filepath.Base(dir)
			}
		}
		if ps.CWD == "" && sum.Info != nil {
			ps.CWD = sum.Info.CWD
		}
		if ps.Model == "" {
			ps.Model = firstNonEmpty(sum.CurrentModelID, sig.PrimaryModelID)
			if ps.Model == "" && len(sig.ModelsUsed) > 0 {
				ps.Model = sig.ModelsUsed[0]
			}
		}
		if ps.Effort == "" {
			ps.Effort = strings.TrimSpace(sum.ReasoningEffort)
		}
		if ps.Client == "" {
			ps.Client = firstNonEmpty(strings.TrimSpace(sum.AgentName), "grok-build")
		}
	}

	var out []model.Turn
	next, err := source.ScanLines(path, st, func(line []byte, lineNo int) error {
		var row grokLine
		if json.Unmarshal(line, &row) != nil || row.Params == nil || row.Params.Update == nil {
			return nil
		}
		upd := row.Params.Update
		switch upd.SessionUpdate {
		case "tool_call":
			if strings.TrimSpace(upd.Title) != "" {
				ps.PendingTools++
				if strings.EqualFold(upd.Status, "error") || strings.EqualFold(upd.Status, "failed") {
					ps.PendingErrs++
				}
			}
			return nil
		case "turn_completed":
			var usage grokUsage
			if len(upd.Usage) == 0 || json.Unmarshal(upd.Usage, &usage) != nil {
				return nil
			}
			if !hasPositiveUsage(usage) {
				return nil
			}
			promptID := strings.TrimSpace(upd.PromptID)
			if promptID == "" {
				ps.Anon++
				promptID = "turn_completed:" + strconv.Itoa(ps.Anon)
			}
			ts := parseGrokTimestamp(row.Timestamp)
			if prev, ok := ps.Turns[promptID]; ok {
				if parsed, err := time.Parse(time.RFC3339Nano, prev.TS); err == nil {
					ts = parsed
				}
				lineNo = prev.Line
			} else {
				ps.Turns[promptID] = first{TS: ts.UTC().Format(time.RFC3339Nano), Line: lineNo}
			}
			if !window.Contains(ts) {
				ps.PendingTools = 0
				ps.PendingErrs = 0
				return nil
			}
			modelID := firstModel(usage.ModelUsage, ps.Model)
			reasoning := usage.ReasoningTokens
			if reasoning > usage.OutputTokens {
				reasoning = usage.OutputTokens
			}
			var nativeTotal *int64
			if usage.TotalTokens != 0 {
				v := usage.TotalTokens
				nativeTotal = &v
			}
			u := model.NewInputIncludesCacheReadUsage(
				usage.InputTokens,
				usage.OutputTokens,
				usage.CachedReadTokens,
				usage.CacheCreationTokens,
				reasoning,
				nativeTotal,
				model.UsageObserved,
			)
			client := ps.Client
			if client == "" {
				client = "grok-build"
			}
			out = append(out, model.Turn{
				Timestamp:       ts,
				Harness:         "grok",
				Client:          client,
				Model:           modelID,
				ReasoningEffort: ps.Effort,
				Project:         projectName(ps.CWD),
				CWD:             ps.CWD,
				SessionID:       ps.SessionID,
				RecordID:        "grok:" + ps.SessionID + ":" + promptID,
				TurnID:          promptID,
				EventKind:       "assistant",
				ToolCalls:       ps.PendingTools,
				ToolErrors:      ps.PendingErrs,
				Usage:           u,
				SourceFile:      path,
				SourceLine:      lineNo,
			})
			ps.PendingTools = 0
			ps.PendingErrs = 0
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

func hasPositiveUsage(u grokUsage) bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CachedReadTokens > 0 || u.CacheCreationTokens > 0 || u.ReasoningTokens > 0
}

func firstModel(modelUsage map[string]json.RawMessage, fallback string) string {
	if len(modelUsage) == 1 {
		for id := range modelUsage {
			if strings.TrimSpace(id) != "" {
				return id
			}
		}
	}
	if len(modelUsage) > 1 {
		// Multiple models on one record: keep the session-level model rather
		// than guessing which inner map key is the one that ran.
		if fallback != "" {
			return fallback
		}
		for id := range modelUsage {
			if strings.TrimSpace(id) != "" {
				return id
			}
		}
	}
	return fallback
}

func hasTurnCompleted(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(raw, []byte(`"turn_completed"`))
}

func readSummary(path string) summaryFile {
	var s summaryFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}

func readSignals(path string) signalsFile {
	var s signalsFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}

func parseGrokTimestamp(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		if t, err := time.Parse(time.RFC3339Nano, asString); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, asString); err == nil {
			return t
		}
	}
	var asNum float64
	if json.Unmarshal(raw, &asNum) == nil && asNum > 0 {
		n := int64(asNum)
		if n < 1e12 {
			return time.Unix(n, 0).UTC()
		}
		return time.UnixMilli(n).UTC()
	}
	return time.Time{}
}

func defaultSessionsDir() string {
	if v := strings.TrimSpace(os.Getenv("GROK_HOME")); v != "" {
		return filepath.Join(v, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".grok", "sessions")
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
