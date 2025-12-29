package formatter

import (
	"fmt"
	"strings"
)

func UpdateBehaviorAnalysis(resourceName, attributeName string, canUpdateInPlace, requiresRecreation bool,
	isComputed, isOptional, isRequired bool, explanation, workaround string, hasCustomDiff bool, customDiffSnippet string,
) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Update Behavior: %s.%s\n\n", resourceName, attributeName)

	if canUpdateInPlace {
		text.WriteString("**Can be updated in-place** (no recreation required)\n\n")
	} else {
		text.WriteString("**Requires resource recreation**\n\n")
	}

	text.WriteString("## Attribute Flags\n\n")
	flags := []string{}
	if isRequired {
		flags = append(flags, "Required")
	}
	if isOptional {
		flags = append(flags, "Optional")
	}
	if isComputed {
		flags = append(flags, "Computed")
	}
	if requiresRecreation {
		flags = append(flags, "ForceNew")
	}

	if len(flags) > 0 {
		fmt.Fprintf(&text, "- %s\n\n", strings.Join(flags, ", "))
	}

	if explanation != "" {
		text.WriteString("## Reason\n\n")
		fmt.Fprintf(&text, "%s\n\n", explanation)
	}

	if hasCustomDiff {
		text.WriteString("## CustomizeDiff Logic\n\n")
		text.WriteString("WARNING: This resource has CustomizeDiff logic that may allow conditional in-place updates even for ForceNew attributes.\n\n")
		if customDiffSnippet != "" {
			text.WriteString("```go\n")
			text.WriteString(customDiffSnippet)
			text.WriteString("\n```\n\n")
		}
	}

	if !canUpdateInPlace && workaround != "" {
		text.WriteString("## Migration Path\n\n")
		fmt.Fprintf(&text, "%s\n\n", workaround)
	}

	return text.String()
}

func BreakingChangeExplanation(resourceName, attributeName string, isBreaking bool,
	reason, workaround string, required, optional, computed bool, deprecationNotice string,
) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Breaking Change Analysis: %s.%s\n\n", resourceName, attributeName)

	if isBreaking {
		text.WriteString("**This attribute is marked ForceNew**\n\n")
		text.WriteString("Changing this attribute will trigger resource recreation.\n\n")
	} else {
		text.WriteString("**This attribute is NOT marked ForceNew**\n\n")
		text.WriteString("This attribute can be updated in-place.\n\n")
	}

	text.WriteString("## Attribute Information\n\n")
	fmt.Fprintf(&text, "- **Required**: %t\n", required)
	fmt.Fprintf(&text, "- **Optional**: %t\n", optional)
	fmt.Fprintf(&text, "- **Computed**: %t\n\n", computed)

	if deprecationNotice != "" {
		text.WriteString("**Deprecation Notice**\n\n")
		fmt.Fprintf(&text, "%s\n\n", deprecationNotice)
	}

	if reason != "" {
		text.WriteString("## Why This Causes Recreation\n\n")
		fmt.Fprintf(&text, "%s\n\n", reason)
	}

	if isBreaking && workaround != "" {
		text.WriteString("## Migration Strategy\n\n")
		fmt.Fprintf(&text, "%s\n\n", workaround)
	}

	return text.String()
}

func ValidationSuggestions(resourceName string, totalAttributes, suggestionsCount int, suggestions []ValidationSuggestion) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Validation Analysis: %s\n\n", resourceName)
	fmt.Fprintf(&text, "**Total Attributes**: %d\n", totalAttributes)
	fmt.Fprintf(&text, "**Suggestions**: %d\n\n", suggestionsCount)

	if suggestionsCount == 0 {
		text.WriteString("No validation improvements identified.\n")
		return text.String()
	}

	text.WriteString("## Suggested Improvements\n\n")
	for i, sugg := range suggestions {
		fmt.Fprintf(&text, "### %d. %s\n\n", i+1, sugg.Attribute)
		fmt.Fprintf(&text, "**Issue**: %s\n\n", sugg.Issue)
		fmt.Fprintf(&text, "**Suggestion**: %s\n\n", sugg.Suggestion)
		if sugg.Example != "" {
			text.WriteString("**Example**:\n```go\n")
			text.WriteString(sugg.Example)
			text.WriteString("\n```\n\n")
		}
	}

	return text.String()
}

type ValidationSuggestion struct {
	Attribute  string
	Issue      string
	Suggestion string
	Example    string
}

func AttributeDependencies(resourceName, attributeName string, conflictsWith, exactlyOneOf, atLeastOneOf, requiredWith []string,
	isRequired, isOptional, isComputed, forcesRecreation bool, dependencyVisualization string,
) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Attribute Dependencies: %s.%s\n\n", resourceName, attributeName)

	text.WriteString("## Flags\n\n")
	if isRequired {
		text.WriteString("- **Required**\n")
	}
	if isOptional {
		text.WriteString("- **Optional**\n")
	}
	if isComputed {
		text.WriteString("- **Computed**\n")
	}
	if forcesRecreation {
		text.WriteString("- **ForceNew** - triggers recreation\n")
	}
	text.WriteString("\n")

	hasAnyDeps := len(conflictsWith) > 0 || len(exactlyOneOf) > 0 || len(atLeastOneOf) > 0 || len(requiredWith) > 0

	if !hasAnyDeps {
		text.WriteString("## Dependencies\n\n")
		text.WriteString("No attribute dependencies or constraints defined.\n\n")
	} else {
		if len(conflictsWith) > 0 {
			text.WriteString("## ConflictsWith\n\n")
			text.WriteString("This attribute conflicts with the following attributes (cannot be set together):\n\n")
			for _, attr := range conflictsWith {
				fmt.Fprintf(&text, "- `%s`\n", attr)
			}
			text.WriteString("\n")
		}

		if len(exactlyOneOf) > 0 {
			text.WriteString("## ExactlyOneOf\n\n")
			text.WriteString("Exactly one of the following attributes must be specified:\n\n")
			for _, attr := range exactlyOneOf {
				fmt.Fprintf(&text, "- `%s`\n", attr)
			}
			text.WriteString("\n")
		}

		if len(atLeastOneOf) > 0 {
			text.WriteString("## AtLeastOneOf\n\n")
			text.WriteString("At least one of the following attributes must be specified:\n\n")
			for _, attr := range atLeastOneOf {
				fmt.Fprintf(&text, "- `%s`\n", attr)
			}
			text.WriteString("\n")
		}

		if len(requiredWith) > 0 {
			text.WriteString("## RequiredWith\n\n")
			text.WriteString("If this attribute is set, the following attributes are also required:\n\n")
			for _, attr := range requiredWith {
				fmt.Fprintf(&text, "- `%s`\n", attr)
			}
			text.WriteString("\n")
		}
	}

	if dependencyVisualization != "" {
		text.WriteString("## Dependency Graph\n\n")
		text.WriteString("```\n")
		text.WriteString(dependencyVisualization)
		text.WriteString("\n```\n\n")
	}

	return text.String()
}

func ResourceComparison(resourceA, resourceB string, similarityScore float64,
	totalAttrsA, totalAttrsB, commonCount, uniqueACount, uniqueBCount, forceNewA, forceNewB int,
	commonNames, uniqueANames, uniqueBNames []string, namesTruncated bool,
) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Resource Comparison: %s vs %s\n\n", resourceA, resourceB)

	text.WriteString("## Similarity\n\n")
	fmt.Fprintf(&text, "**Similarity Score**: %.1f%%\n\n", similarityScore*100)

	text.WriteString("## Overview\n\n")
	fmt.Fprintf(&text, "| Metric | %s | %s |\n", resourceA, resourceB)
	text.WriteString("|--------|------------|------------|\n")
	fmt.Fprintf(&text, "| Total Attributes | %d | %d |\n", totalAttrsA, totalAttrsB)
	fmt.Fprintf(&text, "| ForceNew Count | %d | %d |\n", forceNewA, forceNewB)
	fmt.Fprintf(&text, "| Unique Attributes | %d | %d |\n", uniqueACount, uniqueBCount)
	text.WriteString("\n")

	fmt.Fprintf(&text, "**Common Attributes**: %d\n\n", commonCount)

	if len(commonNames) > 0 {
		text.WriteString("### Shared Attributes\n\n")
		for _, name := range commonNames {
			fmt.Fprintf(&text, "- `%s`\n", name)
		}
		text.WriteString("\n")
	}

	if len(uniqueANames) > 0 {
		fmt.Fprintf(&text, "### Unique to %s\n\n", resourceA)
		for _, name := range uniqueANames {
			fmt.Fprintf(&text, "- `%s`\n", name)
		}
		text.WriteString("\n")
	}

	if len(uniqueBNames) > 0 {
		fmt.Fprintf(&text, "### Unique to %s\n\n", resourceB)
		for _, name := range uniqueBNames {
			fmt.Fprintf(&text, "- `%s`\n", name)
		}
		text.WriteString("\n")
	}

	if namesTruncated {
		text.WriteString("_Note: Attribute lists truncated for readability. Use max_names=-1 to see all._\n\n")
	}

	return text.String()
}

type SimilarResource struct {
	Name            string
	SimilarityScore float64
	CommonAttrCount int
	FilePath        string
}

func SimilarResources(targetResource string, threshold float64, matchesFound int, resources []SimilarResource) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Similar Resources to %s\n\n", targetResource)
	fmt.Fprintf(&text, "**Similarity Threshold**: %.0f%%\n", threshold*100)
	fmt.Fprintf(&text, "**Matches Found**: %d\n\n", matchesFound)

	if matchesFound == 0 {
		text.WriteString("No resources found matching the similarity threshold.\n\n")
		text.WriteString("Try lowering the threshold or check if the target resource exists.\n")
		return text.String()
	}

	text.WriteString("## Similar Resources (Ranked by Similarity)\n\n")
	text.WriteString("| Rank | Resource | Similarity | Common Attributes |\n")
	text.WriteString("|------|----------|------------|-------------------|\n")

	for i, res := range resources {
		fmt.Fprintf(&text, "| %d | %s | %.1f%% | %d |\n",
			i+1, res.Name, res.SimilarityScore*100, res.CommonAttrCount)
	}
	text.WriteString("\n")

	return text.String()
}
