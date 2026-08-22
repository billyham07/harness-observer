package pricing

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/billyham07/harness-observer/internal/logx"
)

// maxCatalogBytes bounds the download. The public table is under 2 MB; the cap
// is generous enough to survive growth and small enough that a redirect into
// something else cannot exhaust memory.
const maxCatalogBytes = 32 << 20

func httpTransport(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing catalog: %s returned %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
}

// Refresh downloads the public catalog now, ignoring the TTL, and adopts it
// only if it parses into a usable table. A failure leaves the current catalog
// exactly as it was: the dashboard degrades to yesterday's prices, never to no
// prices.
func (r *Resolver) Refresh(ctx context.Context) error {
	r.mu.Lock()
	if r.refreshing {
		r.mu.Unlock()
		return nil
	}
	r.refreshing = true
	opts := r.opts
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.refreshing = false
		r.mu.Unlock()
	}()

	fetchCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	now := opts.Now()
	data, err := opts.Transport(fetchCtx, opts.URL)
	if err == nil {
		var catalog *Catalog
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		catalog, err = ParseUpstream(data, SourceRemote, digest, now)
		if err == nil {
			// A cache that cannot be written costs one download next time. It
			// is not a reason to reject prices that already parsed.
			_ = saveCache(opts.CachePath, catalog, opts.URL, now)
			r.mu.Lock()
			r.catalog = catalog
			r.memo = map[string]Match{}
			r.lastTry = now
			r.lastErr = ""
			r.mu.Unlock()
			return nil
		}
	}
	r.mu.Lock()
	r.lastTry = now
	r.lastErr = err.Error()
	r.mu.Unlock()
	return err
}

// MaybeRefresh refreshes only when refreshing is enabled and the current
// catalog is older than the TTL. It runs in the background and reports nothing:
// the caller already has a working catalog, and Status() carries the outcome.
func (r *Resolver) MaybeRefresh(ctx context.Context) {
	if !r.stale() {
		return
	}
	go func() {
		// A failed background refresh is not an error for the caller: the
		// existing catalog is still a correct answer. It is still worth one
		// line, because "prices look stale" is otherwise unexplainable.
		if err := r.Refresh(ctx); err != nil {
			logx.New("pricing").Warn("catalog refresh failed, keeping the current rates", "err", err)
		}
	}()
}

func (r *Resolver) stale() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.opts.Refresh || r.refreshing {
		return false
	}
	now := r.opts.Now()
	// One failed attempt must not turn into a retry per page load.
	if !r.lastTry.IsZero() && now.Sub(r.lastTry) < r.opts.TTL {
		return false
	}
	if r.catalog.Source == SourceBundled || r.catalog.FetchedAt.IsZero() {
		return true
	}
	return now.Sub(r.catalog.FetchedAt) > r.opts.TTL
}

func loadCache(path string) (*Catalog, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	catalog, err := parseSnapshot(data, SourceCached)
	if err != nil {
		// A corrupt cache is discarded, not repaired: the bundled snapshot is
		// already a correct answer.
		return nil, err
	}
	return catalog, nil
}

// saveCache writes through a temporary file in the same directory, so a crash
// or a concurrent reader never sees a half-written catalog.
func saveCache(path string, c *Catalog, url string, fetchedAt time.Time) error {
	if path == "" {
		return nil
	}
	data, err := encodeSnapshot(c, url, fetchedAt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pricing-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
