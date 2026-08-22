package source

import (
	"testing"
	"time"
)

func TestWindowRejectsUnknownTimestampWhenBounded(t *testing.T) {
	now := time.Now()
	w := Window{Since: &now}
	if w.Contains(time.Time{}) {
		t.Fatal("zero timestamp must not be included in a bounded window")
	}
	if !(Window{}).Contains(time.Time{}) {
		t.Fatal("zero timestamp should remain visible in an unbounded raw dump")
	}
}

func TestWindowInclusiveBounds(t *testing.T) {
	since := time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC)
	until := time.Date(2026, 8, 20, 17, 15, 0, 0, time.UTC)
	w := Window{Since: &since, Until: &until}
	if !w.Contains(since) || !w.Contains(until) {
		t.Fatal("window bounds must be inclusive")
	}
	if w.Contains(since.Add(-time.Nanosecond)) || w.Contains(until.Add(time.Nanosecond)) {
		t.Fatal("timestamps outside bounds must be rejected")
	}
}
