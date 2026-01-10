package mcp

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/indexer"
	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestHandleGetReleaseSummary(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	rel := testutil.InsertRelease(t, db, repo.ID, "1.0.0", "v1.0.0", "v0.9.0")
	testutil.ReplaceReleaseEntries(t, db, rel.ID, []database.ProviderReleaseEntry{
		{
			ReleaseID: rel.ID,
			EntryKey:  "entry1",
			Title:     "Added thing",
			Section:   "ADDED",
		},
	})

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	resp := s.handleGetReleaseSummary(map[string]any{"version": "1.0.0"})
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content blocks, got %#v", resp)
	}
	text := content[0].Text
	if !strings.Contains(text, "terraform-provider-azurerm") || !strings.Contains(text, "1.0.0") {
		t.Fatalf("release summary missing expected text: %q", text)
	}
}

func TestHandleGetReleaseSnippet(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_virtual_network", "resource", "internal/services/network/virtual_network_resource.go")
	rel := testutil.InsertRelease(t, db, repo.ID, "1.0.0", "v1.0.0", "v0.9.0")
	testutil.ReplaceReleaseEntries(t, db, rel.ID, []database.ProviderReleaseEntry{
		{
			ReleaseID:    rel.ID,
			EntryKey:     "entry1",
			Identifier:   sqlNull("vn-change"),
			Title:        "Virtual network updates",
			Section:      "CHANGED",
			ResourceName: sqlNull("azurerm_virtual_network"),
		},
	})

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db
	s.syncer = &fakeSyncer{
		compareResult: &indexer.GitHubCompareResult{
			Files: []indexer.GitHubCompareFile{
				{
					Filename: res.FilePath.String,
					Patch:    "@@ -1 +1 @@\n+virtual network change",
				},
			},
		},
	}

	resp := s.handleGetReleaseSnippet(map[string]any{
		"version": "1.0.0",
		"query":   "vn-change",
	})
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content blocks, got %#v", resp)
	}
	text := content[0].Text
	if !strings.Contains(text, "virtual_network_resource.go") || !strings.Contains(text, "virtual network change") {
		t.Fatalf("release snippet missing expected diff: %q", text)
	}

	// No previous tag: should error
	rel.PreviousTag = sqlNull("")
	if _, _, err := db.UpsertProviderRelease(rel); err != nil {
		t.Fatalf("failed to update release: %v", err)
	}
	resp = s.handleGetReleaseSnippet(map[string]any{
		"version": "1.0.0",
		"query":   "vn-change",
	})
	content = resp["content"].([]ContentBlock)
	if !strings.Contains(content[0].Text, "Unable to compute diff") {
		t.Fatalf("expected diff error when previous tag missing, got %s", content[0].Text)
	}
}

func sqlNull(val string) sql.NullString {
	return sql.NullString{String: val, Valid: val != ""}
}

func TestHandleBackfillRelease(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")

	changelog := `# Changelog

## 4.48.0 (2024-01-15)

FEATURES:

* **New Resource:** azurerm_foo

ENHANCEMENTS:

* azurerm_bar: added support for thing

BUG FIXES:

* azurerm_baz: fixed issue

## 4.47.0 (2024-01-01)

FEATURES:

* Initial release
`

	testutil.InsertFile(t, db, repo.ID, "CHANGELOG.md", "markdown", changelog)

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("backfill_existing_version", func(t *testing.T) {
		resp := s.handleBackfillRelease(map[string]any{"version": "4.48.0"})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if !strings.Contains(text, "Backfilled") || !strings.Contains(text, "v4.48.0") {
			t.Fatalf("expected backfill success, got %q", text)
		}

		// Verify the release was stored
		rel, entries, err := db.GetReleaseWithEntriesByVersion(repo.ID, "4.48.0")
		if err != nil {
			t.Fatalf("failed to get backfilled release: %v", err)
		}
		if rel.Version != "4.48.0" {
			t.Fatalf("expected version 4.48.0, got %s", rel.Version)
		}
		if len(entries) == 0 {
			t.Fatal("expected release entries to be stored")
		}
	})

	t.Run("backfill_with_v_prefix", func(t *testing.T) {
		resp := s.handleBackfillRelease(map[string]any{"version": "v4.47.0"})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if !strings.Contains(text, "Backfilled") {
			t.Fatalf("expected backfill success with v prefix, got %q", text)
		}
	})

	t.Run("version_not_found", func(t *testing.T) {
		resp := s.handleBackfillRelease(map[string]any{"version": "9.99.0"})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "not found") {
			t.Fatalf("expected version not found error, got %s", content[0].Text)
		}
	})

	t.Run("missing_version_parameter", func(t *testing.T) {
		resp := s.handleBackfillRelease(map[string]any{})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "version is required") {
			t.Fatalf("expected version required error, got %s", content[0].Text)
		}
	})

	t.Run("repository_not_synced", func(t *testing.T) {
		db2 := testutil.NewTestDB(t)
		s2 := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
		s2.db = db2

		resp := s2.handleBackfillRelease(map[string]any{"version": "4.48.0"})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "not been synced") {
			t.Fatalf("expected repo not synced error, got %s", content[0].Text)
		}
	})
}
