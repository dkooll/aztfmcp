package formatter

import (
	"fmt"
	"strings"

	"github.com/dkooll/aztfmcp/internal/database"
)

type SchemaRenderOptions struct {
	FilterSummary string
	Compact       bool
	Filtered      bool
}

func ProviderResourceList(resources []database.ProviderResource) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# AzureRM Provider Definitions (%d)\n\n", len(resources))

	if len(resources) == 0 {
		text.WriteString("No provider resources indexed. Run sync_provider to load the repository.\n")
		return text.String()
	}

	for _, resource := range resources {
		title := resource.Name
		if resource.DisplayName.Valid {
			title = fmt.Sprintf("%s (%s)", resource.DisplayName.String, resource.Name)
		}
		fmt.Fprintf(&text, "**%s** — %s\n", title, resource.Kind)
		if resource.Description.Valid {
			fmt.Fprintf(&text, "  %s\n", resource.Description.String)
		}
		if resource.FilePath.Valid {
			fmt.Fprintf(&text, "  File: %s\n", resource.FilePath.String)
		}
		if resource.DeprecationMessage.Valid {
			fmt.Fprintf(&text, "  ⚠️ Deprecated: %s\n", resource.DeprecationMessage.String)
		}
		text.WriteString("\n")
	}

	return text.String()
}

func ProviderResourceListCompact(resources []database.ProviderResource) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Resources: %d\n", len(resources))
	for _, resource := range resources {
		kind := resource.Kind
		line := resource.Name
		if resource.FilePath.Valid {
			line = fmt.Sprintf("%s (%s)", resource.Name, resource.FilePath.String)
		}
		fmt.Fprintf(&text, "- %s [%s]\n", line, kind)
	}
	return text.String()
}

func ProviderResourceDetail(resource *database.ProviderResource, attrs []database.ProviderAttribute, opts SchemaRenderOptions) string {
	var text strings.Builder
	title := resource.Name
	if resource.DisplayName.Valid {
		title = fmt.Sprintf("%s (%s)", resource.DisplayName.String, resource.Name)
	}
	fmt.Fprintf(&text, "# %s\n\n", title)
	kindLabel := "Resource"
	if resource.Kind == "data_source" {
		kindLabel = "Data Source"
	}
	fmt.Fprintf(&text, "**Kind:** %s\n", kindLabel)
	if resource.FilePath.Valid {
		fmt.Fprintf(&text, "**File:** %s\n", resource.FilePath.String)
	}
	if resource.Description.Valid {
		fmt.Fprintf(&text, "**Description:** %s\n", resource.Description.String)
	}
	if resource.DeprecationMessage.Valid {
		fmt.Fprintf(&text, "**Deprecation:** %s\n", resource.DeprecationMessage.String)
	}
	text.WriteString("\n")

	if resource.BreakingChanges.Valid && resource.BreakingChanges.String != "" {
		text.WriteString("## Breaking & Conflicting Properties\n\n")
		text.WriteString(resource.BreakingChanges.String)
		text.WriteString("\n\n")
	}

	if opts.FilterSummary != "" {
		fmt.Fprintf(&text, "_Filters applied_: %s\n\n", opts.FilterSummary)
	}

	text.WriteString(formatAttributesSection(attrs, opts))
	text.WriteString(formatRelationshipNotes(attrs))
	return text.String()
}

func formatAttributesSection(attrs []database.ProviderAttribute, opts SchemaRenderOptions) string {
	var text strings.Builder
	fmt.Fprintf(&text, "## Attributes (%d)\n\n", len(attrs))

	if len(attrs) == 0 {
		if opts.Filtered {
			text.WriteString("No attributes matched the requested filters.\n")
		} else {
			text.WriteString("No schema attributes were parsed for this resource.\n")
		}
		return text.String()
	}

	if opts.Compact {
		for _, attr := range attrs {
			desc := attributeDescription(attr)
			flags := strings.Join(attributeFlags(attr), ", ")
			if flags == "" {
				flags = "-"
			}
			fmt.Fprintf(&text, "- `%s` (%s) — %s\n", attr.Name, flags, desc)
		}
		text.WriteString("\n")
		return text.String()
	}

	text.WriteString("| Name | Type | Flags | Description |\n")
	text.WriteString("|------|------|-------|-------------|\n")
	for _, attr := range attrs {
		typeLabel := attr.Type.String
		if typeLabel == "" {
			typeLabel = "(derived)"
		}
		flags := strings.Join(attributeFlags(attr), ", ")
		if flags == "" {
			flags = "-"
		}
		desc := attributeDescription(attr)
		fmt.Fprintf(&text, "| %s | %s | %s | %s |\n",
			attr.Name,
			escapePipes(typeLabel),
			escapePipes(flags),
			escapePipes(desc),
		)
	}
	text.WriteString("\n")
	return text.String()
}

func formatRelationshipNotes(attrs []database.ProviderAttribute) string {
	var conflicts []string
	var exclusives []string
	var nested []string

	for _, attr := range attrs {
		if attr.ConflictsWith.Valid {
			conflicts = append(conflicts, fmt.Sprintf("- `%s` conflicts with `%s`", attr.Name, attr.ConflictsWith.String))
		}
		if attr.ExactlyOneOf.Valid {
			exclusives = append(exclusives, fmt.Sprintf("- `%s` exactly_one_of `%s`", attr.Name, attr.ExactlyOneOf.String))
		}
		if attr.NestedBlock {
			nested = append(nested, fmt.Sprintf("- `%s` nested block → %s", attr.Name, attr.ElemSummary.String))
		}
	}

	if len(conflicts) == 0 && len(exclusives) == 0 && len(nested) == 0 {
		return ""
	}

	var text strings.Builder
	text.WriteString("## Relationship Notes\n\n")
	if len(conflicts) > 0 {
		text.WriteString("**Conflicts**\n")
		text.WriteString(strings.Join(conflicts, "\n"))
		text.WriteString("\n\n")
	}
	if len(exclusives) > 0 {
		text.WriteString("**Mutually Exclusive**\n")
		text.WriteString(strings.Join(exclusives, "\n"))
		text.WriteString("\n\n")
	}
	if len(nested) > 0 {
		text.WriteString("**Nested Blocks**\n")
		text.WriteString(strings.Join(nested, "\n"))
		text.WriteString("\n")
	}
	return text.String()
}

func attributeFlags(attr database.ProviderAttribute) []string {
	var flags []string
	if attr.Required {
		flags = append(flags, "required")
	}
	if attr.Optional {
		flags = append(flags, "optional")
	}
	if attr.Computed {
		flags = append(flags, "computed")
	}
	if attr.ForceNew {
		flags = append(flags, "force_new")
	}
	if attr.Sensitive {
		flags = append(flags, "sensitive")
	}
	if attr.Deprecated.Valid {
		flags = append(flags, "deprecated")
	}
	if attr.NestedBlock {
		flags = append(flags, "nested")
	}
	if attr.MaxItems.Valid {
		flags = append(flags, fmt.Sprintf("max=%d", attr.MaxItems.Int64))
	}
	if attr.MinItems.Valid {
		flags = append(flags, fmt.Sprintf("min=%d", attr.MinItems.Int64))
	}
	return flags
}

func attributeDescription(attr database.ProviderAttribute) string {
	desc := attr.Description.String
	if desc == "" {
		desc = attr.Deprecated.String
	}
	if desc == "" {
		desc = attr.ElemSummary.String
	}
	if desc == "" {
		desc = "-"
	}
	return desc
}

func escapePipes(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return strings.ReplaceAll(value, "|", "\\|")
}

func ProviderAttributeSearch(results []database.ProviderAttributeSearchResult) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Attribute Search (%d matches)\n\n", len(results))

	if len(results) == 0 {
		text.WriteString("No provider attributes matched the supplied filters.\n")
		return text.String()
	}

	text.WriteString("| Resource | Attribute | Flags | Notes |\n")
	text.WriteString("|----------|-----------|-------|-------|\n")
	for _, res := range results {
		resourceLabel := fmt.Sprintf("%s (%s)", res.ResourceName, res.ResourceKind)
		flags := strings.Join(attributeFlags(res.Attribute), ", ")
		if flags == "" {
			flags = "-"
		}
		notes := attributeDescription(res.Attribute)
		if res.Attribute.ConflictsWith.Valid && res.Attribute.ConflictsWith.String != "" {
			notes = fmt.Sprintf("%s — conflicts: %s", notes, res.Attribute.ConflictsWith.String)
		}
		if res.Attribute.Validation.Valid && res.Attribute.Validation.String != "" {
			notes = fmt.Sprintf("%s — validation: %s", notes, res.Attribute.Validation.String)
		}
		if res.Attribute.DiffSuppress.Valid && res.Attribute.DiffSuppress.String != "" {
			notes = fmt.Sprintf("%s — diff suppress: %s", notes, res.Attribute.DiffSuppress.String)
		}
		if res.ResourceFilePath.Valid {
			notes = fmt.Sprintf("%s — %s", notes, res.ResourceFilePath.String)
		}
		fmt.Fprintf(&text, "| %s | `%s` | %s | %s |\n",
			resourceLabel,
			res.Attribute.Name,
			escapePipes(flags),
			escapePipes(notes),
		)
	}

	text.WriteString("\n")
	return text.String()
}

func ProviderAttributeSearchCompact(results []database.ProviderAttributeSearchResult) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Attribute matches: %d\n", len(results))
	for _, res := range results {
		fmt.Fprintf(&text, "- %s.%s [%s]\n", res.ResourceName, res.Attribute.Name, res.ResourceKind)
	}
	return text.String()
}

func ProviderSchemaSource(resourceName, section, filePath, functionName, snippet string, truncated bool) string {
	var text strings.Builder
	sectionTitle := strings.TrimSpace(section)
	if sectionTitle == "" {
		sectionTitle = "Schema"
	} else {
		sectionTitle = strings.ToUpper(sectionTitle[:1]) + sectionTitle[1:]
	}

	fmt.Fprintf(&text, "# %s %s Source\n\n", resourceName, sectionTitle)
	if filePath != "" {
		fmt.Fprintf(&text, "**File:** %s\n", filePath)
	}
	if functionName != "" {
		fmt.Fprintf(&text, "**Function:** %s\n", functionName)
	}
	fmt.Fprintf(&text, "**Section:** %s\n\n", sectionTitle)

	if strings.TrimSpace(snippet) == "" {
		text.WriteString("Snippet not available. Run `get_file_content` to inspect the file directly.\n")
		return text.String()
	}

	text.WriteString("```go\n")
	text.WriteString(snippet)
	text.WriteString("\n```\n")
	if truncated {
		text.WriteString("_Note: snippet trimmed for brevity._\n")
	}
	return text.String()
}
