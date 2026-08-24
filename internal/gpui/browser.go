package gpui

import (
	"context"

	"github.com/billyham07/harness-observer/internal/web"
)

// openBrowserAndWait is the fallback host: same loopback dashboard, default
// browser, process lives until ctx is cancelled.
func openBrowserAndWait(ctx context.Context, url string) error {
	web.OpenBrowser(url)
	<-ctx.Done()
	return nil
}
