// Package gpui is the native desktop host for the embedded dashboard.
//
// The UI itself stays the Vue app in webui/: this package only owns the
// window. On Windows that window is WebView2 (GPU-accelerated, no external
// browser). Elsewhere, and when WebView2 is missing, it falls back to the
// same loopback server plus the default browser.
package gpui

import (
	"context"
	"path/filepath"

	"github.com/billyham07/harness-observer/internal/store"
)

// Options describe one native window.
type Options struct {
	URL   string
	Debug bool
}

// DataDir is where WebView2 keeps its profile (theme, language, window
// localStorage). It sits next to the DuckDB file so a custom --db keeps the
// profile beside that store instead of mixing it with another user's data.
func DataDir() string {
	return filepath.Join(filepath.Dir(store.DefaultPath()), "webview")
}

// Open blocks until the native window closes, or until ctx is cancelled.
// A cancelled context is not an error: Ctrl+C and closing the window are
// both a clean shutdown.
func Open(ctx context.Context, opts Options) error {
	if opts.URL == "" {
		return errEmptyURL
	}
	return open(ctx, opts)
}
