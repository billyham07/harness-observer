//go:build windows

package gpui

import (
	"context"
	"fmt"
	"os"

	"github.com/jchv/go-webview2"
)

const (
	defaultWidth  = 1440
	defaultHeight = 900
	minWidth      = 960
	minHeight     = 640
)

func open(ctx context.Context, opts Options) error {
	if err := os.MkdirAll(DataDir(), 0o755); err != nil {
		return fmt.Errorf("gpui profile dir: %w", err)
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     opts.Debug,
		DataPath:  DataDir(),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Harness Observer",
			Width:  defaultWidth,
			Height: defaultHeight,
			Center: true,
		},
	})
	if w == nil {
		fmt.Fprintln(os.Stderr, "warn: WebView2 is not available; opening the dashboard in a browser")
		return openBrowserAndWait(ctx, opts.URL)
	}

	// Closing the window posts WM_QUIT; Destroy from another thread is how
	// Ctrl+C reaches that path. The extra done channel stops the watcher
	// from calling Destroy after Run has already returned.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			w.Destroy()
		case <-done:
		}
	}()

	w.SetSize(minWidth, minHeight, webview2.HintMin)
	w.SetSize(defaultWidth, defaultHeight, webview2.HintNone)
	w.Navigate(opts.URL)
	w.Run()
	close(done)
	return nil
}
