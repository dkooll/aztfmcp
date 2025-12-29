package mcp

import (
	"fmt"
	"strings"

	"github.com/dkooll/aztfmcp/internal/database"
)

func calculateJaccardSimilarity(attrsA, attrsB []database.ProviderAttribute) float64 {
	namesA := make(map[string]bool)
	namesB := make(map[string]bool)

	for _, attr := range attrsA {
		namesA[attr.Name] = true
	}
	for _, attr := range attrsB {
		namesB[attr.Name] = true
	}

	intersection := 0
	for name := range namesA {
		if namesB[name] {
			intersection++
		}
	}

	union := len(namesA) + len(namesB) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func findCommonAttributes(attrsA, attrsB []database.ProviderAttribute) []string {
	namesA := make(map[string]bool)
	common := []string{}

	for _, attr := range attrsA {
		namesA[attr.Name] = true
	}

	for _, attr := range attrsB {
		if namesA[attr.Name] {
			common = append(common, attr.Name)
		}
	}

	return common
}

func findUniqueAttributes(attrsA, attrsB []database.ProviderAttribute) []string {
	namesB := make(map[string]bool)
	unique := []string{}

	for _, attr := range attrsB {
		namesB[attr.Name] = true
	}

	for _, attr := range attrsA {
		if !namesB[attr.Name] {
			unique = append(unique, attr.Name)
		}
	}

	return unique
}

func explainWhyBreaking(attr database.ProviderAttribute, _ string) string {
	reasons := []string{}
	nameLower := strings.ToLower(attr.Name)

	if attr.ForceNew {
		reasons = append(reasons, "Marked as ForceNew in provider schema")
	}

	if attr.Computed {
		reasons = append(reasons, "Computed by Azure; changes require recreation to pick up a new value")
	}

	if attr.ConflictsWith.Valid && strings.TrimSpace(attr.ConflictsWith.String) != "" {
		reasons = append(reasons, fmt.Sprintf("Part of a mutually exclusive set (%s); replacements avoid invalid combinations", attr.ConflictsWith.String))
	}

	if attr.ExactlyOneOf.Valid && strings.TrimSpace(attr.ExactlyOneOf.String) != "" {
		reasons = append(reasons, fmt.Sprintf("ExactlyOneOf group (%s) prevents in-place switches", attr.ExactlyOneOf.String))
	}

	if attr.AtLeastOneOf.Valid && strings.TrimSpace(attr.AtLeastOneOf.String) != "" {
		reasons = append(reasons, fmt.Sprintf("AtLeastOneOf group (%s) suggests identity/shape coupling", attr.AtLeastOneOf.String))
	}

	identifierPatterns := []string{"_id", "name", "location", "resource_group", "subscription_id"}
	for _, pattern := range identifierPatterns {
		if strings.Contains(nameLower, pattern) {
			reasons = append(reasons, fmt.Sprintf("'%s' is part of the resource identity; Azure requires recreate to change it", attr.Name))
			break
		}
	}

	infraPatterns := []string{"sku", "tier", "size", "capacity", "kind"}
	for _, pattern := range infraPatterns {
		if strings.Contains(nameLower, pattern) {
			reasons = append(reasons, "Represents an infrastructure/SKU choice Azure treats as immutable")
			break
		}
	}

	if attr.Validation.Valid {
		val := attr.Validation.String
		if strings.Contains(val, "StringInSlice") || strings.Contains(val, "StringMatch") {
			reasons = append(reasons, "Validated against a closed set/pattern; typically immutable once created")
		}
	}

	if attr.NestedBlock {
		reasons = append(reasons, "Nested block may contain ForceNew children; parent is ForceNew to maintain state consistency")
	}

	if len(reasons) == 0 {
		return "No clear reason found - may be Azure API limitation"
	}

	return strings.Join(reasons, "; ")
}

func suggestWorkaround(attr database.ProviderAttribute) string {
	nameLower := strings.ToLower(attr.Name)

	if strings.Contains(nameLower, "name") || strings.Contains(nameLower, "_id") || strings.Contains(nameLower, "resource_group") {
		return "Plan recreate; if preserving state, import/mv into a new resource with the desired identity"
	}

	if strings.Contains(nameLower, "location") {
		return "Region moves require recreate; stand up in target region and migrate workload/data"
	}

	if strings.Contains(nameLower, "sku") || strings.Contains(nameLower, "tier") || strings.Contains(nameLower, "capacity") {
		return "Azure often requires new resources for SKU/tier changes; create parallel resource and cut over"
	}

	if attr.NestedBlock {
		return "Adjust nested block in a new resource; consider splitting nested blocks into separate resources if available"
	}

	if attr.Computed {
		return "Computed fields reflect Azure output; recreate or force-refresh after applying dependent changes"
	}

	return "Typically requires recreate; schedule downtime or blue/green cutover"
}

func parseConflictsList(conflictsStr string) []string {
	if conflictsStr == "" {
		return []string{}
	}

	conflicts := strings.Split(conflictsStr, ",")
	result := []string{}
	for _, c := range conflicts {
		trimmed := strings.TrimSpace(c)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func isNameField(attrName string) bool {
	nameLower := strings.ToLower(attrName)
	return strings.HasSuffix(nameLower, "name") || nameLower == "name"
}

func isPortField(attrName string) bool {
	nameLower := strings.ToLower(attrName)
	return strings.Contains(nameLower, "port")
}

func isCredentialID(attrName string) bool {
	nameLower := strings.ToLower(attrName)
	credPatterns := []string{"client_id", "tenant_id", "subscription_id", "application_id"}
	for _, pattern := range credPatterns {
		if strings.Contains(nameLower, pattern) {
			return true
		}
	}
	return false
}

func containsPortValidation(validation string) bool {
	return strings.Contains(validation, "IntBetween") &&
		(strings.Contains(validation, "65535") || strings.Contains(validation, "port"))
}

func buildDependencyGraph(attr database.ProviderAttribute) string {
	var graph strings.Builder
	fmt.Fprintf(&graph, "Attribute: %s\n", attr.Name)

	if attr.Required {
		graph.WriteString("  [Required]\n")
	}
	if attr.Optional {
		graph.WriteString("  [Optional]\n")
	}
	if attr.Computed {
		graph.WriteString("  [Computed]\n")
	}
	if attr.ForceNew {
		graph.WriteString("  [ForceNew - triggers recreation]\n")
	}

	conflicts := parseConflictsList(attr.ConflictsWith.String)
	if len(conflicts) > 0 {
		graph.WriteString("  Conflicts with:\n")
		for _, c := range conflicts {
			fmt.Fprintf(&graph, "    - %s\n", c)
		}
	}

	exactlyOne := parseConflictsList(attr.ExactlyOneOf.String)
	if len(exactlyOne) > 0 {
		graph.WriteString("  Mutually exclusive group (exactly one required):\n")
		for _, e := range exactlyOne {
			fmt.Fprintf(&graph, "    - %s\n", e)
		}
	}

	atLeastOne := parseConflictsList(attr.AtLeastOneOf.String)
	if len(atLeastOne) > 0 {
		graph.WriteString("  At least one required from:\n")
		for _, a := range atLeastOne {
			fmt.Fprintf(&graph, "    - %s\n", a)
		}
	}

	return graph.String()
}

type SimilarityScore struct {
	Resource         database.ProviderResource
	Score            float64
	CommonAttributes []string
}

type ValidationSuggestion struct {
	Attribute  string
	Issue      string
	Suggestion string
	Example    string
}
