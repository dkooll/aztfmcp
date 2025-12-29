package formatter

import (
	"strings"
	"testing"
)

func TestUpdateBehaviorAnalysis(t *testing.T) {
	t.Run("can update in place", func(t *testing.T) {
		result := UpdateBehaviorAnalysis(
			"azurerm_virtual_network", "address_space",
			true, false,
			false, true, false,
			"The address space can be modified without recreation.",
			"",
			false, "",
		)

		if !strings.Contains(result, "# Update Behavior: azurerm_virtual_network.address_space") {
			t.Error("expected header with resource and attribute name")
		}
		if !strings.Contains(result, "**Can be updated in-place**") {
			t.Error("expected in-place update message")
		}
		if !strings.Contains(result, "Optional") {
			t.Error("expected Optional flag")
		}
		if !strings.Contains(result, "The address space can be modified") {
			t.Error("expected explanation")
		}
	})

	t.Run("requires recreation", func(t *testing.T) {
		result := UpdateBehaviorAnalysis(
			"azurerm_storage_account", "name",
			false, true,
			false, false, true,
			"The storage account name cannot be changed.",
			"Use terraform import to import an existing storage account with the new name.",
			false, "",
		)

		if !strings.Contains(result, "**Requires resource recreation**") {
			t.Error("expected recreation message")
		}
		if !strings.Contains(result, "Required") {
			t.Error("expected Required flag")
		}
		if !strings.Contains(result, "ForceNew") {
			t.Error("expected ForceNew flag")
		}
		if !strings.Contains(result, "## Migration Path") {
			t.Error("expected migration path section")
		}
		if !strings.Contains(result, "terraform import") {
			t.Error("expected workaround content")
		}
	})

	t.Run("with custom diff", func(t *testing.T) {
		result := UpdateBehaviorAnalysis(
			"azurerm_kubernetes_cluster", "node_pool",
			false, true,
			true, true, false,
			"",
			"",
			true, "customizeDiff: diffForceNewWhen",
		)

		if !strings.Contains(result, "## CustomizeDiff Logic") {
			t.Error("expected CustomizeDiff section")
		}
		if !strings.Contains(result, "WARNING:") {
			t.Error("expected warning about CustomizeDiff")
		}
		if !strings.Contains(result, "```go") {
			t.Error("expected code block")
		}
		if !strings.Contains(result, "diffForceNewWhen") {
			t.Error("expected custom diff snippet")
		}
	})

	t.Run("all flags set", func(t *testing.T) {
		result := UpdateBehaviorAnalysis(
			"azurerm_resource", "attr",
			false, true,
			true, true, true,
			"", "", false, "",
		)

		if !strings.Contains(result, "Required") {
			t.Error("expected Required flag")
		}
		if !strings.Contains(result, "Optional") {
			t.Error("expected Optional flag")
		}
		if !strings.Contains(result, "Computed") {
			t.Error("expected Computed flag")
		}
		if !strings.Contains(result, "ForceNew") {
			t.Error("expected ForceNew flag")
		}
	})
}

func TestBreakingChangeExplanation(t *testing.T) {
	t.Run("breaking attribute", func(t *testing.T) {
		result := BreakingChangeExplanation(
			"azurerm_storage_account", "account_tier",
			true,
			"The account tier cannot be changed on an existing storage account.",
			"Create a new storage account and migrate data.",
			true, false, false,
			"",
		)

		if !strings.Contains(result, "# Breaking Change Analysis: azurerm_storage_account.account_tier") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "**This attribute is marked ForceNew**") {
			t.Error("expected ForceNew message")
		}
		if !strings.Contains(result, "## Why This Causes Recreation") {
			t.Error("expected reason section")
		}
		if !strings.Contains(result, "## Migration Strategy") {
			t.Error("expected migration section")
		}
		if !strings.Contains(result, "**Required**: true") {
			t.Error("expected Required: true")
		}
	})

	t.Run("non-breaking attribute", func(t *testing.T) {
		result := BreakingChangeExplanation(
			"azurerm_virtual_network", "tags",
			false,
			"",
			"",
			false, true, false,
			"",
		)

		if !strings.Contains(result, "**This attribute is NOT marked ForceNew**") {
			t.Error("expected NOT ForceNew message")
		}
		if !strings.Contains(result, "can be updated in-place") {
			t.Error("expected in-place update message")
		}
		if strings.Contains(result, "## Migration Strategy") {
			t.Error("should not have migration section for non-breaking")
		}
	})

	t.Run("with deprecation notice", func(t *testing.T) {
		result := BreakingChangeExplanation(
			"azurerm_resource", "old_attr",
			false,
			"",
			"",
			false, true, false,
			"This attribute is deprecated. Use new_attr instead.",
		)

		if !strings.Contains(result, "**Deprecation Notice**") {
			t.Error("expected deprecation section")
		}
		if !strings.Contains(result, "Use new_attr instead") {
			t.Error("expected deprecation content")
		}
	})
}

func TestValidationSuggestions(t *testing.T) {
	t.Run("no suggestions", func(t *testing.T) {
		result := ValidationSuggestions("azurerm_storage_account", 50, 0, nil)

		if !strings.Contains(result, "# Validation Analysis: azurerm_storage_account") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "**Total Attributes**: 50") {
			t.Error("expected total attributes")
		}
		if !strings.Contains(result, "**Suggestions**: 0") {
			t.Error("expected suggestions count")
		}
		if !strings.Contains(result, "No validation improvements identified") {
			t.Error("expected no improvements message")
		}
	})

	t.Run("with suggestions", func(t *testing.T) {
		suggestions := []ValidationSuggestion{
			{
				Attribute:  "name",
				Issue:      "No length validation",
				Suggestion: "Add StringLenBetween(3, 24)",
				Example:    `validation.StringLenBetween(3, 24)`,
			},
			{
				Attribute:  "sku",
				Issue:      "No enum validation",
				Suggestion: "Add StringInSlice validation",
				Example:    "",
			},
		}

		result := ValidationSuggestions("azurerm_storage_account", 50, 2, suggestions)

		if !strings.Contains(result, "## Suggested Improvements") {
			t.Error("expected improvements section")
		}
		if !strings.Contains(result, "### 1. name") {
			t.Error("expected first suggestion header")
		}
		if !strings.Contains(result, "**Issue**: No length validation") {
			t.Error("expected issue text")
		}
		if !strings.Contains(result, "**Suggestion**: Add StringLenBetween") {
			t.Error("expected suggestion text")
		}
		if !strings.Contains(result, "**Example**:") {
			t.Error("expected example section")
		}
		if !strings.Contains(result, "```go") {
			t.Error("expected code block for example")
		}
		if !strings.Contains(result, "### 2. sku") {
			t.Error("expected second suggestion header")
		}
	})
}

func TestAttributeDependencies(t *testing.T) {
	t.Run("no dependencies", func(t *testing.T) {
		result := AttributeDependencies(
			"azurerm_resource", "attr",
			nil, nil, nil, nil,
			false, true, false, false,
			"",
		)

		if !strings.Contains(result, "# Attribute Dependencies: azurerm_resource.attr") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "- **Optional**") {
			t.Error("expected Optional flag")
		}
		if !strings.Contains(result, "No attribute dependencies or constraints defined") {
			t.Error("expected no dependencies message")
		}
	})

	t.Run("with all dependency types", func(t *testing.T) {
		result := AttributeDependencies(
			"azurerm_resource", "attr",
			[]string{"other_attr"},
			[]string{"option_a", "option_b"},
			[]string{"choice_1", "choice_2"},
			[]string{"required_peer"},
			true, false, false, true,
			"attr -> required_peer",
		)

		if !strings.Contains(result, "- **Required**") {
			t.Error("expected Required flag")
		}
		if !strings.Contains(result, "- **ForceNew**") {
			t.Error("expected ForceNew flag")
		}
		if !strings.Contains(result, "## ConflictsWith") {
			t.Error("expected ConflictsWith section")
		}
		if !strings.Contains(result, "`other_attr`") {
			t.Error("expected conflicting attribute")
		}
		if !strings.Contains(result, "## ExactlyOneOf") {
			t.Error("expected ExactlyOneOf section")
		}
		if !strings.Contains(result, "`option_a`") {
			t.Error("expected ExactlyOneOf attribute")
		}
		if !strings.Contains(result, "## AtLeastOneOf") {
			t.Error("expected AtLeastOneOf section")
		}
		if !strings.Contains(result, "`choice_1`") {
			t.Error("expected AtLeastOneOf attribute")
		}
		if !strings.Contains(result, "## RequiredWith") {
			t.Error("expected RequiredWith section")
		}
		if !strings.Contains(result, "`required_peer`") {
			t.Error("expected RequiredWith attribute")
		}
		if !strings.Contains(result, "## Dependency Graph") {
			t.Error("expected dependency graph section")
		}
		if !strings.Contains(result, "attr -> required_peer") {
			t.Error("expected dependency visualization")
		}
	})

	t.Run("computed and optional", func(t *testing.T) {
		result := AttributeDependencies(
			"azurerm_resource", "id",
			nil, nil, nil, nil,
			false, true, true, false,
			"",
		)

		if !strings.Contains(result, "- **Optional**") {
			t.Error("expected Optional flag")
		}
		if !strings.Contains(result, "- **Computed**") {
			t.Error("expected Computed flag")
		}
	})
}

func TestResourceComparison(t *testing.T) {
	t.Run("similar resources", func(t *testing.T) {
		result := ResourceComparison(
			"azurerm_storage_account", "azurerm_storage_account_blob_container",
			0.75,
			50, 30, 20, 30, 10, 5, 3,
			[]string{"name", "resource_group_name", "location"},
			[]string{"account_tier", "account_replication_type"},
			[]string{"container_access_type"},
			false,
		)

		if !strings.Contains(result, "# Resource Comparison: azurerm_storage_account vs azurerm_storage_account_blob_container") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "**Similarity Score**: 75.0%") {
			t.Error("expected similarity score")
		}
		if !strings.Contains(result, "| Total Attributes | 50 | 30 |") {
			t.Error("expected total attributes row")
		}
		if !strings.Contains(result, "| ForceNew Count | 5 | 3 |") {
			t.Error("expected ForceNew count row")
		}
		if !strings.Contains(result, "**Common Attributes**: 20") {
			t.Error("expected common attributes count")
		}
		if !strings.Contains(result, "### Shared Attributes") {
			t.Error("expected shared attributes section")
		}
		if !strings.Contains(result, "`name`") {
			t.Error("expected common attribute")
		}
		if !strings.Contains(result, "### Unique to azurerm_storage_account") {
			t.Error("expected unique to A section")
		}
		if !strings.Contains(result, "`account_tier`") {
			t.Error("expected unique A attribute")
		}
		if !strings.Contains(result, "### Unique to azurerm_storage_account_blob_container") {
			t.Error("expected unique to B section")
		}
		if !strings.Contains(result, "`container_access_type`") {
			t.Error("expected unique B attribute")
		}
	})

	t.Run("truncated lists", func(t *testing.T) {
		result := ResourceComparison(
			"azurerm_a", "azurerm_b",
			0.5,
			100, 100, 50, 50, 50, 10, 10,
			[]string{"attr1", "attr2"},
			[]string{"attr3"},
			[]string{"attr4"},
			true,
		)

		if !strings.Contains(result, "_Note: Attribute lists truncated") {
			t.Error("expected truncation note")
		}
		if !strings.Contains(result, "max_names=-1") {
			t.Error("expected max_names hint")
		}
	})

	t.Run("empty unique lists", func(t *testing.T) {
		result := ResourceComparison(
			"azurerm_a", "azurerm_b",
			1.0,
			10, 10, 10, 0, 0, 2, 2,
			[]string{"name", "location"},
			nil,
			nil,
			false,
		)

		if strings.Contains(result, "### Unique to azurerm_a") {
			t.Error("should not have unique to A section when empty")
		}
		if strings.Contains(result, "### Unique to azurerm_b") {
			t.Error("should not have unique to B section when empty")
		}
	})
}

func TestSimilarResources(t *testing.T) {
	t.Run("no matches", func(t *testing.T) {
		result := SimilarResources("azurerm_nonexistent", 0.9, 0, nil)

		if !strings.Contains(result, "# Similar Resources to azurerm_nonexistent") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "**Similarity Threshold**: 90%") {
			t.Error("expected threshold")
		}
		if !strings.Contains(result, "**Matches Found**: 0") {
			t.Error("expected match count")
		}
		if !strings.Contains(result, "No resources found matching the similarity threshold") {
			t.Error("expected no matches message")
		}
		if !strings.Contains(result, "Try lowering the threshold") {
			t.Error("expected threshold hint")
		}
	})

	t.Run("with matches", func(t *testing.T) {
		resources := []SimilarResource{
			{Name: "azurerm_storage_account_blob", SimilarityScore: 0.85, CommonAttrCount: 40},
			{Name: "azurerm_storage_account_queue", SimilarityScore: 0.80, CommonAttrCount: 35},
			{Name: "azurerm_storage_account_table", SimilarityScore: 0.75, CommonAttrCount: 30},
		}

		result := SimilarResources("azurerm_storage_account", 0.7, 3, resources)

		if !strings.Contains(result, "## Similar Resources (Ranked by Similarity)") {
			t.Error("expected similar resources section")
		}
		if !strings.Contains(result, "| Rank | Resource | Similarity | Common Attributes |") {
			t.Error("expected table header")
		}
		if !strings.Contains(result, "| 1 | azurerm_storage_account_blob | 85.0% | 40 |") {
			t.Error("expected first resource row")
		}
		if !strings.Contains(result, "| 2 | azurerm_storage_account_queue | 80.0% | 35 |") {
			t.Error("expected second resource row")
		}
		if !strings.Contains(result, "| 3 | azurerm_storage_account_table | 75.0% | 30 |") {
			t.Error("expected third resource row")
		}
	})
}
