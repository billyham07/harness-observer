package source

import (
	"context"
	"encoding/json"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
)

type Window struct {
	Since *time.Time
	Until *time.Time
}

func (w Window) Contains(ts time.Time) bool {
	// Unknown timestamps are acceptable only for an unbounded dump. They must
	// never leak into a custom time slice because that makes range queries lie.
	if ts.IsZero() {
		return w.Since == nil && w.Until == nil
	}
	if w.Since != nil && ts.Before(*w.Since) {
		return false
	}
	if w.Until != nil && ts.After(*w.Until) {
		return false
	}
	return true
}

type Source interface {
	Name() string
	Roots() []string
	Collect(context.Context, Window) ([]model.Turn, error)
}

// FileState is the resumable position inside one append-only session file.
//
// Offset/Line are what an incremental scan needs to continue where it stopped.
// Meta carries the adapter's own carried-forward parse state (session id, cwd,
// current model, cumulative token baseline, ...) because a session file's
// identity fields are usually declared once on the first line. Without it a
// resumed scan would attribute later turns to an unknown session.
//
// Meta must stay small observability metadata. Never put prompts, tool output
// or credentials in it: it is persisted in the local checkpoint table.
type FileState struct {
	Offset int64           `json:"offset"`
	Line   int             `json:"line"`
	Meta   json.RawMessage `json:"meta,omitempty"`
}

// FileSource is a source that can be ingested incrementally, one session file
// at a time. Adapters stay responsible for their own format; discovery,
// checkpointing, dedup and persistence stay outside of them.
type FileSource interface {
	Source

	// Prepare runs once per scan, before any ParseFile call. Adapters that need
	// a cross-file index (Claude Desktop metadata attribution) build it here.
	Prepare(context.Context) error

	// Files lists the session files this source considers authoritative
	// evidence, already filtered for duplicates/excluded artifacts.
	Files(context.Context) ([]string, error)

	// ParseFile parses path starting at state and returns the turns it found
	// plus the new resumable state. Implementations must be pure with respect
	// to the file system: read-only, and never rewrite the source file.
	ParseFile(path string, state FileState, w Window) ([]model.Turn, FileState, error)
}

// Reporter is extra doctor output: backend, schema, coverage. Paths and
// counters only — never prompts or credentials.
type Reporter interface {
	Report() []string
}

// ContentWatch replaces size/mtime skip for backends that are not append-only
// JSONL. SQLite is the case: writes often land in a WAL file, so the main DB
// can look unchanged while new messages exist. JSONL sources must not
// implement this.
type ContentWatch interface {
	// ContentToken returns a cheap change token for path. ok is false when
	// this path should keep the default size/mtime/fingerprint skip.
	ContentToken(path string) (token string, ok bool, err error)
	SameContent(state FileState, token string) bool
}

// CollectAll is the non-incremental path (NDJSON dump / tests): walk every file
// from offset zero. Duplicate record IDs are dropped so a single scan cannot
// double count; durable runs get the same guarantee from the store's unique key.
func CollectAll(ctx context.Context, s FileSource, w Window) ([]model.Turn, error) {
	if err := s.Prepare(ctx); err != nil {
		return nil, err
	}
	files, err := s.Files(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.Turn
	seen := make(map[string]struct{})
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		turns, _, parseErr := s.ParseFile(path, FileState{}, w)
		if parseErr != nil {
			// A single unreadable/corrupt file must not abort unrelated sessions.
			continue
		}
		for _, t := range turns {
			key := t.Harness + "\x00" + t.RecordID
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, t)
		}
	}
	return out, nil
}
