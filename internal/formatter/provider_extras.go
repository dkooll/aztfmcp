package formatter

import (
	"fmt"
	"sort"
	"strings"
)

func ResourceDocs(resourceName, kind, filePath, section string, sectionFound bool, content string) string {
	var text strings.Builder

	titleKind := "Resource"
	if strings.TrimSpace(kind) == "data_source" {
		titleKind = "Data Source"
	}

	text.WriteString(fmt.Sprintf("# Documentation: %s\n\n", resourceName))
	text.WriteString(fmt.Sprintf("**Kind:** %s\n", titleKind))
	if filePath != "" {
		text.WriteString(fmt.Sprintf("**Source:** %s\n", filePath))
	}
	if section != "" {
		if sectionFound {
			text.WriteString(fmt.Sprintf("**Section:** %s\n", section))
		} else {
			text.WriteString(fmt.Sprintf("**Section:** %s (not found, showing closest match)\n", section))
		}
	}
	text.WriteString("\n")
	text.WriteString(strings.TrimSpace(content))
	text.WriteString("\n")
	return text.String()
}

type ResourceTestFile struct {
	FilePath string
	Tests    []string
}

func ResourceTestOverview(resourceName, kind string, files []ResourceTestFile) string {
	var text strings.Builder

	titleKind := "Resource"
	if strings.TrimSpace(kind) == "data_source" {
		titleKind = "Data Source"
	}

	text.WriteString(fmt.Sprintf("# Acceptance Tests for %s (%s)\n\n", resourceName, titleKind))

	if len(files) == 0 {
		text.WriteString("No acceptance tests were discovered for this definition.\n")
		return text.String()
	}

	totalTests := 0
	for _, f := range files {
		totalTests += len(f.Tests)
	}
	text.WriteString(fmt.Sprintf("Discovered %d test file(s) with %d test case(s).\n\n", len(files), totalTests))

	for _, file := range files {
		text.WriteString(fmt.Sprintf("## %s\n", file.FilePath))
		if len(file.Tests) == 0 {
			text.WriteString("_No matching test cases found in this file._\n\n")
			continue
		}
		for _, test := range file.Tests {
			text.WriteString(fmt.Sprintf("- %s\n", test))
		}
		text.WriteString("\n")
	}

	return text.String()
}

type FeatureFlagInfo struct {
	Key         string
	Description string
	Default     string
	Stage       string
	DisabledFor []string
}

func FeatureFlagList(flags []FeatureFlagInfo) string {
	var text strings.Builder
	text.WriteString(fmt.Sprintf("# Feature Flags (%d)\n\n", len(flags)))

	if len(flags) == 0 {
		text.WriteString("No feature flags were detected in the provider configuration.\n")
		return text.String()
	}

	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Key < flags[j].Key
	})

	for _, flag := range flags {
		text.WriteString(fmt.Sprintf("## %s\n", flag.Key))
		if flag.Description != "" {
			text.WriteString(fmt.Sprintf("%s\n\n", flag.Description))
		}
		if flag.Stage != "" {
			text.WriteString(fmt.Sprintf("- **Stage:** %s\n", flag.Stage))
		}
		if flag.Default != "" {
			text.WriteString(fmt.Sprintf("- **Default:** %s\n", flag.Default))
		}
		if len(flag.DisabledFor) > 0 {
			text.WriteString(fmt.Sprintf("- **Disabled for:** %s\n", strings.Join(flag.DisabledFor, ", ")))
		}
		text.WriteString("\n")
	}

	return text.String()
}

type TimeoutDetail struct {
	Name  string
	Value string
}

type ResourceBehaviorInfo struct {
	FilePath      string
	FunctionName  string
	Importer      string
	CustomizeDiff []string
	Timeouts      []TimeoutDetail
	TimeoutsRaw   string
	Notes         []string
}

func ResourceBehaviors(resourceName, kind string, info ResourceBehaviorInfo) string {
	var text strings.Builder

	titleKind := "Resource"
	if strings.TrimSpace(kind) == "data_source" {
		titleKind = "Data Source"
	}

	text.WriteString(fmt.Sprintf("# Behaviors for %s (%s)\n\n", resourceName, titleKind))
	if info.FilePath != "" {
		text.WriteString(fmt.Sprintf("**File:** %s\n", info.FilePath))
	}
	if info.FunctionName != "" {
		text.WriteString(fmt.Sprintf("**Function:** %s\n", info.FunctionName))
	}
	text.WriteString("\n")

	if len(info.Timeouts) > 0 {
		text.WriteString("## Timeouts\n\n")
		for _, t := range info.Timeouts {
			text.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Value))
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
			text.WriteString(fmt.Sprintf("- %s\n", entry))
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
			text.WriteString(fmt.Sprintf("- %s\n", note))
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

type ExampleFile struct {
	FileName string
	FilePath string
	Content  string
}

func ExampleDirectory(examplePath string, files []ExampleFile) string {
	var text strings.Builder
	text.WriteString(fmt.Sprintf("# Example: %s\n\n", examplePath))

	if len(files) == 0 {
		text.WriteString("No files were found for this example.\n")
		return text.String()
	}

	text.WriteString(fmt.Sprintf("Contains %d file(s).\n\n", len(files)))

	sort.Slice(files, func(i, j int) bool {
		return files[i].FilePath < files[j].FilePath
	})

	for _, file := range files {
		text.WriteString(fmt.Sprintf("## %s\n\n", file.FilePath))
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
