package formatter

import (
	"strings"
	"testing"
	"time"
)

func TestIncrementalSyncProgress(t *testing.T) {
	out := IncrementalSyncProgress(3, 2, 1, []string{"a", "b"}, []string{"err1", "err2", "err3", "err4", "err5", "err6", "err7", "err8", "err9", "err10", "err11"})
	if !strings.Contains(out, "Updated repositories: 2") {
		t.Fatalf("expected updated count, got: %s", out)
	}
	if !strings.Contains(out, "... and 1 more errors") {
		t.Fatalf("expected truncation note, got: %s", out)
	}
}

func TestJobDetailsAndList(t *testing.T) {
	start := time.Now().Add(-2 * time.Minute)
	end := time.Now()

	detail := JobDetails("id1", "full", "completed", start, &end, "oops", "progress text")
	if !strings.Contains(detail, "oops") || !strings.Contains(detail, "progress text") {
		t.Fatalf("expected error and progress in detail: %s", detail)
	}

	list := JobList([]JobInfo{
		{ID: "id1", Type: "full", Status: "completed", StartedAt: start, CompletedAt: &end},
	})
	if !strings.Contains(list, "id1 (full)") || !strings.Contains(list, "COMPLETED") {
		t.Fatalf("expected job entry in list: %s", list)
	}
}

func TestJobListEmpty(t *testing.T) {
	if got := JobList(nil); !strings.Contains(got, "No sync jobs") {
		t.Fatalf("expected empty message, got: %s", got)
	}
}
