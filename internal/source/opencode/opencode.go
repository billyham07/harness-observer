// Package opencode reads OpenCode local session evidence: file-based JSON under
// storage/ (current releases) and SQLite opencode*.db (older/variant installs).
//
// Child/subagent sessions are not independent conversations. Their assistant
// messages are still emitted, but SessionID is the root session so totals do
// not appear twice.
package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

type Source struct {
	// RootsOverride pins discovery to exact data directories (tests, forks).
	// Empty means the default cross-platform search list.
	RootsOverride []string

	mu       sync.Mutex
	sessions map[string]sessionMeta
	notes    []string
}

type sessionMeta struct {
	ID       string
	ParentID string
	CWD      string
	Title    string
	Variant  string
	Agent    string
}

func New(roots ...string) *Source {
	return &Source{RootsOverride: roots}
}

func (s *Source) Name() string { return "opencode" }

func (s *Source) Roots() []string {
	if len(s.RootsOverride) > 0 {
		return append([]string(nil), s.RootsOverride...)
	}
	return defaultDataDirs()
}

func (s *Source) Prepare(ctx context.Context) error {
	sessions := make(map[string]sessionMeta)
	var notes []string
	var fileJSON, sqliteN int
	for _, root := range s.Roots() {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := indexSessionFiles(root, sessions)
		fileJSON += n
		dbs := sqliteFiles(root)
		sqliteN += len(dbs)
		for _, dbPath := range dbs {
			if err := indexSQLiteSessions(dbPath, sessions); err != nil {
				notes = append(notes, err.Error())
			}
		}
	}
	switch {
	case fileJSON > 0 && sqliteN > 0:
		notes = append([]string{fmt.Sprintf("backend file-json (%d session files) + sqlite (%d db)", fileJSON, sqliteN)}, notes...)
	case fileJSON > 0:
		notes = append([]string{fmt.Sprintf("backend file-json (%d session files)", fileJSON)}, notes...)
	case sqliteN > 0:
		notes = append([]string{fmt.Sprintf("backend sqlite (%d db)", sqliteN)}, notes...)
	default:
		notes = append([]string{"backend none (no storage/ or opencode*.db)"}, notes...)
	}
	s.mu.Lock()
	s.sessions = sessions
	s.notes = notes
	s.mu.Unlock()
	return nil
}

func (s *Source) Report() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.notes...)
}

func (s *Source) Files(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string
	add := func(path string) {
		key := strings.ToLower(filepath.Clean(path))
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	for _, root := range s.Roots() {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		msgRoot := filepath.Join(root, "storage", "message")
		_ = filepath.WalkDir(msgRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
				add(path)
			}
			return nil
		})
		for _, dbPath := range sqliteFiles(root) {
			add(dbPath)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Source) Collect(ctx context.Context, w source.Window) ([]model.Turn, error) {
	return source.CollectAll(ctx, s, w)
}

func (s *Source) ParseFile(path string, st source.FileState, window source.Window) ([]model.Turn, source.FileState, error) {
	if isSQLitePath(path) {
		return s.parseSQLite(path, st, window)
	}
	return s.parseMessageFile(path, st, window)
}

func (s *Source) parseMessageFile(path string, st source.FileState, window source.Window) ([]model.Turn, source.FileState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, st, nil
		}
		return nil, st, err
	}
	var msg messageData
	if json.Unmarshal(raw, &msg) != nil {
		return nil, st, nil
	}
	sessionID := firstNonEmpty(msg.SessionID, filepath.Base(filepath.Dir(path)))
	msgID := firstNonEmpty(msg.ID, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	parts := readParts(filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), "part", msgID))
	t := s.turnFromMessage(path, 1, sessionID, msgID, msg, parts, window)
	next := st
	if info, statErr := os.Stat(path); statErr == nil {
		next.Offset = info.Size()
		next.Line = 1
	}
	if t == nil {
		return nil, next, nil
	}
	return []model.Turn{*t}, next, nil
}

func (s *Source) turnFromMessage(path string, line int, sessionID, msgID string, msg messageData, parts []partData, window source.Window) *model.Turn {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	if role != "assistant" && role != "model" {
		return nil
	}
	in, outTok, cacheRead, cacheWrite, reasoning, nativeTotal := msg.tokenBuckets()
	toolCalls, toolErrors, hasActivity := inspectParts(parts)
	allZero := in == 0 && outTok == 0 && cacheRead == 0 && cacheWrite == 0 && reasoning == 0
	if allZero && msg.Cost == 0 && !hasActivity {
		return nil
	}
	ts := parseMillis(msg.Time.Completed)
	if ts.IsZero() {
		ts = parseMillis(msg.Time.Created)
	}
	if !window.Contains(ts) {
		return nil
	}
	s.mu.Lock()
	meta, ok := s.sessions[sessionID]
	s.mu.Unlock()
	rootID := sessionID
	cwd := ""
	if msg.Path != nil {
		cwd = msg.Path.CWD
	}
	effort := strings.TrimSpace(msg.Variant)
	if ok {
		rootID = s.rootID(sessionID)
		if meta.CWD != "" {
			cwd = meta.CWD
		}
		if effort == "" {
			effort = meta.Variant
		}
	}
	modelID := firstNonEmpty(msg.ModelID, msg.Model)
	provider := strings.TrimSpace(msg.ProviderID)
	u := model.NewSplitUsage(in, outTok, cacheRead, cacheWrite, reasoning, nativeTotal, model.UsageObserved)
	var cost *float64
	if msg.Cost != 0 {
		v := msg.Cost
		cost = &v
	}
	client := "opencode"
	if strings.TrimSpace(msg.Agent) != "" {
		client = strings.TrimSpace(msg.Agent)
	}
	return &model.Turn{
		Timestamp:       ts,
		Harness:         "opencode",
		Client:          client,
		Provider:        provider,
		Model:           modelID,
		ReasoningEffort: effort,
		Project:         projectName(cwd),
		CWD:             cwd,
		SessionID:       rootID,
		RecordID:        "opencode:" + sessionID + ":" + msgID,
		TurnID:          msgID,
		EventKind:       "assistant",
		ToolCalls:       toolCalls,
		ToolErrors:      toolErrors,
		Usage:           u,
		CostUSD:         cost,
		SourceFile:      path,
		SourceLine:      line,
	}
}

func (s *Source) rootID(id string) string {
	s.mu.Lock()
	sessions := s.sessions
	s.mu.Unlock()
	seen := map[string]struct{}{}
	cur := id
	for range 64 {
		meta, ok := sessions[cur]
		if !ok || meta.ParentID == "" {
			return cur
		}
		if _, loop := seen[cur]; loop {
			return cur
		}
		seen[cur] = struct{}{}
		cur = meta.ParentID
	}
	return cur
}

type messageData struct {
	ID         string  `json:"id"`
	SessionID  string  `json:"sessionID"`
	Role       string  `json:"role"`
	ModelID    string  `json:"modelID"`
	Model      string  `json:"model"`
	ProviderID string  `json:"providerID"`
	Agent      string  `json:"agent"`
	Variant    string  `json:"variant"`
	Cost       float64 `json:"cost"`
	Tokens     *struct {
		Total     int64 `json:"total"`
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     *struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Usage *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		CacheRead    int64 `json:"cache_read_input_tokens"`
		CacheWrite   int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Path *struct {
		CWD string `json:"cwd"`
	} `json:"path"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
}

func (m messageData) tokenBuckets() (input, output, cacheRead, cacheWrite, reasoning int64, nativeTotal *int64) {
	if m.Tokens != nil {
		input = m.Tokens.Input
		output = m.Tokens.Output
		reasoning = m.Tokens.Reasoning
		if m.Tokens.Cache != nil {
			cacheRead = m.Tokens.Cache.Read
			cacheWrite = m.Tokens.Cache.Write
		}
		if m.Tokens.Total != 0 {
			v := m.Tokens.Total
			nativeTotal = &v
		}
		return
	}
	if m.Usage != nil {
		input = m.Usage.InputTokens
		output = m.Usage.OutputTokens
		cacheRead = m.Usage.CacheRead
		cacheWrite = m.Usage.CacheWrite
	}
	return
}

type partData struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Text  string `json:"text"`
	Tool  string `json:"tool"`
	State *struct {
		Status string `json:"status"`
	} `json:"state"`
}

func inspectParts(parts []partData) (toolCalls, toolErrors int, hasActivity bool) {
	for _, p := range parts {
		switch p.Type {
		case "tool", "tool-call", "tool_call":
			hasActivity = true
			if strings.TrimSpace(p.Tool) != "" {
				toolCalls++
			}
			if p.State != nil && strings.EqualFold(p.State.Status, "error") {
				toolErrors++
			}
		case "text":
			if strings.TrimSpace(p.Text) != "" {
				hasActivity = true
			}
		case "reasoning", "file", "tool-result", "tool_result":
			hasActivity = true
		}
	}
	return
}

func readParts(dir string) []partData {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var parts []partData
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var p partData
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		if p.ID == "" {
			p.ID = strings.TrimSuffix(name, filepath.Ext(name))
		}
		parts = append(parts, p)
	}
	return parts
}

type sessionFile struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentID"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Agent     string `json:"agent"`
	Model     *struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
		Variant    string `json:"variant"`
	} `json:"model"`
}

func indexSessionFiles(root string, into map[string]sessionMeta) int {
	sessionRoot := filepath.Join(root, "storage", "session")
	n := 0
	_ = filepath.WalkDir(sessionRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var sf sessionFile
		if json.Unmarshal(raw, &sf) != nil || sf.ID == "" {
			return nil
		}
		into[sf.ID] = sessionMeta{
			ID:       sf.ID,
			ParentID: sf.ParentID,
			CWD:      sf.Directory,
			Title:    sf.Title,
			Agent:    sf.Agent,
			Variant:  variantOf(sf.Model),
		}
		n++
		return nil
	})
	return n
}

func variantOf(m *struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant"`
}) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Variant)
}

func defaultDataDirs() []string {
	if v := strings.TrimSpace(os.Getenv("OPENCODE_DATA_DIR")); v != "" {
		return []string{v}
	}
	home, _ := os.UserHomeDir()
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		add(filepath.Join(xdg, "opencode"))
	}
	if home != "" {
		add(filepath.Join(home, ".local", "share", "opencode"))
	}
	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			add(filepath.Join(local, "opencode"))
		}
	}
	return out
}

func dbPrefix() string {
	if v := strings.TrimSpace(os.Getenv("OPENCODE_DB_PREFIX")); v != "" {
		return v
	}
	return "opencode"
}

func sqliteFiles(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	prefix := strings.ToLower(dbPrefix())
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".db") {
			continue
		}
		if strings.Contains(name, "-wal") || strings.Contains(name, "-shm") {
			continue
		}
		out = append(out, filepath.Join(root, e.Name()))
	}
	sort.Strings(out)
	return out
}

func isSQLitePath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".db")
}

func parseMillis(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	if v < 1e12 {
		return time.Unix(v, 0).UTC()
	}
	return time.UnixMilli(v).UTC()
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
