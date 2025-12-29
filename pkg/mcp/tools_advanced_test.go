package mcp

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestHandleAnalyzeUpdateBehavior(t *testing.T) {
	s, resource := setupServerWithResource(t, database.ProviderAttribute{
		Name:     "name",
		ForceNew: true,
		Required: true,
	})
	testutil.UpsertResourceSource(t, s.db, resource.ID, "custom diff")

	resp := s.handleAnalyzeUpdateBehavior(map[string]any{
		"resource_name":  resource.Name,
		"attribute_path": "name",
	})

	// Check for successful response format
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array, got %#v", resp)
	}

	text := content[0].Text
	if !strings.Contains(text, "Requires resource recreation") {
		t.Fatalf("expected recreation message, got %s", text)
	}
	if !strings.Contains(text, "Required, ForceNew") {
		t.Fatalf("expected ForceNew flag, got %s", text)
	}
	if !strings.Contains(text, "CustomizeDiff") {
		t.Fatalf("expected CustomizeDiff mention, got %s", text)
	}
}

func TestHandleCompareResources(t *testing.T) {
	s, resource := setupServerWithResource(t, database.ProviderAttribute{Name: "name"})
	other := testutil.InsertResource(t, s.db, resource.RepositoryID, "azurerm_other", "resource", "")
	testutil.InsertAttribute(t, s.db, other.ID, database.ProviderAttribute{Name: "name"})
	testutil.InsertAttribute(t, s.db, other.ID, database.ProviderAttribute{Name: "other_only"})

	resp := s.handleCompareResources(map[string]any{
		"resource_a": resource.Name,
		"resource_b": other.Name,
		"max_names":  10,
	})

	// Check for successful response format
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array, got %#v", resp)
	}

	text := content[0].Text
	if !strings.Contains(text, "Common Attributes") {
		t.Fatalf("expected common attributes section, got %s", text)
	}
	if !strings.Contains(text, "name") {
		t.Fatalf("expected 'name' in shared attributes, got %s", text)
	}
}

func TestHandleFindSimilarResources(t *testing.T) {
	s, resource := setupServerWithResource(t, database.ProviderAttribute{Name: "name"})
	other := testutil.InsertResource(t, s.db, resource.RepositoryID, "azurerm_other", "resource", "")
	testutil.InsertAttribute(t, s.db, other.ID, database.ProviderAttribute{Name: "name"})
	testutil.InsertAttribute(t, s.db, other.ID, database.ProviderAttribute{Name: "shared"})

	resp := s.handleFindSimilarResources(map[string]any{
		"resource_name":        resource.Name,
		"similarity_threshold": 0.1,
	})

	// Check for successful response format
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array, got %#v", resp)
	}

	text := content[0].Text
	if !strings.Contains(text, "Similar Resources") {
		t.Fatalf("expected similar resources title, got %s", text)
	}
	// Should find azurerm_other as similar
	if !strings.Contains(text, "azurerm_other") {
		t.Fatalf("expected azurerm_other in results, got %s", text)
	}
}

func TestHandleExplainBreakingChange(t *testing.T) {
	s, resource := setupServerWithResource(t, database.ProviderAttribute{
		Name:     "location",
		ForceNew: true,
	})

	resp := s.handleExplainBreakingChange(map[string]any{
		"resource_name":  resource.Name,
		"attribute_name": "location",
	})

	// Check for successful response format
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array, got %#v", resp)
	}

	text := content[0].Text
	if !strings.Contains(text, "marked ForceNew") {
		t.Fatalf("expected ForceNew mention, got %s", text)
	}
	if !strings.Contains(text, "Breaking Change Analysis") {
		t.Fatalf("expected breaking change title, got %s", text)
	}
}

func TestHandleSuggestValidationImprovements(t *testing.T) {
	s, resource := setupServerWithResource(t, database.ProviderAttribute{
		Name:     "name",
		Required: true,
	})

	resp := s.handleSuggestValidationImprovements(map[string]any{
		"resource_name": resource.Name,
	})

	// Check for successful response format
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array, got %#v", resp)
	}

	text := content[0].Text
	if !strings.Contains(text, "Validation Analysis") {
		t.Fatalf("expected validation analysis title, got %s", text)
	}
	if !strings.Contains(text, "Suggestions") {
		t.Fatalf("expected suggestions section, got %s", text)
	}
}

func TestHandleTraceAttributeDependencies(t *testing.T) {
	s, resource := setupServerWithResource(t, database.ProviderAttribute{
		Name:          "endpoint",
		Required:      true,
		ConflictsWith: sql.NullString{String: "other", Valid: true},
		RequiredWith:  sql.NullString{String: "dependent", Valid: true},
	})

	resp := s.handleTraceAttributeDependencies(map[string]any{
		"resource_name":  resource.Name,
		"attribute_name": "endpoint",
	})

	// Check for successful response format
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array, got %#v", resp)
	}

	text := content[0].Text
	if !strings.Contains(text, "Attribute Dependencies") {
		t.Fatalf("expected dependencies title, got %s", text)
	}
	if !strings.Contains(text, "ConflictsWith") {
		t.Fatalf("expected ConflictsWith section, got %s", text)
	}
	if !strings.Contains(text, "other") {
		t.Fatalf("expected 'other' in conflicts, got %s", text)
	}
}

func setupServerWithResource(t *testing.T, attr database.ProviderAttribute) (*Server, *database.ProviderResource) {
	t.Helper()
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "path/to/file.go")
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{Name: "unique_to_a"})
	testutil.InsertAttribute(t, db, res.ID, attr)

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db
	s.syncer = &fakeSyncer{}
	return s, res
}
