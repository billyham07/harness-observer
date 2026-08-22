// Command genpricing regenerates internal/pricing/catalog.json, the offline
// pricing snapshot the binary embeds.
//
//	go run ./scripts/genpricing
//	go run ./scripts/genpricing -url file:///tmp/model_prices.json
//
// It downloads LiteLLM's public model pricing table and keeps only what a cost
// estimate needs: the four per-token rates, the provider, and models that are
// chat shaped. That turns a ~1.8 MB upstream file into a ~190 KB embedded one.
//
// This runs by hand, not on every build. A build that reaches the network is a
// build that fails on a train, and a pricing table that changes underneath a
// release is a number nobody can reproduce.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

type entry struct {
	Provider   string   `json:"p,omitempty"`
	Input      *float64 `json:"i,omitempty"`
	Output     *float64 `json:"o,omitempty"`
	CacheRead  *float64 `json:"r,omitempty"`
	CacheWrite *float64 `json:"w,omitempty"`
}

type upstream struct {
	Provider   string   `json:"litellm_provider"`
	Mode       string   `json:"mode"`
	Input      *float64 `json:"input_cost_per_token"`
	Output     *float64 `json:"output_cost_per_token"`
	CacheRead  *float64 `json:"cache_read_input_token_cost"`
	CacheWrite *float64 `json:"cache_creation_input_token_cost"`
}

func main() {
	url := flag.String("url", defaultURL, "public pricing catalog to snapshot")
	out := flag.String("out", "internal/pricing/catalog.json", "snapshot to write")
	timeout := flag.Duration("timeout", 60*time.Second, "download timeout")
	flag.Parse()

	if err := run(*url, *out, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(url, out string, timeout time.Duration) error {
	data, err := fetch(url, timeout)
	if err != nil {
		return err
	}
	var raw map[string]upstream
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", url, err)
	}

	models := map[string]entry{}
	for name, u := range raw {
		if name == "sample_spec" {
			continue
		}
		switch u.Mode {
		case "", "chat", "responses", "completion":
		default:
			continue
		}
		e := entry{
			Provider:   u.Provider,
			Input:      sane(u.Input),
			Output:     sane(u.Output),
			CacheRead:  sane(u.CacheRead),
			CacheWrite: sane(u.CacheWrite),
		}
		// Cache rates alone cannot price a turn, so a row without an input or
		// output rate is not a usable catalog entry.
		if e.Input == nil && e.Output == nil {
			continue
		}
		models[name] = e
	}
	if len(models) < 100 {
		return fmt.Errorf("only %d usable models in %s, refusing to write a snapshot", len(models), url)
	}

	snapshot := struct {
		Source      string           `json:"source"`
		URL         string           `json:"url"`
		GeneratedAt string           `json:"generated_at"`
		Upstream    string           `json:"upstream_sha256"`
		Models      map[string]entry `json:"models"`
	}{
		Source:      "litellm",
		URL:         url,
		GeneratedAt: time.Now().UTC().Format("2006-01-02"),
		Upstream:    fmt.Sprintf("%x", sha256.Sum256(data)),
		Models:      models,
	}
	// encoding/json sorts map keys, which keeps the diff between two
	// regenerations readable.
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d models, %d bytes, upstream %s\n", out, len(models), len(encoded), snapshot.Upstream[:12])
	return nil
}

func fetch(url string, timeout time.Duration) ([]byte, error) {
	if strings.HasPrefix(url, "file://") {
		return os.ReadFile(strings.TrimPrefix(url, "file://"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// sane drops a rate that is missing, negative or obviously in the wrong units.
// One dollar per token is a million dollars per million tokens.
func sane(v *float64) *float64 {
	if v == nil || *v < 0 || *v > 1 {
		return nil
	}
	return v
}
