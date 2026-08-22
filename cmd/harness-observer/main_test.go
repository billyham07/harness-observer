package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

// A top level help flag used to be swallowed by collect's flag set, which
// printed "Usage of collect:" and three flags, so the dashboard command was
// invisible to anyone who only ran --help.
func TestSplitCommandRoutesTopLevelHelp(t *testing.T) {
	for _, arg := range []string{"-h", "-help", "--help", "help"} {
		command, rest := splitCommand([]string{arg})
		if command != arg {
			t.Fatalf("splitCommand(%q) = %q, want %q", arg, command, arg)
		}
		if len(rest) != 0 {
			t.Fatalf("splitCommand(%q) left args %v", arg, rest)
		}
	}
}

func TestSplitCommandKeepsDefaultAndSubcommands(t *testing.T) {
	if command, rest := splitCommand(nil); command != "collect" || len(rest) != 0 {
		t.Fatalf("splitCommand(nil) = %q, %v", command, rest)
	}
	// A non-help leading flag still belongs to the default command.
	if command, rest := splitCommand([]string{"--since", "2026-08-20"}); command != "collect" || len(rest) != 2 {
		t.Fatalf("splitCommand(--since ...) = %q, %v", command, rest)
	}
	if command, rest := splitCommand([]string{"web", "--no-open"}); command != "web" || len(rest) != 1 {
		t.Fatalf("splitCommand(web --no-open) = %q, %v", command, rest)
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)
	for _, command := range []string{"web", "collect", "ingest", "doctor", "query", "logs", "serve", "pricing", "version"} {
		if !strings.Contains(buf.String(), "\n  "+command+" ") {
			t.Errorf("usage does not list %q:\n%s", command, buf.String())
		}
	}
}

// --version is the most conventional question a binary is asked, and it used
// to reach collect's flag set and come back as "flag provided but not
// defined".
func TestSplitCommandRoutesTopLevelVersion(t *testing.T) {
	for _, arg := range []string{"-V", "-version", "--version", "version"} {
		command, rest := splitCommand([]string{arg})
		if command != arg {
			t.Fatalf("splitCommand(%q) = %q, want %q", arg, command, arg)
		}
		if len(rest) != 0 {
			t.Fatalf("splitCommand(%q) left args %v", arg, rest)
		}
	}
}

// The store and parser versions belong next to the build: a binary newer than
// the database it opens is exactly what this output exists to explain.
func TestPrintVersionReportsBuildAndStore(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf)
	out := buf.String()

	if !strings.HasPrefix(out, "harness-observer ") {
		t.Fatalf("version output does not name the binary:\n%s", out)
	}
	if strings.Contains(out, "harness-observer \n") {
		t.Fatalf("version is empty; buildinfo must always answer something:\n%s", out)
	}
	for _, needle := range []string{runtime.Version(), "store schema", "parser"} {
		if !strings.Contains(out, needle) {
			t.Errorf("version output is missing %q:\n%s", needle, out)
		}
	}
}
