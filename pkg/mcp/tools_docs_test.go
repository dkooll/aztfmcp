package mcp

import (
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestHandleGetResourceDocs(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "internal/example/resource.go")
	docContent := strings.Join([]string{
		"---",
		"title: example",
		"---",
		"# Heading",
		"## Usage",
		"Use it well.",
	}, "\n")
	testutil.InsertFile(t, db, repo.ID, "docs/resources/example.md", "markdown", docContent)

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("section extract", func(t *testing.T) {
		resp := s.handleGetResourceDocs(map[string]any{
			"name":    "azurerm_example",
			"section": "Usage",
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "Use it well.") {
			t.Fatalf("expected usage section, got %s", content[0].Text)
		}
	})

	t.Run("not found", func(t *testing.T) {
		resp := s.handleGetResourceDocs(map[string]any{
			"name": "azurerm_missing",
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "not found") {
			t.Fatalf("expected not found error, got %s", content[0].Text)
		}
	})
}

func TestHandleGetExample(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	testutil.InsertFile(t, db, repo.ID, "examples/basic/main.tf", "terraform", "resource \"foo\" \"bar\" {}")
	testutil.InsertFile(t, db, repo.ID, "examples/basic/variables.tf", "terraform", "variable \"name\" {}")

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	resp := s.handleGetExample(map[string]any{"path": "basic"})
	content := resp["content"].([]ContentBlock)
	if !strings.Contains(content[0].Text, "main.tf") || !strings.Contains(content[0].Text, "resource \"foo\"") {
		t.Fatalf("expected example file content, got %s", content[0].Text)
	}
	if !strings.Contains(content[0].Text, "variables.tf") {
		t.Fatalf("expected variables.tf included, got %s", content[0].Text)
	}
}

func TestHandleListResourceTests(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "internal/example/resource.go")
	testContent := `package example
import "testing"
func TestAccAzureRMExample_basic(t *testing.T) {}
func TestAccAzAPIExample_other(t *testing.T) {}
`
	testutil.InsertFile(t, db, repo.ID, "internal/example/resource_test.go", "go", testContent)

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	resp := s.handleListResourceTests(map[string]any{"name": res.Name})
	content := resp["content"].([]ContentBlock)
	if !strings.Contains(content[0].Text, "TestAccAzureRMExample_basic") {
		t.Fatalf("expected test name in output, got %s", content[0].Text)
	}
}

func TestHandleListFeatureFlags(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	source := `
package features
var Features = map[string]struct{
	Description string
	Default     bool
}{
	"flag_one": {Description: "first", Default: true},
}
`
	testutil.InsertFile(t, db, repo.ID, "internal/features/config/features.go", "go", source)

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	resp := s.handleListFeatureFlags()
	content := resp["content"].([]ContentBlock)
	if !strings.Contains(content[0].Text, "flag_one") {
		t.Fatalf("expected feature flag in output, got %s", content[0].Text)
	}
}

func TestHandleGetResourceBehaviors(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "internal/example/resource.go")
	schemaSnippet := `&schema.Resource{
Timeouts: &schema.ResourceTimeout{Create: "30m"},
CustomizeDiff: customizeDiffFunc,
Importer: &schema.ResourceImporter{},
}`
	if err := db.UpsertProviderResourceSource(res.ID, "Example", "internal/example/resource.go", "", schemaSnippet, "", "", "", ""); err != nil {
		t.Fatalf("failed to upsert resource source: %v", err)
	}

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	resp := s.handleGetResourceBehaviors(map[string]any{"name": res.Name})
	content := resp["content"].([]ContentBlock)
	if !strings.Contains(content[0].Text, "Timeouts") {
		t.Fatalf("expected timeouts info, got %s", content[0].Text)
	}
}
