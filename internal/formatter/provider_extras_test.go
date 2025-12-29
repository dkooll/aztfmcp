package formatter

import (
	"strings"
	"testing"
)

func TestResourceDocs(t *testing.T) {
	t.Run("resource with section found", func(t *testing.T) {
		result := ResourceDocs(
			"azurerm_virtual_network",
			"resource",
			"website/docs/r/virtual_network.html.markdown",
			"Example Usage",
			true,
			"```hcl\nresource \"azurerm_virtual_network\" \"example\" {}\n```",
		)

		if !strings.Contains(result, "# Documentation: azurerm_virtual_network") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "**Kind:** Resource") {
			t.Error("expected resource kind")
		}
		if !strings.Contains(result, "**Source:** website/docs/r/virtual_network.html.markdown") {
			t.Error("expected source path")
		}
		if !strings.Contains(result, "**Section:** Example Usage") {
			t.Error("expected section header")
		}
		if strings.Contains(result, "not found") {
			t.Error("should not have 'not found' when section was found")
		}
		if !strings.Contains(result, "azurerm_virtual_network") {
			t.Error("expected content")
		}
	})

	t.Run("data source with section not found", func(t *testing.T) {
		result := ResourceDocs(
			"azurerm_resource_group",
			"data_source",
			"website/docs/d/resource_group.html.markdown",
			"Arguments Reference",
			false,
			"Content here...",
		)

		if !strings.Contains(result, "**Kind:** Data Source") {
			t.Error("expected data source kind")
		}
		if !strings.Contains(result, "(not found, showing closest match)") {
			t.Error("expected not found message")
		}
	})

	t.Run("no section specified", func(t *testing.T) {
		result := ResourceDocs(
			"azurerm_resource",
			"resource",
			"",
			"",
			false,
			"Full content",
		)

		if strings.Contains(result, "**Section:**") {
			t.Error("should not have section line when not specified")
		}
		if strings.Contains(result, "**Source:**") {
			t.Error("should not have source line when not specified")
		}
	})
}

func TestResourceTestOverview(t *testing.T) {
	t.Run("no tests found", func(t *testing.T) {
		result := ResourceTestOverview("azurerm_unknown_resource", "resource", nil)

		if !strings.Contains(result, "# Acceptance Tests for azurerm_unknown_resource (Resource)") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "No acceptance tests were discovered") {
			t.Error("expected no tests message")
		}
	})

	t.Run("data source with tests", func(t *testing.T) {
		files := []ResourceTestFile{
			{
				FilePath: "internal/services/network/network_test.go",
				Tests:    []string{"TestAccVirtualNetwork_basic", "TestAccVirtualNetwork_update"},
			},
			{
				FilePath: "internal/services/network/network_datasource_test.go",
				Tests:    []string{"TestAccDataSourceVirtualNetwork_basic"},
			},
		}

		result := ResourceTestOverview("azurerm_virtual_network", "data_source", files)

		if !strings.Contains(result, "(Data Source)") {
			t.Error("expected data source label")
		}
		if !strings.Contains(result, "Discovered 2 test file(s) with 3 test case(s)") {
			t.Error("expected file and test counts")
		}
		if !strings.Contains(result, "## internal/services/network/network_test.go") {
			t.Error("expected first file header")
		}
		if !strings.Contains(result, "- TestAccVirtualNetwork_basic") {
			t.Error("expected test case")
		}
	})

	t.Run("file with no matching tests", func(t *testing.T) {
		files := []ResourceTestFile{
			{
				FilePath: "internal/services/network/empty_test.go",
				Tests:    []string{},
			},
		}

		result := ResourceTestOverview("azurerm_resource", "resource", files)

		if !strings.Contains(result, "_No matching test cases found in this file._") {
			t.Error("expected no tests message for empty file")
		}
	})
}

func TestFeatureFlagList(t *testing.T) {
	t.Run("no flags", func(t *testing.T) {
		result := FeatureFlagList(nil)

		if !strings.Contains(result, "# Feature Flags (0)") {
			t.Error("expected header with count")
		}
		if !strings.Contains(result, "No feature flags were detected") {
			t.Error("expected no flags message")
		}
	})

	t.Run("with flags", func(t *testing.T) {
		flags := []FeatureFlagInfo{
			{
				Key:         "disable_terraform_partner_id",
				Description: "Disables the Terraform Partner ID which is used for tracking usage.",
				Default:     "false",
				Stage:       "stable",
				DisabledFor: nil,
			},
			{
				Key:         "allow_beta_features",
				Description: "Enables beta features in the provider.",
				Default:     "false",
				Stage:       "beta",
				DisabledFor: []string{"production", "gov-cloud"},
			},
		}

		result := FeatureFlagList(flags)

		if !strings.Contains(result, "# Feature Flags (2)") {
			t.Error("expected header with count")
		}
		if !strings.Contains(result, "## allow_beta_features") {
			t.Error("expected first flag (sorted alphabetically)")
		}
		if !strings.Contains(result, "## disable_terraform_partner_id") {
			t.Error("expected second flag")
		}
		if !strings.Contains(result, "- **Stage:** beta") {
			t.Error("expected stage")
		}
		if !strings.Contains(result, "- **Default:** false") {
			t.Error("expected default")
		}
		if !strings.Contains(result, "- **Disabled for:** production, gov-cloud") {
			t.Error("expected disabled for list")
		}
	})

	t.Run("flag with minimal info", func(t *testing.T) {
		flags := []FeatureFlagInfo{
			{
				Key: "simple_flag",
			},
		}

		result := FeatureFlagList(flags)

		if !strings.Contains(result, "## simple_flag") {
			t.Error("expected flag header")
		}
		if strings.Contains(result, "**Stage:**") {
			t.Error("should not have stage when not set")
		}
		if strings.Contains(result, "**Default:**") {
			t.Error("should not have default when not set")
		}
	})
}

func TestResourceBehaviors(t *testing.T) {
	t.Run("no behaviors", func(t *testing.T) {
		result := ResourceBehaviors("azurerm_resource", "resource", ResourceBehaviorInfo{})

		if !strings.Contains(result, "# Behaviors for azurerm_resource (Resource)") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "No additional behaviours were detected") {
			t.Error("expected no behaviors message")
		}
	})

	t.Run("data source with all behaviors", func(t *testing.T) {
		info := ResourceBehaviorInfo{
			FilePath:     "internal/services/network/virtual_network_resource.go",
			FunctionName: "resourceArmVirtualNetwork",
			Importer:     "pluginsdk.ImporterValidatingResourceId(validateVirtualNetworkID)",
			CustomizeDiff: []string{
				"customdiff.ForceNewIfChange(\"address_space\")",
				"customdiff.ValidateAll",
			},
			Timeouts: []TimeoutDetail{
				{Name: "Create", Value: "30m"},
				{Name: "Update", Value: "30m"},
				{Name: "Delete", Value: "30m"},
				{Name: "Read", Value: "5m"},
			},
			Notes: []string{
				"Supports gradual address space expansion",
				"Has special import logic for peered networks",
			},
		}

		result := ResourceBehaviors("azurerm_virtual_network", "data_source", info)

		if !strings.Contains(result, "(Data Source)") {
			t.Error("expected data source label")
		}
		if !strings.Contains(result, "**File:** internal/services/network/virtual_network_resource.go") {
			t.Error("expected file path")
		}
		if !strings.Contains(result, "**Function:** resourceArmVirtualNetwork") {
			t.Error("expected function name")
		}
		if !strings.Contains(result, "## Timeouts") {
			t.Error("expected timeouts section")
		}
		if !strings.Contains(result, "- Create: 30m") {
			t.Error("expected create timeout")
		}
		if !strings.Contains(result, "## CustomizeDiff") {
			t.Error("expected customize diff section")
		}
		if !strings.Contains(result, "ForceNewIfChange") {
			t.Error("expected customize diff entry")
		}
		if !strings.Contains(result, "## Importer") {
			t.Error("expected importer section")
		}
		if !strings.Contains(result, "validateVirtualNetworkID") {
			t.Error("expected importer content")
		}
		if !strings.Contains(result, "## Additional Notes") {
			t.Error("expected notes section")
		}
		if !strings.Contains(result, "gradual address space expansion") {
			t.Error("expected note content")
		}
	})

	t.Run("with raw timeouts", func(t *testing.T) {
		info := ResourceBehaviorInfo{
			TimeoutsRaw: "Create: 30 minutes\nUpdate: 30 minutes",
		}

		result := ResourceBehaviors("azurerm_resource", "resource", info)

		if !strings.Contains(result, "## Timeouts") {
			t.Error("expected timeouts section")
		}
		if !strings.Contains(result, "Create: 30 minutes") {
			t.Error("expected raw timeout content")
		}
	})
}

func TestExampleDirectory(t *testing.T) {
	t.Run("no files", func(t *testing.T) {
		result := ExampleDirectory("examples/virtual_network/basic", nil)

		if !strings.Contains(result, "# Example: examples/virtual_network/basic") {
			t.Error("expected header")
		}
		if !strings.Contains(result, "No files were found for this example") {
			t.Error("expected no files message")
		}
	})

	t.Run("with various file types", func(t *testing.T) {
		files := []ExampleFile{
			{
				FileName: "main.tf",
				FilePath: "examples/virtual_network/basic/main.tf",
				Content:  "resource \"azurerm_virtual_network\" \"example\" {}",
			},
			{
				FileName: "variables.tf",
				FilePath: "examples/virtual_network/basic/variables.tf",
				Content:  "variable \"location\" {}",
			},
			{
				FileName: "config.yaml",
				FilePath: "examples/virtual_network/basic/config.yaml",
				Content:  "key: value",
			},
			{
				FileName: "README.md",
				FilePath: "examples/virtual_network/basic/README.md",
				Content:  "# Example\n\nThis is an example.",
			},
			{
				FileName: "setup.sh",
				FilePath: "examples/virtual_network/basic/setup.sh",
				Content:  "#!/bin/bash\necho hello",
			},
			{
				FileName: "data.json",
				FilePath: "examples/virtual_network/basic/data.json",
				Content:  "{\"key\": \"value\"}",
			},
			{
				FileName: "Makefile",
				FilePath: "examples/virtual_network/basic/Makefile",
				Content:  "all:\n\techo test",
			},
		}

		result := ExampleDirectory("examples/virtual_network/basic", files)

		if !strings.Contains(result, "Contains 7 file(s)") {
			t.Error("expected file count")
		}

		// Check terraform file has HCL code block
		if !strings.Contains(result, "```hcl") {
			t.Error("expected HCL code block for .tf file")
		}

		// Check YAML file has YAML code block
		if !strings.Contains(result, "```yaml") {
			t.Error("expected YAML code block")
		}

		// Check bash file has bash code block
		if !strings.Contains(result, "```bash") {
			t.Error("expected bash code block for .sh file")
		}

		// Check JSON file has JSON code block
		if !strings.Contains(result, "```json") {
			t.Error("expected JSON code block")
		}

		// Check Makefile has generic code block
		if !strings.Contains(result, "```\nall:") {
			t.Error("expected generic code block for unknown file type")
		}

		// Check README.md is rendered as plain markdown (no code block wrapping)
		// The file should be sorted, check ordering
		if !strings.Contains(result, "## examples/virtual_network/basic/Makefile") {
			t.Error("expected Makefile header")
		}
	})

	t.Run("file content without trailing newline", func(t *testing.T) {
		files := []ExampleFile{
			{
				FileName: "main.tf",
				FilePath: "examples/test/main.tf",
				Content:  "resource {}",
			},
		}

		result := ExampleDirectory("examples/test", files)

		// Should have added newline before closing code block
		if !strings.Contains(result, "resource {}\n```") {
			t.Error("expected newline added before code block end")
		}
	})

	t.Run("yml extension", func(t *testing.T) {
		files := []ExampleFile{
			{
				FileName: "ci.yml",
				FilePath: "examples/test/ci.yml",
				Content:  "name: CI\n",
			},
		}

		result := ExampleDirectory("examples/test", files)

		if !strings.Contains(result, "```yaml") {
			t.Error("expected YAML code block for .yml file")
		}
	})
}

func TestRenderExampleFileContent(t *testing.T) {
	tests := []struct {
		name       string
		file       ExampleFile
		wantLang   string
		wantNoLang bool
	}{
		{
			name:     "terraform file",
			file:     ExampleFile{FileName: "main.tf", Content: "content"},
			wantLang: "```hcl",
		},
		{
			name:     "yaml file",
			file:     ExampleFile{FileName: "config.yaml", Content: "content"},
			wantLang: "```yaml",
		},
		{
			name:     "yml file",
			file:     ExampleFile{FileName: "config.yml", Content: "content"},
			wantLang: "```yaml",
		},
		{
			name:     "json file",
			file:     ExampleFile{FileName: "data.json", Content: "content"},
			wantLang: "```json",
		},
		{
			name:     "shell file",
			file:     ExampleFile{FileName: "script.sh", Content: "content"},
			wantLang: "```bash",
		},
		{
			name:       "unknown extension",
			file:       ExampleFile{FileName: "Makefile", Content: "content"},
			wantNoLang: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderExampleFileContent(tt.file)

			if tt.wantNoLang {
				if strings.Contains(result, "```hcl") || strings.Contains(result, "```yaml") ||
					strings.Contains(result, "```json") || strings.Contains(result, "```bash") {
					t.Error("should have generic code block without language")
				}
				if !strings.Contains(result, "```\n") {
					t.Error("expected generic code block")
				}
			} else {
				if !strings.Contains(result, tt.wantLang) {
					t.Errorf("expected %s code block", tt.wantLang)
				}
			}
		})
	}
}
