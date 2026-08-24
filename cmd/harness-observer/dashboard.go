package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/billyham07/harness-observer/internal/gpui"
	"github.com/billyham07/harness-observer/internal/logx"
	"github.com/billyham07/harness-observer/internal/source"
	"github.com/billyham07/harness-observer/internal/store"
	"github.com/billyham07/harness-observer/internal/web"
)

type dashboardKind int

const (
	dashboardKindWeb dashboardKind = iota
	dashboardKindGPUI
)

func (k dashboardKind) name() string {
	if k == dashboardKindGPUI {
		return "gpui"
	}
	return "web"
}

// runDashboard is the shared body of `web` and `gpui`. The Vue app, the
// loopback API and background ingest are the same; only the host window
// differs. DuckDB still has one writer, so the two commands cannot share a
// store the same way `web` cannot share one with `ingest`.
func runDashboard(ctx context.Context, args []string, kind dashboardKind) error {
	fs := flag.NewFlagSet(kind.name(), flag.ExitOnError)
	dbPath := fs.String("db", "", "database path (default: per-user data dir, or $HARNESS_OBSERVER_DB)")
	addr := fs.String("addr", web.DefaultAddr, "listen address (loopback by default: this is local-only data)")
	spec := sourcesFlag(fs)
	interval := fs.Duration("ingest-interval", web.DefaultIngestInterval, "background incremental ingest interval (0 disables it; manual refresh still works)")
	var noOpen *bool
	if kind == dashboardKindWeb {
		noOpen = fs.Bool("no-open", false, "do not open a browser window")
	}
	buildPricing := pricingFlags(fs)
	debugHelp := "verbose program diagnostics on stderr (also: " + logx.EnvVar + "=debug)"
	if kind == dashboardKindGPUI {
		debugHelp += "; enables native DevTools"
	}
	debug := fs.Bool("debug", false, debugHelp)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *debug {
		logx.SetLevel(logx.LevelDebug)
	}
	if _, err := buildSources(*spec); err != nil {
		return err
	}
	resolver, err := buildPricing()
	if err != nil {
		return err
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	server := &web.Server{
		DB:             db,
		Sources:        func() ([]source.FileSource, error) { return buildSources(*spec) },
		IngestInterval: *interval,
		Pricing:        resolver,
	}

	if kind == dashboardKindWeb {
		return server.ListenAndServe(ctx, *addr, noOpen == nil || !*noOpen)
	}

	gpui.HideOwnedConsole()
	inst, err := server.Start(ctx, *addr)
	if err != nil {
		return err
	}
	defer inst.Close()

	fmt.Println("harness-observer gpui:", inst.URL)
	fmt.Println("database:", inst.DBPath)

	errc := make(chan error, 1)
	go func() { errc <- inst.Serve() }()
	if err := web.WaitReady(ctx, inst.URL); err != nil {
		return err
	}
	if err := gpui.Open(ctx, gpui.Options{URL: inst.URL, Debug: *debug}); err != nil {
		return err
	}
	_ = inst.Close()
	return <-errc
}
