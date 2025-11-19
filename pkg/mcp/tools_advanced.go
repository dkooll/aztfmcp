package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dkooll/aztfmcp/internal/database"
)

func (s *Server) handleAnalyzeUpdateBehavior(args any) map[string]any {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return map[string]any{"error": "Invalid arguments"}
	}

	resourceName, _ := argsMap["resource_name"].(string)
	attributePath, _ := argsMap["attribute_path"].(string)

	if resourceName == "" || attributePath == "" {
		return map[string]any{"error": "resource_name and attribute_path are required"}
	}

	resource, err := s.db.GetProviderResource(resourceName)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Resource not found: %v", err)}
	}

	attrs, err := s.db.GetProviderResourceAttributes(resource.ID)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Failed to get attributes: %v", err)}
	}

	var targetAttr *database.ProviderAttribute
	for _, attr := range attrs {
		if attr.Name == attributePath || strings.HasPrefix(attributePath, attr.Name+".") {
			targetAttr = &attr
			break
		}
	}

	if targetAttr == nil {
		return map[string]any{"error": fmt.Sprintf("Attribute '%s' not found in resource", attributePath)}
	}

	source, _ := s.db.GetProviderResourceSource(resource.ID)
	hasCustomDiff := source != nil && source.CustomizeDiffSnippet.Valid && source.CustomizeDiffSnippet.String != ""

	analysis := map[string]any{
		"resource":            resourceName,
		"attribute":           attributePath,
		"can_update_in_place": !targetAttr.ForceNew,
		"requires_recreation": targetAttr.ForceNew,
		"is_computed":         targetAttr.Computed,
		"is_optional":         targetAttr.Optional,
		"is_required":         targetAttr.Required,
		"explanation":         explainWhyBreaking(*targetAttr, resourceName),
		"workaround":          suggestWorkaround(*targetAttr),
		"has_custom_diff":     hasCustomDiff,
	}

	if hasCustomDiff {
		analysis["custom_diff_note"] = "This resource has CustomizeDiff logic that may allow conditional updates"
		analysis["custom_diff_snippet"] = source.CustomizeDiffSnippet.String
	}

	return map[string]any{"result": analysis}
}

func (s *Server) handleCompareResources(args any) map[string]any {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return map[string]any{"error": "Invalid arguments"}
	}

	resourceA, _ := argsMap["resource_a"].(string)
	resourceB, _ := argsMap["resource_b"].(string)

	if resourceA == "" || resourceB == "" {
		return map[string]any{"error": "resource_a and resource_b are required"}
	}

	resA, err := s.db.GetProviderResource(resourceA)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Resource A not found: %v", err)}
	}

	resB, err := s.db.GetProviderResource(resourceB)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Resource B not found: %v", err)}
	}

	attrsA, _ := s.db.GetProviderResourceAttributes(resA.ID)
	attrsB, _ := s.db.GetProviderResourceAttributes(resB.ID)

	common := findCommonAttributes(attrsA, attrsB)
	uniqueA := findUniqueAttributes(attrsA, attrsB)
	uniqueB := findUniqueAttributes(attrsB, attrsA)

	similarity := calculateJaccardSimilarity(attrsA, attrsB)

	forceNewA := 0
	forceNewB := 0
	for _, attr := range attrsA {
		if attr.ForceNew {
			forceNewA++
		}
	}
	for _, attr := range attrsB {
		if attr.ForceNew {
			forceNewB++
		}
	}

	comparison := map[string]any{
		"resource_a":        resourceA,
		"resource_b":        resourceB,
		"similarity_score":  fmt.Sprintf("%.2f%%", similarity*100),
		"total_attrs_a":     len(attrsA),
		"total_attrs_b":     len(attrsB),
		"common_attributes": len(common),
		"unique_to_a":       len(uniqueA),
		"unique_to_b":       len(uniqueB),
		"force_new_count_a": forceNewA,
		"force_new_count_b": forceNewB,
		"common_attr_names": common,
		"unique_to_a_names": uniqueA,
		"unique_to_b_names": uniqueB,
	}

	return map[string]any{"result": comparison}
}

func (s *Server) handleFindSimilarResources(args any) map[string]any {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return map[string]any{"error": "Invalid arguments"}
	}

	resourceName, _ := argsMap["resource_name"].(string)
	threshold := 0.7
	if t, ok := argsMap["similarity_threshold"].(float64); ok {
		threshold = t
	}

	limit := 5
	if l, ok := argsMap["limit"].(float64); ok {
		limit = int(l)
	}

	if resourceName == "" {
		return map[string]any{"error": "resource_name is required"}
	}

	targetResource, err := s.db.GetProviderResource(resourceName)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Resource not found: %v", err)}
	}

	targetAttrs, err := s.db.GetProviderResourceAttributes(targetResource.ID)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Failed to get attributes: %v", err)}
	}

	allResources, err := s.db.ListProviderResources("resource", 0)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Failed to list resources: %v", err)}
	}

	similarities := []SimilarityScore{}

	for _, resource := range allResources {
		if resource.ID == targetResource.ID {
			continue
		}

		attrs, err := s.db.GetProviderResourceAttributes(resource.ID)
		if err != nil {
			continue
		}

		score := calculateJaccardSimilarity(targetAttrs, attrs)

		if score >= threshold {
			similarities = append(similarities, SimilarityScore{
				Resource:         resource,
				Score:            score,
				CommonAttributes: findCommonAttributes(targetAttrs, attrs),
			})
		}
	}

	sort.Slice(similarities, func(i, j int) bool {
		return similarities[i].Score > similarities[j].Score
	})

	if len(similarities) > limit {
		similarities = similarities[:limit]
	}

	results := []map[string]any{}
	for _, sim := range similarities {
		results = append(results, map[string]any{
			"resource_name":      sim.Resource.Name,
			"similarity_score":   fmt.Sprintf("%.2f%%", sim.Score*100),
			"common_attrs_count": len(sim.CommonAttributes),
			"file_path":          sim.Resource.FilePath.String,
		})
	}

	return map[string]any{
		"result": map[string]any{
			"target_resource":   resourceName,
			"threshold":         threshold,
			"matches_found":     len(results),
			"similar_resources": results,
		},
	}
}

func (s *Server) handleExplainBreakingChange(args any) map[string]any {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return map[string]any{"error": "Invalid arguments"}
	}

	resourceName, _ := argsMap["resource_name"].(string)
	attributeName, _ := argsMap["attribute_name"].(string)

	if resourceName == "" || attributeName == "" {
		return map[string]any{"error": "resource_name and attribute_name are required"}
	}

	resource, err := s.db.GetProviderResource(resourceName)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Resource not found: %v", err)}
	}

	attrs, err := s.db.GetProviderResourceAttributes(resource.ID)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Failed to get attributes: %v", err)}
	}

	var targetAttr *database.ProviderAttribute
	for _, attr := range attrs {
		if attr.Name == attributeName {
			targetAttr = &attr
			break
		}
	}

	if targetAttr == nil {
		return map[string]any{"error": fmt.Sprintf("Attribute '%s' not found", attributeName)}
	}

	explanation := map[string]any{
		"resource":    resourceName,
		"attribute":   attributeName,
		"is_breaking": targetAttr.ForceNew,
		"reason":      explainWhyBreaking(*targetAttr, resourceName),
		"workaround":  suggestWorkaround(*targetAttr),
		"required":    targetAttr.Required,
		"optional":    targetAttr.Optional,
		"computed":    targetAttr.Computed,
	}

	if targetAttr.Deprecated.Valid && targetAttr.Deprecated.String != "" {
		explanation["deprecation_notice"] = targetAttr.Deprecated.String
	}

	return map[string]any{"result": explanation}
}

func (s *Server) handleSuggestValidationImprovements(args any) map[string]any {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return map[string]any{"error": "Invalid arguments"}
	}

	resourceName, _ := argsMap["resource_name"].(string)
	if resourceName == "" {
		return map[string]any{"error": "resource_name is required"}
	}

	resource, err := s.db.GetProviderResource(resourceName)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Resource not found: %v", err)}
	}

	attrs, err := s.db.GetProviderResourceAttributes(resource.ID)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Failed to get attributes: %v", err)}
	}

	suggestions := []ValidationSuggestion{}

	for _, attr := range attrs {
		validation := ""
		if attr.Validation.Valid {
			validation = attr.Validation.String
		}

		if isNameField(attr.Name) && validation == "" {
			suggestions = append(suggestions, ValidationSuggestion{
				Attribute:  attr.Name,
				Issue:      "Name field lacks validation",
				Suggestion: "Add regex pattern matching Azure naming rules",
				Example:    `validation.StringMatch(regexp.MustCompile("^[a-zA-Z0-9][-a-zA-Z0-9_]{0,63}$"), "name must start with alphanumeric")`,
			})
		}

		if isPortField(attr.Name) && !containsPortValidation(validation) {
			suggestions = append(suggestions, ValidationSuggestion{
				Attribute:  attr.Name,
				Issue:      "Port field lacks range validation",
				Suggestion: "Add port range validation (1-65535)",
				Example:    `validation.IntBetween(1, 65535)`,
			})
		}

		if isCredentialID(attr.Name) && !strings.Contains(validation, "IsUUID") {
			suggestions = append(suggestions, ValidationSuggestion{
				Attribute:  attr.Name,
				Issue:      "Credential ID field may need UUID validation",
				Suggestion: "Verify if this should be a UUID and add validation",
				Example:    `validation.IsUUID`,
			})
		}

		if attr.Required && !strings.Contains(validation, "StringIsNotEmpty") {
			suggestions = append(suggestions, ValidationSuggestion{
				Attribute:  attr.Name,
				Issue:      "Required field should prevent empty strings",
				Suggestion: "Add StringIsNotEmpty validation",
				Example:    `validation.StringIsNotEmpty`,
			})
		}
	}

	result := map[string]any{
		"resource":          resourceName,
		"total_attributes":  len(attrs),
		"suggestions_count": len(suggestions),
		"suggestions":       []map[string]any{},
	}

	for _, sugg := range suggestions {
		result["suggestions"] = append(result["suggestions"].([]map[string]any), map[string]any{
			"attribute":  sugg.Attribute,
			"issue":      sugg.Issue,
			"suggestion": sugg.Suggestion,
			"example":    sugg.Example,
		})
	}

	return map[string]any{"result": result}
}

func (s *Server) handleTraceAttributeDependencies(args any) map[string]any {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return map[string]any{"error": "Invalid arguments"}
	}

	resourceName, _ := argsMap["resource_name"].(string)
	attributeName, _ := argsMap["attribute_name"].(string)

	if resourceName == "" || attributeName == "" {
		return map[string]any{"error": "resource_name and attribute_name are required"}
	}

	resource, err := s.db.GetProviderResource(resourceName)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Resource not found: %v", err)}
	}

	attrs, err := s.db.GetProviderResourceAttributes(resource.ID)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Failed to get attributes: %v", err)}
	}

	var targetAttr *database.ProviderAttribute
	for _, attr := range attrs {
		if attr.Name == attributeName {
			targetAttr = &attr
			break
		}
	}

	if targetAttr == nil {
		return map[string]any{"error": fmt.Sprintf("Attribute '%s' not found", attributeName)}
	}

	conflicts := []string{}
	if targetAttr.ConflictsWith.Valid {
		conflicts = parseConflictsList(targetAttr.ConflictsWith.String)
	}

	exactlyOne := []string{}
	if targetAttr.ExactlyOneOf.Valid {
		exactlyOne = parseConflictsList(targetAttr.ExactlyOneOf.String)
	}

	atLeastOne := []string{}
	if targetAttr.AtLeastOneOf.Valid {
		atLeastOne = parseConflictsList(targetAttr.AtLeastOneOf.String)
	}

	requiredWith := []string{}
	if targetAttr.RequiredWith.Valid {
		requiredWith = parseConflictsList(targetAttr.RequiredWith.String)
	}

	dependencies := map[string]any{
		"resource":                 resourceName,
		"attribute":                attributeName,
		"conflicts_with":           conflicts,
		"exactly_one_of_group":     exactlyOne,
		"at_least_one_of_group":    atLeastOne,
		"required_with":            requiredWith,
		"is_required":              targetAttr.Required,
		"is_optional":              targetAttr.Optional,
		"is_computed":              targetAttr.Computed,
		"forces_recreation":        targetAttr.ForceNew,
		"dependency_visualization": buildDependencyGraph(*targetAttr),
	}

	return map[string]any{"result": dependencies}
}
