package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
	"github.com/billyham07/harness-observer/internal/store"
)

// slowSource stands in for a large session tree: it counts concurrent parses so
// the test can prove two ingestion runs never overlap.
type slowSource struct {
	dir     string
	delay   time.Duration
	active  atomic.Int32
	maxSeen atomic.Int32
	parses  atomic.Int32
}

func (s *slowSource) Name() string    { return "slow" }
func (s *slowSource) Roots() []string { return []string{s.dir} }

func (s *slowSource) Prepare(context.Context) error { return nil }

func (s *slowSource) Files(context.Context) ([]string, error) {
	return []string{filepath.Join(s.dir, "session.jsonl")}, nil
}

func (s *slowSource) Collect(context.Context, source.Window) ([]model.Turn, error) { return nil, nil }

func (s *slowSource) ParseFile(path string, st source.FileState, w source.Window) ([]model.Turn, source.FileState, error) {
	now := s.active.Add(1)
	defer s.active.Add(-1)
	for {
		peak := s.maxSeen.Load()
		if now <= peak || s.maxSeen.CompareAndSwap(peak, now) {
			break
		}
	}
	s.parses.Add(1)
	time.Sleep(s.delay)
	return []model.Turn{{
		Timestamp: time.Now().UTC(),
		Harness:   "slow",
		SessionID: "s1",
		RecordID:  "s1:1",
		EventKind: "assistant",
		Usage:     model.NewSplitUsage(1, 1, 0, 0, 0, nil, model.UsageObserved),
	}}, source.FileState{Offset: 1}, nil
}

func newTestServer(t *testing.T, src *slowSource) *Server {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{DB: db, Sources: func() ([]source.FileSource, error) { return []source.FileSource{src}, nil }}
	s.ingester = newIngester(db, s.Sources, 0, s.Pricing)
	return s
}

func writeSlowSource(t *testing.T, delay time.Duration) *slowSource {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &slowSource{dir: dir, delay: delay}
}

// Overlapping ingestion would double-scan the same files and race on the
// checkpoint rows. Runs happen on one goroutine, so a burst of triggers can
// only ever queue one follow-up.
func TestIngestionRunsNeverOverlap(t *testing.T) {
	src := writeSlowSource(t, 40*time.Millisecond)
	s := newTestServer(t, src)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.ingester.loop(ctx)
		close(done)
	}()

	deadline := time.After(3 * time.Second)
	for {
		if s.ingester.snapshot().Runs >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the startup ingest never completed")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	for i := 0; i < 50; i++ {
		s.ingester.trigger()
	}
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done

	if peak := src.maxSeen.Load(); peak > 1 {
		t.Fatalf("%d ingestion runs overlapped", peak)
	}
	// A burst of 50 triggers must not turn into 50 scans.
	if runs := s.ingester.snapshot().Runs; runs > 5 {
		t.Fatalf("collapsed triggers still produced %d runs", runs)
	}
}

// The refresh endpoint hands the work to the coordinator and returns; it must
// not hold the request open for the duration of a scan.
func TestRefreshEndpointDoesNotBlockOnIngestion(t *testing.T) {
	src := writeSlowSource(t, 750*time.Millisecond)
	s := newTestServer(t, src)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.ingester.loop(ctx)

	handler := s.Handler()
	start := time.Now()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/refresh", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh returned %d: %s", rec.Code, rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("refresh blocked for %v while an ingest was running", elapsed)
	}

	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	var st Status
	if err := json.Unmarshal(statusRec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.DBPath == "" {
		t.Fatalf("status must report the database in use: %+v", st)
	}
}

func TestRefreshRejectsGet(t *testing.T) {
	s := newTestServer(t, writeSlowSource(t, 0))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/refresh", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/refresh returned %d, want 405", rec.Code)
	}
}

// dashboardBuilt reports whether the Vite output was embedded into this test
// binary. Node is a build-time tool, so `go test` alone can legitimately run
// against a tree where the frontend has not been generated yet; the assertions
// below then check the explicit failure mode instead of silently passing. CI
// builds the frontend first, so the real assertions are the ones that run
// there.
func dashboardBuilt() bool {
	dist, err := fs.Sub(assets, "assets/dist")
	if err != nil {
		return false
	}
	_, err = fs.Stat(dist, "index.html")
	return err == nil
}

// The dashboard is embedded in the binary: no npm, no runtime asset path.
func TestEmbeddedDashboardIsServed(t *testing.T) {
	s := newTestServer(t, writeSlowSource(t, 0))
	handler := s.Handler()
	if !dashboardBuilt() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("unbuilt frontend served %d, want 503", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "build-web") {
			t.Fatal("the unbuilt page must name the build step that was skipped")
		}
		return
	}
	for _, path := range []string{"/", "/app.js", "/app.css"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("GET %s served an empty body", path)
		}
	}
}

func TestReadAPIIsMountedAlongsideTheUI(t *testing.T) {
	s := newTestServer(t, writeSlowSource(t, 0))
	handler := s.Handler()
	for _, path := range []string{"/api/health", "/api/query", "/api/logs", "/api/hierarchy", "/api/facets", "/metrics"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

// /logs is a client-side route. A direct link, a bookmark and a refresh all
// have to serve the same shell the client router boots from, or the page only
// works when it is navigated to from Overview.
func TestLogsPathServesTheDashboard(t *testing.T) {
	if !dashboardBuilt() {
		t.Skip("frontend not built; run scripts/build-web.sh")
	}
	s := newTestServer(t, writeSlowSource(t, 0))
	handler := s.Handler()

	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	logs := httptest.NewRecorder()
	handler.ServeHTTP(logs, httptest.NewRequest(http.MethodGet, "/logs", nil))

	if logs.Code != http.StatusOK {
		t.Fatalf("GET /logs returned %d", logs.Code)
	}
	if logs.Body.String() != root.Body.String() {
		t.Fatal("/logs must serve the same shell as Overview")
	}
	if !strings.Contains(logs.Body.String(), `id="app"`) {
		t.Fatal("the shell must carry the client mount point")
	}
}

// The product is offline by construction. The generated shell may not pull a
// script, stylesheet or font from anywhere but this binary, because the
// machine running it is not assumed to have a network at all.
func TestEmbeddedShellLoadsNothingRemote(t *testing.T) {
	if !dashboardBuilt() {
		t.Skip("frontend not built; run scripts/build-web.sh")
	}
	shell := mustAsset(t, "dist/index.html")
	for _, needle := range []string{"src=\"http", "href=\"http", "//cdn.", "//fonts."} {
		if strings.Contains(shell, needle) {
			t.Fatalf("the embedded shell references a remote origin (%q)", needle)
		}
	}
}

// The Logs page is server-paginated. These are the wire-level facts the Go
// side guarantees; the client-side invariants behind them (no local slicing,
// correlated sel tuples, unknown-not-zero rendering) are asserted by the
// frontend test suite in webui/src.
func TestFrontendKeepsLogsOnTheServer(t *testing.T) {
	if !dashboardBuilt() {
		t.Skip("frontend not built; run scripts/build-web.sh")
	}
	bundle := mustAsset(t, "dist/app.js")
	for _, needle := range []string{"/api/logs", "/api/hierarchy", "/api/facets", "/api/query", "/api/status", "page_size"} {
		if !strings.Contains(bundle, needle) {
			t.Fatalf("the bundle never mentions %q", needle)
		}
	}
}

func TestNotBuiltHandlerExplainsTheMissingStep(t *testing.T) {
	rec := httptest.NewRecorder()
	notBuilt().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("returned %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	for _, needle := range []string{"build-web", "internal/web/assets/dist"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("the unbuilt page does not mention %q", needle)
		}
	}
}

func mustAsset(t *testing.T, name string) string {
	t.Helper()
	raw, err := assets.ReadFile("assets/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Start is what the native window uses: bind, ingest, then serve in the
// background so the window can own the process lifetime.
func TestStartServesHealth(t *testing.T) {
	s := newTestServer(t, writeSlowSource(t, 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inst, err := s.Start(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()

	errc := make(chan error, 1)
	go func() { errc <- inst.Serve() }()

	ready, cancelReady := context.WithTimeout(ctx, 2*time.Second)
	defer cancelReady()
	if err := WaitReady(ready, inst.URL); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(inst.URL + "api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health %d", resp.StatusCode)
	}

	if err := inst.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after Close")
	}
}

func TestWaitReadyFailsWhenContextIsAlreadyDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WaitReady(ctx, "http://127.0.0.1:9")
	if err == nil {
		t.Fatal("expected an error against a cancelled context")
	}
}
