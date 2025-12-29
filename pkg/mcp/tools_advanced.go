package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/formatter"
)

func (s *Server) handleAnalyzeUpdateBehavior(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	argsMap, ok := args.(map[string]any)
	if !ok {
		return ErrorResponse("Invalid arguments")
	}

	resourceName, _ := argsMap["resource_name"].(string)
	attributePath, _ := argsMap["attribute_path"].(string)

	if resourceName == "" || attributePath == "" {
		return ErrorResponse("resource_name and attribute_path are required")
	}

	resource, err := s.db.GetProviderResource(resourceName)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Resource not found: %v", err))
	}

	attrs, err := s.db.GetProviderResourceAttributes(resource.ID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to get attributes: %v", err))
	}

	findAttr := func(path string) *database.ProviderAttribute {
		for _, attr := range attrs {
			if attr.Name == path || strings.HasPrefix(path, attr.Name+".") {
				return &attr
			}
		}
		return nil
	}

	targetAttr := findAttr(attributePath)

	if targetAttr == nil {
		if base, found := strings.CutSuffix(attributePath, "_id"); found {
			targetAttr = findAttr(base)
		} else {
			targetAttr = findAttr(attributePath + "_id")
		}
	}

	if targetAttr == nil {
		var suggestions []string
		for _, attr := range attrs {
			tag := "update_in_place"
			if attr.ForceNew {
				tag = "forces_recreation"
			}
			suggestions = append(suggestions, fmt.Sprintf("- %s (%s)", attr.Name, tag))
		}

		text := fmt.Sprintf(
			"# Update Behavior: %s.%s\n\nAttribute '%s' not found in resource schema.\n\nClosest available attributes (ForceNew/in-place):\n%s\n",
			resourceName, attributePath, attributePath, strings.Join(suggestions, "\n"),
		)
		return SuccessResponse(text)
	}

	source, _ := s.db.GetProviderResourceSource(resource.ID)
	hasCustomDiff := source != nil && source.CustomizeDiffSnippet.Valid && source.CustomizeDiffSnippet.String != ""

	customDiffSnippet := ""
	if hasCustomDiff && source.CustomizeDiffSnippet.Valid {
		customDiffSnippet = source.CustomizeDiffSnippet.String
	}

	text := formatter.UpdateBehaviorAnalysis(
		resourceName,
		attributePath,
		!targetAttr.ForceNew,
		targetAttr.ForceNew,
		targetAttr.Computed,
		targetAttr.Optional,
		targetAttr.Required,
		explainWhyBreaking(*targetAttr, resourceName),
		suggestWorkaround(*targetAttr),
		hasCustomDiff,
		customDiffSnippet,
	)

	return SuccessResponse(text)
}

func (s *Server) handleCompareResources(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	argsMap, ok := args.(map[string]any)
	if !ok {
		return map[string]any{"error": "Invalid arguments"}
	}

	resourceA, _ := argsMap["resource_a"].(string)
	resourceB, _ := argsMap["resource_b"].(string)
	maxNames := 30
	if v, ok := argsMap["max_names"].(float64); ok {
		if v < 0 {
			maxNames = 0
		} else if v > 0 {
			maxNames = int(v)
		}
	}

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

	commonTrimmed, commonTruncated := trimStrings(common, maxNames)
	uniqueATrimmed, aTruncated := trimStrings(uniqueA, maxNames)
	uniqueBTrimmed, bTruncated := trimStrings(uniqueB, maxNames)

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

	text := formatter.ResourceComparison(
		resourceA,
		resourceB,
		similarity,
		len(attrsA),
		len(attrsB),
		len(common),
		len(uniqueA),
		len(uniqueB),
		forceNewA,
		forceNewB,
		commonTrimmed,
		uniqueATrimmed,
		uniqueBTrimmed,
		commonTruncated || aTruncated || bTruncated,
	)

	return SuccessResponse(text)
}

func (s *Server) handleFindSimilarResources(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

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

	formatterResources := make([]formatter.SimilarResource, len(similarities))
	for i, sim := range similarities {
		formatterResources[i] = formatter.SimilarResource{
			Name:            sim.Resource.Name,
			SimilarityScore: sim.Score,
			CommonAttrCount: len(sim.CommonAttributes),
			FilePath:        sim.Resource.FilePath.String,
		}
	}

	text := formatter.SimilarResources(
		resourceName,
		threshold,
		len(similarities),
		formatterResources,
	)

	return SuccessResponse(text)
}

func (s *Server) handleExplainBreakingChange(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

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

	deprecationNotice := ""
	if targetAttr.Deprecated.Valid {
		deprecationNotice = targetAttr.Deprecated.String
	}

	text := formatter.BreakingChangeExplanation(
		resourceName,
		attributeName,
		targetAttr.ForceNew,
		explainWhyBreaking(*targetAttr, resourceName),
		suggestWorkaround(*targetAttr),
		targetAttr.Required,
		targetAttr.Optional,
		targetAttr.Computed,
		deprecationNotice,
	)

	return SuccessResponse(text)
}

func (s *Server) handleSuggestValidationImprovements(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

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

	formatterSuggestions := make([]formatter.ValidationSuggestion, len(suggestions))
	for i, sugg := range suggestions {
		formatterSuggestions[i] = formatter.ValidationSuggestion{
			Attribute:  sugg.Attribute,
			Issue:      sugg.Issue,
			Suggestion: sugg.Suggestion,
			Example:    sugg.Example,
		}
	}

	text := formatter.ValidationSuggestions(
		resourceName,
		len(attrs),
		len(suggestions),
		formatterSuggestions,
	)

	return SuccessResponse(text)
}

func (s *Server) handleTraceAttributeDependencies(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

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

	text := formatter.AttributeDependencies(
		resourceName,
		attributeName,
		conflicts,
		exactlyOne,
		atLeastOne,
		requiredWith,
		targetAttr.Required,
		targetAttr.Optional,
		targetAttr.Computed,
		targetAttr.ForceNew,
		buildDependencyGraph(*targetAttr),
	)

	return SuccessResponse(text)
}

func trimStrings(values []string, limit int) ([]string, bool) {
	if limit <= 0 || len(values) <= limit {
		return values, false
	}
	return values[:limit], true
}
