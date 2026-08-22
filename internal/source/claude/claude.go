// Package claude reads Claude transcripts (~/.claude/projects plus the
// transcripts nested under a Claude Desktop session root) and splits them by
// the frontend that produced each turn.
//
// One transcript tree is shared by several frontends, so attribution is
// evidence rather than a directory convention: each row carries its own
// `entrypoint` ("cli", "claude-desktop", ...), which is what separates
// harness=claude-code from harness=claude-desktop. A session that was started
// in one frontend and resumed in the other is therefore split at the turn where
// the evidence changes instead of being attributed wholesale to either.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

type Source struct {
	RootsOverride    []string
	ProjectsOverride string

	mu       sync.Mutex
	attached map[string]attachment // comparable transcript path -> Desktop attribution
}

func New(roots ...string) *Source { return &Source{RootsOverride: roots} }

// Name is the ingestion/checkpoint identity of the adapter. It stays a single
// source even though it emits several harnesses: the frontends share one
// transcript tree, and Desktop attribution needs the cross-file index that a
// split adapter would have to build twice.
func (s *Source) Name() string { return "claude" }

// Roots is what `doctor` reports: the Desktop session roots plus the shared
// ~/.claude/projects tree.
func (s *Source) Roots() []string {
	out := s.desktopRoots()
	if projects := s.projectsRoot(); projects != "" {
		out = append(out, projects)
	}
	return out
}

// desktopRoots are the Claude Desktop session roots only. They are the fallback
// evidence for a transcript that records no entrypoint of its own.
func (s *Source) desktopRoots() []string {
	if len(s.RootsOverride) > 0 {
		return append([]string(nil), s.RootsOverride...)
	}
	return defaultRoots()
}

func defaultRoots() []string {
	home, _ := os.UserHomeDir()
	var bases []string
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			bases = []string{filepath.Join(appData, "Claude"), filepath.Join(appData, "Claude-3p")}
		}
	case "darwin":
		bases = []string{
			filepath.Join(home, "Library", "Application Support", "Claude"),
			filepath.Join(home, "Library", "Application Support", "Claude-3p"),
		}
	default:
		bases = []string{filepath.Join(home, ".config", "Claude")}
	}
	var out []string
	for _, b := range bases {
		out = append(out, filepath.Join(b, "local-agent-mode-sessions"), filepath.Join(b, "claude-code-sessions"))
	}
	return out
}

// projectsRoot is the shared ~/.claude/projects tree. An explicit RootsOverride
// without a ProjectsOverride means a caller pinned the scan to a temporary tree
// (tests, `--roots`), so the real home directory is deliberately not reached.
func (s *Source) projectsRoot() string {
	if s.ProjectsOverride != "" {
		return s.ProjectsOverride
	}
	if len(s.RootsOverride) > 0 {
		return ""
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// attachment ties a Desktop conversation to the transcript it claims. Path is
// the real path to open; the map key is only a comparison form (lowercased),
// which must never be used as a path on a case-sensitive filesystem.
type attachment struct {
	Path string
	Meta desktopMeta
}

type desktopMeta struct {
	CLISessionID        string   `json:"cliSessionId"`
	SessionID           string   `json:"sessionId"`
	Title               string   `json:"title"`
	Model               string   `json:"model"`
	CWD                 string   `json:"cwd"`
	OriginCWD           string   `json:"originCwd"`
	UserSelectedFolders []string `json:"userSelectedFolders"`
	LastActivityAt      int64    `json:"lastActivityAt"`
	metadataMtime       int64
}

type outputDetails struct {
	Thinking int64 `json:"thinking_tokens"`
}

type usage struct {
	Input         int64          `json:"input_tokens"`
	Output        int64          `json:"output_tokens"`
	CacheRead     int64          `json:"cache_read_input_tokens"`
	CacheCreate   int64          `json:"cache_creation_input_tokens"`
	OutputDetails *outputDetails `json:"output_tokens_details"`
}

type entry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	UUID      string `json:"uuid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	// Effort is persisted per assistant entry by Claude Code / Claude Desktop
	// transcripts. It is read per row rather than carried forward: a row that
	// does not declare one stays unknown instead of inheriting a neighbour's.
	Effort string `json:"effort"`
	// Entrypoint is the frontend that wrote the row ("cli", "claude-desktop",
	// ...). Claude Code and Claude Desktop share one transcript tree, so this
	// is the only per-turn evidence of which one produced the call.
	Entrypoint string `json:"entrypoint"`
	Message    *struct {
		ID      string          `json:"id"`
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Usage   *usage          `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// Prepare builds the Desktop metadata index. Desktop keeps a `local_*.json`
// descriptor per conversation whose cliSessionId points at the authoritative
// Claude transcript; the descriptor itself is not usage evidence.
func (s *Source) Prepare(ctx context.Context) error {
	index := s.desktopIndex(ctx)
	attached := make(map[string]attachment, len(index))
	if len(index) > 0 {
		_ = filepath.WalkDir(s.projectsRoot(), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || ctx.Err() != nil {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
				return nil
			}
			cliID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if meta, ok := index[cliID]; ok {
				attached[cleanPath(path)] = attachment{Path: path, Meta: meta}
			}
			return nil
		})
	}
	s.mu.Lock()
	s.attached = attached
	s.mu.Unlock()
	return nil
}

// Files returns every Claude transcript: the ones nested under a Desktop
// session root and the whole shared ~/.claude/projects tree, which is where
// both the CLI and the Desktop-claimed sessions actually live. The sets are
// de-duplicated by resolved path so a session reachable both ways is not
// counted twice. audit.jsonl is excluded: it is a streaming/partial mirror of
// the same assistant events.
func (s *Source) Files(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string
	add := func(path string) {
		key := cleanPath(path)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}

	transcripts := func(root string, requireProjectsPath bool) error {
		if root == "" {
			return nil
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			if !strings.HasSuffix(name, ".jsonl") || name == "audit.jsonl" {
				return nil
			}
			if requireProjectsPath && !strings.Contains(filepath.ToSlash(path), "/.claude/projects/") {
				return nil
			}
			add(path)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	for _, root := range s.desktopRoots() {
		if err := transcripts(root, true); err != nil {
			return out, err
		}
	}
	if err := transcripts(s.projectsRoot(), false); err != nil {
		return out, err
	}

	// Desktop-claimed transcripts are already covered when the projects tree is
	// scanned, but a claim can point outside it (a custom projects location),
	// and add() de-duplicates either way.
	s.mu.Lock()
	attached := s.attached
	s.mu.Unlock()
	for _, a := range attached {
		add(a.Path)
	}
	return out, nil
}

func (s *Source) Collect(ctx context.Context, w source.Window) ([]model.Turn, error) {
	return source.CollectAll(ctx, s, w)
}

type parseState struct {
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
	// Entrypoint is carried forward because assistant rows are not the only
	// rows that declare it; a resumed scan must not lose the attribution that
	// an earlier chunk of the same file established.
	Entrypoint string `json:"entrypoint,omitempty"`
}

func (s *Source) ParseFile(path string, st source.FileState, window source.Window) ([]model.Turn, source.FileState, error) {
	s.mu.Lock()
	a, hasMeta := s.attached[cleanPath(path)]
	s.mu.Unlock()
	meta := a.Meta

	var ps parseState
	if len(st.Meta) > 0 {
		_ = json.Unmarshal(st.Meta, &ps)
	}
	if ps.SessionID == "" {
		ps.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if hasMeta && meta.SessionID != "" {
			ps.SessionID = meta.SessionID
		}
	}
	if ps.CWD == "" && hasMeta {
		ps.CWD = firstNonEmpty(meta.OriginCWD, firstFolder(meta.UserSelectedFolders), meta.CWD)
	}
	if ps.Model == "" && hasMeta {
		ps.Model = meta.Model
	}
	// A Desktop claim (or a transcript stored inside a Desktop session root) is
	// the fallback attribution for rows that record no entrypoint of their own.
	desktopEvidence := hasMeta || s.underDesktopRoot(path)

	var out []model.Turn
	next, err := source.ScanLines(path, st, func(line []byte, lineNo int) error {
		var row entry
		if json.Unmarshal(line, &row) != nil {
			return nil
		}
		if row.CWD != "" {
			ps.CWD = row.CWD
		}
		if row.Entrypoint != "" {
			ps.Entrypoint = row.Entrypoint
		}
		if row.SessionID != "" {
			// The transcript's own sessionId is the identity both discovery
			// paths agree on, so it is what keeps a Desktop-attached session
			// and a directly discovered one from becoming two sessions.
			ps.SessionID = row.SessionID
		}
		if row.Type != "assistant" || row.Message == nil || row.Message.Role != "assistant" || row.Message.Usage == nil {
			return nil
		}
		raw := row.Message.Usage
		if raw.Input == 0 && raw.Output == 0 && raw.CacheRead == 0 && raw.CacheCreate == 0 {
			// Synthetic/injected assistant messages carry a usage object with
			// no counters. They are not model calls, so they are not evidence.
			return nil
		}
		ts := parseTime(row.Timestamp)
		if !window.Contains(ts) {
			return nil
		}
		id := firstNonEmpty(row.Message.ID, row.UUID, fmt.Sprintf("%s:%d", ps.SessionID, lineNo))
		if row.Message.Model != "" {
			ps.Model = row.Message.Model
		}
		var reasoning int64
		if raw.OutputDetails != nil {
			// Anthropic reports thinking inside output_tokens; keep it as a
			// visible sub-bucket, never as an extra additive bucket.
			reasoning = raw.OutputDetails.Thinking
		}
		u := model.NewSplitUsage(raw.Input, raw.Output, raw.CacheRead, raw.CacheCreate, reasoning, nil, model.UsageObserved)
		harness, client := classify(ps.Entrypoint, desktopEvidence)
		out = append(out, model.Turn{
			Timestamp:       ts,
			Harness:         harness,
			Client:          client,
			Provider:        "anthropic",
			Model:           ps.Model,
			ReasoningEffort: strings.TrimSpace(row.Effort),
			Project:         projectName(ps.CWD),
			CWD:             ps.CWD,
			SessionID:       ps.SessionID,
			RecordID:        "claude:" + id,
			EventKind:       "assistant",
			ToolCalls:       countTools(row.Message.Content),
			Usage:           u,
			SourceFile:      path,
			SourceLine:      lineNo,
		})
		return nil
	})
	if err != nil {
		return out, next, err
	}
	if metaJSON, marshalErr := json.Marshal(ps); marshalErr == nil {
		next.Meta = metaJSON
	}
	return out, next, nil
}

// classify maps the transcript's own entrypoint to a harness and a client.
//
// The harness answers "which frontend produced this turn", which is a different
// question from "which vendor/model served it": Claude Code and Claude Desktop
// write into the same tree, so lumping them together made the shared
// ~/.claude/projects sessions unattributable. An entrypoint this build has not
// seen keeps the generic "claude" harness and reports its raw value as the
// client rather than being guessed into one of the known frontends.
func classify(entrypoint string, desktopEvidence bool) (harness, client string) {
	raw := strings.TrimSpace(entrypoint)
	switch v := strings.ToLower(raw); {
	case v == "":
		if desktopEvidence {
			return "claude-desktop", "Claude Desktop"
		}
		return "claude-code", "Claude Code CLI"
	case strings.Contains(v, "desktop"):
		return "claude-desktop", "Claude Desktop"
	case v == "cli", strings.Contains(v, "terminal"):
		return "claude-code", "Claude Code CLI"
	case strings.Contains(v, "vscode"), strings.Contains(v, "jetbrains"), strings.Contains(v, "ide"):
		return "claude-ide", raw
	case strings.Contains(v, "sdk"), strings.Contains(v, "action"):
		return "claude-sdk", raw
	default:
		return "claude", raw
	}
}

// underDesktopRoot reports whether a transcript lives inside a Desktop session
// root. Path comparison is done on the same normalized form the attachment
// index uses, so a case-insensitive filesystem does not produce a false miss.
func (s *Source) underDesktopRoot(path string) bool {
	target := cleanPath(path)
	for _, root := range s.desktopRoots() {
		if root == "" {
			continue
		}
		prefix := cleanPath(root)
		if strings.HasPrefix(target, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Source) desktopIndex(ctx context.Context) map[string]desktopMeta {
	result := map[string]desktopMeta{}
	for _, root := range s.desktopRoots() {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || ctx.Err() != nil {
				return nil
			}
			name := strings.ToLower(d.Name())
			if !strings.HasPrefix(name, "local_") || !strings.HasSuffix(name, ".json") {
				return nil
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			var m desktopMeta
			if json.Unmarshal(b, &m) != nil || m.CLISessionID == "" {
				return nil
			}
			if info, statErr := os.Stat(path); statErr == nil {
				m.metadataMtime = info.ModTime().UnixMilli()
			}
			previous, exists := result[m.CLISessionID]
			if !exists || m.LastActivityAt > previous.LastActivityAt ||
				(m.LastActivityAt == previous.LastActivityAt && m.metadataMtime > previous.metadataMtime) {
				result[m.CLISessionID] = m
			}
			return nil
		})
	}
	return result
}

func firstFolder(v []string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func cleanPath(v string) string {
	if p, err := filepath.Abs(v); err == nil {
		v = p
	}
	return strings.ToLower(filepath.Clean(v))
}

func countTools(raw json.RawMessage) int {
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) != nil {
		return 0
	}
	n := 0
	for _, b := range blocks {
		if t, _ := b["type"].(string); t == "tool_use" {
			n++
		}
	}
	return n
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
