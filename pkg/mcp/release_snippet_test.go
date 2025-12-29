package mcp

import (
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
)

func TestFormatReleaseSnippetResponseGolden(t *testing.T) {
	repo := &database.Repository{
		Name:     "terraform-provider-azurerm",
		FullName: "hashicorp/terraform-provider-azurerm",
	}
	release := &database.ProviderRelease{
		Version:         "1.2.3",
		ComparisonURL:   sqlNull("https://example.com/compare"),
		PreviousTag:     sqlNull("v1.2.2"),
		PreviousVersion: sqlNull("1.2.2"),
	}
	entry := &database.ProviderReleaseEntry{
		Title: "Bug fix",
	}

	output := formatReleaseSnippetResponse(repo, release, entry, "path/to/resource.go", "diffline1\n+diffline2", true, 2, true, true, true, true)
	want := strings.Join([]string{
		"Release 1.2.3 – Bug fix",
		"Repository: hashicorp/terraform-provider-azurerm",
		"File: path/to/resource.go",
		"```diff",
		"diffline1",
		"+diffline2",
		"```",
		"… showing first 2 diff lines",
		"Compare: https://example.com/compare",
	}, "\n")

	if strings.TrimSpace(want) != strings.TrimSpace(output) {
		t.Fatalf("golden mismatch\nwant:\n%s\n\ngot:\n%s", want, output)
	}
}
