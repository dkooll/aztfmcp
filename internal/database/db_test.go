package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			t.Skipf("sqlite build without fts5: %v", err)
		}
		t.Fatalf("failed to create db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInsertAndFetchRepository(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	id, err := db.InsertRepository(repo)
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	fetched, err := db.GetRepository("terraform-provider-azurerm")
	if err != nil || fetched.ID != id {
		t.Fatalf("get repo: %v %+v", err, fetched)
	}

	byID, err := db.GetRepositoryByID(id)
	if err != nil || byID == nil || byID.ID != id {
		t.Fatalf("get repo by id: %v %+v", err, byID)
	}

	list, err := db.ListRepositories()
	if err != nil || len(list) == 0 {
		t.Fatalf("list repositories: %v len=%d", err, len(list))
	}
}

func TestInsertResourceAndAttributes(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	res := &ProviderResource{RepositoryID: repoID, Name: "azurerm_example", Kind: "resource"}
	resID, err := db.InsertProviderResource(res)
	if err != nil {
		t.Fatalf("insert resource: %v", err)
	}

	attr := &ProviderAttribute{ResourceID: resID, Name: "name", Required: true}
	if err := db.InsertProviderAttribute(attr); err != nil {
		t.Fatalf("insert attr: %v", err)
	}

	gotRes, err := db.GetProviderResource("azurerm_example")
	if err != nil || gotRes.ID != resID {
		t.Fatalf("get resource: %v %+v", err, gotRes)
	}
	attrs, err := db.GetProviderResourceAttributes(resID)
	if err != nil || len(attrs) != 1 || attrs[0].Name != "name" {
		t.Fatalf("get attrs: %v %+v", err, attrs)
	}
}

func TestUpsertReleaseAndEntries(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	rel := &ProviderRelease{RepositoryID: repoID, Version: "1.0.0", Tag: "v1.0.0"}
	relID, err := db.UpsertProviderRelease(rel)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	entries := []ProviderReleaseEntry{
		{EntryKey: "key1", Title: "Added", Section: "Features"},
	}
	if err := db.ReplaceReleaseEntries(relID, entries); err != nil {
		t.Fatalf("replace entries: %v", err)
	}

	gotRel, gotEntries, err := db.GetReleaseWithEntriesByVersion(repoID, "1.0.0")
	if err != nil {
		t.Fatalf("get release: %v", err)
	}
	if gotRel.ID != relID || len(gotEntries) != 1 || gotEntries[0].EntryKey != "key1" {
		t.Fatalf("unexpected release/entries: %+v %+v", gotRel, gotEntries)
	}

	latest, entries, err := db.GetLatestReleaseWithEntries(repoID)
	if err != nil || latest.ID != relID || len(entries) != 1 {
		t.Fatalf("latest release fetch failed: %v %+v", err, entries)
	}
}

func TestSearchProviderResourcesFTS(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)
	res := &ProviderResource{
		RepositoryID: repoID,
		Name:         "azurerm_example",
		Kind:         "resource",
		Description:  sql.NullString{Valid: true, String: "example resource"},
	}
	if _, err := db.InsertProviderResource(res); err != nil {
		t.Fatalf("insert resource: %v", err)
	}

	results, err := db.SearchProviderResources("example", 5)
	if err != nil {
		if strings.Contains(err.Error(), "fts") {
			t.Skipf("sqlite build without FTS support: %v", err)
		}
		t.Fatalf("search resources: %v", err)
	}
	if len(results) != 1 || results[0].Name != "azurerm_example" {
		t.Fatalf("expected one resource match, got %+v", results)
	}

	// Test that escapeFTS5 handles special characters gracefully
	if _, err := db.SearchProviderResources("\"", 5); err != nil {
		t.Fatalf("escapeFTS5 should handle quotes: %v", err)
	}
}

func TestSearchProviderAttributesFilters(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)
	res := &ProviderResource{RepositoryID: repoID, Name: "azurerm_example", Kind: "resource"}
	resID, _ := db.InsertProviderResource(res)

	attrRequired := &ProviderAttribute{ResourceID: resID, Name: "name", Required: true}
	attrOptional := &ProviderAttribute{ResourceID: resID, Name: "opt", Optional: true}
	if err := db.InsertProviderAttribute(attrRequired); err != nil {
		t.Fatalf("insert required attr: %v", err)
	}
	if err := db.InsertProviderAttribute(attrOptional); err != nil {
		t.Fatalf("insert optional attr: %v", err)
	}

	results, err := db.SearchProviderAttributes(AttributeSearchFilters{
		Flags: []string{"required"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search attrs: %v", err)
	}
	if len(results) != 1 || results[0].Attribute.Name != "name" {
		t.Fatalf("expected required attribute only, got %+v", results)
	}

	results, err = db.SearchProviderAttributes(AttributeSearchFilters{
		NameContains:   "opt",
		ResourcePrefix: "azurerm_",
		Limit:          10,
	})
	if err != nil || len(results) != 1 || results[0].Attribute.Name != "opt" {
		t.Fatalf("expected filtered optional attribute, got %+v err=%v", results, err)
	}
}

func TestSearchFilesAndGetFile(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)
	file := &RepositoryFile{
		RepositoryID: repoID,
		FileName:     "example.go",
		FilePath:     "path/example.go",
		FileType:     "go",
		Content:      "package main\n// example content",
		SizeBytes:    32,
	}
	if err := db.InsertFile(file); err != nil {
		t.Fatalf("insert file: %v", err)
	}

	files, err := db.SearchFiles("example", 5)
	if err != nil || len(files) == 0 {
		t.Fatalf("expected search files result, got err=%v len=%d", err, len(files))
	}

	ftsFiles, err := db.SearchFilesFTS("\"example\"", 5)
	if err != nil || len(ftsFiles) == 0 {
		t.Fatalf("expected search files fts result, got err=%v len=%d", err, len(ftsFiles))
	}

	got, err := db.GetFile(repo.Name, "path/example.go")
	if err != nil || got.FileName != "example.go" {
		t.Fatalf("get file failed: %v %+v", err, got)
	}
}

func TestInsertFileDuplicateHandling(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	file := &RepositoryFile{
		RepositoryID: repoID,
		FileName:     "main.go",
		FilePath:     "path/main.go",
		FileType:     "go",
		Content:      "original content",
		SizeBytes:    16,
	}
	if err := db.InsertFile(file); err != nil {
		t.Fatalf("initial insert: %v", err)
	}

	file.Content = "updated content"
	file.SizeBytes = 15
	if err := db.InsertFile(file); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := db.GetFile(repo.Name, "path/main.go")
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if got.Content != "updated content" || got.SizeBytes != 15 {
		t.Errorf("expected upsert to update content, got content=%q size=%d", got.Content, got.SizeBytes)
	}
}

func TestGetRepositoryFiles(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	files, err := db.GetRepositoryFiles(repoID)
	if err != nil {
		t.Fatalf("get empty files: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no files, got %d", len(files))
	}

	for i := range [3]int{} {
		f := &RepositoryFile{
			RepositoryID: repoID,
			FileName:     "file" + string(rune('0'+i)) + ".go",
			FilePath:     "path/file" + string(rune('0'+i)) + ".go",
			FileType:     "go",
			Content:      "content",
			SizeBytes:    7,
		}
		if err := db.InsertFile(f); err != nil {
			t.Fatalf("insert file %d: %v", i, err)
		}
	}

	files, err = db.GetRepositoryFiles(repoID)
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestUpsertAndGetProviderResourceSource(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	res := &ProviderResource{RepositoryID: repoID, Name: "azurerm_example", Kind: "resource"}
	resID, _ := db.InsertProviderResource(res)

	_, err := db.GetProviderResourceSource(resID)
	if err == nil {
		t.Fatal("expected error for missing source")
	}

	err = db.UpsertProviderResourceSource(
		resID,
		"resourceArmExample",
		"path/example.go",
		"func resourceArmExample() {}",
		"schema: map[string]*Schema{}",
		"customizeDiff: func(){}",
		`{"Create": "30m"}`,
		"[]StateUpgrade{}",
		"importerValidatingResourceId()",
	)
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}

	src, err := db.GetProviderResourceSource(resID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src.FunctionName.String != "resourceArmExample" {
		t.Errorf("expected function name, got %s", src.FunctionName.String)
	}
	if src.SchemaSnippet.String != "schema: map[string]*Schema{}" {
		t.Errorf("expected schema snippet, got %s", src.SchemaSnippet.String)
	}
	if src.CustomizeDiffSnippet.String != "customizeDiff: func(){}" {
		t.Errorf("expected customize diff, got %s", src.CustomizeDiffSnippet.String)
	}
	if src.TimeoutsJSON.String != `{"Create": "30m"}` {
		t.Errorf("expected timeouts, got %s", src.TimeoutsJSON.String)
	}
	if src.ImporterSnippet.String != "importerValidatingResourceId()" {
		t.Errorf("expected importer, got %s", src.ImporterSnippet.String)
	}
}

func TestClearRepositoryData(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, err := db.InsertRepository(repo)
	if err != nil {
		t.Fatalf("insert repository: %v", err)
	}

	res := &ProviderResource{RepositoryID: repoID, Name: "azurerm_example", Kind: "resource"}
	resID, err := db.InsertProviderResource(res)
	if err != nil {
		t.Fatalf("insert resource: %v", err)
	}

	attr := &ProviderAttribute{ResourceID: resID, Name: "name", Required: true}
	if err := db.InsertProviderAttribute(attr); err != nil {
		t.Fatalf("insert attribute: %v", err)
	}

	file := &RepositoryFile{RepositoryID: repoID, FileName: "test.go", FilePath: "test.go", FileType: "go", Content: "x", SizeBytes: 1}
	if err := db.InsertFile(file); err != nil {
		t.Fatalf("insert file: %v", err)
	}

	rel := &ProviderRelease{RepositoryID: repoID, Version: "1.0.0", Tag: "v1.0.0"}
	if _, err := db.UpsertProviderRelease(rel); err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	// Verify data exists before clearing
	preResources, err := db.ListProviderResources("", 100)
	if err != nil {
		t.Fatalf("list resources before clear: %v", err)
	}
	if len(preResources) != 1 {
		t.Errorf("expected 1 resource before clear, got %d", len(preResources))
	}

	if err := db.ClearRepositoryData(repoID); err != nil {
		t.Fatalf("clear data: %v", err)
	}

	// Verify all data was cleared
	resources, err := db.ListProviderResources("", 100)
	if err != nil {
		t.Fatalf("list resources after clear: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources after clear, got %d", len(resources))
	}

	files, err := db.GetRepositoryFiles(repoID)
	if err != nil {
		t.Fatalf("get files after clear: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files after clear, got %d", len(files))
	}

	_, err = db.GetLatestProviderRelease(repoID)
	if err == nil {
		t.Error("expected no release after clear")
	}

	// Verify repository itself still exists
	stillExists, err := db.GetRepositoryByID(repoID)
	if err != nil {
		t.Errorf("repository should still exist after clear: %v", err)
	}
	if stillExists.Name != "terraform-provider-azurerm" {
		t.Errorf("repository name changed: got %s", stillExists.Name)
	}
}

func TestDeleteRepositoryByID(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	err := db.DeleteRepositoryByID(repoID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = db.GetRepositoryByID(repoID)
	if err == nil {
		t.Error("expected error for deleted repository")
	}

	err = db.DeleteRepositoryByID(99999)
	if err != nil {
		t.Error("delete of nonexistent ID should not error")
	}
}

func TestGetProviderReleaseByTag(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	_, err := db.GetProviderReleaseByTag(repoID, "v1.0.0")
	if err == nil {
		t.Fatal("expected error for missing release")
	}

	rel := &ProviderRelease{RepositoryID: repoID, Version: "1.0.0", Tag: "v1.0.0"}
	_, _ = db.UpsertProviderRelease(rel)

	got, err := db.GetProviderReleaseByTag(repoID, "v1.0.0")
	if err != nil {
		t.Fatalf("get by tag: %v", err)
	}
	if got.Version != "1.0.0" || got.Tag != "v1.0.0" {
		t.Errorf("unexpected release: %+v", got)
	}
}

func TestGetReleaseWithEntriesByTag(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	rel := &ProviderRelease{RepositoryID: repoID, Version: "1.0.0", Tag: "v1.0.0"}
	relID, _ := db.UpsertProviderRelease(rel)

	entries := []ProviderReleaseEntry{
		{EntryKey: "feat-001", Title: "Added feature", Section: "Features"},
		{EntryKey: "bug-001", Title: "Fixed bug", Section: "Bug Fixes"},
	}
	_ = db.ReplaceReleaseEntries(relID, entries)

	release, gotEntries, err := db.GetReleaseWithEntriesByTag(repoID, "v1.0.0")
	if err != nil {
		t.Fatalf("get release by tag: %v", err)
	}
	if release.Version != "1.0.0" {
		t.Errorf("unexpected version: %s", release.Version)
	}
	if len(gotEntries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(gotEntries))
	}
}

func TestParseCacheOperations(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetParseCacheEntry("nonexistent.go")
	if err == nil {
		t.Fatal("expected error for missing cache entry")
	}

	entry := &ParseCacheEntry{
		FilePath:       "path/resource.go",
		ContentHash:    "abc123",
		ResourceCount:  5,
		AttributeCount: 25,
	}
	if err := db.UpsertParseCacheEntry(entry); err != nil {
		t.Fatalf("upsert cache: %v", err)
	}

	got, err := db.GetParseCacheEntry("path/resource.go")
	if err != nil {
		t.Fatalf("get cache: %v", err)
	}
	if got.ContentHash != "abc123" || got.ResourceCount != 5 || got.AttributeCount != 25 {
		t.Errorf("unexpected cache entry: %+v", got)
	}

	entry.ContentHash = "def456"
	entry.ResourceCount = 10
	if err := db.UpsertParseCacheEntry(entry); err != nil {
		t.Fatalf("update cache: %v", err)
	}

	got, err = db.GetParseCacheEntry("path/resource.go")
	if err != nil {
		t.Fatalf("get updated cache: %v", err)
	}
	if got.ContentHash != "def456" || got.ResourceCount != 10 {
		t.Errorf("unexpected updated cache: %+v", got)
	}
}

func TestListProviderResourcesFilters(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	resources := []*ProviderResource{
		{RepositoryID: repoID, Name: "azurerm_storage_account", Kind: "resource"},
		{RepositoryID: repoID, Name: "azurerm_virtual_network", Kind: "resource"},
		{RepositoryID: repoID, Name: "azurerm_storage_account", Kind: "data_source"},
	}
	for _, r := range resources {
		if _, err := db.InsertProviderResource(r); err != nil {
			t.Fatalf("insert resource: %v", err)
		}
	}

	all, err := db.ListProviderResources("", 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 resources, got %d", len(all))
	}

	onlyResources, err := db.ListProviderResources("resource", 0)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(onlyResources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(onlyResources))
	}

	limited, err := db.ListProviderResources("", 1)
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("expected 1 resource with limit, got %d", len(limited))
	}
}

func TestSearchProviderAttributesAdvancedFilters(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, _ := db.InsertRepository(repo)

	res := &ProviderResource{RepositoryID: repoID, Name: "azurerm_test", Kind: "resource"}
	resID, _ := db.InsertProviderResource(res)

	attrs := []*ProviderAttribute{
		{ResourceID: resID, Name: "sensitive_field", Sensitive: true},
		{ResourceID: resID, Name: "force_new_field", ForceNew: true},
		{ResourceID: resID, Name: "computed_field", Computed: true},
		{ResourceID: resID, Name: "deprecated_field", Deprecated: sql.NullString{Valid: true, String: "Use new_field instead"}},
		{ResourceID: resID, Name: "nested_field", NestedBlock: true},
		{ResourceID: resID, Name: "validated_field", Validation: sql.NullString{Valid: true, String: "StringLenBetween(1,255)"}},
		{ResourceID: resID, Name: "diff_suppress_field", DiffSuppress: sql.NullString{Valid: true, String: "suppress.CaseDifference"}},
		{ResourceID: resID, Name: "conflict_field", ConflictsWith: sql.NullString{Valid: true, String: "other_field"}},
	}
	for _, a := range attrs {
		if err := db.InsertProviderAttribute(a); err != nil {
			t.Fatalf("insert attr: %v", err)
		}
	}

	tests := []struct {
		name     string
		filters  AttributeSearchFilters
		expected int
	}{
		{"sensitive", AttributeSearchFilters{Flags: []string{"sensitive"}, Limit: 10}, 1},
		{"force_new", AttributeSearchFilters{Flags: []string{"force_new"}, Limit: 10}, 1},
		{"computed", AttributeSearchFilters{Flags: []string{"computed"}, Limit: 10}, 1},
		{"deprecated", AttributeSearchFilters{Flags: []string{"deprecated"}, Limit: 10}, 1},
		{"nested", AttributeSearchFilters{Flags: []string{"nested"}, Limit: 10}, 1},
		{"has validation", AttributeSearchFilters{HasValidation: true, Limit: 10}, 1},
		{"validation contains", AttributeSearchFilters{ValidationContains: "StringLen", Limit: 10}, 1},
		{"has diff suppress", AttributeSearchFilters{HasDiffSuppress: true, Limit: 10}, 1},
		{"diff suppress contains", AttributeSearchFilters{DiffSuppressContains: "CaseDifference", Limit: 10}, 1},
		{"conflicts with", AttributeSearchFilters{ConflictsWith: "other", Limit: 10}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := db.SearchProviderAttributes(tt.filters)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(results) != tt.expected {
				t.Errorf("expected %d results, got %d", tt.expected, len(results))
			}
		})
	}
}

func TestSearchRepositories(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{
		Name:        "terraform-provider-azurerm",
		Description: "Azure Resource Manager Provider",
	}
	if _, err := db.InsertRepository(repo); err != nil {
		t.Fatalf("insert: %v", err)
	}

	results, err := db.SearchRepositories("Azure", 5)
	if err != nil {
		if strings.Contains(err.Error(), "fts") {
			t.Skipf("sqlite build without FTS support: %v", err)
		}
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 || results[0].Name != "terraform-provider-azurerm" {
		t.Errorf("expected to find repository, got %+v", results)
	}
}

func TestGetProviderReleaseEntryByKey(t *testing.T) {
	db := newTestDB(t)
	repo := &Repository{Name: "terraform-provider-azurerm"}
	repoID, err := db.InsertRepository(repo)
	if err != nil {
		t.Fatalf("insert repository: %v", err)
	}

	rel := &ProviderRelease{RepositoryID: repoID, Version: "1.0.0", Tag: "v1.0.0"}
	relID, err := db.UpsertProviderRelease(rel)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	entries := []ProviderReleaseEntry{
		{EntryKey: "unique-key-001", Title: "Some change", Section: "Features"},
	}
	if err := db.ReplaceReleaseEntries(relID, entries); err != nil {
		t.Fatalf("replace entries: %v", err)
	}

	entry, err := db.GetProviderReleaseEntryByKey(relID, "unique-key-001")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if entry.Title != "Some change" {
		t.Errorf("unexpected title: %s", entry.Title)
	}
	if entry.Section != "Features" {
		t.Errorf("unexpected section: %s", entry.Section)
	}
	if entry.ReleaseID != relID {
		t.Errorf("unexpected release ID: got %d, want %d", entry.ReleaseID, relID)
	}

	_, err = db.GetProviderReleaseEntryByKey(relID, "nonexistent-key")
	if err == nil {
		t.Error("expected error for missing entry")
	}
}

// Integration test: Full workflow from repository creation to complex queries
func TestIntegrationFullWorkflow(t *testing.T) {
	db := newTestDB(t)

	// 1. Create repository
	repo := &Repository{
		Name:        "terraform-provider-azurerm",
		FullName:    "hashicorp/terraform-provider-azurerm",
		Description: "Azure Resource Manager Provider",
		RepoURL:     "https://github.com/hashicorp/terraform-provider-azurerm",
		LastUpdated: "2024-01-01T00:00:00Z",
	}
	repoID, err := db.InsertRepository(repo)
	if err != nil {
		t.Fatalf("insert repository: %v", err)
	}

	// 2. Add resources with various attributes
	resources := []struct {
		name string
		kind string
	}{
		{"azurerm_storage_account", "resource"},
		{"azurerm_storage_account", "data_source"},
		{"azurerm_virtual_network", "resource"},
	}

	for _, r := range resources {
		res := &ProviderResource{
			RepositoryID: repoID,
			Name:         r.name,
			Kind:         r.kind,
			DisplayName:  sql.NullString{Valid: true, String: strings.ToUpper(r.name[:1]) + r.name[1:]},
			Description:  sql.NullString{Valid: true, String: "Test resource"},
		}
		resID, err := db.InsertProviderResource(res)
		if err != nil {
			t.Fatalf("insert resource %s: %v", r.name, err)
		}

		// Add attributes
		attrs := []*ProviderAttribute{
			{ResourceID: resID, Name: "name", Required: true, Type: sql.NullString{Valid: true, String: "string"}},
			{ResourceID: resID, Name: "location", Optional: true, Type: sql.NullString{Valid: true, String: "string"}},
			{ResourceID: resID, Name: "tags", Optional: true, Computed: true},
		}
		for _, attr := range attrs {
			if err := db.InsertProviderAttribute(attr); err != nil {
				t.Fatalf("insert attribute %s.%s: %v", r.name, attr.Name, err)
			}
		}

		// Add source info
		if err := db.UpsertProviderResourceSource(resID, "resource"+r.name, "path.go", "func() {}", "schema", "", "", "", ""); err != nil {
			t.Fatalf("upsert source: %v", err)
		}
	}

	// 3. Add files
	files := []RepositoryFile{
		{RepositoryID: repoID, FileName: "main.go", FilePath: "internal/main.go", FileType: "go", Content: "package main", SizeBytes: 12},
		{RepositoryID: repoID, FileName: "README.md", FilePath: "README.md", FileType: "markdown", Content: "# Provider", SizeBytes: 10},
	}
	for _, f := range files {
		if err := db.InsertFile(&f); err != nil {
			t.Fatalf("insert file %s: %v", f.FileName, err)
		}
	}

	// 4. Add release with entries
	rel := &ProviderRelease{
		RepositoryID:    repoID,
		Version:         "1.0.0",
		Tag:             "v1.0.0",
		PreviousVersion: sql.NullString{Valid: true, String: "0.9.0"},
		ReleaseDate:     sql.NullString{Valid: true, String: "2024-01-01"},
	}
	relID, err := db.UpsertProviderRelease(rel)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	entries := []ProviderReleaseEntry{
		{EntryKey: "feat-001", Title: "Add storage account", Section: "Features", ResourceName: sql.NullString{Valid: true, String: "azurerm_storage_account"}},
		{EntryKey: "fix-001", Title: "Fix virtual network", Section: "Bug Fixes", ResourceName: sql.NullString{Valid: true, String: "azurerm_virtual_network"}},
	}
	if err := db.ReplaceReleaseEntries(relID, entries); err != nil {
		t.Fatalf("replace entries: %v", err)
	}

	// 5. Query and verify: List all resources
	allResources, err := db.ListProviderResources("", 0)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(allResources) != 3 {
		t.Errorf("expected 3 resources, got %d", len(allResources))
	}

	// 6. Query: Get specific resource with attributes
	storageRes, err := db.GetProviderResource("azurerm_storage_account")
	if err != nil {
		t.Fatalf("get storage account: %v", err)
	}
	if storageRes.Kind != "resource" {
		t.Errorf("expected kind=resource, got %s", storageRes.Kind)
	}

	storageAttrs, err := db.GetProviderResourceAttributes(storageRes.ID)
	if err != nil {
		t.Fatalf("get attributes: %v", err)
	}
	if len(storageAttrs) != 3 {
		t.Errorf("expected 3 attributes, got %d", len(storageAttrs))
	}

	// Verify attribute details
	foundRequired := false
	for _, attr := range storageAttrs {
		if attr.Name == "name" && attr.Required && attr.Type.String == "string" {
			foundRequired = true
		}
	}
	if !foundRequired {
		t.Error("required 'name' attribute not found or incorrect")
	}

	// 7. Query: Get resource source
	src, err := db.GetProviderResourceSource(storageRes.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src.FunctionName.String != "resourceazurerm_storage_account" {
		t.Errorf("unexpected function name: %s", src.FunctionName.String)
	}

	// 8. Query: Search attributes with filters
	requiredAttrs, err := db.SearchProviderAttributes(AttributeSearchFilters{
		Flags: []string{"required"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search attributes: %v", err)
	}
	if len(requiredAttrs) != 3 {
		t.Errorf("expected 3 required attributes (one per resource), got %d", len(requiredAttrs))
	}

	// 9. Query: Get release with entries
	release, releaseEntries, err := db.GetReleaseWithEntriesByVersion(repoID, "1.0.0")
	if err != nil {
		t.Fatalf("get release: %v", err)
	}
	if release.Version != "1.0.0" {
		t.Errorf("wrong version: %s", release.Version)
	}
	if len(releaseEntries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(releaseEntries))
	}

	// Verify entry linked to resource
	featEntry := releaseEntries[0]
	if featEntry.ResourceName.String != "azurerm_storage_account" {
		t.Errorf("wrong resource name in entry: %s", featEntry.ResourceName.String)
	}

	// 10. Query: Get files
	repoFiles, err := db.GetRepositoryFiles(repoID)
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	if len(repoFiles) != 2 {
		t.Errorf("expected 2 files, got %d", len(repoFiles))
	}

	// 11. Test update: Upsert same resource with new description
	updateRes := &ProviderResource{
		RepositoryID: repoID,
		Name:         "azurerm_storage_account",
		Kind:         "resource",
		Description:  sql.NullString{Valid: true, String: "Updated description"},
	}
	updatedID, err := db.InsertProviderResource(updateRes)
	if err != nil {
		t.Fatalf("update resource: %v", err)
	}
	if updatedID != storageRes.ID {
		t.Errorf("upsert changed ID: got %d, want %d", updatedID, storageRes.ID)
	}

	// Verify update took effect
	updated, err := db.GetProviderResource("azurerm_storage_account")
	if err != nil {
		t.Fatalf("get updated resource: %v", err)
	}
	if updated.Description.String != "Updated description" {
		t.Errorf("description not updated: got %s", updated.Description.String)
	}

	// 12. Test cascade: Clear repository data
	if err := db.ClearRepositoryData(repoID); err != nil {
		t.Fatalf("clear repository: %v", err)
	}

	// Verify everything cleared except repository
	clearedResources, err := db.ListProviderResources("", 100)
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if len(clearedResources) != 0 {
		t.Errorf("resources not cleared: %d remain", len(clearedResources))
	}

	// Repository should still exist
	stillExists, err := db.GetRepositoryByID(repoID)
	if err != nil || stillExists == nil {
		t.Error("repository should still exist after clear")
	}
}
