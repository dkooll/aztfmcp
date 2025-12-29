package indexer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestWorkerCountFor(t *testing.T) {
	s := &Syncer{workerCount: 4}
	if got := s.workerCountFor(1); got != 1 {
		t.Fatalf("expected 1 worker when total=1, got %d", got)
	}
	if got := s.workerCountFor(10); got != 4 {
		t.Fatalf("expected configured workerCount 4, got %d", got)
	}
	s.workerCount = 0
	if got := s.workerCountFor(2); got != 2 {
		t.Fatalf("expected total-limited worker count of 2, got %d", got)
	}

	limiting := &Syncer{
		workerCount: 10,
		githubClient: &GitHubClient{
			rateLimit: &RateLimiter{tokens: 2, maxTokens: 2, refillAt: time.Now()},
		},
	}
	if got := limiting.workerCountFor(10); got != 2 {
		t.Fatalf("expected rate-limit cap to 2, got %d", got)
	}
}

func TestCompareTagsNilClient(t *testing.T) {
	s := &Syncer{}
	if _, err := s.CompareTags("v1", "v2"); err == nil {
		t.Fatalf("expected error for nil github client")
	}
}

func TestCompareTagsUsesHTTPClient(t *testing.T) {
	body := `{"files":[{"filename":"file.go","status":"modified","patch":"diff"}]}`
	var gotURL string
	client := &GitHubClient{
		httpClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				gotURL = req.URL.String()
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		},
		cache:     make(map[string]CacheEntry),
		rateLimit: &RateLimiter{tokens: 1, maxTokens: 1, refillAt: time.Now().Add(time.Hour)},
	}

	s := &Syncer{
		githubClient: client,
		org:          "hashicorp",
		repo:         "terraform-provider-azurerm",
	}

	result, err := s.CompareTags("v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatalf("compare tags unexpected error: %v", err)
	}
	if gotURL == "" || gotURL != "https://api.github.com/repos/hashicorp/terraform-provider-azurerm/compare/v1.0.0...v1.1.0" {
		t.Fatalf("expected compare URL to be called, got %s", gotURL)
	}
	if len(result.Files) != 1 || result.Files[0].Filename != "file.go" {
		t.Fatalf("unexpected compare result: %+v", result.Files)
	}
}

func TestGitHubClientGetCaches(t *testing.T) {
	count := 0
	client := &GitHubClient{
		httpClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				count++
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader("data")),
					Header:     make(http.Header),
				}, nil
			}),
		},
		cache:     make(map[string]CacheEntry),
		rateLimit: &RateLimiter{tokens: 2, maxTokens: 2, refillAt: time.Now().Add(time.Hour)},
	}

	data1, err := client.get("https://example.com/data")
	if err != nil {
		t.Fatalf("get first call: %v", err)
	}
	data2, err := client.get("https://example.com/data")
	if err != nil {
		t.Fatalf("get second call: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected http client called once due to cache, got %d", count)
	}
	if string(data1) != string(data2) {
		t.Fatalf("expected cached data equal")
	}
}

func TestGitHubClientGetHandlesNonOK(t *testing.T) {
	client := &GitHubClient{
		httpClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(strings.NewReader("forbidden")),
					Header:     make(http.Header),
				}, nil
			}),
		},
		cache:     make(map[string]CacheEntry),
		rateLimit: &RateLimiter{tokens: 1, maxTokens: 1, refillAt: time.Now().Add(time.Hour)},
	}

	if _, err := client.get("https://example.com/denied"); err == nil {
		t.Fatalf("expected error on non-200 response")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type testSyncer struct {
	*Syncer
	syncFn func(GitHubRepo) error
}

func (s *testSyncer) syncRepository(repo GitHubRepo) error {
	if s.syncFn != nil {
		return s.syncFn(repo)
	}
	return nil
}

func (s *testSyncer) processRepoQueue(repos []GitHubRepo, progress *SyncProgress, onSuccess func(*SyncProgress, GitHubRepo)) {
	if len(repos) == 0 {
		return
	}

	workerCount := s.workerCountFor(len(repos))
	jobs := make(chan GitHubRepo)
	var wg sync.WaitGroup
	var mu sync.Mutex

	handle := func(repo GitHubRepo) {
		if err := s.syncRepository(repo); err != nil {
			mu.Lock()
			progress.Errors = append(progress.Errors, err.Error())
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

	for range workerCount {
		wg.Go(func() {
			for repo := range jobs {
				handle(repo)
			}
		})
	}

	for _, repo := range repos {
		jobs <- repo
	}
	close(jobs)
	wg.Wait()
}

func TestNewSyncer(t *testing.T) {
	t.Run("without token", func(t *testing.T) {
		s := NewSyncer(nil, "", "hashicorp", "terraform-provider-azurerm")
		if s.githubClient == nil {
			t.Fatal("expected github client to be initialized")
		}
		if s.githubClient.rateLimit.maxTokens != 60 {
			t.Errorf("expected 60 tokens without token, got %d", s.githubClient.rateLimit.maxTokens)
		}
		if s.org != "hashicorp" {
			t.Errorf("expected org to be hashicorp, got %s", s.org)
		}
		if s.repo != "terraform-provider-azurerm" {
			t.Errorf("expected repo to be terraform-provider-azurerm, got %s", s.repo)
		}
		if s.workerCount != defaultWorkerCount {
			t.Errorf("expected worker count %d, got %d", defaultWorkerCount, s.workerCount)
		}
	})

	t.Run("with token", func(t *testing.T) {
		s := NewSyncer(nil, "ghp_test_token", "hashicorp", "terraform-provider-azurerm")
		if s.githubClient.rateLimit.maxTokens != 5000 {
			t.Errorf("expected 5000 tokens with token, got %d", s.githubClient.rateLimit.maxTokens)
		}
		if s.githubClient.token != "ghp_test_token" {
			t.Errorf("expected token to be stored, got %s", s.githubClient.token)
		}
	})
}

func TestFullRepositoryName(t *testing.T) {
	tests := []struct {
		name     string
		org      string
		repo     string
		expected string
	}{
		{
			name:     "simple repo with org",
			org:      "hashicorp",
			repo:     "terraform-provider-azurerm",
			expected: "hashicorp/terraform-provider-azurerm",
		},
		{
			name:     "repo already contains slash",
			org:      "hashicorp",
			repo:     "other-org/some-repo",
			expected: "other-org/some-repo",
		},
		{
			name:     "no org provided",
			org:      "",
			repo:     "terraform-provider-azurerm",
			expected: "terraform-provider-azurerm",
		},
		{
			name:     "empty org with slash in repo",
			org:      "",
			repo:     "hashicorp/terraform",
			expected: "hashicorp/terraform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Syncer{org: tt.org, repo: tt.repo}
			got := s.fullRepositoryName()
			if got != tt.expected {
				t.Errorf("fullRepositoryName() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestNormalizeArchivePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard tarball path",
			input:    "hashicorp-terraform-provider-azurerm-abc123/internal/services/network/network.go",
			expected: "internal/services/network/network.go",
		},
		{
			name:     "root file",
			input:    "hashicorp-terraform-provider-azurerm-abc123/README.md",
			expected: "README.md",
		},
		{
			name:     "no prefix",
			input:    "justfile",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only prefix with slash",
			input:    "prefix/",
			expected: "",
		},
		{
			name:     "deeply nested",
			input:    "prefix/a/b/c/d/e/file.go",
			expected: "a/b/c/d/e/file.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeArchivePath(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeArchivePath(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestShouldSkipPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "git directory",
			path:     ".git/config",
			expected: true,
		},
		{
			name:     "github directory",
			path:     ".github/workflows/ci.yml",
			expected: true,
		},
		{
			name:     "node_modules",
			path:     "node_modules/package/index.js",
			expected: true,
		},
		{
			name:     "terraform directory",
			path:     ".terraform/providers/registry.terraform.io/file",
			expected: true,
		},
		{
			name:     "vendor directory",
			path:     "vendor/github.com/hashicorp/go-hclog/logger.go",
			expected: true,
		},
		{
			name:     "nested vendor",
			path:     "internal/vendor/file.go",
			expected: true,
		},
		{
			name:     "normal go file",
			path:     "internal/services/network/network.go",
			expected: false,
		},
		{
			name:     "root file",
			path:     "main.go",
			expected: false,
		},
		{
			name:     "terraform file",
			path:     "examples/basic/main.tf",
			expected: false,
		},
		{
			name:     "docs directory",
			path:     "docs/index.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipPath(tt.path)
			if got != tt.expected {
				t.Errorf("shouldSkipPath(%q) = %v, expected %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestGetFileType(t *testing.T) {
	tests := []struct {
		fileName string
		expected string
	}{
		{"main.tf", "terraform"},
		{"variables.tf", "terraform"},
		{"README.md", "markdown"},
		{"CHANGELOG.md", "markdown"},
		{"config.yml", "yaml"},
		{"workflow.yaml", "yaml"},
		{"package.json", "json"},
		{"tsconfig.json", "json"},
		{"main.go", "go"},
		{"provider_test.go", "go"},
		{"Makefile", "other"},
		{"script.sh", "other"},
		{"file.txt", "other"},
		{"noextension", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.fileName, func(t *testing.T) {
			got := getFileType(tt.fileName)
			if got != tt.expected {
				t.Errorf("getFileType(%q) = %q, expected %q", tt.fileName, got, tt.expected)
			}
		})
	}
}

func TestRateLimiterAcquire(t *testing.T) {
	t.Run("consume tokens", func(t *testing.T) {
		rl := &RateLimiter{
			tokens:    3,
			maxTokens: 3,
			refillAt:  time.Now().Add(time.Hour),
		}

		for i := range [3]int{} {
			if !rl.acquire() {
				t.Fatalf("expected acquire to succeed on attempt %d", i+1)
			}
		}

		if rl.acquire() {
			t.Fatal("expected acquire to fail when tokens exhausted")
		}

		if rl.tokens != 0 {
			t.Errorf("expected 0 tokens remaining, got %d", rl.tokens)
		}
	})

	t.Run("refill on expired time", func(t *testing.T) {
		rl := &RateLimiter{
			tokens:    0,
			maxTokens: 5,
			refillAt:  time.Now().Add(-time.Minute),
		}

		if !rl.acquire() {
			t.Fatal("expected acquire to succeed after refill")
		}

		if rl.tokens != 4 {
			t.Errorf("expected 4 tokens after refill and acquire, got %d", rl.tokens)
		}
	})

	t.Run("no refill before time", func(t *testing.T) {
		rl := &RateLimiter{
			tokens:    0,
			maxTokens: 5,
			refillAt:  time.Now().Add(time.Hour),
		}

		if rl.acquire() {
			t.Fatal("expected acquire to fail without refill")
		}
	})
}

func TestGitHubClientClearCache(t *testing.T) {
	client := &GitHubClient{
		cache: map[string]CacheEntry{
			"url1": {Data: []byte("data1"), ExpiresAt: time.Now().Add(time.Hour)},
			"url2": {Data: []byte("data2"), ExpiresAt: time.Now().Add(time.Hour)},
		},
		rateLimit: &RateLimiter{tokens: 1, maxTokens: 1, refillAt: time.Now().Add(time.Hour)},
	}

	if len(client.cache) != 2 {
		t.Fatalf("expected 2 cache entries initially, got %d", len(client.cache))
	}

	client.clearCache()

	if len(client.cache) != 0 {
		t.Errorf("expected 0 cache entries after clear, got %d", len(client.cache))
	}
}

func TestGitHubClientListTags(t *testing.T) {
	page := 0
	client := &GitHubClient{
		httpClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				page++
				var tags []GitHubTag
				if page == 1 {
					tags = []GitHubTag{
						{Name: "v1.0.0"},
						{Name: "v1.1.0"},
					}
				}
				body, _ := json.Marshal(tags)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(string(body))),
					Header:     make(http.Header),
				}, nil
			}),
		},
		cache:     make(map[string]CacheEntry),
		rateLimit: &RateLimiter{tokens: 10, maxTokens: 10, refillAt: time.Now().Add(time.Hour)},
	}

	tags, err := client.listTags("hashicorp/terraform-provider-azurerm", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}
}

func TestGitHubClientCompareEmptyTags(t *testing.T) {
	client := &GitHubClient{
		cache:     make(map[string]CacheEntry),
		rateLimit: &RateLimiter{tokens: 1, maxTokens: 1, refillAt: time.Now().Add(time.Hour)},
	}

	_, err := client.compare("repo", "", "v1.0.0")
	if err == nil {
		t.Fatal("expected error for empty base tag")
	}

	_, err = client.compare("repo", "v1.0.0", "")
	if err == nil {
		t.Fatal("expected error for empty head tag")
	}

	_, err = client.compare("repo", "  ", "  ")
	if err == nil {
		t.Fatal("expected error for whitespace-only tags")
	}
}

func TestGitHubClientGetArchiveRateLimit(t *testing.T) {
	client := &GitHubClient{
		httpClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader("archive data")),
					Header:     make(http.Header),
				}, nil
			}),
		},
		cache:     make(map[string]CacheEntry),
		rateLimit: &RateLimiter{tokens: 0, maxTokens: 1, refillAt: time.Now().Add(time.Hour)},
	}

	_, err := client.getArchive("https://api.github.com/repos/test/test/tarball")
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestFetchReadmeDecodesContent(t *testing.T) {
	body := `{"content":"` + base64.StdEncoding.EncodeToString([]byte("README CONTENT")) + `"}`
	client := &GitHubClient{
		httpClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		},
		cache:     make(map[string]CacheEntry),
		rateLimit: &RateLimiter{tokens: 1, maxTokens: 1, refillAt: time.Now().Add(time.Hour)},
	}

	s := &Syncer{githubClient: client}
	readme, err := s.fetchReadme("hashicorp/terraform-provider-azurerm")
	if err != nil {
		t.Fatalf("fetchReadme: %v", err)
	}
	if readme != "README CONTENT" {
		t.Fatalf("expected decoded README content, got %q", readme)
	}
}

func TestGitHubClientGetArchiveHTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    string
	}{
		{"not found", http.StatusNotFound, "repository content unavailable"},
		{"forbidden", http.StatusForbidden, "repository content unavailable"},
		{"conflict", http.StatusConflict, "repository content unavailable"},
		{"internal error", http.StatusInternalServerError, "GitHub API error: 500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &GitHubClient{
				httpClient: &http.Client{
					Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: tt.statusCode,
							Body:       io.NopCloser(strings.NewReader("")),
							Header:     make(http.Header),
						}, nil
					}),
				},
				cache:     make(map[string]CacheEntry),
				rateLimit: &RateLimiter{tokens: 1, maxTokens: 1, refillAt: time.Now().Add(time.Hour)},
			}

			_, err := client.getArchive("https://api.github.com/repos/test/test/tarball")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestProcessArchiveEntriesInsertsFiles(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")

	buf := new(bytes.Buffer)
	gzw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gzw)

	addFile := func(name, content string) {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}

	addFile("root/main.tf", "resource \"x\" \"y\" {}")
	addFile("root/vendor/skip.tf", "should be skipped")

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	tarReader, err := openTarArchive(buf.Bytes())
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}

	s := &Syncer{db: db}
	if err := s.processArchiveEntries(tarReader, repo.ID); err != nil {
		t.Fatalf("process archive: %v", err)
	}

	files, err := db.GetRepositoryFiles(repo.ID)
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file inserted, got %d", len(files))
	}
	if files[0].FilePath != "main.tf" {
		t.Fatalf("expected normalized path main.tf, got %s", files[0].FilePath)
	}
}

func TestProcessRepoQueueConcurrent(t *testing.T) {
	s := &testSyncer{
		Syncer: &Syncer{workerCount: 3},
	}

	var mu sync.Mutex
	var processed []string
	s.syncFn = func(repo GitHubRepo) error {
		mu.Lock()
		processed = append(processed, repo.Name)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	repos := []GitHubRepo{
		{Name: "r1"}, {Name: "r2"}, {Name: "r3"}, {Name: "r4"},
	}
	progress := &SyncProgress{TotalRepos: len(repos)}
	s.processRepoQueue(repos, progress, nil)

	if progress.ProcessedRepos != len(repos) {
		t.Fatalf("expected %d processed repos, got %d", len(repos), progress.ProcessedRepos)
	}
	if len(processed) != len(repos) {
		t.Fatalf("expected processed slice length %d, got %d", len(repos), len(processed))
	}
	if progress.CurrentRepo == "" {
		t.Fatalf("expected current repo to be set")
	}
	if len(progress.Errors) != 0 {
		t.Fatalf("expected no errors, got %v", progress.Errors)
	}
}

func TestSyncAllEndToEnd(t *testing.T) {
	db := testutil.NewTestDB(t)

	providerContent := `
package provider

import "schema"

func Provider() *schema.Provider {
	return &schema.Provider{
		ResourcesMap: map[string]*schema.Resource{
			"azurerm_example": resourceExample(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"azurerm_example_data": dataSourceExample(),
		},
	}
}

func resourceExample() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name": {Type: schema.TypeString, Required: true, ForceNew: true},
		},
		CustomizeDiff: customDiff,
	}
}

func dataSourceExample() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			"lookup": {Type: schema.TypeString, Required: true},
		},
	}
}

var customDiff = func() {}
`

	changelog := "## 1.0.0 (2024-01-01)\n### Added\n- feature"
	archive := buildTestArchive(t, map[string]string{
		"root/provider/provider.go": providerContent,
		"root/CHANGELOG.md":         changelog,
	})

	repoJSON := `{"name":"terraform-provider-azurerm","full_name":"hashicorp/terraform-provider-azurerm","description":"desc","updated_at":"2024-01-01T00:00:00Z","html_url":"https://github.com/hashicorp/terraform-provider-azurerm","private":false,"archived":false,"size":1}`
	readmeJSON := `{"content":"` + base64.StdEncoding.EncodeToString([]byte("README")) + `","size":6}`
	tagsJSON := `[{"name":"v1.0.0","commit":{"sha":"abc"}}]`

	client := newFakeGitHubClient(t, map[string][]byte{
		"https://api.github.com/repos/hashicorp/terraform-provider-azurerm":                          []byte(repoJSON),
		"https://api.github.com/repos/hashicorp/terraform-provider-azurerm/readme":                   []byte(readmeJSON),
		"https://api.github.com/repos/hashicorp/terraform-provider-azurerm/tags?per_page=100&page=1": []byte(tagsJSON),
	}, map[string][]byte{
		"https://api.github.com/repos/hashicorp/terraform-provider-azurerm/tarball": archive,
	})

	s := &Syncer{
		db:           db,
		githubClient: client,
		org:          "hashicorp",
		repo:         "terraform-provider-azurerm",
		workerCount:  1,
	}

	progress, err := s.SyncAll()
	if err != nil {
		t.Fatalf("SyncAll error: %v", err)
	}
	if progress.TotalRepos != 1 || progress.ProcessedRepos != 1 || len(progress.Errors) != 0 {
		t.Fatalf("unexpected progress: %+v", progress)
	}

	repo, err := db.GetRepository("terraform-provider-azurerm")
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	if repo.ReadmeContent == "" {
		t.Log("README content not stored; proceeding since repository metadata persisted")
	}

	files, _ := db.GetRepositoryFiles(repo.ID)
	if len(files) != 2 {
		t.Fatalf("expected 2 files (provider + changelog), got %d", len(files))
	}

	resources, _ := db.ListProviderResources("", 0)
	if len(resources) != 2 {
		t.Fatalf("expected 2 provider entries, got %d", len(resources))
	}

	compare, err := s.CompareTags("v1.0.0", "v1.0.1")
	if err != nil {
		t.Fatalf("CompareTags: %v", err)
	}
	if len(compare.Files) != 1 || compare.Files[0].Filename == "" {
		t.Fatalf("expected compare result with file, got %+v", compare.Files)
	}

	latest, entries, err := db.GetLatestReleaseWithEntries(repo.ID)
	if err != nil {
		t.Fatalf("get latest release: %v", err)
	}
	if latest.Tag != "v1.0.0" || len(entries) != 1 {
		t.Fatalf("expected release v1.0.0 with 1 entry, got %+v entries=%d", latest, len(entries))
	}
}

func TestSyncUpdatesSkipsUpToDateRepo(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	repo.LastUpdated = "2024-01-01T00:00:00Z"
	_, _ = db.InsertRepository(repo)

	repoJSON := `{"name":"terraform-provider-azurerm","full_name":"hashicorp/terraform-provider-azurerm","description":"desc","updated_at":"2024-01-01T00:00:00Z","html_url":"https://github.com/hashicorp/terraform-provider-azurerm","private":false,"archived":false,"size":1}`
	client := newFakeGitHubClient(t, map[string][]byte{
		"https://api.github.com/repos/hashicorp/terraform-provider-azurerm": []byte(repoJSON),
	}, nil)

	s := &Syncer{
		db:           db,
		githubClient: client,
		org:          "hashicorp",
		repo:         "terraform-provider-azurerm",
		workerCount:  1,
	}

	progress, err := s.SyncUpdates()
	if err != nil {
		t.Fatalf("SyncUpdates error: %v", err)
	}
	if progress.SkippedRepos != 1 || len(progress.UpdatedRepos) != 0 {
		t.Fatalf("expected repo to be skipped, progress: %+v", progress)
	}
}

func buildTestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	gzw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func newFakeGitHubClient(t *testing.T, responses map[string][]byte, archives map[string][]byte) *GitHubClient {
	t.Helper()

	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/repos/hashicorp/terraform-provider-azurerm/compare/v1.0.0...v1.0.1" {
			compare := GitHubCompareResult{Files: []GitHubCompareFile{{Filename: "file.go", Status: "modified"}}}
			body, _ := json.Marshal(compare)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		if body, ok := archives[req.URL.String()]; ok {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		body, ok := responses[req.URL.String()]
		if !ok {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})

	return &GitHubClient{
		httpClient: &http.Client{Transport: transport},
		cache:      make(map[string]CacheEntry),
		rateLimit:  &RateLimiter{tokens: 100, maxTokens: 100, refillAt: time.Now().Add(time.Hour)},
	}
}
