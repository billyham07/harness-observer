package logx

import (
	"bytes"
	"strings"
	"testing"
)

func capture(t *testing.T, lvl Level, emit func(*Logger)) string {
	t.Helper()
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetLevel(lvl)
	t.Cleanup(func() {
		SetOutput(discard{})
		SetLevel(LevelInfo)
	})
	emit(New("test.component"))
	return buf.String()
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestLevelsFilter(t *testing.T) {
	got := capture(t, LevelWarn, func(l *Logger) {
		l.Debug("hidden debug")
		l.Info("hidden info")
		l.Warn("visible warning")
		l.Error("visible error")
	})
	if strings.Contains(got, "hidden") {
		t.Fatalf("output below the level leaked: %s", got)
	}
	if !strings.Contains(got, "visible warning") || !strings.Contains(got, "visible error") {
		t.Fatalf("output at or above the level missing: %s", got)
	}
	if lines := strings.Count(got, "\n"); lines != 2 {
		t.Fatalf("got %d lines, want 2: %s", lines, got)
	}
}

func TestLineCarriesTimestampComponentAndPairs(t *testing.T) {
	got := strings.TrimSpace(capture(t, LevelInfo, func(l *Logger) {
		l.Info("scan finished", "files", 12, "path", "C:/Program Files/x", "took", "1.2s")
	}))
	for _, want := range []string{"INFO", "[test.component]", "scan finished", "files=12", `path="C:/Program Files/x"`, "took=1.2s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("line %q is missing %q", got, want)
		}
	}
	if !strings.HasPrefix(got, "20") || !strings.Contains(got, "T") {
		t.Fatalf("line %q does not start with an RFC3339 timestamp", got)
	}
}

func TestOddKeyIsNotDropped(t *testing.T) {
	got := capture(t, LevelInfo, func(l *Logger) { l.Warn("partial", "orphan") })
	if !strings.Contains(got, "orphan") {
		t.Fatalf("an unpaired value must still be printed: %s", got)
	}
}

func TestOffSilencesEverything(t *testing.T) {
	got := capture(t, LevelOff, func(l *Logger) {
		l.Error("not this either")
	})
	if got != "" {
		t.Fatalf("LevelOff still wrote %q", got)
	}
}

func TestParseLevel(t *testing.T) {
	for input, want := range map[string]Level{
		"debug": LevelDebug, "INFO": LevelInfo, "warning": LevelWarn, " error ": LevelError, "off": LevelOff,
	} {
		got, err := ParseLevel(input)
		if err != nil || got != want {
			t.Fatalf("ParseLevel(%q) = %v, %v", input, got, err)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Fatal("an unknown level must be an error, not a silent default")
	}
}
