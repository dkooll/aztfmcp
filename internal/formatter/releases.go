package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/dkooll/aztfmcp/internal/database"
)

func ReleaseSummary(repoFullName string, release *database.ProviderRelease, entries []database.ProviderReleaseEntry) string {
	if release == nil {
		return "Incremental Provider Sync Summary\n- No release metadata available"
	}

	name := repoFullName
	if name == "" {
		name = "hashicorp/terraform-provider-azurerm"
	}

	var b strings.Builder
	b.WriteString("Incremental Provider Sync Summary\n")
	fmt.Fprintf(&b, "- Repository: %s\n", name)
	fmt.Fprintf(&b, "- Range: %s\n", renderRange(release))
	fmt.Fprintf(&b, "- Date: %s\n", releaseDateOrFallback(release))

	sections := groupEntriesBySection(entries)
	if len(sections.order) == 0 {
		b.WriteString("- No categorized entries found\n")
		return b.String()
	}

	for _, section := range sections.order {
		fmt.Fprintf(&b, "- %s\n", section)
		for _, title := range sections.entries[section] {
			fmt.Fprintf(&b, "    - %s\n", title)
		}
	}

	return b.String()
}

type sectionGrouping struct {
	order   []string
	entries map[string][]string
}

func groupEntriesBySection(entries []database.ProviderReleaseEntry) sectionGrouping {
	grouping := sectionGrouping{
		order:   []string{},
		entries: make(map[string][]string),
	}

	fallbackOrder := []string{"Features", "Enhancements", "Bug Fixes", "Breaking Changes", "Security"}
	seenFallback := make(map[string]bool)
	appearanceOrder := []string{}
	appearanceTracker := make(map[string]bool)

	for _, entry := range entries {
		section := strings.TrimSpace(entry.Section)
		if section == "" {
			section = "Other"
		}
		grouping.entries[section] = append(grouping.entries[section], entry.Title)
		if !appearanceTracker[section] {
			appearanceTracker[section] = true
			appearanceOrder = append(appearanceOrder, section)
		}
	}

	for _, preferred := range fallbackOrder {
		if titles, ok := grouping.entries[preferred]; ok && len(titles) > 0 {
			grouping.order = append(grouping.order, preferred)
			seenFallback[preferred] = true
		}
	}

	for _, section := range appearanceOrder {
		if seenFallback[section] {
			continue
		}
		grouping.order = append(grouping.order, section)
	}

	return grouping
}

func renderRange(release *database.ProviderRelease) string {
	head := formatTag(release.Tag, release.CommitSHA.String)
	if release.PreviousTag.Valid {
		prev := formatTag(release.PreviousTag.String, release.PreviousCommitSHA.String)
		if prev != "" {
			return fmt.Sprintf("%s → %s", prev, head)
		}
	}
	return head
}

func formatTag(tag, sha string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	if sha != "" {
		return fmt.Sprintf("%s (%s)", tag, shortSHA(sha))
	}
	return tag
}

func releaseDateOrFallback(release *database.ProviderRelease) string {
	if release.ReleaseDate.Valid && release.ReleaseDate.String != "" {
		if t, err := time.Parse("2006-01-02", release.ReleaseDate.String); err == nil {
			return t.Format("January 2, 2006")
		}
		return release.ReleaseDate.String
	}
	return "unknown"
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}
