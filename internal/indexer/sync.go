// Package indexer handles synchronization of the AzureRM provider repository content from GitHub.
package indexer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dkooll/aztfmcp/internal/database"
)

type Syncer struct {
	db           *database.DB
	githubClient *GitHubClient
	org          string
	repo         string
	workerCount  int
}

const defaultWorkerCount = 4

type GitHubRepo struct {
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
	HTMLURL     string `json:"html_url"`
	Private     bool   `json:"private"`
	Archived    bool   `json:"archived"`
	Size        int    `json:"size"`
}

type GitHubContent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
	Content     string `json:"content"`
	Size        int64  `json:"size"`
}

type GitHubTag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
		URL string `json:"url"`
	} `json:"commit"`
}

type GitHubCompareResult struct {
	Files []GitHubCompareFile `json:"files"`
}

type GitHubCompareFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Patch    string `json:"patch"`
}

type GitHubClient struct {
	httpClient *http.Client
	cache      map[string]CacheEntry
	cacheMutex sync.RWMutex
	rateLimit  *RateLimiter
	token      string
}

type CacheEntry struct {
	Data      any
	ExpiresAt time.Time
}

type RateLimiter struct {
	tokens    int
	maxTokens int
	refillAt  time.Time
	mutex     sync.Mutex
}

type SyncProgress struct {
	TotalRepos     int
	ProcessedRepos int
	SkippedRepos   int
	CurrentRepo    string
	Errors         []string
	UpdatedRepos   []string
}

var ErrRepoContentUnavailable = errors.New("repository content unavailable")

func NewSyncer(db *database.DB, token string, org string, repo string) *Syncer {
	client := &GitHubClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]CacheEntry),
		rateLimit:  &RateLimiter{tokens: 60, maxTokens: 60, refillAt: time.Now().Add(time.Hour)},
		token:      token,
	}

	if token != "" {
		client.rateLimit.maxTokens = 5000
		client.rateLimit.tokens = 5000
	}

	return &Syncer{
		db:           db,
		githubClient: client,
		org:          org,
		repo:         repo,
		workerCount:  defaultWorkerCount,
	}
}

func (s *Syncer) workerCountFor(total int) int {
	if total <= 1 {
		if total < 1 {
			return 0
		}
		return 1
	}

	count := s.workerCount
	if count <= 0 {
		count = defaultWorkerCount
	}

	if s.githubClient != nil && s.githubClient.rateLimit != nil && s.githubClient.rateLimit.maxTokens > 0 && count > s.githubClient.rateLimit.maxTokens {
		count = s.githubClient.rateLimit.maxTokens
	}

	if count > total {
		count = total
	}

	if count < 1 {
		count = 1
	}

	return count
}

func (s *Syncer) fullRepositoryName() string {
	if strings.Contains(s.repo, "/") {
		return s.repo
	}
	if s.org != "" {
		return fmt.Sprintf("%s/%s", s.org, s.repo)
	}
	return s.repo
}

func (s *Syncer) CompareTags(baseTag, headTag string) (*GitHubCompareResult, error) {
	if s.githubClient == nil {
		return nil, fmt.Errorf("github client is not initialized")
	}
	return s.githubClient.compare(s.fullRepositoryName(), baseTag, headTag)
}

func (s *Syncer) SyncAll() (*SyncProgress, error) {
	progress := &SyncProgress{}

	log.Println("Fetching repositories from GitHub...")
	repos, err := s.fetchRepositories()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repositories: %w", err)
	}

	progress.TotalRepos = len(repos)
	log.Printf("Found %d repositories", len(repos))

	s.processRepoQueue(repos, progress, nil)

	log.Printf("Sync completed: %d/%d repositories synced successfully",
		progress.ProcessedRepos-len(progress.Errors), progress.TotalRepos)

	return progress, nil
}

func (s *Syncer) SyncUpdates() (*SyncProgress, error) {
	progress := &SyncProgress{}

	s.githubClient.clearCache()
	log.Println("Fetching repositories from GitHub (cache cleared)...")
	repos, err := s.fetchRepositories()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repositories: %w", err)
	}

	progress.TotalRepos = len(repos)
	log.Printf("Found %d repositories", len(repos))

	reposToSync := make([]GitHubRepo, 0, len(repos))

	for _, repo := range repos {
		progress.CurrentRepo = repo.Name

		existingRepository, err := s.db.GetRepository(repo.Name)
		if err != nil {
			log.Printf("Repository %s not found in DB (error: %v), will sync", repo.Name, err)
			reposToSync = append(reposToSync, repo)
			continue
		}

		if existingRepository == nil {
			log.Printf("Repository %s not found in DB (nil), will sync", repo.Name)
			reposToSync = append(reposToSync, repo)
			continue
		}

		if existingRepository.LastUpdated == repo.UpdatedAt {
			log.Printf("Skipping %s (already up-to-date)", repo.Name)
			progress.SkippedRepos++
			progress.ProcessedRepos++
			continue
		}

		log.Printf("Repository %s needs update: DB='%s' vs GitHub='%s'", repo.Name, existingRepository.LastUpdated, repo.UpdatedAt)
		reposToSync = append(reposToSync, repo)
	}

	onSuccess := func(p *SyncProgress, repo GitHubRepo) {
		p.UpdatedRepos = append(p.UpdatedRepos, repo.Name)
	}

	s.processRepoQueue(reposToSync, progress, onSuccess)

	syncedCount := len(progress.UpdatedRepos)

	log.Printf("Sync completed: %d/%d repositories synced, %d skipped (up-to-date), %d errors",
		syncedCount, progress.TotalRepos, progress.SkippedRepos, len(progress.Errors))

	return progress, nil
}

func (s *Syncer) processRepoQueue(repos []GitHubRepo, progress *SyncProgress, onSuccess func(*SyncProgress, GitHubRepo)) {
	if len(repos) == 0 {
		return
	}

	workerCount := s.workerCountFor(len(repos))
	var startedCounter atomic.Int64
	var mu sync.Mutex
	startOffset := int64(progress.ProcessedRepos)

	handleRepo := func(repo GitHubRepo) {
		seq := startOffset + startedCounter.Add(1)
		log.Printf("Syncing repository: %s (%d/%d)", repo.Name, seq, progress.TotalRepos)

		mu.Lock()
		progress.CurrentRepo = repo.Name
		mu.Unlock()

		err := s.syncRepository(repo)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to sync %s: %v", repo.Name, err)
			log.Println(errMsg)
			mu.Lock()
			progress.Errors = append(progress.Errors, errMsg)
			progress.ProcessedRepos++
			progress.CurrentRepo = repo.Name
			mu.Unlock()
			return
		}

		mu.Lock()
		progress.ProcessedRepos++
		progress.CurrentRepo = repo.Name
		if onSuccess != nil {
			onSuccess(progress, repo)
		}
		mu.Unlock()
	}

	if workerCount <= 1 {
		for _, repo := range repos {
			handleRepo(repo)
		}
		return
	}

	jobs := make(chan GitHubRepo)
	var wg sync.WaitGroup

	for range workerCount {
		wg.Go(func() {
			for repo := range jobs {
				handleRepo(repo)
			}
		})
	}

	for _, repo := range repos {
		jobs <- repo
	}

	close(jobs)
	wg.Wait()
}

func (s *Syncer) fetchRepositories() ([]GitHubRepo, error) {
	repo, err := s.fetchRepositoryByName(s.repo)
	if err != nil {
		return nil, err
	}
	return []GitHubRepo{repo}, nil
}

func (s *Syncer) fetchRepositoryByName(name string) (GitHubRepo, error) {
	target := name
	if !strings.Contains(name, "/") && s.org != "" {
		target = fmt.Sprintf("%s/%s", s.org, name)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s", target)
	data, err := s.githubClient.get(url)
	if err != nil {
		return GitHubRepo{}, err
	}

	var repo GitHubRepo
	if err := json.Unmarshal(data, &repo); err != nil {
		return GitHubRepo{}, err
	}

	if repo.Private {
		return GitHubRepo{}, fmt.Errorf("repository %s is private", target)
	}
	if repo.Archived {
		return GitHubRepo{}, fmt.Errorf("repository %s is archived", target)
	}
	if repo.Size <= 0 {
		return GitHubRepo{}, fmt.Errorf("repository %s is empty", target)
	}

	return repo, nil
}

func (s *Syncer) syncRepository(repo GitHubRepo) error {
	repositoryID, err := s.insertRepositoryMetadata(repo)
	if err != nil {
		return err
	}

	if err := s.clearExistingRepositoryData(repositoryID); err != nil {
		log.Printf("Warning: failed to clear old data for %s: %v", repo.Name, err)
	}

	if err := s.syncReadme(repositoryID, repo); err != nil {
		log.Printf("Warning: failed to fetch README for %s: %v", repo.Name, err)
	}

	if err := s.syncRepositoryContent(repositoryID, repo); err != nil {
		if errors.Is(err, ErrRepoContentUnavailable) {
			return s.handleUnavailableRepo(repositoryID, repo.Name)
		}
		return fmt.Errorf("failed to sync files: %w", err)
	}

	if err := s.parseProviderRepository(repositoryID, repo); err != nil {
		log.Printf("Warning: failed to parse provider resources for %s: %v", repo.Name, err)
	}

	if err := s.captureReleaseMetadata(repositoryID, repo); err != nil {
		log.Printf("Warning: failed to ingest release metadata for %s: %v", repo.Name, err)
	}

	if err := s.persistRepositoryTags(repositoryID); err != nil {
		log.Printf("Warning: failed to persist tags for %s: %v", repo.Name, err)
	}

	if err := s.persistRepositoryAliases(repositoryID); err != nil {
		log.Printf("Warning: failed to persist aliases for %s: %v", repo.Name, err)
	}

	return nil
}

func (s *Syncer) insertRepositoryMetadata(repo GitHubRepo) (int64, error) {
	repository := &database.Repository{
		Name:        repo.Name,
		FullName:    repo.FullName,
		Description: repo.Description,
		RepoURL:     repo.HTMLURL,
		LastUpdated: repo.UpdatedAt,
	}

	repositoryID, err := s.db.InsertRepository(repository)
	if err != nil {
		return 0, fmt.Errorf("failed to insert repository: %w", err)
	}

	return repositoryID, nil
}

func (s *Syncer) clearExistingRepositoryData(repositoryID int64) error {
	existingRepository, _ := s.db.GetRepositoryByID(repositoryID)
	if existingRepository != nil && existingRepository.ID != 0 {
		return s.db.ClearRepositoryData(repositoryID)
	}
	return nil
}

func (s *Syncer) syncReadme(repositoryID int64, repo GitHubRepo) error {
	readme, err := s.fetchReadme(repo.FullName)
	if err != nil {
		return err
	}

	repository := &database.Repository{
		ID:            repositoryID,
		Name:          repo.Name,
		FullName:      repo.FullName,
		Description:   repo.Description,
		RepoURL:       repo.HTMLURL,
		LastUpdated:   repo.UpdatedAt,
		ReadmeContent: readme,
	}

	_, err = s.db.InsertRepository(repository)
	return err
}

func (s *Syncer) syncRepositoryContent(repositoryID int64, repo GitHubRepo) error {
	return s.syncRepositoryFromArchive(repositoryID, repo)
}

func (s *Syncer) handleUnavailableRepo(repositoryID int64, repoName string) error {
	log.Printf("Skipping %s: repository content unavailable", repoName)
	if delErr := s.db.DeleteRepositoryByID(repositoryID); delErr != nil {
		log.Printf("Warning: failed to delete repository record for %s: %v", repoName, delErr)
	}
	return nil
}

func (s *Syncer) persistRepositoryTags(repositoryID int64) error {
	_ = repositoryID
	return nil
}

func (s *Syncer) persistRepositoryAliases(repositoryID int64) error {
	_ = repositoryID
	return nil
}

func (s *Syncer) syncRepositoryFromArchive(repositoryID int64, repo GitHubRepo) error {
	archiveURL := fmt.Sprintf("https://api.github.com/repos/%s/tarball", repo.FullName)
	data, err := s.githubClient.getArchive(archiveURL)
	if err != nil {
		if errors.Is(err, ErrRepoContentUnavailable) {
			return ErrRepoContentUnavailable
		}
		return err
	}

	tarReader, err := openTarArchive(data)
	if err != nil {
		return err
	}

	return s.processArchiveEntries(tarReader, repositoryID)
}

func openTarArchive(data []byte) (*tar.Reader, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to open archive: %w", err)
	}
	return tar.NewReader(gzipReader), nil
}

func (s *Syncer) processArchiveEntries(tarReader *tar.Reader, repositoryID int64) error {
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}

		if !isRegularFile(header.Typeflag) {
			continue
		}

		relativePath := normalizeArchivePath(header.Name)
		if relativePath == "" || shouldSkipPath(relativePath) {
			continue
		}

		contentBytes, err := io.ReadAll(tarReader)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", relativePath, err)
		}

		if err := s.insertRepositoryFile(repositoryID, relativePath, header.Size, contentBytes); err != nil {
			log.Printf("Warning: failed to insert file %s: %v", relativePath, err)
		}
	}

	return nil
}

func (s *Syncer) insertRepositoryFile(repositoryID int64, relativePath string, size int64, content []byte) error {
	fileName := path.Base(relativePath)
	file := &database.RepositoryFile{
		RepositoryID: repositoryID,
		FileName:     fileName,
		FilePath:     relativePath,
		FileType:     getFileType(fileName),
		Content:      string(content),
		SizeBytes:    size,
	}

	return s.db.InsertFile(file)
}

func normalizeArchivePath(name string) string {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func shouldSkipPath(relativePath string) bool {
	skipDirs := map[string]struct{}{
		".git":         {},
		".github":      {},
		"node_modules": {},
		".terraform":   {},
		"vendor":       {},
	}

	rest := relativePath
	for {
		seg, r, ok := strings.Cut(rest, "/")
		if _, skip := skipDirs[seg]; skip {
			return true
		}
		if !ok {
			break
		}
		rest = r
	}

	return false
}

func isRegularFile(typeFlag byte) bool {
	return typeFlag == tar.TypeReg
}

func (s *Syncer) fetchReadme(repoFullName string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/readme", repoFullName)
	data, err := s.githubClient.get(url)
	if err != nil {
		return "", err
	}

	var content GitHubContent
	if err := json.Unmarshal(data, &content); err != nil {
		return "", err
	}

	return s.fetchFileContent(content)
}

func (s *Syncer) fetchFileContent(content GitHubContent) (string, error) {
	if content.DownloadURL != "" {
		data, err := s.githubClient.get(content.DownloadURL)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	if content.Content != "" {
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}

	return "", fmt.Errorf("no content available")
}

func getFileType(fileName string) string {
	if strings.HasSuffix(fileName, ".tf") {
		return "terraform"
	} else if strings.HasSuffix(fileName, ".md") {
		return "markdown"
	} else if strings.HasSuffix(fileName, ".yml") || strings.HasSuffix(fileName, ".yaml") {
		return "yaml"
	} else if strings.HasSuffix(fileName, ".json") {
		return "json"
	} else if strings.HasSuffix(fileName, ".go") {
		return "go"
	}
	return "other"
}

func (rl *RateLimiter) acquire() bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	if time.Now().After(rl.refillAt) {
		rl.tokens = rl.maxTokens
		rl.refillAt = time.Now().Add(time.Hour)
	}

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}
	return false
}

func (gc *GitHubClient) clearCache() {
	gc.cacheMutex.Lock()
	gc.cache = make(map[string]CacheEntry)
	gc.cacheMutex.Unlock()
}

func (gc *GitHubClient) get(url string) ([]byte, error) {
	gc.cacheMutex.RLock()
	if entry, exists := gc.cache[url]; exists && time.Now().Before(entry.ExpiresAt) {
		gc.cacheMutex.RUnlock()
		if data, ok := entry.Data.([]byte); ok {
			return data, nil
		}
	}
	gc.cacheMutex.RUnlock()

	if !gc.rateLimit.acquire() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if gc.token != "" {
		req.Header.Set("Authorization", "token "+gc.token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "az-cn-azurerm-mcp/1.0.0")

	resp, err := gc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	gc.cacheMutex.Lock()
	gc.cache[url] = CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	gc.cacheMutex.Unlock()

	return data, nil
}

func (gc *GitHubClient) listTags(repoFullName string, maxPages int) ([]GitHubTag, error) {
	if maxPages <= 0 {
		maxPages = 1
	}
	var tags []GitHubTag
	for page := 1; page <= maxPages; page++ {
		endpoint := fmt.Sprintf("https://api.github.com/repos/%s/tags?per_page=100&page=%d", repoFullName, page)
		data, err := gc.get(endpoint)
		if err != nil {
			return nil, err
		}
		var batch []GitHubTag
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, err
		}
		tags = append(tags, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return tags, nil
}

func (gc *GitHubClient) compare(repoFullName, base, head string) (*GitHubCompareResult, error) {
	base = strings.TrimSpace(base)
	head = strings.TrimSpace(head)
	if base == "" || head == "" {
		return nil, fmt.Errorf("base and head tags are required")
	}
	compareURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/compare/%s...%s",
		repoFullName,
		url.PathEscape(base),
		url.PathEscape(head),
	)
	data, err := gc.get(compareURL)
	if err != nil {
		return nil, err
	}
	var result GitHubCompareResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (gc *GitHubClient) getArchive(url string) ([]byte, error) {
	if !gc.rateLimit.acquire() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if gc.token != "" {
		req.Header.Set("Authorization", "token "+gc.token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "az-cn-azurerm-mcp/1.0.0")

	resp, err := gc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("%w: status %d", ErrRepoContentUnavailable, resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
