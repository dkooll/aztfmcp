package formatter

import (
	"fmt"
	"sort"
	"strings"
)

// ResourceDocs renders documentation extracted from the provider docs tree.
func ResourceDocs(resourceName, kind, filePath, section string, sectionFound bool, content string) string {
	var text strings.Builder

	titleKind := "Resource"
	if strings.TrimSpace(kind) == "data_source" {
		titleKind = "Data Source"
	}

	fmt.Fprintf(&text, "# Documentation: %s\n\n", resourceName)
	fmt.Fprintf(&text, "**Kind:** %s\n", titleKind)
	if filePath != "" {
		fmt.Fprintf(&text, "**Source:** %s\n", filePath)
	}
	if section != "" {
		if sectionFound {
			fmt.Fprintf(&text, "**Section:** %s\n", section)
		} else {
			fmt.Fprintf(&text, "**Section:** %s (not found, showing closest match)\n", section)
		}
	}
	text.WriteString("\n")
	text.WriteString(strings.TrimSpace(content))
	text.WriteString("\n")
	return text.String()
}

// ResourceTestFile represents a Go test file and the test cases discovered within it.
type ResourceTestFile struct {
	FilePath string
	Tests    []string
}

// ResourceTestOverview renders a summary of acceptance tests associated with a resource or data source.
func ResourceTestOverview(resourceName, kind string, files []ResourceTestFile) string {
	var text strings.Builder

	titleKind := "Resource"
	if strings.TrimSpace(kind) == "data_source" {
		titleKind = "Data Source"
	}

	fmt.Fprintf(&text, "# Acceptance Tests for %s (%s)\n\n", resourceName, titleKind)

	if len(files) == 0 {
		text.WriteString("No acceptance tests were discovered for this definition.\n")
		return text.String()
	}

	totalTests := 0
	for _, f := range files {
		totalTests += len(f.Tests)
	}
	fmt.Fprintf(&text, "Discovered %d test file(s) with %d test case(s).\n\n", len(files), totalTests)

	for _, file := range files {
		fmt.Fprintf(&text, "## %s\n", file.FilePath)
		if len(file.Tests) == 0 {
			text.WriteString("_No matching test cases found in this file._\n\n")
			continue
		}
		for _, test := range file.Tests {
			fmt.Fprintf(&text, "- %s\n", test)
		}
		text.WriteString("\n")
	}

	return text.String()
}

// FeatureFlagInfo captures metadata about a provider feature flag.
type FeatureFlagInfo struct {
	Key         string
	Description string
	Default     string
	Stage       string
	DisabledFor []string
}

// FeatureFlagList renders the available feature flags and their metadata.
func FeatureFlagList(flags []FeatureFlagInfo) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Feature Flags (%d)\n\n", len(flags))

	if len(flags) == 0 {
		text.WriteString("No feature flags were detected in the provider configuration.\n")
		return text.String()
	}

	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Key < flags[j].Key
	})

	for _, flag := range flags {
		fmt.Fprintf(&text, "## %s\n", flag.Key)
		if flag.Description != "" {
			fmt.Fprintf(&text, "%s\n\n", flag.Description)
		}
		if flag.Stage != "" {
			fmt.Fprintf(&text, "- **Stage:** %s\n", flag.Stage)
		}
		if flag.Default != "" {
			fmt.Fprintf(&text, "- **Default:** %s\n", flag.Default)
		}
		if len(flag.DisabledFor) > 0 {
			fmt.Fprintf(&text, "- **Disabled for:** %s\n", strings.Join(flag.DisabledFor, ", "))
		}
		text.WriteString("\n")
	}

	return text.String()
}

// TimeoutDetail represents a single timeout configuration entry.
type TimeoutDetail struct {
	Name  string
	Value string
}

// ResourceBehaviorInfo summarises advanced behaviours configured on a resource schema.
type ResourceBehaviorInfo struct {
	FilePath      string
	FunctionName  string
	Importer      string
	CustomizeDiff []string
	Timeouts      []TimeoutDetail
	TimeoutsRaw   string
	Notes         []string
}

// ResourceBehaviors renders the behavioural summary for a resource/data source.
func ResourceBehaviors(resourceName, kind string, info ResourceBehaviorInfo) string {
	var text strings.Builder

	titleKind := "Resource"
	if strings.TrimSpace(kind) == "data_source" {
		titleKind = "Data Source"
	}

	fmt.Fprintf(&text, "# Behaviors for %s (%s)\n\n", resourceName, titleKind)
	if info.FilePath != "" {
		fmt.Fprintf(&text, "**File:** %s\n", info.FilePath)
	}
	if info.FunctionName != "" {
		fmt.Fprintf(&text, "**Function:** %s\n", info.FunctionName)
	}
	text.WriteString("\n")

	if len(info.Timeouts) > 0 {
		text.WriteString("## Timeouts\n\n")
		for _, t := range info.Timeouts {
			fmt.Fprintf(&text, "- %s: %s\n", t.Name, t.Value)
		}
		text.WriteString("\n")
	} else if strings.TrimSpace(info.TimeoutsRaw) != "" {
		text.WriteString("## Timeouts\n\n")
		text.WriteString(info.TimeoutsRaw)
		if !strings.HasSuffix(info.TimeoutsRaw, "\n") {
			text.WriteString("\n")
		}
		text.WriteString("\n")
	}

	if len(info.CustomizeDiff) > 0 {
		text.WriteString("## CustomizeDiff\n\n")
		for _, entry := range info.CustomizeDiff {
			fmt.Fprintf(&text, "- %s\n", entry)
		}
		text.WriteString("\n")
	}

	if strings.TrimSpace(info.Importer) != "" {
		text.WriteString("## Importer\n\n")
		text.WriteString(info.Importer)
		if !strings.HasSuffix(info.Importer, "\n") {
			text.WriteString("\n")
		}
		text.WriteString("\n")
	}

	if len(info.Notes) > 0 {
		text.WriteString("## Additional Notes\n\n")
		for _, note := range info.Notes {
			fmt.Fprintf(&text, "- %s\n", note)
		}
		text.WriteString("\n")
	}

	if len(info.Timeouts) == 0 && strings.TrimSpace(info.TimeoutsRaw) == "" &&
		len(info.CustomizeDiff) == 0 && strings.TrimSpace(info.Importer) == "" &&
		len(info.Notes) == 0 {
		text.WriteString("No additional behaviours were detected.\n")
	}

	return text.String()
}

// ExampleFile describes a single file included in an example directory.
type ExampleFile struct {
	FileName string
	FilePath string
	Content  string
}

// ExampleDirectory renders the files that make up an example scenario.
func ExampleDirectory(examplePath string, files []ExampleFile) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Example: %s\n\n", examplePath)

	if len(files) == 0 {
		text.WriteString("No files were found for this example.\n")
		return text.String()
	}

	fmt.Fprintf(&text, "Contains %d file(s).\n\n", len(files))

	sort.Slice(files, func(i, j int) bool {
		return files[i].FilePath < files[j].FilePath
	})

	for _, file := range files {
		fmt.Fprintf(&text, "## %s\n\n", file.FilePath)
		text.WriteString(renderExampleFileContent(file))
	}

	return text.String()
}

func renderExampleFileContent(file ExampleFile) string {
	var text strings.Builder

	language := ""
	switch {
	case strings.HasSuffix(file.FileName, ".tf"):
		language = "hcl"
	case strings.HasSuffix(file.FileName, ".yaml"), strings.HasSuffix(file.FileName, ".yml"):
		language = "yaml"
	case strings.HasSuffix(file.FileName, ".json"):
		language = "json"
	case strings.HasSuffix(file.FileName, ".sh"):
		language = "bash"
	case strings.HasSuffix(file.FileName, ".md"):
		language = ""
	}

	if language == "" && !strings.HasSuffix(file.FileName, ".md") {
		text.WriteString("```\n")
		text.WriteString(file.Content)
		if !strings.HasSuffix(file.Content, "\n") {
			text.WriteString("\n")
		}
		text.WriteString("```\n\n")
		return text.String()
	}

	if strings.HasSuffix(file.FileName, ".md") {
		text.WriteString(file.Content)
		if !strings.HasSuffix(file.Content, "\n") {
			text.WriteString("\n")
		}
		text.WriteString("\n")
		return text.String()
	}

	text.WriteString("```" + language + "\n")
	text.WriteString(file.Content)
	if !strings.HasSuffix(file.Content, "\n") {
		text.WriteString("\n")
	}
	text.WriteString("```\n\n")
	return text.String()
}
