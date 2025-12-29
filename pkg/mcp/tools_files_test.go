package mcp

import (
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestHandleGetFileContent(t *testing.T) {
	t.Run("repository not found", func(t *testing.T) {
		s := NewServer("", "", "org", "repo")
		s.db = testutil.NewTestDB(t)

		resp := s.handleGetFileContent(map[string]any{"file_path": "missing.txt"})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "Repository") {
			t.Fatalf("expected repository error, got %v", content[0].Text)
		}
	})

	t.Run("returns snippet with lines", func(t *testing.T) {
		db := testutil.NewTestDB(t)
		repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
		testutil.InsertFile(t, db, repo.ID, "path/file.go", "go", "line1\nline2\nline3")

		s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
		s.db = db

		resp := s.handleGetFileContent(map[string]any{
			"repository": "terraform-provider-azurerm",
			"file_path":  "path/file.go",
			"start_line": 2,
			"end_line":   3,
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "Lines:** 2-3 of 3") || !strings.Contains(content[0].Text, "line2") {
			t.Fatalf("expected line window in response, got: %s", content[0].Text)
		}

		resp = s.handleGetFileContent(map[string]any{
			"repository": "terraform-provider-azurerm",
			"file_path":  "path/file.go",
			"start_line": 5,
			"end_line":   1,
		})
		content = resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "Lines:** 1-1 of 3") {
			t.Fatalf("expected clamped line window, got: %s", content[0].Text)
		}
	})
}

func TestHandleGetSchemaSource(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "path/to/file.go")
	testutil.UpsertResourceSource(t, db, res.ID, "")
	if err := db.UpsertProviderResourceSource(res.ID, "Example", "path/to/file.go", "func(){}", "schema {}", "", "", "", ""); err != nil {
		t.Fatalf("failed to upsert resource source: %v", err)
	}

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("invalid section", func(t *testing.T) {
		resp := s.handleGetSchemaSource(map[string]any{
			"name":    "azurerm_example",
			"section": "bad",
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "section must be") {
			t.Fatalf("expected section error, got %s", content[0].Text)
		}
	})

	t.Run("returns schema snippet", func(t *testing.T) {
		resp := s.handleGetSchemaSource(map[string]any{
			"name":      "azurerm_example",
			"section":   "schema",
			"max_lines": 1,
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "schema {") {
			t.Fatalf("expected schema snippet, got %s", content[0].Text)
		}
	})
}

func TestHandleSearchResourcesAndAttributes(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "path/to/resource.go")
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:     "name",
		Required: true,
	})
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:     "optional_attr",
		Optional: true,
	})

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("search resources requires query", func(t *testing.T) {
		resp := s.handleSearchResources(map[string]any{"query": ""})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "query is required") {
			t.Fatalf("expected query error, got %s", content[0].Text)
		}
	})

	t.Run("search resource attributes with flags filter", func(t *testing.T) {
		resp := s.handleSearchResourceAttributes(map[string]any{
			"flags": []string{"required"},
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "name") || strings.Contains(content[0].Text, "optional_attr") {
			t.Fatalf("expected only required attribute, got %s", content[0].Text)
		}
	})

	t.Run("list resources compact", func(t *testing.T) {
		resp := s.handleListResources(map[string]any{"compact": true, "limit": 10, "kind": "resource"})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "Resources:") || !strings.Contains(content[0].Text, "azurerm_example") {
			t.Fatalf("expected compact resource list, got %s", content[0].Text)
		}
	})

	t.Run("search code with path prefix", func(t *testing.T) {
		testutil.InsertFile(t, db, repo.ID, "internal/example/file.go", "go", "package example\n// searchme")
		resp := s.handleSearchCode(map[string]any{
			"query":       "searchme",
			"path_prefix": "internal/example",
			"limit":       5,
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "internal/example/file.go") {
			t.Fatalf("expected search result with path, got %s", content[0].Text)
		}
	})
}
