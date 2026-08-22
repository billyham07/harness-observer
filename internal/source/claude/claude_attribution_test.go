package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/source"
)

// writeProjects lays out a standalone ~/.claude/projects tree with no Desktop
// session root in sight, which is what a CLI-only machine looks like.
func writeProjects(t *testing.T, files map[string]string) *Source {
	t.Helper()
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	for name, body := range files {
		path := filepath.Join(projects, "demo", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := New(filepath.Join(root, "no-desktop"))
	s.ProjectsOverride = projects
	return s
}

func collect(t *testing.T, s *Source) []struct{ Harness, Client string } {
	t.Helper()
	rows, err := s.Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]struct{ Harness, Client string }, 0, len(rows))
	for _, r := range rows {
		out = append(out, struct{ Harness, Client string }{r.Harness, r.Client})
	}
	return out
}

// Claude Code and Claude Desktop write into the same ~/.claude/projects tree,
// so the per-row entrypoint is the only thing that tells them apart.
func TestEntrypointSplitsCLIFromDesktop(t *testing.T) {
	s := writeProjects(t, map[string]string{
		"cli.jsonl":     `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","entrypoint":"cli","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n",
		"desktop.jsonl": `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s2","entrypoint":"claude-desktop","message":{"id":"msg-2","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n",
	})
	got := collect(t, s)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	seen := map[string]string{}
	for _, r := range got {
		seen[r.Harness] = r.Client
	}
	if seen["claude-code"] != "Claude Code CLI" {
		t.Fatalf("cli entrypoint not attributed to claude-code: %+v", got)
	}
	if seen["claude-desktop"] != "Claude Desktop" {
		t.Fatalf("desktop entrypoint not attributed to claude-desktop: %+v", got)
	}
}

// The entrypoint is declared on user rows too, and an assistant row that omits
// it belongs to the same frontend as the rest of its transcript.
func TestEntrypointCarriesForwardWithinATranscript(t *testing.T) {
	s := writeProjects(t, map[string]string{
		"s.jsonl": `{"type":"user","timestamp":"2026-08-20T10:59:00Z","sessionId":"s1","entrypoint":"claude-desktop","message":{"role":"user"}}` + "\n" +
			`{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n",
	})
	got := collect(t, s)
	if len(got) != 1 || got[0].Harness != "claude-desktop" {
		t.Fatalf("entrypoint from an earlier row was not carried forward: %+v", got)
	}
}

// A transcript that records no entrypoint at all is CLI unless something
// Desktop-side claims it. Guessing the other way round would relabel every
// pre-entrypoint CLI session as Desktop usage.
func TestTranscriptWithoutEntrypointDefaultsToCLI(t *testing.T) {
	s := writeProjects(t, map[string]string{
		"s.jsonl": `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n",
	})
	got := collect(t, s)
	if len(got) != 1 || got[0].Harness != "claude-code" {
		t.Fatalf("unclaimed transcript should be claude-code: %+v", got)
	}
}

// A Desktop conversation can claim a transcript that the CLI actually wrote
// (Desktop resuming a CLI session). The rows say who produced them, and the
// claim does not get to overrule them.
func TestRowEntrypointBeatsTheDesktopClaim(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "desktop", "local-agent-mode-sessions", "workspace")
	projects := filepath.Join(root, "projects", "demo")
	for _, dir := range []string{desktop, projects} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	meta := `{"cliSessionId":"cli-1","sessionId":"desktop-1","model":"claude-opus-5","originCwd":"C:/work/demo"}`
	if err := os.WriteFile(filepath.Join(desktop, "local_1.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","entrypoint":"cli","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, "cli-1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(filepath.Join(root, "desktop", "local-agent-mode-sessions"))
	s.ProjectsOverride = filepath.Join(root, "projects")
	got := collect(t, s)
	if len(got) != 1 || got[0].Harness != "claude-code" {
		t.Fatalf("claimed transcript ignored its own entrypoint: %+v", got)
	}
}

// An entrypoint this build has never seen must not be forced into one of the
// known frontends: it keeps the generic harness and reports itself as client.
func TestUnknownEntrypointStaysGeneric(t *testing.T) {
	s := writeProjects(t, map[string]string{
		"s.jsonl": `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","entrypoint":"some-future-frontend","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n",
	})
	got := collect(t, s)
	if len(got) != 1 || got[0].Harness != "claude" || got[0].Client != "some-future-frontend" {
		t.Fatalf("unknown entrypoint was guessed into a known frontend: %+v", got)
	}
}
