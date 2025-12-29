package formatter

import (
	"fmt"
	"strings"
	"time"
)

func IncrementalSyncProgress(totalRepos, synced, skipped int, updatedRepos, errors []string) string {
	var text strings.Builder
	text.WriteString("# Incremental Sync Completed\n\n")

	fmt.Fprintf(&text, "Checked %d repositories\n", totalRepos)
	fmt.Fprintf(&text, "Updated repositories: %d\n", synced)
	fmt.Fprintf(&text, "Skipped (up-to-date): %d\n\n", skipped)

	if synced > 0 {
		text.WriteString("Updated repositories:\n")
		for _, repo := range updatedRepos {
			fmt.Fprintf(&text, "- %s\n", repo)
		}
		text.WriteString("\n")
	}

	if len(errors) > 0 {
		fmt.Fprintf(&text, "%d errors occurred:\n", len(errors))
		for i, err := range errors {
			if i >= 10 {
				fmt.Fprintf(&text, "... and %d more errors\n", len(errors)-10)
				break
			}
			fmt.Fprintf(&text, "- %s\n", err)
		}
	}

	return text.String()
}

func JobDetails(jobID, jobType, status string, startedAt time.Time, completedAt *time.Time, errorMsg string, progressText string) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Sync Job %s (%s)\n\n", jobID, jobType)
	fmt.Fprintf(&text, "Status: %s\n", strings.ToUpper(status))
	fmt.Fprintf(&text, "Started: %s\n", startedAt.Format(time.RFC3339))
	if completedAt != nil {
		duration := completedAt.Sub(startedAt)
		fmt.Fprintf(&text, "Completed: %s (duration %s)\n", completedAt.Format(time.RFC3339), duration.Round(time.Second))
	} else {
		fmt.Fprintf(&text, "Elapsed: %s\n", time.Since(startedAt).Round(time.Second))
	}

	if errorMsg != "" {
		fmt.Fprintf(&text, "\nError: %s\n", errorMsg)
	}

	if progressText != "" {
		text.WriteString("\n")
		text.WriteString(progressText)
	}

	return text.String()
}

func JobList(jobs []JobInfo) string {
	if len(jobs) == 0 {
		return "No sync jobs have been scheduled yet."
	}

	var text strings.Builder
	text.WriteString("# Sync Jobs\n\n")
	for _, job := range jobs {
		fmt.Fprintf(&text, "- %s (%s) — %s", job.ID, job.Type, strings.ToUpper(job.Status))
		if job.CompletedAt != nil {
			duration := job.CompletedAt.Sub(job.StartedAt)
			fmt.Fprintf(&text, " in %s", duration.Round(time.Second))
		}
		text.WriteString("\n")
	}

	text.WriteString("\nUse `sync_status` with a job_id for detailed information.\n")
	return text.String()
}

type JobInfo struct {
	ID          string
	Type        string
	Status      string
	StartedAt   time.Time
	CompletedAt *time.Time
}
