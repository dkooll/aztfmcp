package formatter

import (
	"fmt"
	"strings"

	"github.com/dkooll/aztfmcp/internal/indexer"
)

func SyncProgress(progress *indexer.SyncProgress) string {
	if progress == nil {
		return ""
	}

	var text strings.Builder
	text.WriteString("## Summary\n\n")
	succeeded := progress.ProcessedRepos - len(progress.Errors)
	fmt.Fprintf(&text, "Successfully synced %d/%d repositories\n\n", succeeded, progress.TotalRepos)

	if len(progress.UpdatedRepos) > 0 {
		text.WriteString("Updated repositories:\n")
		for _, repo := range progress.UpdatedRepos {
			fmt.Fprintf(&text, "- %s\n", repo)
		}
		text.WriteString("\n")
	}

	if len(progress.Errors) > 0 {
		fmt.Fprintf(&text, "%d errors occurred:\n", len(progress.Errors))
		for i, err := range progress.Errors {
			if i >= 10 {
				remaining := len(progress.Errors) - 10
				fmt.Fprintf(&text, "... and %d more errors\n", remaining)
				break
			}
			fmt.Fprintf(&text, "- %s\n", err)
		}
	}

	return text.String()
}
