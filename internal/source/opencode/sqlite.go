package opencode

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
	_ "modernc.org/sqlite"
)

type sqliteCursor struct {
	Watch string `json:"watch,omitempty"`
	Time  int64  `json:"time,omitempty"`
	ID    string `json:"id,omitempty"`
}

func (s *Source) ContentToken(path string) (string, bool, error) {
	if !isSQLitePath(path) {
		return "", false, nil
	}
	return sqliteWatchToken(path), true, nil
}

func (s *Source) SameContent(state source.FileState, token string) bool {
	var cur sqliteCursor
	if len(state.Meta) > 0 {
		_ = json.Unmarshal(state.Meta, &cur)
	}
	return cur.Watch != "" && cur.Watch == token
}

func sqliteWatchToken(path string) string {
	var dbSize, dbMtime, walSize, walMtime int64
	if info, err := os.Stat(path); err == nil {
		dbSize = info.Size()
		dbMtime = info.ModTime().UnixMilli()
	}
	if info, err := os.Stat(path + "-wal"); err == nil {
		walSize = info.Size()
		walMtime = info.ModTime().UnixMilli()
	}
	return fmt.Sprintf("%d:%d:%d:%d", dbSize, dbMtime, walSize, walMtime)
}

func (s *Source) parseSQLite(path string, st source.FileState, window source.Window) ([]model.Turn, source.FileState, error) {
	var cur sqliteCursor
	if len(st.Meta) > 0 {
		_ = json.Unmarshal(st.Meta, &cur)
	}
	db, err := openReadOnly(path)
	if err != nil {
		return nil, st, fmt.Errorf("opencode sqlite open %s: %w", path, err)
	}
	defer db.Close()

	missing, err := missingTables(db, "session", "message", "part")
	if err != nil {
		return nil, st, fmt.Errorf("opencode sqlite %s: %w", path, err)
	}
	if len(missing) > 0 {
		return nil, st, fmt.Errorf("opencode sqlite schema incompatible at %s: missing tables (%s); expected session, message, part", path, strings.Join(missing, ", "))
	}

	if err := s.loadSQLiteSessionIndex(db); err != nil {
		return nil, st, fmt.Errorf("opencode sqlite sessions %s: %w", path, err)
	}

	partsByMsg, err := loadAllParts(db)
	if err != nil {
		return nil, st, fmt.Errorf("opencode sqlite parts %s: %w", path, err)
	}

	rows, err := db.Query(`
		SELECT id, session_id, time_created, data
		FROM message
		WHERE time_created > ?
		   OR (time_created = ? AND id > ?)
		ORDER BY time_created ASC, id ASC`, cur.Time, cur.Time, cur.ID)
	if err != nil {
		return nil, st, fmt.Errorf("opencode sqlite query %s: %w", path, err)
	}
	defer rows.Close()

	var out []model.Turn
	lastTime, lastID := cur.Time, cur.ID
	line := st.Line
	for rows.Next() {
		var (
			id, sessionID string
			timeCreated   int64
			data          []byte
		)
		if scanErr := rows.Scan(&id, &sessionID, &timeCreated, &data); scanErr != nil {
			continue
		}
		lastTime, lastID = timeCreated, id
		line++
		var msg messageData
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID == "" {
			msg.ID = id
		}
		if msg.SessionID == "" {
			msg.SessionID = sessionID
		}
		if t := s.turnFromMessage(path, line, sessionID, id, msg, partsByMsg[id], window); t != nil {
			out = append(out, *t)
		}
	}
	if err := rows.Err(); err != nil {
		return out, st, fmt.Errorf("opencode sqlite iterate %s: %w", path, err)
	}

	next := st
	if info, statErr := os.Stat(path); statErr == nil {
		next.Offset = info.Size()
		next.Line = line
	}
	nextCur := sqliteCursor{Watch: sqliteWatchToken(path), Time: lastTime, ID: lastID}
	if meta, marshalErr := json.Marshal(nextCur); marshalErr == nil {
		next.Meta = meta
	}
	return out, next, nil
}

func (s *Source) loadSQLiteSessionIndex(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, parent_id, directory, title FROM session`)
	if err != nil {
		return err
	}
	defer rows.Close()
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = make(map[string]sessionMeta)
	}
	s.mu.Unlock()
	for rows.Next() {
		var id string
		var parent, directory, title sql.NullString
		if scanErr := rows.Scan(&id, &parent, &directory, &title); scanErr != nil {
			continue
		}
		s.mu.Lock()
		s.sessions[id] = sessionMeta{
			ID:       id,
			ParentID: parent.String,
			CWD:      directory.String,
			Title:    title.String,
		}
		s.mu.Unlock()
	}
	return rows.Err()
}

func indexSQLiteSessions(path string, into map[string]sessionMeta) error {
	db, err := openReadOnly(path)
	if err != nil {
		if isBusy(err) {
			return fmt.Errorf("sqlite locked %s (OpenCode owns the DB; retry when idle)", path)
		}
		return fmt.Errorf("sqlite open %s: %w", path, err)
	}
	defer db.Close()
	missing, err := missingTables(db, "session", "message", "part")
	if err != nil {
		if isBusy(err) {
			return fmt.Errorf("sqlite locked %s (OpenCode owns the DB; retry when idle)", path)
		}
		return fmt.Errorf("sqlite %s: %w", path, err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("sqlite schema incompatible at %s: missing tables (%s)", path, strings.Join(missing, ", "))
	}
	rows, err := db.Query(`SELECT id, parent_id, directory, title FROM session`)
	if err != nil {
		return fmt.Errorf("sqlite sessions %s: %w", path, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var parent, directory, title sql.NullString
		if scanErr := rows.Scan(&id, &parent, &directory, &title); scanErr != nil {
			continue
		}
		into[id] = sessionMeta{ID: id, ParentID: parent.String, CWD: directory.String, Title: title.String}
	}
	return rows.Err()
}

func loadAllParts(db *sql.DB) (map[string][]partData, error) {
	rows, err := db.Query(`SELECT message_id, id, data FROM part ORDER BY message_id ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]partData{}
	for rows.Next() {
		var messageID, id string
		var data []byte
		if scanErr := rows.Scan(&messageID, &id, &data); scanErr != nil {
			continue
		}
		var p partData
		if json.Unmarshal(data, &p) != nil {
			continue
		}
		if p.ID == "" {
			p.ID = id
		}
		out[messageID] = append(out[messageID], p)
	}
	return out, rows.Err()
}
func missingTables(db *sql.DB, names ...string) ([]string, error) {
	var missing []string
	for _, name := range names {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

func openReadOnly(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	dsn := filepath.ToSlash(abs) + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(1000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if pingErr := db.Ping(); pingErr != nil {
		db.Close()
		return nil, pingErr
	}
	return db, nil
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "busy") || strings.Contains(msg, "locked")
}
