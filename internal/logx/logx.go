// Package logx is the program's own diagnostics: a few lines on stderr about
// what this process is doing. It has nothing to do with the product Logs
// surface, which is a query over normalized agent events in the local store.
//
// It is deliberately about 100 lines. The alternative was a logging framework,
// and this project emits diagnostics from three places.
//
// What must never reach it: prompts, model responses, tool arguments or output,
// session content, credentials. Paths, counters, durations and error strings
// from our own code are fine, and are the whole point.
package logx

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	// LevelOff silences everything, for tests that assert on stderr elsewhere.
	LevelOff
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

// ParseLevel reads a level name. An unknown name is an error rather than a
// silent fallback: a typo in a debug flag that quietly changes nothing is how
// an afternoon disappears.
func ParseLevel(v string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	case "off", "none", "silent":
		return LevelOff, nil
	}
	return LevelInfo, fmt.Errorf("unknown log level %q (use debug, info, warn, error or off)", v)
}

// EnvVar overrides the default level. It is read once, at startup.
const EnvVar = "HARNESS_OBSERVER_LOG"

var (
	mu    sync.Mutex
	out   io.Writer = os.Stderr
	level           = LevelInfo
	now             = time.Now
)

func init() {
	if v := os.Getenv(EnvVar); v != "" {
		if parsed, err := ParseLevel(v); err == nil {
			level = parsed
		} else {
			// Reported rather than ignored, at the level that is still on.
			fmt.Fprintf(os.Stderr, "warn [logx] %v; keeping info\n", err)
		}
	}
}

// SetLevel raises or lowers verbosity, e.g. from a --debug flag.
func SetLevel(l Level) {
	mu.Lock()
	defer mu.Unlock()
	level = l
}

// SetOutput redirects the stream. Tests use it; the program does not.
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	out = w
}

// Logger is a component-tagged emitter. Components are short and stable
// ("web.ingest", "pricing"), so a grep for one subsystem is one grep.
type Logger struct{ component string }

func New(component string) *Logger { return &Logger{component: component} }

func (l *Logger) Debug(msg string, kv ...any) { l.log(LevelDebug, msg, kv...) }
func (l *Logger) Info(msg string, kv ...any)  { l.log(LevelInfo, msg, kv...) }
func (l *Logger) Warn(msg string, kv ...any)  { l.log(LevelWarn, msg, kv...) }
func (l *Logger) Error(msg string, kv ...any) { l.log(LevelError, msg, kv...) }

// log writes one line: timestamp, level, component, message, then key=value
// pairs. An odd trailing key is printed as a bare value rather than dropped,
// because losing half a diagnostic is worse than an ugly line.
func (l *Logger) log(lvl Level, msg string, kv ...any) {
	mu.Lock()
	defer mu.Unlock()
	if lvl < level || level == LevelOff {
		return
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "%s %-5s [%s] %s", now().UTC().Format(time.RFC3339), levelNames[lvl], l.component, msg)
	for i := 0; i < len(kv); i += 2 {
		if i+1 < len(kv) {
			fmt.Fprintf(b, " %v=%v", kv[i], format(kv[i+1]))
			continue
		}
		fmt.Fprintf(b, " %v", kv[i])
	}
	b.WriteString("\n")
	_, _ = io.WriteString(out, b.String())
}

// format quotes a value that contains spaces, so one field cannot look like
// two. Paths with spaces in them are the common case.
func format(v any) any {
	s, ok := v.(string)
	if !ok {
		if err, isErr := v.(error); isErr {
			s = err.Error()
		} else {
			return v
		}
	}
	if strings.ContainsAny(s, " \t\"") {
		return fmt.Sprintf("%q", s)
	}
	return s
}
