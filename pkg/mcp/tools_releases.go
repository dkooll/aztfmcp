package mcp

import (
	"database/sql"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/formatter"
	"github.com/dkooll/aztfmcp/internal/indexer"
)

type releaseSummaryArgs struct {
	Version string   `json:"version"`
	Fields  []string `json:"fields"`
}

type releaseSnippetArgs struct {
	Version       string   `json:"version"`
	Query         string   `json:"query"`
	MaxContext    int      `json:"max_context_lines"`
	FallbackMatch string   `json:"fallback_match"`
	Fields        []string `json:"fields"`
}

func (s *Server) handleGetReleaseSummary(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[releaseSummaryArgs](args)
	if err != nil {
		params = releaseSummaryArgs{}
	}

	repo, err := s.primaryRepository()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorResponse("Repository has not been synced yet")
		}
		return ErrorResponse(fmt.Sprintf("Failed to load repository metadata: %v", err))
	}

	var (
		release *database.ProviderRelease
		entries []database.ProviderReleaseEntry
	)

	version := strings.TrimSpace(params.Version)
	if version == "" {
		release, entries, err = s.db.GetLatestReleaseWithEntries(repo.ID)
	} else {
		// Try exact version first
		relVersion := strings.TrimPrefix(version, "v")
		release, entries, err = s.db.GetReleaseWithEntriesByVersion(repo.ID, relVersion)
		if err != nil {
			// Then try by tag (with v prefix)
			tag := version
			if !strings.HasPrefix(strings.ToLower(tag), "v") {
				tag = "v" + tag
			}
			release, entries, err = s.db.GetReleaseWithEntriesByTag(repo.ID, tag)
		}
	}

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if version == "" {
				return ErrorResponse("No release metadata available. Try running an incremental sync first.")
			}
			return ErrorResponse(fmt.Sprintf("No release metadata found for version %s", version))
		}
		return ErrorResponse(fmt.Sprintf("Failed to load release metadata: %v", err))
	}

	fullName := repo.FullName
	if fullName == "" {
		fullName = repo.Name
	}

	includeEntries := fieldIncluded(params.Fields, "entries")
	if len(params.Fields) == 0 {
		includeEntries = true
	}

	if includeEntries {
		summary := formatter.ReleaseSummary(fullName, release, entries)
		return SuccessResponse(summary)
	}

	summary := minimalReleaseSummary(fullName, release)
	return SuccessResponse(summary)
}

func (s *Server) handleGetReleaseSnippet(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[releaseSnippetArgs](args)
	if err != nil {
		return ErrorResponse("Error: Invalid parameters")
	}

	version := strings.TrimSpace(params.Version)
	query := strings.TrimSpace(params.Query)
	if version == "" || query == "" {
		return ErrorResponse("version and query are required")
	}

	repo, err := s.primaryRepository()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorResponse("Repository has not been synced yet")
		}
		return ErrorResponse(fmt.Sprintf("Failed to load repository metadata: %v", err))
	}

	// Resolve version or tag
	relVersion := strings.TrimPrefix(version, "v")
	release, entries, err := s.db.GetReleaseWithEntriesByVersion(repo.ID, relVersion)
	if err != nil {
		tag := version
		if stripped, ok := strings.CutPrefix(tag, "v"); ok {
			tag = "v" + stripped
		}
		release, entries, err = s.db.GetReleaseWithEntriesByTag(repo.ID, tag)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorResponse(fmt.Sprintf("No release metadata found for version %s", version))
		}
		return ErrorResponse(fmt.Sprintf("Failed to load release metadata: %v", err))
	}

	entry := selectReleaseEntry(entries, query, params.FallbackMatch)
	if entry == nil {
		return ErrorResponse("No matching release entry found for that query")
	}

	if !release.PreviousTag.Valid || release.PreviousTag.String == "" {
		return ErrorResponse("Unable to compute diff for the earliest release (missing previous tag)")
	}

	var entryFilePath string
	if entry.ResourceName.Valid {
		if res, err := s.db.GetProviderResource(entry.ResourceName.String); err == nil {
			if res.FilePath.Valid {
				entryFilePath = res.FilePath.String
			}
		}
	}

	if s.syncer == nil {
		return ErrorResponse("Syncer is not initialized; run a sync first")
	}

	compare, err := s.syncer.CompareTags(release.PreviousTag.String, release.Tag)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to fetch GitHub compare diff: %v", err))
	}

	filename, patch := locatePatchForEntry(compare, entry, query, entryFilePath)
	if filename == "" || patch == "" {
		return ErrorResponse("Diff data not available for that entry. Try a different query or rerun the incremental sync.")
	}

	maxLines := params.MaxContext
	if maxLines <= 0 {
		maxLines = 24
	}

	includeHeader := fieldIncluded(params.Fields, "header")
	includeFile := fieldIncluded(params.Fields, "file")
	includeDiff := fieldIncluded(params.Fields, "diff")
	includeCompare := fieldIncluded(params.Fields, "compare_url")
	if len(params.Fields) == 0 {
		includeHeader, includeFile, includeDiff, includeCompare = true, true, true, true
	}

	trimmed, truncated := trimPatchLines(patch, maxLines)
	text := formatReleaseSnippetResponse(repo, release, entry, filename, trimmed, truncated, maxLines, includeHeader, includeFile, includeDiff, includeCompare)
	return SuccessResponse(text)
}

func (s *Server) primaryRepository() (*database.Repository, error) {
	name := s.repoShortName()
	return s.db.GetRepository(name)
}

type backfillReleaseArgs struct {
	Version string `json:"version"`
}

func (s *Server) handleBackfillRelease(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[backfillReleaseArgs](args)
	if err != nil || strings.TrimSpace(params.Version) == "" {
		return ErrorResponse("version is required")
	}

	repo, err := s.primaryRepository()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorResponse("Repository has not been synced yet")
		}
		return ErrorResponse(fmt.Sprintf("Failed to load repository metadata: %v", err))
	}

	// Load the stored CHANGELOG.md from DB
	file, err := s.db.GetFile(repo.Name, "CHANGELOG.md")
	if err != nil {
		return ErrorResponse("CHANGELOG.md not found in local index; run a full sync first")
	}

	// Extract single release block and persist
	raw := strings.TrimSpace(file.Content)
	if raw == "" {
		return ErrorResponse("CHANGELOG.md is empty")
	}

	ver := strings.TrimSpace(params.Version)
	normalizedVersion := strings.TrimPrefix(strings.ToLower(ver), "v")
	tag := ver
	if !strings.HasPrefix(strings.ToLower(tag), "v") {
		tag = "v" + tag
	}

	relBlock, date, ok := extractReleaseBlock(raw, normalizedVersion)
	if !ok {
		return ErrorResponse(fmt.Sprintf("Version %s not found in changelog", ver))
	}

	entries := parseReleaseEntriesFromBlock(relBlock)

	rel := &database.ProviderRelease{
		RepositoryID:  repo.ID,
		Version:       normalizedVersion,
		Tag:           tag,
		ReleaseDate:   sql.NullString{String: date, Valid: date != ""},
		ComparisonURL: sql.NullString{},
	}

	releaseID, err := s.db.UpsertProviderRelease(rel)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to store release: %v", err))
	}

	if err := s.db.ReplaceReleaseEntries(releaseID, entries); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to store release entries: %v", err))
	}

	return SuccessResponse(fmt.Sprintf("Backfilled release %s with %d entries", tag, len(entries)))
}

// extractReleaseBlock finds the section for a specific version and returns its text and date.
func extractReleaseBlock(changelog string, version string) (string, string, bool) {
	// Match heading like: ## [4.48.0] (2024-01-01) or ## 4.48.0 (2024-01-01)
	// Build a regex that captures the heading and subsequent text until next '## '
	esc := regexp.QuoteMeta(version)
	// Allow optional brackets and optional leading v in text
	heading := regexp.MustCompile(`(?m)^##\s*(?:\[` + esc + `\]|v?` + esc + `)\s*(?:\(([^)]+)\))?\s*$`)
	loc := heading.FindStringSubmatchIndex(changelog)
	if loc == nil {
		return "", "", false
	}
	start := loc[0]
	date := ""
	if len(loc) >= 4 && loc[2] != -1 && loc[3] != -1 {
		date = strings.TrimSpace(changelog[loc[2]:loc[3]])
	}
	// Find next heading
	next := regexp.MustCompile(`(?m)^##\s+`).FindStringIndex(changelog[start+2:])
	var end int
	if next == nil {
		end = len(changelog)
	} else {
		end = start + 2 + next[0]
	}
	block := strings.TrimSpace(changelog[start:end])
	return block, date, true
}

func parseReleaseEntriesFromBlock(block string) []database.ProviderReleaseEntry {
	lines := strings.Split(block, "\n")
	section := ""
	out := []database.ProviderReleaseEntry{}
	order := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") {
			continue
		}
		if after, ok := strings.CutPrefix(t, "### "); ok {
			section = strings.TrimSpace(after)
			continue
		}
		if strings.HasPrefix(t, "-") || strings.HasPrefix(t, "*") {
			// bullet
			title := strings.TrimSpace(strings.TrimLeft(t, "-* "))
			key := fmt.Sprintf("%s-%04d", safeSlug(section), order)
			out = append(out, database.ProviderReleaseEntry{
				Section:    ifEmpty(section, "Other"),
				EntryKey:   key,
				Title:      title,
				OrderIndex: order,
			})
			order++
		}
	}
	return out
}

func ifEmpty(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

func safeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "section"
	}
	b := strings.Builder{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func selectReleaseEntry(entries []database.ProviderReleaseEntry, query string, fallback string) *database.ProviderReleaseEntry {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return nil
	}

	for idx := range entries {
		entry := &entries[idx]
		if entry.Identifier.Valid && strings.EqualFold(entry.Identifier.String, normalized) {
			return entry
		}
		if entry.ResourceName.Valid && strings.EqualFold(entry.ResourceName.String, normalized) {
			return entry
		}
	}

	if fallback != "" {
		normalizedFallback := strings.ToLower(strings.TrimSpace(fallback))
		for idx := range entries {
			entry := &entries[idx]
			if strings.Contains(strings.ToLower(entry.Title), normalizedFallback) {
				return entry
			}
		}
	}

	for idx := range entries {
		entry := &entries[idx]
		if strings.Contains(strings.ToLower(entry.Title), normalized) {
			return entry
		}
	}

	return nil
}

func locatePatchForEntry(compare *indexer.GitHubCompareResult, entry *database.ProviderReleaseEntry, query string, filePath string) (string, string) {
	if compare == nil {
		return "", ""
	}

	preferredTargets := buildReleaseEntryTargets(entry, query, filePath)
	bestScore := -1 << 30
	bestFile := ""
	bestPatch := ""

	for _, file := range compare.Files {
		if file.Patch == "" {
			continue
		}
		score := scorePatchCandidate(file.Filename, file.Patch, preferredTargets)
		if score > bestScore {
			bestScore = score
			bestFile = file.Filename
			bestPatch = file.Patch
		}
	}

	if bestPatch != "" {
		return bestFile, bestPatch
	}

	return "", ""
}

type releaseEntryTargets struct {
	filePaths            []string
	filenameTokens       []string
	contentTokens        []string
	fallbackContentToken string
	preferredDirSegments []string
}

func buildReleaseEntryTargets(entry *database.ProviderReleaseEntry, query string, filePath string) releaseEntryTargets {
	targets := releaseEntryTargets{}

	if filePath != "" {
		lower := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
		targets.filePaths = append(targets.filePaths, lower)
		if base := path.Base(lower); base != "" {
			targets.filenameTokens = append(targets.filenameTokens, base)
		}
		if dir := path.Dir(lower); dir != "." {
			targets.preferredDirSegments = append(targets.preferredDirSegments, strings.Split(strings.Trim(dir, "/"), "/")...)
		}
	}

	if entry.ResourceName.Valid {
		nameLower := strings.ToLower(entry.ResourceName.String)
		trimmed := strings.TrimPrefix(nameLower, "azurerm_")
		targets.filenameTokens = append(targets.filenameTokens, nameLower, trimmed)
		targets.contentTokens = append(targets.contentTokens, nameLower, trimmed)
		if trimmed != "" {
			targets.filenameTokens = append(targets.filenameTokens, trimmed+"_resource")
		}
		camel := toCamelCaseToken(entry.ResourceName.String)
		if camel != "" {
			targets.contentTokens = append(targets.contentTokens, camel)
		}
	}

	if entry.Identifier.Valid {
		idLower := strings.ToLower(entry.Identifier.String)
		targets.contentTokens = append(targets.contentTokens, idLower)
	}

	if query != "" {
		qLower := strings.ToLower(query)
		targets.contentTokens = append(targets.contentTokens, qLower)
		targets.filenameTokens = append(targets.filenameTokens, qLower)
		targets.fallbackContentToken = qLower
	} else if entry.Title != "" {
		targets.fallbackContentToken = strings.ToLower(entry.Title)
	}

	targets.filePaths = uniqueStrings(targets.filePaths)
	targets.filenameTokens = uniqueStrings(targets.filenameTokens)
	targets.contentTokens = uniqueStrings(targets.contentTokens)
	targets.preferredDirSegments = uniqueStrings(targets.preferredDirSegments)
	return targets
}

func scorePatchCandidate(filename string, patch string, targets releaseEntryTargets) int {
	lowerPath := strings.ToLower(strings.ReplaceAll(filename, "\\", "/"))
	lowerPatch := strings.ToLower(patch)
	score := 0

	for _, pathMatch := range targets.filePaths {
		if pathMatch != "" && lowerPath == pathMatch {
			score += 1000
		}
	}

	if strings.HasSuffix(lowerPath, "_resource.go") {
		score += 120
	}
	if strings.Contains(lowerPath, "/internal/services/") && strings.HasSuffix(lowerPath, ".go") {
		score += 60
	}
	if strings.HasSuffix(lowerPath, "_test.go") {
		score -= 180
	}
	if strings.Contains(lowerPath, "/website/") || strings.HasSuffix(lowerPath, "changelog.md") {
		score -= 250
	}

	for _, segment := range targets.preferredDirSegments {
		if segment != "" && strings.Contains(lowerPath, segment) {
			score += 20
		}
	}

	for _, token := range targets.filenameTokens {
		if token != "" && strings.Contains(lowerPath, token) {
			score += 35
		}
	}

	for _, token := range targets.contentTokens {
		if token != "" && strings.Contains(lowerPatch, token) {
			score += 15
		}
	}

	// Encourage larger patches when scores tie
	score += len(patch) / 500

	return score
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func toCamelCaseToken(val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}
	val = strings.TrimPrefix(strings.ToLower(val), "azurerm_")
	parts := strings.FieldsFunc(val, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(strings.ToLower(part[1:]))
		}
	}
	return strings.ToLower(b.String())
}

func trimPatchLines(patch string, maxLines int) (string, bool) {
	if maxLines <= 0 {
		return patch, false
	}
	lines := strings.Split(patch, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	return strings.Join(lines, "\n"), truncated
}

func formatReleaseSnippetResponse(repo *database.Repository, release *database.ProviderRelease, entry *database.ProviderReleaseEntry, filename, patch string, truncated bool, maxLines int, includeHeader, includeFile, includeDiff, includeCompare bool) string {
	var b strings.Builder
	fullName := repo.FullName
	if fullName == "" {
		fullName = repo.Name
	}

	if includeHeader {
		fmt.Fprintf(&b, "Release %s – %s\n", release.Version, entry.Title)
		fmt.Fprintf(&b, "Repository: %s\n", fullName)
	}
	if includeFile && filename != "" {
		fmt.Fprintf(&b, "File: %s\n", filename)
	}
	if includeDiff {
		b.WriteString("```diff\n")
		b.WriteString(patch)
		b.WriteString("\n```")
		if truncated {
			fmt.Fprintf(&b, "\n… showing first %d diff lines", maxLines)
		}
	}
	if includeCompare && release.ComparisonURL.Valid && release.ComparisonURL.String != "" {
		fmt.Fprintf(&b, "\nCompare: %s", release.ComparisonURL.String)
	}
	return b.String()
}

func fieldIncluded(fields []string, target string) bool {
	if len(fields) == 0 {
		return true
	}
	target = strings.ToLower(strings.TrimSpace(target))
	for _, f := range fields {
		if strings.ToLower(strings.TrimSpace(f)) == target {
			return true
		}
	}
	return false
}

func minimalReleaseSummary(repoName string, release *database.ProviderRelease) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release %s\nRepository: %s\n", release.Version, repoName)
	if release.Tag != "" {
		fmt.Fprintf(&b, "Tag: %s\n", release.Tag)
	}
	if release.ReleaseDate.Valid && release.ReleaseDate.String != "" {
		fmt.Fprintf(&b, "Date: %s\n", release.ReleaseDate.String)
	}
	return strings.TrimSpace(b.String())
}
