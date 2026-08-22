package serve

// The Logs endpoint is the row-level read side. It shares RequestFromValues
// with every other endpoint so a Logs page, the chart above it and the CLI
// cannot end up with three different definitions of the same filter.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/billyham07/harness-observer/internal/query"
)

// logs answers one page of normalized events:
//
//	/api/logs?from=2026-08-01&to=2026-08-21&harness=codex-cli&page=2&page_size=50
//	          &session=019a...&sort=time&dir=desc
//
// Filtering, ordering and paging happen in DuckDB. The response carries the
// total, the effective page size and, when the result is empty, which of the
// three empty states it is.
func (s Server) logs(w http.ResponseWriter, r *http.Request) {
	q, err := LogQueryFromValues(r.URL.Query().Get, multi(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := query.QueryLogs(r.Context(), s.DB.SQL(), q, s.pricer())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, page)
}

// LogQueryFromValues builds a log page request from a flat key/value source,
// shared by the HTTP handler and the CLI.
//
// The filter half is parsed by RequestFromValues, deliberately: harness/model/
// effort correlation, time parsing and every flat dimension keep exactly one
// implementation. Only paging, ordering and the default time bound are added
// here.
func LogQueryFromValues(get func(string) string, getAll func(string) []string) (query.LogQuery, error) {
	var q query.LogQuery
	req, err := RequestFromValues(get, getAll)
	if err != nil {
		return q, err
	}
	q.Filter = req.Filter
	// Logs default to a half-open window: a row-level list is read page by page
	// and window by window, and an inclusive upper bound makes two adjacent
	// windows both contain the event on the boundary. An explicit
	// bound=inclusive still gets the aggregation's semantics.
	if strings.TrimSpace(get("bound")) == "" {
		q.Filter.ToExclusive = true
	}

	q.Sort = query.LogSort(strings.TrimSpace(get("sort")))
	switch dir := strings.ToLower(strings.TrimSpace(get("dir"))); dir {
	case "", "desc":
	case "asc":
		q.Ascending = true
	default:
		return q, fmt.Errorf("invalid dir %q (use asc or desc)", dir)
	}

	if q.Page, err = parsePositive(get("page"), "page"); err != nil {
		return q, err
	}
	// page_size is the log-specific name; limit is accepted as an alias so the
	// CLI's --limit and the other endpoints' limit mean the same thing here.
	size := strings.TrimSpace(get("page_size"))
	if size == "" {
		size = strings.TrimSpace(get("limit"))
	}
	if q.PageSize, err = parsePositive(size, "page_size"); err != nil {
		return q, err
	}
	return q, nil
}

func parsePositive(v, name string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, v)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid %s %d", name, n)
	}
	return n, nil
}
