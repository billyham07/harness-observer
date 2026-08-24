// Package web serves the built-in local dashboard: an embedded static
// frontend, the read-only query API, and the background incremental ingestion
// that keeps the numbers fresh without a cron job.
//
// The process boundary matters here. Ingestion is the only part that writes,
// and it writes only to the local store; agent-owned session files stay
// read-only. HTTP handlers never ingest inline, they only ask the coordinator
// to run, so a slow scan cannot block a page load.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/billyham07/harness-observer/internal/buildinfo"
	"github.com/billyham07/harness-observer/internal/ingest"
	"github.com/billyham07/harness-observer/internal/logx"
	"github.com/billyham07/harness-observer/internal/pricing"
	"github.com/billyham07/harness-observer/internal/serve"
	"github.com/billyham07/harness-observer/internal/source"
	"github.com/billyham07/harness-observer/internal/store"
)

//go:embed assets
var assets embed.FS

// DefaultAddr binds loopback: this is usage data about one person's machine.
const DefaultAddr = "127.0.0.1:9464"

// DefaultIngestInterval is how often the dashboard process rescans session
// files. Unchanged files are skipped, so a frequent interval is cheap.
const DefaultIngestInterval = 2 * time.Minute

// SourceBuilder produces a fresh set of adapters for one ingestion run.
type SourceBuilder func() ([]source.FileSource, error)

type Server struct {
	DB      *store.DB
	Sources SourceBuilder
	// IngestInterval <= 0 disables background ingestion; the dashboard then
	// only shows what is already stored and refresh is manual.
	IngestInterval time.Duration
	// Pricing is the catalog behind the API-equivalent cost estimate. It is
	// read-only with respect to session evidence: prices are applied per query
	// and nothing about them is ever written back into the store.
	Pricing *pricing.Resolver

	ingester *ingester
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	serve.Server{DB: s.DB, Pricing: s.Pricing}.Register(mux)
	mux.HandleFunc("/api/status", s.status)
	mux.HandleFunc("/api/refresh", s.refresh)
	mux.HandleFunc("/api/pricing/refresh", s.refreshPricing)

	mux.Handle("/", dashboard())
	return mux
}

// dashboard serves the generated frontend out of the binary.
//
// Node and npm are build-time only: what ships is the Vite output embedded at
// compile time, so the running product needs no Node runtime, no dev server
// and no CDN. When those generated assets are absent the failure is explicit
// rather than a stack of 404s — the API still answers, and the page says which
// build step was skipped.
func dashboard() http.Handler {
	dist, err := fs.Sub(assets, "assets/dist")
	if err != nil {
		return notBuilt()
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return notBuilt()
	}
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Client-side routes are destinations of the same embedded page, so a
		// direct link or a refresh on /logs has to serve the shell, not a 404.
		if isAppRoute(r.URL.Path) {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

// isAppRoute reports whether a path belongs to the client-side router.
func isAppRoute(path string) bool {
	return path == "/logs" || path == "/logs/"
}

func notBuilt() http.Handler {
	page, err := assets.ReadFile("assets/notbuilt.html")
	if err != nil {
		page = []byte("the dashboard assets were not built; run scripts/build-web.sh")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(page)
	})
}

// Instance is a dashboard that has bound a port and started background
// ingestion. Serve must be called to accept HTTP; Close stops both.
//
// The split exists so a native window can own the process lifetime: the HTTP
// server runs in the background, and closing the window is what shuts it down.
type Instance struct {
	URL    string
	DBPath string

	srv        *http.Server
	ln         net.Listener
	stopIngest context.CancelFunc
}

// Start binds addr and starts background ingestion. It does not serve HTTP
// until Serve is called, and it does not print or open a browser.
func (s *Server) Start(ctx context.Context, addr string) (*Instance, error) {
	if addr == "" {
		addr = DefaultAddr
	}
	s.ingester = newIngester(s.DB, s.Sources, s.IngestInterval, s.Pricing)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	ingestCtx, stopIngest := context.WithCancel(ctx)
	go s.ingester.loop(ingestCtx)

	return &Instance{
		URL:        "http://" + listener.Addr().String() + "/",
		DBPath:     s.DB.Path(),
		srv:        &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second},
		ln:         listener,
		stopIngest: stopIngest,
	}, nil
}

// Serve accepts HTTP until Close or a listener error. A clean shutdown is nil.
func (i *Instance) Serve() error {
	if i == nil || i.srv == nil {
		return fmt.Errorf("dashboard is not started")
	}
	err := i.srv.Serve(i.ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close stops ingestion and the HTTP server. It is safe to call more than once.
func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	if i.stopIngest != nil {
		i.stopIngest()
		i.stopIngest = nil
	}
	if i.srv == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return i.srv.Shutdown(shutdownCtx)
}

// ListenAndServe binds addr, starts background ingestion and serves until ctx
// is cancelled. The returned error is nil on a clean shutdown.
func (s *Server) ListenAndServe(ctx context.Context, addr string, openBrowser bool) error {
	inst, err := s.Start(ctx, addr)
	if err != nil {
		return err
	}
	fmt.Println("harness-observer dashboard:", inst.URL)
	fmt.Println("database:", inst.DBPath)
	if openBrowser {
		OpenBrowser(inst.URL)
	}

	go func() {
		<-ctx.Done()
		_ = inst.Close()
	}()
	return inst.Serve()
}

// WaitReady polls the dashboard until /api/health answers, or ctx is done.
// A native window must not navigate before the loopback server is accepting,
// or the first paint is an error page.
func WaitReady(ctx context.Context, base string) error {
	health := strings.TrimRight(base, "/") + "/api/health"
	client := &http.Client{Timeout: 200 * time.Millisecond}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, health, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("health %s", resp.Status)
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("dashboard did not become ready: %w", last)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	version, _ := s.DB.SchemaVersion()
	st := s.ingester.snapshot()
	st.SchemaVersion = version
	st.DBPath = s.DB.Path()
	st.Version = buildinfo.Version()
	writeJSON(w, st)
}

// refresh asks for an ingestion run and returns immediately. The UI polls
// /api/status for the outcome, so a long scan never holds a request open.
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	queued := s.ingester.trigger()
	writeJSON(w, map[string]any{"queued": queued, "status": s.ingester.snapshot()})
}

// refreshPricing forces a public catalog download instead of waiting for the
// TTL. It downloads a price list and nothing else: no usage, session or model
// data leaves the machine. A failure is reported and changes nothing, because
// the previous catalog is still a correct answer.
func (s *Server) refreshPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if s.Pricing == nil {
		http.Error(w, "pricing is not configured", http.StatusNotFound)
		return
	}
	err := s.Pricing.Refresh(r.Context())
	payload := map[string]any{"status": s.Pricing.Status()}
	if err != nil {
		payload["error"] = err.Error()
	}
	writeJSON(w, payload)
}

// Status is what the dashboard shows about freshness.
type Status struct {
	Running         bool           `json:"running"`
	Runs            int64          `json:"runs"`
	IntervalSeconds float64        `json:"interval_seconds"`
	LastStart       *time.Time     `json:"last_start,omitempty"`
	LastFinish      *time.Time     `json:"last_finish,omitempty"`
	LastDurationMS  int64          `json:"last_duration_ms"`
	LastError       string         `json:"last_error,omitempty"`
	LastStats       []ingest.Stats `json:"last_stats,omitempty"`
	SchemaVersion   int            `json:"schema_version,omitempty"`
	DBPath          string         `json:"db,omitempty"`
	Version         string         `json:"version,omitempty"`
}

// ingester owns every ingestion run in this process. Runs happen on one
// goroutine only, which is what makes overlapping jobs impossible by
// construction rather than by a lock that somebody could forget to take.
type ingester struct {
	db       *store.DB
	build    SourceBuilder
	interval time.Duration
	pricing  *pricing.Resolver

	requests chan struct{}
	log      *logx.Logger

	mu     sync.RWMutex
	status Status
}

func newIngester(db *store.DB, build SourceBuilder, interval time.Duration, prices *pricing.Resolver) *ingester {
	return &ingester{
		db:       db,
		build:    build,
		interval: interval,
		pricing:  prices,
		// Capacity one: a request that arrives while a run is in flight is
		// collapsed into a single follow-up run instead of queueing up.
		requests: make(chan struct{}, 1),
		log:      logx.New("web.ingest"),
		status:   Status{IntervalSeconds: interval.Seconds()},
	}
}

func (i *ingester) trigger() bool {
	select {
	case i.requests <- struct{}{}:
		return true
	default:
		return false
	}
}

func (i *ingester) loop(ctx context.Context) {
	if i.build == nil {
		return
	}
	i.once(ctx)
	if i.interval <= 0 {
		// Background scanning disabled: still serve manual refreshes.
		for {
			select {
			case <-ctx.Done():
				return
			case <-i.requests:
				i.once(ctx)
			}
		}
	}
	ticker := time.NewTicker(i.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.once(ctx)
		case <-i.requests:
			i.once(ctx)
		}
	}
}

func (i *ingester) once(ctx context.Context) {
	// The pricing catalog rides along with the scan the user already expects,
	// so a page load never triggers a network request. It is TTL limited and
	// asynchronous, and a failure leaves the existing catalog in place.
	if i.pricing != nil {
		i.pricing.MaybeRefresh(ctx)
	}
	start := time.Now()
	i.mu.Lock()
	i.status.Running = true
	i.status.LastStart = &start
	i.status.LastError = ""
	i.mu.Unlock()

	var (
		stats []ingest.Stats
		err   error
	)
	sources, buildErr := i.build()
	if buildErr != nil {
		err = buildErr
	} else {
		stats, err = ingest.Ingestor{DB: i.db, Sources: sources}.Run(ctx)
	}

	finish := time.Now()
	i.mu.Lock()
	i.status.Running = false
	i.status.LastFinish = &finish
	i.status.LastDurationMS = finish.Sub(start).Milliseconds()
	i.status.LastStats = stats
	i.status.Runs++
	if err != nil && !errors.Is(err, context.Canceled) {
		i.status.LastError = err.Error()
	}
	i.mu.Unlock()

	// Background ingestion has no console of its own: without these lines a
	// failing scan is only visible to whoever thinks to poll /api/status.
	// Counters and paths only, never session content.
	i.report(stats, err, finish.Sub(start))
}

func (i *ingester) report(stats []ingest.Stats, err error, took time.Duration) {
	if err != nil && !errors.Is(err, context.Canceled) {
		i.log.Error("ingest run failed", "err", err, "took", took)
		return
	}
	var parsed, ingested int
	for _, st := range stats {
		parsed += st.TurnsParsed
		ingested += st.TurnsIngested
		for _, warning := range st.Warnings {
			i.log.Warn("ingest warning", "source", st.Source, "detail", warning)
		}
	}
	i.log.Debug("ingest run finished", "sources", len(stats), "turns_parsed", parsed, "turns_new", ingested, "took", took)
}

func (i *ingester) snapshot() Status {
	if i == nil {
		return Status{}
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.status
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// OpenBrowser best-effort launches the default browser. A failure is not an
// error: the URL is printed either way, and a native window can fall back to
// this when WebView2 is missing.
func OpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
