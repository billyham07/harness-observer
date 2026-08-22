package codex

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/source"
)

// Codex persists the reasoning effort of a turn in turn_context.payload.effort.
// It is declared once per turn and applies to the token_count events that
// follow, so a usage record that arrives after a change must carry the new
// value and not the one it started the session with.
func TestCodexEffortComesFromTurnContextAndFollowsChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "2026", "08", "20", "rollout-2026-08-20T10-00-00-019ff900.jsonl")
	writeSession(t, path, `{"type":"session_meta","timestamp":"2026-08-20T10:00:00Z","payload":{"id":"019ff900","cwd":"/work/demo","originator":"Codex Desktop","model_provider":"openai"}}
{"type":"turn_context","timestamp":"2026-08-20T10:00:01Z","payload":{"turn_id":"t1","cwd":"/work/demo","effort":"low","collaboration_mode":{"mode":"default","settings":{"reasoning_effort":"low"}}}}
{"type":"event_msg","timestamp":"2026-08-20T10:00:05Z","payload":{"type":"token_count","info":{"model":"gpt-5.6-sol","last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"total_tokens":110}}}}
{"type":"turn_context","timestamp":"2026-08-20T10:05:00Z","payload":{"turn_id":"t2","cwd":"/work/demo","effort":"xhigh"}}
{"type":"event_msg","timestamp":"2026-08-20T10:05:09Z","payload":{"type":"token_count","info":{"model":"gpt-5.6-sol","last_token_usage":{"input_tokens":200,"cached_input_tokens":80,"output_tokens":20,"total_tokens":220}}}}
`)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ReasoningEffort != "low" {
		t.Fatalf("first turn effort = %q, want low", rows[0].ReasoningEffort)
	}
	if rows[1].ReasoningEffort != "xhigh" {
		t.Fatalf("second turn effort = %q, want xhigh", rows[1].ReasoningEffort)
	}
}

// Real rollouts also carry the effort under collaboration_mode.settings. A
// rollout that only has the nested form must still be attributed.
func TestCodexEffortFallsBackToCollaborationModeSettings(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "rollout-2026-08-20T11-00-00-019ff901.jsonl")
	writeSession(t, path, `{"type":"session_meta","timestamp":"2026-08-20T11:00:00Z","payload":{"id":"019ff901","cwd":"/work/demo","originator":"codex-cli"}}
{"type":"turn_context","timestamp":"2026-08-20T11:00:01Z","payload":{"turn_id":"t1","collaboration_mode":{"mode":"default","settings":{"reasoning_effort":"max"}}}}
{"type":"event_msg","timestamp":"2026-08-20T11:00:05Z","payload":{"type":"token_count","info":{"model":"gpt-5.6-luna","last_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}
`)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ReasoningEffort != "max" {
		t.Fatalf("nested effort not attributed: %+v", rows)
	}
}

// session_meta advertises a cron tool whose input schema mentions
// "reasoningEffort" with an enum of levels in its description. That is a tool
// definition, not this session's setting, and must never become evidence.
func TestCodexToolSchemaMentioningEffortIsNotEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "rollout-2026-08-20T12-00-00-019ff902.jsonl")
	writeSession(t, path, `{"type":"session_meta","timestamp":"2026-08-20T12:00:00Z","payload":{"id":"019ff902","cwd":"/work/demo","originator":"Codex Desktop","dynamic_tools":[{"name":"cron","inputSchema":{"properties":{"reasoningEffort":{"type":"string","description":"One of none, minimal, low, medium, high, xhigh."}}}}]}}
{"type":"event_msg","timestamp":"2026-08-20T12:00:05Z","payload":{"type":"token_count","info":{"model":"gpt-5.6-sol","last_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}
`)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].ReasoningEffort != "" {
		t.Fatalf("effort %q invented from a tool schema", rows[0].ReasoningEffort)
	}
}

// turn_context appears once, near the top of a long rollout. An incremental
// resume starts past it, so the effort has to survive in the checkpoint state
// or every appended turn would lose the dimension.
func TestCodexEffortSurvivesIncrementalResume(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "rollout-2026-08-20T13-00-00-019ff903.jsonl")
	head := `{"type":"session_meta","timestamp":"2026-08-20T13:00:00Z","payload":{"id":"019ff903","cwd":"/work/demo","originator":"Codex Desktop"}}
{"type":"turn_context","timestamp":"2026-08-20T13:00:01Z","payload":{"turn_id":"t1","effort":"high"}}
{"type":"event_msg","timestamp":"2026-08-20T13:00:05Z","payload":{"type":"token_count","info":{"model":"gpt-5.6-sol","last_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}
`
	writeSession(t, path, head)
	s := New(root)
	first, state, err := s.ParseFile(path, source.FileState{}, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ReasoningEffort != "high" {
		t.Fatalf("first pass: %+v", first)
	}
	var carried parseState
	if err := json.Unmarshal(state.Meta, &carried); err != nil {
		t.Fatal(err)
	}
	if carried.Effort != "high" {
		t.Fatalf("checkpoint state lost the effort: %+v", carried)
	}

	appended := head + `{"type":"event_msg","timestamp":"2026-08-20T13:10:00Z","payload":{"type":"token_count","info":{"model":"gpt-5.6-sol","last_token_usage":{"input_tokens":20,"output_tokens":4,"total_tokens":24}}}}
`
	writeSession(t, path, appended)
	resumed, _, err := s.ParseFile(path, state, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed) != 1 {
		t.Fatalf("resume parsed %d turns, want 1", len(resumed))
	}
	if resumed[0].ReasoningEffort != "high" {
		t.Fatalf("resumed turn effort = %q, want high", resumed[0].ReasoningEffort)
	}
}
