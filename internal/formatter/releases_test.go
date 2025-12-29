package formatter

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
)

func TestReleaseSummaryEmptyRelease(t *testing.T) {
	out := ReleaseSummary("", nil, nil)
	if !contains(out, "No release metadata") {
		t.Fatalf("expected fallback message, got: %s", out)
	}
}

func TestReleaseSummaryWithSections(t *testing.T) {
	release := &database.ProviderRelease{
		Version: "1.0.0",
		Tag:     "v1.0.0",
		PreviousTag: sql.NullString{
			Valid:  true,
			String: "v0.9.0",
		},
		ReleaseDate: sql.NullString{Valid: true, String: "2024-01-02"},
	}
	entries := []database.ProviderReleaseEntry{
		{Section: "Bug Fixes", Title: "Patched issues"},
		{Section: "Features", Title: "New shiny"},
	}

	out := ReleaseSummary("hashicorp/terraform-provider-azurerm", release, entries)
	if !contains(out, "Features") || !contains(out, "Bug Fixes") {
		t.Fatalf("expected sections in output, got: %s", out)
	}
	if !contains(out, "v0.9.0 → v1.0.0") {
		t.Fatalf("expected range with tags, got: %s", out)
	}
	if !contains(out, "January 2, 2024") {
		t.Fatalf("expected formatted date, got: %s", out)
	}
}

func TestRenderHelpers(t *testing.T) {
	release := &database.ProviderRelease{
		Tag:               "v1.0.0",
		PreviousTag:       sql.NullString{Valid: true, String: "v0.9.0"},
		CommitSHA:         sql.NullString{Valid: true, String: "abcdef123456"},
		PreviousCommitSHA: sql.NullString{Valid: true, String: "000001234567"},
	}
	if got := renderRange(release); got != "v0.9.0 (0000012) → v1.0.0 (abcdef1)" {
		t.Fatalf("unexpected range: %s", got)
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Fatalf("shortSHA should not pad: %s", got)
	}
	if got := formatTag("v1", ""); got != "v1" {
		t.Fatalf("formatTag without sha: %s", got)
	}
}

// contains is a tiny helper to avoid repeated strings.Contains in tests.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
