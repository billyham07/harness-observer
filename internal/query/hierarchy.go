package query

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// HierarchyLevels are the three correlated levels of the primary selector.
// They are one unit on purpose: the observed tuples are what exists, and a UI
// that offers them as three independent lists can produce combinations the
// evidence never contained.
var HierarchyLevels = []string{"harness", "model", "reasoning_effort"}

// Tri-state of a hierarchy node.
const (
	SelectionNone    = ""
	SelectionFull    = "full"
	SelectionPartial = "partial"
)

// Node is one hierarchy node. Path is the wire form used to select it.
type Node struct {
	Value          string `json:"value"`
	Path           string `json:"path"`
	Turns          int64  `json:"turns"`
	ObservedTokens int64  `json:"observed_tokens"`
	Selection      string `json:"selection,omitempty"`
	Children       []Node `json:"children,omitempty"`
}

// Hierarchy is the observed harness -> model -> reasoning_effort tree under the
// current filter, plus the selections that no longer match anything.
type Hierarchy struct {
	Levels []string `json:"levels"`
	Nodes  []Node   `json:"nodes"`
	// Stale lists selected tuples with no rows in the current context. They are
	// reported rather than dropped: a selection that silently disappears makes
	// the numbers on screen unexplainable.
	Stale []StaleSelection `json:"stale_selections,omitempty"`
}

type StaleSelection struct {
	Path  string `json:"path"`
	Tuple Tuple  `json:"tuple"`
}

// BuildHierarchy computes the tree under filter, ignoring filter.Selection.
//
// Excluding the selection from its own option query is what makes the selector
// usable: filtering the options by what is already selected would leave a user
// unable to add a second tuple, and would hide the counts being compared.
func BuildHierarchy(ctx context.Context, db *sql.DB, filter Filter) (Hierarchy, error) {
	options := filter
	options.Selection = nil
	where, args := options.where()

	q := `SELECT harness, COALESCE(model, ''), COALESCE(reasoning_effort, ''),
	             COUNT(*), COALESCE(SUM(prompt_tokens + output_tokens), 0)
	      FROM agent_turns` + where + `
	      GROUP BY harness, COALESCE(model, ''), COALESCE(reasoning_effort, '')`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return Hierarchy{}, err
	}
	defer rows.Close()

	type key struct{ harness, model string }
	var (
		harnessOrder []string
		modelOrder   = map[string][]string{}
		efforts      = map[key][]Node{}
		harnessNodes = map[string]*Node{}
		modelNodes   = map[key]*Node{}
	)
	for rows.Next() {
		var (
			harness, modelName, effort string
			turns, tokens              int64
		)
		if err := rows.Scan(&harness, &modelName, &effort, &turns, &tokens); err != nil {
			return Hierarchy{}, err
		}
		if _, ok := harnessNodes[harness]; !ok {
			harnessNodes[harness] = &Node{Value: harness, Path: Tuple{Harness: harness}.String()}
			harnessOrder = append(harnessOrder, harness)
		}
		h := harnessNodes[harness]
		h.Turns += turns
		h.ObservedTokens += tokens

		k := key{harness, modelName}
		if _, ok := modelNodes[k]; !ok {
			name := modelName
			modelNodes[k] = &Node{Value: modelName, Path: Tuple{Harness: harness, Model: &name}.String()}
			modelOrder[harness] = append(modelOrder[harness], modelName)
		}
		m := modelNodes[k]
		m.Turns += turns
		m.ObservedTokens += tokens

		mv, ev := modelName, effort
		efforts[k] = append(efforts[k], Node{
			Value:          effort,
			Path:           Tuple{Harness: harness, Model: &mv, Effort: &ev}.String(),
			Turns:          turns,
			ObservedTokens: tokens,
		})
	}
	if err := rows.Err(); err != nil {
		return Hierarchy{}, err
	}

	sel := newSelectionIndex(filter.Selection)
	out := Hierarchy{Levels: HierarchyLevels}
	sort.Strings(harnessOrder)
	for _, harness := range harnessOrder {
		h := *harnessNodes[harness]
		h.Children = nil
		models := modelOrder[harness]
		sort.Strings(models)
		for _, modelName := range models {
			k := key{harness, modelName}
			m := *modelNodes[k]
			leaves := efforts[k]
			sort.Slice(leaves, func(i, j int) bool { return leaves[i].Value < leaves[j].Value })
			for i := range leaves {
				if sel.covers(harness, modelName, leaves[i].Value) {
					leaves[i].Selection = SelectionFull
				}
			}
			m.Children = leaves
			m.Selection = rollUp(leaves)
			h.Children = append(h.Children, m)
		}
		h.Selection = rollUp(h.Children)
		out.Nodes = append(out.Nodes, h)
	}

	for _, t := range filter.Selection {
		if sel.matched[t.String()] {
			continue
		}
		out.Stale = append(out.Stale, StaleSelection{Path: t.String(), Tuple: t})
	}
	return out, nil
}

// rollUp derives a parent's tri-state from its children.
func rollUp(children []Node) string {
	if len(children) == 0 {
		return SelectionNone
	}
	selected := 0
	for _, c := range children {
		switch c.Selection {
		case SelectionFull:
			selected++
		case SelectionPartial:
			return SelectionPartial
		}
	}
	switch selected {
	case 0:
		return SelectionNone
	case len(children):
		return SelectionFull
	default:
		return SelectionPartial
	}
}

// selectionIndex answers "is this observed leaf covered by the selection?" and
// records which selections matched something, so the rest can be called stale.
type selectionIndex struct {
	tuples  []Tuple
	matched map[string]bool
}

func newSelectionIndex(tuples []Tuple) *selectionIndex {
	return &selectionIndex{tuples: tuples, matched: make(map[string]bool, len(tuples))}
}

func (s *selectionIndex) covers(harness, model, effort string) bool {
	hit := false
	for _, t := range s.tuples {
		if t.Harness != harness {
			continue
		}
		if t.Model != nil && *t.Model != model {
			continue
		}
		if t.Effort != nil && *t.Effort != effort {
			continue
		}
		s.matched[t.String()] = true
		hit = true
	}
	return hit
}

// FacetDimensions are the flat, independent dimensions. harness/model/effort
// are deliberately absent: they are correlated and belong to the hierarchy.
var FacetDimensions = []string{"client", "provider", "project", "event_kind", "basis", "semantics"}

type FacetValue struct {
	Value          string `json:"value"`
	Turns          int64  `json:"turns"`
	ObservedTokens int64  `json:"observed_tokens"`
	Selected       bool   `json:"selected,omitempty"`
	// Stale marks a value that is still selected but no longer present under
	// the rest of the filter.
	Stale bool `json:"stale,omitempty"`
}

type Facet struct {
	Dimension string       `json:"dimension"`
	Values    []FacetValue `json:"values"`
}

// BuildFacets returns the available values per flat dimension.
//
// Each facet is computed with its own dimension's selection removed. Applying a
// dimension to its own option list is the classic faceted-search bug: after
// picking one client every other client disappears and can never be added.
func BuildFacets(ctx context.Context, db *sql.DB, filter Filter, dimensions []string) ([]Facet, error) {
	if len(dimensions) == 0 {
		dimensions = FacetDimensions
	}
	out := make([]Facet, 0, len(dimensions))
	for _, dim := range dimensions {
		column, ok := groupColumns[strings.ToLower(strings.TrimSpace(dim))]
		if !ok {
			return nil, fmt.Errorf("unknown facet dimension %q (known: %s)", dim, strings.Join(GroupDimensions(), ", "))
		}
		options, selected := withoutDimension(filter, dim)
		where, args := options.where()
		q := fmt.Sprintf(`SELECT COALESCE(%s, ''), COUNT(*), COALESCE(SUM(prompt_tokens + output_tokens), 0)
		                  FROM agent_turns%s GROUP BY COALESCE(%s, '') ORDER BY 3 DESC`, column, where, column)
		facet, err := collectFacet(ctx, db, dim, q, args, selected)
		if err != nil {
			return nil, err
		}
		out = append(out, facet)
	}
	return out, nil
}

func collectFacet(ctx context.Context, db *sql.DB, dim, q string, args []any, selected map[string]bool) (Facet, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return Facet{}, err
	}
	defer rows.Close()

	facet := Facet{Dimension: dim}
	seen := make(map[string]bool)
	for rows.Next() {
		var v FacetValue
		if err := rows.Scan(&v.Value, &v.Turns, &v.ObservedTokens); err != nil {
			return Facet{}, err
		}
		v.Selected = selected[v.Value]
		seen[v.Value] = true
		facet.Values = append(facet.Values, v)
	}
	if err := rows.Err(); err != nil {
		return Facet{}, err
	}
	// A selected value that is no longer available stays visible and is marked,
	// so the user can see why the numbers changed and clear it deliberately.
	var stale []string
	for v := range selected {
		if !seen[v] {
			stale = append(stale, v)
		}
	}
	sort.Strings(stale)
	for _, v := range stale {
		facet.Values = append(facet.Values, FacetValue{Value: v, Selected: true, Stale: true})
	}
	return facet, nil
}

// withoutDimension returns the filter with one dimension cleared, plus the set
// of values that dimension currently has selected.
func withoutDimension(f Filter, dim string) (Filter, map[string]bool) {
	selected := map[string]bool{}
	mark := func(values []string) {
		for _, v := range values {
			selected[v] = true
		}
	}
	switch strings.ToLower(strings.TrimSpace(dim)) {
	case "client":
		mark(f.Client)
		f.Client = nil
	case "provider":
		mark(f.Provider)
		f.Provider = nil
	case "project":
		mark(f.Project)
		f.Project = nil
	case "event_kind":
		mark(f.EventKind)
		f.EventKind = nil
	case "basis":
		mark(f.Basis)
		f.Basis = nil
	case "semantics":
		mark(f.Semantics)
		f.Semantics = nil
	case "model":
		mark(f.Model)
		f.Model = nil
	case "effort":
		mark(f.Effort)
		f.Effort = nil
	case "harness":
		mark(f.Harness)
		f.Harness = nil
	}
	return f, selected
}
