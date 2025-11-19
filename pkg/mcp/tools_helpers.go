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

	if attr.ForceNew {
		reasons = append(reasons, "Marked as ForceNew in provider schema")
	}

	identifierPatterns := []string{"_id", "name", "location", "resource_group"}
	for _, pattern := range identifierPatterns {
		if strings.Contains(strings.ToLower(attr.Name), pattern) {
			reasons = append(reasons, fmt.Sprintf("'%s' is part of resource identifier - cannot be changed after creation", attr.Name))
			break
		}
	}

	infraPatterns := []string{"sku", "tier", "size", "capacity"}
	for _, pattern := range infraPatterns {
		if strings.Contains(strings.ToLower(attr.Name), pattern) {
			reasons = append(reasons, "Represents infrastructure-level decision that cannot be modified")
			break
		}
	}

	if len(reasons) == 0 {
		return "No clear reason found - may be Azure API limitation"
	}

	return strings.Join(reasons, "; ")
}

func suggestWorkaround(attr database.ProviderAttribute) string {
	if strings.Contains(strings.ToLower(attr.Name), "name") {
		return "Use terraform state mv to rename the resource, or create new resource and migrate data"
	}

	if strings.Contains(strings.ToLower(attr.Name), "location") {
		return "Azure doesn't support moving resources between regions - must recreate"
	}

	if strings.Contains(strings.ToLower(attr.Name), "sku") || strings.Contains(strings.ToLower(attr.Name), "tier") {
		return "Some SKU changes may be supported via Azure Portal - check Azure documentation"
	}

	return "Typically requires resource recreation - plan for maintenance window"
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
	graph := fmt.Sprintf("Attribute: %s\n", attr.Name)

	if attr.Required {
		graph += "  [Required]\n"
	}
	if attr.Optional {
		graph += "  [Optional]\n"
	}
	if attr.Computed {
		graph += "  [Computed]\n"
	}
	if attr.ForceNew {
		graph += "  [ForceNew - triggers recreation]\n"
	}

	conflicts := parseConflictsList(attr.ConflictsWith.String)
	if len(conflicts) > 0 {
		graph += "  Conflicts with:\n"
		for _, c := range conflicts {
			graph += fmt.Sprintf("    - %s\n", c)
		}
	}

	exactlyOne := parseConflictsList(attr.ExactlyOneOf.String)
	if len(exactlyOne) > 0 {
		graph += "  Mutually exclusive group (exactly one required):\n"
		for _, e := range exactlyOne {
			graph += fmt.Sprintf("    - %s\n", e)
		}
	}

	atLeastOne := parseConflictsList(attr.AtLeastOneOf.String)
	if len(atLeastOne) > 0 {
		graph += "  At least one required from:\n"
		for _, a := range atLeastOne {
			graph += fmt.Sprintf("    - %s\n", a)
		}
	}

	return graph
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
