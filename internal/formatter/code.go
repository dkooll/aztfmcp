// Package formatter renders textual responses returned by the MCP server tools.
package formatter

import (
	"fmt"
	"strings"

	"github.com/dkooll/aztfmcp/internal/database"
)

func CodeSearchResults(query string, files []database.RepositoryFile, getRepositoryName func(int64) string) string {
	var text strings.Builder
	text.WriteString(fmt.Sprintf("# Code Search Results for '%s' (%d matches)\n\n", query, len(files)))

	if len(files) == 0 {
		text.WriteString("No code matches found.\n")
		return text.String()
	}

	for _, file := range files {
		repositoryName := getRepositoryName(file.RepositoryID)
		text.WriteString(fmt.Sprintf("## %s / %s\n", repositoryName, file.FilePath))
		text.WriteString("```\n")
		text.WriteString(ExtractCodeContext(file.Content, query))
		text.WriteString("```\n\n")
	}

	return text.String()
}

func ExtractCodeContext(content, query string) string {
	var text strings.Builder
	lines := strings.Split(content, "\n")
	queryLower := strings.ToLower(query)

	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), queryLower) {
			start := max(i-2, 0)
			end := min(i+3, len(lines))

			for j := start; j < end; j++ {
				if j == i {
					text.WriteString(fmt.Sprintf("→ %d: %s\n", j+1, lines[j]))
				} else {
					text.WriteString(fmt.Sprintf("  %d: %s\n", j+1, lines[j]))
				}
			}
			text.WriteString("...\n")
			break
		}
	}

	return text.String()
}

func FileContent(repositoryName, filePath, fileType string, sizeBytes int64, content string, startLine, endLine, totalLines int) string {
	var text strings.Builder
	text.WriteString(fmt.Sprintf("# %s / %s\n\n", repositoryName, filePath))
	text.WriteString(fmt.Sprintf("**Size:** %d bytes\n", sizeBytes))
	text.WriteString(fmt.Sprintf("**Type:** %s\n\n", fileType))
	if startLine > 0 {
		if endLine == 0 {
			endLine = totalLines
		}
		text.WriteString(fmt.Sprintf("**Lines:** %d-%d of %d\n\n", startLine, endLine, totalLines))
	}
	lang := ""
	switch fileType {
	case "terraform":
		lang = "hcl"
	case "go":
		lang = "go"
	case "yaml":
		lang = "yaml"
	case "json":
		lang = "json"
	case "markdown":
		lang = "markdown"
	}
	if lang == "" {
		text.WriteString("```\n")
	} else {
		text.WriteString("```" + lang + "\n")
	}
	text.WriteString(content)
	text.WriteString("\n```\n")
	return text.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
