//go:build !windows

package gpui

import "context"

// HideOwnedConsole is a Windows Explorer concern.
func HideOwnedConsole() {}

func open(ctx context.Context, opts Options) error {
	return openBrowserAndWait(ctx, opts.URL)
}
