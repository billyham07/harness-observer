package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/store"
)

// seedTuples builds the exact shape the PR calls out: one harness with two
// models, where both models were used at more than one effort. It is the shape
// where independent dropdowns and correlated tuples give different answers.
//
//	codex-desktop
//	├─ gpt-5.6-sol   -> high, xhigh, max
//	└─ gpt-5.6-luna  -> high, max
func seedTuples(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	n := 0
	turn := func(harness, modelName, effort, client, project string, tokens int64) model.Turn {
		n++
		return model.Turn{
			Timestamp:       time.Date(2026, 8, 20, 9, n, 0, 0, time.UTC),
			Harness:         harness,
			Client:          client,
			Provider:        "openai",
			Model:           modelName,
			ReasoningEffort: effort,
			Project:         project,
			SessionID:       "s" + modelName + effort,
			RecordID:        harness + ":" + modelName + ":" + effort,
			EventKind:       "assistant",
			Usage:           model.NewSplitUsage(tokens, 0, 0, 0, 0, nil, model.UsageObserved),
		}
	}
	turns := []model.Turn{
		turn("codex-desktop", "gpt-5.6-sol", "high", "Codex Desktop", "demo", 1),
		turn("codex-desktop", "gpt-5.6-sol", "xhigh", "Codex Desktop", "demo", 2),
		turn("codex-desktop", "gpt-5.6-sol", "max", "Codex Desktop", "demo", 4),
		turn("codex-desktop", "gpt-5.6-luna", "high", "Codex Desktop", "other", 8),
		turn("codex-desktop", "gpt-5.6-luna", "max", "Codex Desktop", "other", 16),
		turn("omp", "grok-4.6", "", "omp", "demo", 32),
	}
	if _, err := db.InsertTurns(context.Background(), turns); err != nil {
		t.Fatal(err)
	}
	return db.SQL()
}

func ptr(v string) *string { return &v }

// The requirement the PR states explicitly: selecting sol/high plus luna/max
// must mean exactly those two tuples. Independent IN-lists would also admit
// sol/max and luna/high, which exist in this fixture precisely so the wrong
// implementation cannot pass.
func TestSelectionKeepsTupleCorrelation(t *testing.T) {
	db := seedTuples(t)
	filter := Filter{Selection: []Tuple{
		{Harness: "codex-desktop", Model: ptr("gpt-5.6-sol"), Effort: ptr("high")},
		{Harness: "codex-desktop", Model: ptr("gpt-5.6-luna"), Effort: ptr("max")},
	}}
	rows, err := Aggregate(context.Background(), db, Request{Filter: filter, GroupBy: []string{"model", "effort"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	var total int64
	for _, r := range rows {
		got[r.Group["model"]+"/"+r.Group["effort"]] = r.Turns
		total += r.ObservedTokens
	}
	if len(got) != 2 || got["gpt-5.6-sol/high"] != 1 || got["gpt-5.6-luna/max"] != 1 {
		t.Fatalf("selection widened into a cartesian product: %+v", got)
	}
	if _, leaked := got["gpt-5.6-sol/max"]; leaked {
		t.Fatal("sol/max leaked into a sol/high + luna/max selection")
	}
	if _, leaked := got["gpt-5.6-luna/high"]; leaked {
		t.Fatal("luna/high leaked into a sol/high + luna/max selection")
	}
	if want := int64(1 + 16); total != want {
		t.Fatalf("observed tokens = %d, want %d", total, want)
	}
}

// A harness node selects everything under it, and a model node everything under
// that model, without listing the leaves.
func TestSelectionAtHarnessAndModelDepth(t *testing.T) {
	db := seedTuples(t)
	harnessOnly, err := Aggregate(context.Background(), db, Request{
		Filter: Filter{Selection: []Tuple{{Harness: "codex-desktop"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(harnessOnly) != 1 || harnessOnly[0].Turns != 5 {
		t.Fatalf("harness-depth selection: %+v", harnessOnly)
	}

	modelOnly, err := Aggregate(context.Background(), db, Request{
		Filter: Filter{Selection: []Tuple{{Harness: "codex-desktop", Model: ptr("gpt-5.6-sol")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(modelOnly) != 1 || modelOnly[0].Turns != 3 {
		t.Fatalf("model-depth selection: %+v", modelOnly)
	}
}

// An unknown effort is a value, not a wildcard: selecting it must match only
// the rows whose effort the source never recorded.
func TestSelectionCanTargetUnknownEffort(t *testing.T) {
	db := seedTuples(t)
	rows, err := Aggregate(context.Background(), db, Request{
		Filter: Filter{Selection: []Tuple{{Harness: "omp", Model: ptr("grok-4.6"), Effort: ptr("")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Turns != 1 {
		t.Fatalf("unknown-effort selection: %+v", rows)
	}
}

func TestTupleRoundTripsThroughItsWireForm(t *testing.T) {
	cases := []struct {
		in    string
		depth int
		want  string
	}{
		{"codex-desktop", 1, "codex-desktop"},
		{"codex-desktop|gpt-5.6-sol", 2, "codex-desktop|gpt-5.6-sol"},
		{"codex-desktop|gpt-5.6-sol|high", 3, "codex-desktop|gpt-5.6-sol|high"},
		{"omp|grok-4.6|", 3, "omp|grok-4.6|"},
		{"omp|", 2, "omp|"},
		{`odd\|name|m|e`, 3, `odd\|name|m|e`},
	}
	for _, c := range cases {
		got, err := ParseTuple(c.in)
		if err != nil {
			t.Fatalf("ParseTuple(%q): %v", c.in, err)
		}
		if got.Depth() != c.depth {
			t.Fatalf("ParseTuple(%q).Depth() = %d, want %d", c.in, got.Depth(), c.depth)
		}
		if got.String() != c.want {
			t.Fatalf("ParseTuple(%q).String() = %q, want %q", c.in, got.String(), c.want)
		}
	}
	if _, err := ParseTuple("a|b|c|d"); err == nil {
		t.Fatal("a four-level selection must be rejected, not silently truncated")
	}
	if _, err := ParseTuple(""); err == nil {
		t.Fatal("an empty selection must be rejected")
	}
}

func TestHierarchyReportsObservedTuplesWithCounts(t *testing.T) {
	db := seedTuples(t)
	h, err := BuildHierarchy(context.Background(), db, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Nodes) != 2 {
		t.Fatalf("got %d harnesses, want 2: %+v", len(h.Nodes), h.Nodes)
	}
	codex := h.Nodes[0]
	if codex.Value != "codex-desktop" || codex.Turns != 5 {
		t.Fatalf("codex node: %+v", codex)
	}
	if len(codex.Children) != 2 {
		t.Fatalf("codex models: %+v", codex.Children)
	}
	luna, sol := codex.Children[0], codex.Children[1]
	if luna.Value != "gpt-5.6-luna" || len(luna.Children) != 2 {
		t.Fatalf("luna node: %+v", luna)
	}
	if sol.Value != "gpt-5.6-sol" || len(sol.Children) != 3 {
		t.Fatalf("sol node: %+v", sol)
	}
	// The hierarchy must not offer an effort a model was never used at.
	for _, e := range luna.Children {
		if e.Value == "xhigh" {
			t.Fatal("luna/xhigh was manufactured; that tuple does not exist")
		}
	}
	if sol.Children[0].Path != "codex-desktop|gpt-5.6-sol|high" {
		t.Fatalf("leaf path = %q", sol.Children[0].Path)
	}
}

func TestHierarchyMarksPartialParents(t *testing.T) {
	db := seedTuples(t)
	h, err := BuildHierarchy(context.Background(), db, Filter{Selection: []Tuple{
		{Harness: "codex-desktop", Model: ptr("gpt-5.6-sol"), Effort: ptr("high")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	codex := h.Nodes[0]
	if codex.Selection != SelectionPartial {
		t.Fatalf("harness selection = %q, want partial", codex.Selection)
	}
	for _, m := range codex.Children {
		switch m.Value {
		case "gpt-5.6-sol":
			if m.Selection != SelectionPartial {
				t.Fatalf("sol selection = %q, want partial", m.Selection)
			}
		case "gpt-5.6-luna":
			if m.Selection != SelectionNone {
				t.Fatalf("luna selection = %q, want none", m.Selection)
			}
		}
	}

	full, err := BuildHierarchy(context.Background(), db, Filter{Selection: []Tuple{
		{Harness: "codex-desktop", Model: ptr("gpt-5.6-luna")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range full.Nodes[0].Children {
		if m.Value != "gpt-5.6-luna" {
			continue
		}
		if m.Selection != SelectionFull {
			t.Fatalf("model-depth selection should mark the model full: %+v", m)
		}
		for _, e := range m.Children {
			if e.Selection != SelectionFull {
				t.Fatalf("children of a selected model must read as selected: %+v", e)
			}
		}
	}
}

// The hierarchy is recomputed under the rest of the filter, so narrowing the
// time range or another dimension removes tuples that have no evidence left.
func TestHierarchyRespectsOtherFiltersAndFlagsStaleSelections(t *testing.T) {
	db := seedTuples(t)
	filter := Filter{
		Project: []string{"other"},
		Selection: []Tuple{
			{Harness: "codex-desktop", Model: ptr("gpt-5.6-luna"), Effort: ptr("max")},
			{Harness: "codex-desktop", Model: ptr("gpt-5.6-sol"), Effort: ptr("high")},
		},
	}
	h, err := BuildHierarchy(context.Background(), db, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Nodes) != 1 || len(h.Nodes[0].Children) != 1 {
		t.Fatalf("hierarchy should collapse to the filtered project: %+v", h.Nodes)
	}
	if len(h.Stale) != 1 || h.Stale[0].Path != "codex-desktop|gpt-5.6-sol|high" {
		t.Fatalf("stale selection not reported: %+v", h.Stale)
	}

	// A stale selection contributes nothing rather than silently widening the
	// result back to rows the user can no longer see in the selector.
	rows, err := Aggregate(context.Background(), db, Request{Filter: filter})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Turns != 1 {
		t.Fatalf("stale selection changed the aggregate: %+v", rows)
	}
}

// Selecting one value in a facet must not hide the other values of that same
// facet, or a second value could never be added.
func TestFacetsExcludeTheirOwnDimension(t *testing.T) {
	db := seedTuples(t)
	facets, err := BuildFacets(context.Background(), db, Filter{Project: []string{"demo"}}, []string{"project", "client"})
	if err != nil {
		t.Fatal(err)
	}
	byDim := map[string]Facet{}
	for _, f := range facets {
		byDim[f.Dimension] = f
	}
	project := byDim["project"]
	if len(project.Values) != 2 {
		t.Fatalf("project facet lost its unselected options: %+v", project.Values)
	}
	selected := 0
	for _, v := range project.Values {
		if v.Selected {
			selected++
			if v.Value != "demo" {
				t.Fatalf("wrong value marked selected: %+v", v)
			}
		}
	}
	if selected != 1 {
		t.Fatalf("expected exactly one selected project value: %+v", project.Values)
	}
	// Other dimensions stay cross-filtered by the active project.
	client := byDim["client"]
	if len(client.Values) != 2 {
		t.Fatalf("client facet should be cross-filtered by project: %+v", client.Values)
	}
}

func TestFacetsKeepSelectedValuesThatNoLongerExist(t *testing.T) {
	db := seedTuples(t)
	facets, err := BuildFacets(context.Background(), db, Filter{
		Project:   []string{"other"},
		EventKind: []string{"compaction"},
	}, []string{"event_kind"})
	if err != nil {
		t.Fatal(err)
	}
	var stale []FacetValue
	for _, v := range facets[0].Values {
		if v.Stale {
			stale = append(stale, v)
		}
	}
	if len(stale) != 1 || stale[0].Value != "compaction" || !stale[0].Selected {
		t.Fatalf("selected-but-unavailable value was dropped instead of marked: %+v", facets[0].Values)
	}
}
