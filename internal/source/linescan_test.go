package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func collect(t *testing.T, path string, state FileState) ([]string, FileState) {
	t.Helper()
	var lines []string
	next, err := ScanLines(path, state, func(line []byte, lineNo int) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return lines, next
}

func TestScanLinesResumesAtOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, state := collect(t, path, FileState{})
	if len(lines) != 2 || state.Line != 2 || state.Offset != 4 {
		t.Fatalf("first pass: lines=%v state=%+v", lines, state)
	}
	if err := appendTo(path, "c\n"); err != nil {
		t.Fatal(err)
	}
	lines, state = collect(t, path, state)
	if len(lines) != 1 || lines[0] != "c" || state.Line != 3 {
		t.Fatalf("resume: lines=%v state=%+v", lines, state)
	}
}

// A JSONL writer can be caught mid-record. The partial tail is still handed to
// the caller (invalid JSON simply fails to parse) but the offset must not move
// past it, so the completed record is read exactly once afterwards.
func TestScanLinesDoesNotConsumePartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("full\npart"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, state := collect(t, path, FileState{})
	if len(lines) != 2 || lines[1] != "part" {
		t.Fatalf("expected the partial tail to be offered, got %v", lines)
	}
	if state.Offset != 5 || state.Line != 1 {
		t.Fatalf("partial tail must not advance the checkpoint: %+v", state)
	}
	if err := appendTo(path, "ial\n"); err != nil {
		t.Fatal(err)
	}
	lines, state = collect(t, path, state)
	if len(lines) != 1 || lines[0] != "partial" {
		t.Fatalf("completed record not re-read: %v", lines)
	}
	if state.Line != 2 {
		t.Fatalf("line numbering drifted: %+v", state)
	}
}

// Line numbers are part of nothing durable by themselves, but they must stay
// consistent between a full scan and a resumed one so provenance stays honest.
func TestScanLinesLineNumbersMatchFullScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	body := "1\n2\n3\n4\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var full []int
	if _, err := ScanLines(path, FileState{}, func(_ []byte, lineNo int) error {
		full = append(full, lineNo)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var resumed []int
	state := FileState{}
	for i := 0; i < 4; i++ {
		next, err := ScanLines(path, FileState{Offset: state.Offset, Line: state.Line}, func(_ []byte, lineNo int) error {
			resumed = append(resumed, lineNo)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if next.Offset == state.Offset {
			break
		}
		state = FileState{Offset: state.Offset + 2, Line: state.Line + 1}
	}
	if len(full) != 4 || full[3] != 4 {
		t.Fatalf("full scan line numbers: %v", full)
	}
	if len(resumed) == 0 || resumed[0] != 1 {
		t.Fatalf("resumed scan line numbers: %v", resumed)
	}
}

func TestScanLinesIgnoresCarriageReturns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("a\r\nb\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, _ := collect(t, path, FileState{})
	if strings.Join(lines, ",") != "a,b" {
		t.Fatalf("CRLF not trimmed: %v", lines)
	}
}

func appendTo(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}
