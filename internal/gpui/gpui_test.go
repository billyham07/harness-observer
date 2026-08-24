package gpui

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/billyham07/harness-observer/internal/store"
)

func TestDataDirSitsBesideTheStore(t *testing.T) {
	dir := DataDir()
	if dir == "" {
		t.Fatal("DataDir is empty")
	}
	if filepath.Base(dir) != "webview" {
		t.Fatalf("DataDir base = %q, want webview", filepath.Base(dir))
	}
	if filepath.Dir(dir) != filepath.Dir(store.DefaultPath()) {
		t.Fatalf("DataDir %q is not next to the store %q", dir, store.DefaultPath())
	}
}

func TestOpenRequiresAURL(t *testing.T) {
	err := Open(context.Background(), Options{})
	if !errors.Is(err, errEmptyURL) {
		t.Fatalf("Open(empty) = %v, want %v", err, errEmptyURL)
	}
}

func TestOpenReturnsWhenContextIsAlreadyDone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Open owns a native message loop")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Open(ctx, Options{URL: "http://127.0.0.1:9/"}); err != nil {
		t.Fatal(err)
	}
}

func TestHideOwnedConsoleDoesNotPanic(t *testing.T) {
	HideOwnedConsole()
}

func TestDataDirHonoursCustomStore(t *testing.T) {
	t.Setenv("HARNESS_OBSERVER_DB", filepath.Join(t.TempDir(), "custom", "store.duckdb"))
	dir := DataDir()
	if !strings.HasSuffix(filepath.ToSlash(dir), "/custom/webview") {
		t.Fatalf("custom store should keep the profile beside it, got %q", dir)
	}
}
