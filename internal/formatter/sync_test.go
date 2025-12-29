package formatter

import (
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/indexer"
)

func TestSyncProgress(t *testing.T) {
	progress := &indexer.SyncProgress{
		TotalRepos:     11,
		ProcessedRepos: 11,
		Errors:         []string{"err1", "err2", "err3", "err4", "err5", "err6", "err7", "err8", "err9", "err10", "err11"},
		UpdatedRepos:   []string{"a", "b", "c"},
	}
	out := SyncProgress(progress)
	if !strings.Contains(out, "Successfully synced 0/11") {
		t.Fatalf("expected success count, got: %s", out)
	}
	if !strings.Contains(out, "... and 1 more errors") || !strings.Contains(out, "- a") {
		t.Fatalf("expected truncation of errors, got: %s", out)
	}
}
