// Package testutil provides helpers for setting up temporary databases and fixtures in tests.
package testutil

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
)

func NewTestDB(t *testing.T) *database.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			t.Skipf("sqlite3 built without fts5 module: %v", err)
		}
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func InsertRepository(t *testing.T, db *database.DB, name string) *database.Repository {
	t.Helper()
	repo := &database.Repository{
		Name:     name,
		FullName: name,
		RepoURL:  "https://github.com/example/" + name,
	}
	id, err := db.InsertRepository(repo)
	if err != nil {
		t.Fatalf("failed to insert repository: %v", err)
	}
	repo.ID = id
	return repo
}

func InsertResource(t *testing.T, db *database.DB, repoID int64, name string, kind string, filePath string) *database.ProviderResource {
	t.Helper()
	res := &database.ProviderResource{
		RepositoryID: repoID,
		Name:         name,
		Kind:         kind,
		FilePath:     sql.NullString{String: filePath, Valid: filePath != ""},
	}
	id, err := db.InsertProviderResource(res)
	if err != nil {
		t.Fatalf("failed to insert provider resource: %v", err)
	}
	res.ID = id
	return res
}

func InsertAttribute(t *testing.T, db *database.DB, resourceID int64, attr database.ProviderAttribute) database.ProviderAttribute {
	t.Helper()
	attr.ResourceID = resourceID
	if err := db.InsertProviderAttribute(&attr); err != nil {
		t.Fatalf("failed to insert provider attribute %s: %v", attr.Name, err)
	}
	return attr
}

func InsertFile(t *testing.T, db *database.DB, repoID int64, filePath, fileType, content string) *database.RepositoryFile {
	t.Helper()
	f := &database.RepositoryFile{
		RepositoryID: repoID,
		FileName:     filepath.Base(filePath),
		FilePath:     filePath,
		FileType:     fileType,
		Content:      content,
		SizeBytes:    int64(len(content)),
	}
	if err := db.InsertFile(f); err != nil {
		t.Fatalf("failed to insert repository file %s: %v", filePath, err)
	}
	return f
}

func UpsertResourceSource(t *testing.T, db *database.DB, resourceID int64, customizeDiff string) {
	t.Helper()
	if err := db.UpsertProviderResourceSource(
		resourceID,
		"Func",
		"/tmp/file.go",
		"", "",
		customizeDiff,
		"", "",
		"",
	); err != nil {
		t.Fatalf("failed to upsert resource source: %v", err)
	}
}

func InsertRelease(t *testing.T, db *database.DB, repoID int64, version, tag, previousTag string) *database.ProviderRelease {
	t.Helper()
	rel := &database.ProviderRelease{
		RepositoryID: repoID,
		Version:      version,
		Tag:          tag,
		PreviousTag:  sql.NullString{String: previousTag, Valid: previousTag != ""},
	}
	id, _, err := db.UpsertProviderRelease(rel)
	if err != nil {
		t.Fatalf("failed to upsert release: %v", err)
	}
	rel.ID = id
	return rel
}

func ReplaceReleaseEntries(t *testing.T, db *database.DB, releaseID int64, entries []database.ProviderReleaseEntry) {
	t.Helper()
	if err := db.ReplaceReleaseEntries(releaseID, entries); err != nil {
		t.Fatalf("failed to replace release entries: %v", err)
	}
}
