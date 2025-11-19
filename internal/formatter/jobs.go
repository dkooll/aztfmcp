package formatter

import (
	"fmt"
	"strings"
	"time"
)

func IncrementalSyncProgress(totalRepos, synced, skipped int, updatedRepos, errors []string) string {
	var text strings.Builder
	text.WriteString("# Incremental Sync Completed\n\n")

	text.WriteString(fmt.Sprintf("Checked %d repositories\n", totalRepos))
	text.WriteString(fmt.Sprintf("Updated repositories: %d\n", synced))
	text.WriteString(fmt.Sprintf("Skipped (up-to-date): %d\n\n", skipped))

	if synced > 0 {
		text.WriteString("Updated repositories:\n")
		for _, repo := range updatedRepos {
			text.WriteString(fmt.Sprintf("- %s\n", repo))
		}
		text.WriteString("\n")
	}

	if len(errors) > 0 {
		text.WriteString(fmt.Sprintf("%d errors occurred:\n", len(errors)))
		for i, err := range errors {
			if i >= 10 {
				text.WriteString(fmt.Sprintf("... and %d more errors\n", len(errors)-10))
				break
			}
			text.WriteString(fmt.Sprintf("- %s\n", err))
		}
	}

	return text.String()
}

func JobDetails(jobID, jobType, status string, startedAt time.Time, completedAt *time.Time, errorMsg string, progressText string) string {
	var text strings.Builder
	text.WriteString(fmt.Sprintf("# Sync Job %s (%s)\n\n", jobID, jobType))
	text.WriteString(fmt.Sprintf("Status: %s\n", strings.ToUpper(status)))
	text.WriteString(fmt.Sprintf("Started: %s\n", startedAt.Format(time.RFC3339)))
	if completedAt != nil {
		duration := completedAt.Sub(startedAt)
		text.WriteString(fmt.Sprintf("Completed: %s (duration %s)\n", completedAt.Format(time.RFC3339), duration.Round(time.Second)))
	} else {
		text.WriteString(fmt.Sprintf("Elapsed: %s\n", time.Since(startedAt).Round(time.Second)))
	}

	if errorMsg != "" {
		text.WriteString(fmt.Sprintf("\nError: %s\n", errorMsg))
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
		text.WriteString(fmt.Sprintf("- %s (%s) — %s", job.ID, job.Type, strings.ToUpper(job.Status)))
		if job.CompletedAt != nil {
			duration := job.CompletedAt.Sub(job.StartedAt)
			text.WriteString(fmt.Sprintf(" in %s", duration.Round(time.Second)))
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
