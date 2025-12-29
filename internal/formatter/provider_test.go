package formatter

import (
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
)

func TestProviderResourceListEmpty(t *testing.T) {
	out := ProviderResourceList(nil)
	if !strings.Contains(out, "No provider resources indexed") {
		t.Fatalf("expected empty notice, got: %s", out)
	}
}

func TestProviderResourceListNonEmpty(t *testing.T) {
	resources := []database.ProviderResource{
		{
			Name:        "azurerm_example",
			Kind:        "resource",
			FilePath:    sql.NullString{Valid: true, String: "path/file.go"},
			DisplayName: sql.NullString{Valid: true, String: "Example"},
			Description: sql.NullString{Valid: true, String: "desc"},
		},
	}
	out := ProviderResourceList(resources)
	if !strings.Contains(out, "azurerm_example") || !strings.Contains(out, "Example") || !strings.Contains(out, "resource") {
		t.Fatalf("expected resource details, got: %s", out)
	}
}

func TestProviderResourceListCompact(t *testing.T) {
	resources := []database.ProviderResource{
		{Name: "azurerm_example", Kind: "resource", FilePath: sql.NullString{Valid: true, String: "path.go"}},
	}
	out := ProviderResourceListCompact(resources)
	if !strings.Contains(out, "Resources: 1") || !strings.Contains(out, "azurerm_example") {
		t.Fatalf("expected compact listing, got: %s", out)
	}
}

func TestProviderResourceDetail(t *testing.T) {
	resource := &database.ProviderResource{
		Name:            "azurerm_example",
		DisplayName:     sql.NullString{Valid: true, String: "Example"},
		Kind:            "resource",
		FilePath:        sql.NullString{Valid: true, String: "path.go"},
		Description:     sql.NullString{Valid: true, String: "desc"},
		BreakingChanges: sql.NullString{Valid: true, String: "breaking"},
	}
	attrs := []database.ProviderAttribute{
		{Name: "name", Required: true, Description: sql.NullString{Valid: true, String: "desc"}},
		{Name: "count", Computed: true, Optional: true},
	}

	out := ProviderResourceDetail(resource, attrs, SchemaRenderOptions{Compact: true})
	if !strings.Contains(out, "# Example") || !strings.Contains(out, "Attributes (2)") {
		t.Fatalf("expected heading and attributes count, got: %s", out)
	}
	if !strings.Contains(out, "breaking") {
		t.Fatalf("expected breaking changes section")
	}
}

func TestProviderSchemaSource(t *testing.T) {
	out := ProviderSchemaSource("azurerm_example", "schema", "path.go", "Example", "fn()", true)
	if !strings.Contains(out, "path.go") || !strings.Contains(out, "fn()") || !strings.Contains(out, "Note") {
		t.Fatalf("expected schema source content, got: %s", out)
	}
	empty := ProviderSchemaSource("azurerm_example", "", "", "", "", false)
	if !strings.Contains(empty, "Snippet not available") {
		t.Fatalf("expected fallback message, got: %s", empty)
	}
}

func TestProviderAttributeSearch(t *testing.T) {
	t.Run("no matches", func(t *testing.T) {
		result := ProviderAttributeSearch(nil)
		if !strings.Contains(result, "# Attribute Search (0 matches)") {
			t.Error("expected header with zero count")
		}
		if !strings.Contains(result, "No provider attributes matched") {
			t.Error("expected no matches message")
		}
	})

	t.Run("with matches", func(t *testing.T) {
		results := []database.ProviderAttributeSearchResult{
			{
				ResourceName:     "azurerm_storage_account",
				ResourceKind:     "resource",
				ResourceFilePath: sql.NullString{Valid: true, String: "path/storage.go"},
				Attribute: database.ProviderAttribute{
					Name:          "name",
					Required:      true,
					Description:   sql.NullString{Valid: true, String: "The storage account name"},
					ConflictsWith: sql.NullString{Valid: true, String: "other_attr"},
					Validation:    sql.NullString{Valid: true, String: "StringLenBetween(3,24)"},
					DiffSuppress:  sql.NullString{Valid: true, String: "suppress.CaseInsensitive"},
				},
			},
			{
				ResourceName: "azurerm_storage_account",
				ResourceKind: "resource",
				Attribute: database.ProviderAttribute{
					Name:     "location",
					Optional: true,
					Computed: true,
				},
			},
		}

		result := ProviderAttributeSearch(results)

		if !strings.Contains(result, "# Attribute Search (2 matches)") {
			t.Error("expected header with match count")
		}
		if !strings.Contains(result, "| Resource | Attribute | Flags | Notes |") {
			t.Error("expected table header")
		}
		if !strings.Contains(result, "azurerm_storage_account (resource)") {
			t.Error("expected resource name and kind")
		}
		if !strings.Contains(result, "`name`") {
			t.Error("expected attribute name")
		}
		if !strings.Contains(result, "required") {
			t.Error("expected required flag")
		}
		if !strings.Contains(result, "conflicts: other_attr") {
			t.Error("expected conflicts note")
		}
		if !strings.Contains(result, "validation: StringLenBetween") {
			t.Error("expected validation note")
		}
		if !strings.Contains(result, "diff suppress: suppress.CaseInsensitive") {
			t.Error("expected diff suppress note")
		}
		if !strings.Contains(result, "path/storage.go") {
			t.Error("expected file path")
		}
	})
}

func TestProviderAttributeSearchCompact(t *testing.T) {
	results := []database.ProviderAttributeSearchResult{
		{
			ResourceName: "azurerm_virtual_network",
			ResourceKind: "resource",
			Attribute:    database.ProviderAttribute{Name: "address_space"},
		},
		{
			ResourceName: "azurerm_virtual_network",
			ResourceKind: "data_source",
			Attribute:    database.ProviderAttribute{Name: "name"},
		},
	}

	result := ProviderAttributeSearchCompact(results)

	if !strings.Contains(result, "Attribute matches: 2") {
		t.Error("expected match count")
	}
	if !strings.Contains(result, "- azurerm_virtual_network.address_space [resource]") {
		t.Error("expected first result line")
	}
	if !strings.Contains(result, "- azurerm_virtual_network.name [data_source]") {
		t.Error("expected second result line")
	}
}

func TestEscapePipes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple text", "simple text"},
		{"text|with|pipes", "text\\|with\\|pipes"},
		{"", "-"},
		{"  ", "-"},
		{"  trimmed  ", "trimmed"},
		{"|pipe|start|end|", "\\|pipe\\|start\\|end\\|"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapePipes(tt.input)
			if result != tt.expected {
				t.Errorf("escapePipes(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAttributeFlags(t *testing.T) {
	t.Run("all flags set", func(t *testing.T) {
		attr := database.ProviderAttribute{
			Required:    true,
			Optional:    true,
			Computed:    true,
			ForceNew:    true,
			Sensitive:   true,
			Deprecated:  sql.NullString{Valid: true, String: "Use new_attr"},
			NestedBlock: true,
			MaxItems:    sql.NullInt64{Valid: true, Int64: 5},
			MinItems:    sql.NullInt64{Valid: true, Int64: 1},
		}

		flags := attributeFlags(attr)

		expected := []string{"required", "optional", "computed", "force_new", "sensitive", "deprecated", "nested", "max=5", "min=1"}
		if len(flags) != len(expected) {
			t.Errorf("expected %d flags, got %d: %v", len(expected), len(flags), flags)
		}
		for _, exp := range expected {
			if !slices.Contains(flags, exp) {
				t.Errorf("expected flag %q not found in %v", exp, flags)
			}
		}
	})

	t.Run("no flags", func(t *testing.T) {
		attr := database.ProviderAttribute{}
		flags := attributeFlags(attr)
		if len(flags) != 0 {
			t.Errorf("expected no flags, got %v", flags)
		}
	})
}

func TestAttributeDescription(t *testing.T) {
	t.Run("uses description when present", func(t *testing.T) {
		attr := database.ProviderAttribute{
			Description: sql.NullString{Valid: true, String: "Main description"},
			Deprecated:  sql.NullString{Valid: true, String: "Deprecated notice"},
			ElemSummary: sql.NullString{Valid: true, String: "Elem summary"},
		}
		if got := attributeDescription(attr); got != "Main description" {
			t.Errorf("expected Main description, got %s", got)
		}
	})

	t.Run("falls back to deprecated", func(t *testing.T) {
		attr := database.ProviderAttribute{
			Deprecated:  sql.NullString{Valid: true, String: "Deprecated notice"},
			ElemSummary: sql.NullString{Valid: true, String: "Elem summary"},
		}
		if got := attributeDescription(attr); got != "Deprecated notice" {
			t.Errorf("expected Deprecated notice, got %s", got)
		}
	})

	t.Run("falls back to elem summary", func(t *testing.T) {
		attr := database.ProviderAttribute{
			ElemSummary: sql.NullString{Valid: true, String: "Elem summary"},
		}
		if got := attributeDescription(attr); got != "Elem summary" {
			t.Errorf("expected Elem summary, got %s", got)
		}
	})

	t.Run("falls back to dash", func(t *testing.T) {
		attr := database.ProviderAttribute{}
		if got := attributeDescription(attr); got != "-" {
			t.Errorf("expected -, got %s", got)
		}
	})
}

func TestFormatAttributesSection(t *testing.T) {
	t.Run("empty with filter", func(t *testing.T) {
		opts := SchemaRenderOptions{Filtered: true}
		result := formatAttributesSection(nil, opts)
		if !strings.Contains(result, "No attributes matched the requested filters") {
			t.Error("expected filtered empty message")
		}
	})

	t.Run("empty without filter", func(t *testing.T) {
		opts := SchemaRenderOptions{Filtered: false}
		result := formatAttributesSection(nil, opts)
		if !strings.Contains(result, "No schema attributes were parsed") {
			t.Error("expected non-filtered empty message")
		}
	})

	t.Run("compact mode", func(t *testing.T) {
		attrs := []database.ProviderAttribute{
			{Name: "name", Required: true, Description: sql.NullString{Valid: true, String: "The name"}},
		}
		opts := SchemaRenderOptions{Compact: true}
		result := formatAttributesSection(attrs, opts)

		if !strings.Contains(result, "## Attributes (1)") {
			t.Error("expected attributes header")
		}
		if !strings.Contains(result, "- `name` (required) — The name") {
			t.Error("expected compact format")
		}
		if strings.Contains(result, "| Name |") {
			t.Error("should not have table in compact mode")
		}
	})

	t.Run("table mode", func(t *testing.T) {
		attrs := []database.ProviderAttribute{
			{Name: "name", Required: true, Type: sql.NullString{Valid: true, String: "string"}},
		}
		opts := SchemaRenderOptions{Compact: false}
		result := formatAttributesSection(attrs, opts)

		if !strings.Contains(result, "| Name | Type | Flags | Description |") {
			t.Error("expected table header")
		}
		if !strings.Contains(result, "| name | string | required |") {
			t.Error("expected table row")
		}
	})

	t.Run("derived type", func(t *testing.T) {
		attrs := []database.ProviderAttribute{
			{Name: "attr", Type: sql.NullString{}},
		}
		opts := SchemaRenderOptions{Compact: false}
		result := formatAttributesSection(attrs, opts)

		if !strings.Contains(result, "(derived)") {
			t.Error("expected derived type placeholder")
		}
	})
}

func TestFormatRelationshipNotes(t *testing.T) {
	t.Run("no relationships", func(t *testing.T) {
		attrs := []database.ProviderAttribute{
			{Name: "simple"},
		}
		result := formatRelationshipNotes(attrs)
		if result != "" {
			t.Error("expected empty result for no relationships")
		}
	})

	t.Run("with all relationship types", func(t *testing.T) {
		attrs := []database.ProviderAttribute{
			{
				Name:          "attr1",
				ConflictsWith: sql.NullString{Valid: true, String: "attr2"},
			},
			{
				Name:         "attr3",
				ExactlyOneOf: sql.NullString{Valid: true, String: "attr4"},
			},
			{
				Name:        "nested_block",
				NestedBlock: true,
				ElemSummary: sql.NullString{Valid: true, String: "list of objects"},
			},
		}
		result := formatRelationshipNotes(attrs)

		if !strings.Contains(result, "## Relationship Notes") {
			t.Error("expected relationship notes header")
		}
		if !strings.Contains(result, "**Conflicts**") {
			t.Error("expected conflicts section")
		}
		if !strings.Contains(result, "`attr1` conflicts with `attr2`") {
			t.Error("expected conflict note")
		}
		if !strings.Contains(result, "**Mutually Exclusive**") {
			t.Error("expected mutually exclusive section")
		}
		if !strings.Contains(result, "`attr3` exactly_one_of `attr4`") {
			t.Error("expected exactly one of note")
		}
		if !strings.Contains(result, "**Nested Blocks**") {
			t.Error("expected nested blocks section")
		}
		if !strings.Contains(result, "`nested_block` nested block → list of objects") {
			t.Error("expected nested block note")
		}
	})
}

func TestProviderResourceDetailDataSource(t *testing.T) {
	resource := &database.ProviderResource{
		Name: "azurerm_virtual_network",
		Kind: "data_source",
	}
	attrs := []database.ProviderAttribute{}

	result := ProviderResourceDetail(resource, attrs, SchemaRenderOptions{})

	// Verify structure
	lines := strings.Split(result, "\n")

	// Check header
	if !strings.HasPrefix(lines[0], "# azurerm_virtual_network") {
		t.Errorf("expected header to start with resource name, got: %s", lines[0])
	}

	// Check kind line exists with exact format
	if !slices.Contains(lines, "**Kind:** Data Source") {
		t.Error("expected exact line '**Kind:** Data Source'")
	}

	// Should have attributes section even if empty
	if !strings.Contains(result, "## Attributes (0)") {
		t.Error("expected attributes section with count")
	}
}

func TestProviderResourceDetailWithFilters(t *testing.T) {
	resource := &database.ProviderResource{
		Name: "azurerm_resource",
		Kind: "resource",
	}
	attrs := []database.ProviderAttribute{}

	result := ProviderResourceDetail(resource, attrs, SchemaRenderOptions{
		FilterSummary: "required=true, force_new=true",
		Filtered:      true,
	})

	if !strings.Contains(result, "_Filters applied_: required=true, force_new=true") {
		t.Error("expected filter summary")
	}
}

func TestProviderResourceListWithDeprecation(t *testing.T) {
	resources := []database.ProviderResource{
		{
			Name:               "azurerm_old_resource",
			Kind:               "resource",
			DeprecationMessage: sql.NullString{Valid: true, String: "Use azurerm_new_resource instead"},
		},
	}
	result := ProviderResourceList(resources)

	if !strings.Contains(result, "⚠️ Deprecated: Use azurerm_new_resource instead") {
		t.Error("expected deprecation warning")
	}
}
