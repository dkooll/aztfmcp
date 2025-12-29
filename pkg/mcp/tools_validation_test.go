package mcp

import (
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestHandleSearchValidations(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "path/to/resource.go")
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:       "name",
		Validation: sqlNull("validation.StringIsNotEmpty"),
	})
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name: "other",
	})

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	resp := s.handleSearchValidations(map[string]any{
		"contains": "StringIsNotEmpty",
	})
	content := resp["content"].([]ContentBlock)
	if !strings.Contains(content[0].Text, "name") || strings.Contains(content[0].Text, "other") {
		t.Fatalf("expected validated attribute only, got %s", content[0].Text)
	}
}
