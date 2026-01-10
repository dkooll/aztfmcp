// Package database provides persistence for indexed AzureRM provider repository metadata.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

type Repository struct {
	ID            int64
	Name          string
	FullName      string
	Description   string
	RepoURL       string
	LastUpdated   string
	SyncedAt      time.Time
	ReadmeContent string
}

type RepositoryFile struct {
	ID           int64
	RepositoryID int64
	FileName     string
	FilePath     string
	FileType     string
	Content      string
	SizeBytes    int64
}

type ProviderService struct {
	ID                int64
	RepositoryID      int64
	Name              string
	FilePath          sql.NullString
	WebsiteCategories sql.NullString
	GitHubLabel       sql.NullString
}

type ProviderResource struct {
	ID                 int64
	RepositoryID       int64
	ServiceID          sql.NullInt64
	Name               string
	DisplayName        sql.NullString
	Kind               string
	FilePath           sql.NullString
	Description        sql.NullString
	DeprecationMessage sql.NullString
	VersionAdded       sql.NullString
	VersionRemoved     sql.NullString
	BreakingChanges    sql.NullString
	APIVersion         sql.NullString
}

type ProviderAttribute struct {
	ID             int64
	ResourceID     int64
	Name           string
	Type           sql.NullString
	Required       bool
	Optional       bool
	Computed       bool
	ForceNew       bool
	Sensitive      bool
	Deprecated     sql.NullString
	Description    sql.NullString
	ConflictsWith  sql.NullString
	ExactlyOneOf   sql.NullString
	AtLeastOneOf   sql.NullString
	MaxItems       sql.NullInt64
	MinItems       sql.NullInt64
	ElemType       sql.NullString
	ElemSummary    sql.NullString
	NestedBlock    bool
	Validation     sql.NullString
	DiffSuppress   sql.NullString
	DefaultValue   sql.NullString
	StateFunc      sql.NullString
	SetFunc        sql.NullString
	ElemSchemaJSON sql.NullString
	TypeDetails    sql.NullString
	RequiredWith   sql.NullString
}

type ProviderResourceSource struct {
	ID                   int64
	ResourceID           int64
	FunctionName         sql.NullString
	FilePath             sql.NullString
	FunctionSnippet      sql.NullString
	SchemaSnippet        sql.NullString
	CustomizeDiffSnippet sql.NullString
	TimeoutsJSON         sql.NullString
	StateUpgraders       sql.NullString
	ImporterSnippet      sql.NullString
}

type ProviderRelease struct {
	ID                int64
	RepositoryID      int64
	Version           string
	Tag               string
	PreviousVersion   sql.NullString
	PreviousTag       sql.NullString
	CommitSHA         sql.NullString
	PreviousCommitSHA sql.NullString
	ReleaseDate       sql.NullString
	ComparisonURL     sql.NullString
	CreatedAt         time.Time
}

type ProviderReleaseEntry struct {
	ID           int64
	ReleaseID    int64
	Section      string
	EntryKey     string
	Title        string
	Details      sql.NullString
	ResourceName sql.NullString
	Identifier   sql.NullString
	ChangeType   sql.NullString
	OrderIndex   int
}

type ParseCacheEntry struct {
	FilePath       string
	ContentHash    string
	ParsedAt       time.Time
	ResourceCount  int
	AttributeCount int
}

type ProviderAttributeSearchResult struct {
	Attribute        ProviderAttribute
	ResourceName     string
	ResourceKind     string
	ResourceFilePath sql.NullString
}

type AttributeSearchFilters struct {
	NameContains         string
	ResourcePrefix       string
	Flags                []string
	ConflictsWith        string
	DescriptionQuery     string
	ValidationContains   string
	DiffSuppressContains string
	HasValidation        bool
	HasDiffSuppress      bool
	Limit                int
}

func New(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if _, err := conn.Exec(Schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func escapeFTS5(query string) string {
	query = strings.ReplaceAll(query, `"`, `""`)
	return `"` + query + `"`
}

func (db *DB) InsertRepository(m *Repository) (int64, error) {
	_, err := db.conn.Exec(`
		INSERT INTO repositories (name, full_name, description, repo_url, last_updated, readme_content)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			full_name = excluded.full_name,
			description = excluded.description,
			repo_url = excluded.repo_url,
			last_updated = excluded.last_updated,
			readme_content = excluded.readme_content,
			synced_at = CURRENT_TIMESTAMP
	`, m.Name, m.FullName, m.Description, m.RepoURL, m.LastUpdated, m.ReadmeContent)
	if err != nil {
		return 0, err
	}

	var id int64
	if err := db.conn.QueryRow(`SELECT id FROM repositories WHERE name = ?`, m.Name).Scan(&id); err != nil {
		return 0, err
	}

	return id, nil
}

func (db *DB) GetRepository(name string) (*Repository, error) {
	var m Repository
	err := db.conn.QueryRow(`
		SELECT id, name, full_name, description, repo_url, last_updated, synced_at, readme_content
		FROM repositories WHERE name = ?
	`, name).Scan(&m.ID, &m.Name, &m.FullName, &m.Description, &m.RepoURL, &m.LastUpdated, &m.SyncedAt, &m.ReadmeContent)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (db *DB) GetRepositoryByID(id int64) (*Repository, error) {
	var m Repository
	err := db.conn.QueryRow(`
		SELECT id, name, full_name, description, repo_url, last_updated, synced_at, readme_content
		FROM repositories WHERE id = ?
	`, id).Scan(&m.ID, &m.Name, &m.FullName, &m.Description, &m.RepoURL, &m.LastUpdated, &m.SyncedAt, &m.ReadmeContent)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (db *DB) ListRepositories() ([]Repository, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, full_name, description, repo_url, last_updated, synced_at, readme_content
		FROM repositories ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repositories []Repository
	for rows.Next() {
		var m Repository
		if err := rows.Scan(&m.ID, &m.Name, &m.FullName, &m.Description, &m.RepoURL, &m.LastUpdated, &m.SyncedAt, &m.ReadmeContent); err != nil {
			return nil, err
		}
		repositories = append(repositories, m)
	}

	return repositories, rows.Err()
}

func (db *DB) SearchRepositories(query string, limit int) ([]Repository, error) {
	rows, err := db.conn.Query(`
		SELECT m.id, m.name, m.full_name, m.description, m.repo_url, m.last_updated, m.synced_at, m.readme_content
		FROM repositories m
		JOIN repositories_fts ON repositories_fts.rowid = m.id
		WHERE repositories_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, escapeFTS5(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repositories []Repository
	for rows.Next() {
		var m Repository
		if err := rows.Scan(&m.ID, &m.Name, &m.FullName, &m.Description, &m.RepoURL, &m.LastUpdated, &m.SyncedAt, &m.ReadmeContent); err != nil {
			return nil, err
		}
		repositories = append(repositories, m)
	}

	return repositories, rows.Err()
}

func (db *DB) InsertFile(f *RepositoryFile) error {
	_, err := db.conn.Exec(`
		INSERT INTO repository_files (repository_id, file_name, file_path, file_type, content, size_bytes)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(repository_id, file_path) DO UPDATE SET
			file_name = excluded.file_name,
			file_type = excluded.file_type,
			content = excluded.content,
			size_bytes = excluded.size_bytes
	`, f.RepositoryID, f.FileName, f.FilePath, f.FileType, f.Content, f.SizeBytes)

	return err
}

func (db *DB) GetRepositoryFiles(repositoryID int64) ([]RepositoryFile, error) {
	rows, err := db.conn.Query(`
		SELECT id, repository_id, file_name, file_path, file_type, content, size_bytes
		FROM repository_files WHERE repository_id = ?
	`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []RepositoryFile
	for rows.Next() {
		var f RepositoryFile
		if err := rows.Scan(&f.ID, &f.RepositoryID, &f.FileName, &f.FilePath, &f.FileType, &f.Content, &f.SizeBytes); err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	return files, rows.Err()
}

func (db *DB) SearchFiles(query string, limit int) ([]RepositoryFile, error) {
	rows, err := db.conn.Query(`
		SELECT mf.id, mf.repository_id, mf.file_name, mf.file_path, mf.file_type, mf.content, mf.size_bytes
		FROM repository_files mf
		JOIN repository_files_fts ON repository_files_fts.rowid = mf.id
		WHERE repository_files_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, escapeFTS5(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []RepositoryFile
	for rows.Next() {
		var f RepositoryFile
		if err := rows.Scan(&f.ID, &f.RepositoryID, &f.FileName, &f.FilePath, &f.FileType, &f.Content, &f.SizeBytes); err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	return files, rows.Err()
}

func (db *DB) SearchFilesFTS(match string, limit int) ([]RepositoryFile, error) {
	rows, err := db.conn.Query(`
        SELECT mf.id, mf.repository_id, mf.file_name, mf.file_path, mf.file_type, mf.content, mf.size_bytes
        FROM repository_files mf
        JOIN repository_files_fts ON repository_files_fts.rowid = mf.id
        WHERE repository_files_fts MATCH ?
        ORDER BY
            CASE
                WHEN mf.file_path LIKE '%.go' AND mf.file_path NOT LIKE '%_test.go' THEN rank * 2.5
                WHEN mf.file_path LIKE '%_test.go' THEN rank * 1.8
                ELSE rank
            END
        LIMIT ?
    `, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []RepositoryFile
	for rows.Next() {
		var f RepositoryFile
		if err := rows.Scan(&f.ID, &f.RepositoryID, &f.FileName, &f.FilePath, &f.FileType, &f.Content, &f.SizeBytes); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (db *DB) GetFile(repositoryName string, filePath string) (*RepositoryFile, error) {
	var f RepositoryFile
	err := db.conn.QueryRow(`
		SELECT mf.id, mf.repository_id, mf.file_name, mf.file_path, mf.file_type, mf.content, mf.size_bytes
		FROM repository_files mf
		JOIN repositories m ON m.id = mf.repository_id
		WHERE m.name = ? AND mf.file_path = ?
	`, repositoryName, filePath).Scan(&f.ID, &f.RepositoryID, &f.FileName, &f.FilePath, &f.FileType, &f.Content, &f.SizeBytes)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (db *DB) ClearRepositoryData(repositoryID int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
        DELETE FROM provider_resource_attributes
        WHERE resource_id IN (
            SELECT id FROM provider_resources WHERE repository_id = ?
        )
    `, repositoryID); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM provider_resources WHERE repository_id = ?`, repositoryID); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM provider_releases WHERE repository_id = ?`, repositoryID); err != nil {
		return err
	}

	tables := []string{
		"repository_files",
	}

	for _, table := range tables {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE repository_id = ?", table), repositoryID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`INSERT INTO repositories_fts(repositories_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("failed to rebuild repositories_fts: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO repository_files_fts(repository_files_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("failed to rebuild repository_files_fts: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO provider_resources_fts(provider_resources_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("failed to rebuild provider_resources_fts: %w", err)
	}

	return tx.Commit()
}

func (db *DB) DeleteRepositoryByID(repositoryID int64) error {
	_, err := db.conn.Exec(`DELETE FROM repositories WHERE id = ?`, repositoryID)
	return err
}

func (db *DB) InsertProviderService(s *ProviderService) (int64, error) {
	_, err := db.conn.Exec(`
		INSERT INTO provider_services (repository_id, name, file_path, website_categories, github_label)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(repository_id, name) DO UPDATE SET
			file_path = excluded.file_path,
			website_categories = excluded.website_categories,
			github_label = excluded.github_label
	`, s.RepositoryID, s.Name, s.FilePath, s.WebsiteCategories, s.GitHubLabel)
	if err != nil {
		return 0, err
	}

	var id int64
	if err := db.conn.QueryRow(`SELECT id FROM provider_services WHERE repository_id = ? AND name = ?`, s.RepositoryID, s.Name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (db *DB) InsertProviderResource(r *ProviderResource) (int64, error) {
	_, err := db.conn.Exec(`
		INSERT INTO provider_resources (repository_id, service_id, name, display_name, kind, file_path, description, deprecation_message, version_added, version_removed, breaking_changes, api_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repository_id, name, kind) DO UPDATE SET
			service_id = excluded.service_id,
			display_name = excluded.display_name,
			file_path = excluded.file_path,
			description = excluded.description,
			deprecation_message = excluded.deprecation_message,
			version_added = excluded.version_added,
			version_removed = excluded.version_removed,
			breaking_changes = excluded.breaking_changes,
			api_version = excluded.api_version
	`, r.RepositoryID, r.ServiceID, r.Name, r.DisplayName, r.Kind, r.FilePath, r.Description, r.DeprecationMessage, r.VersionAdded, r.VersionRemoved, r.BreakingChanges, r.APIVersion)
	if err != nil {
		return 0, err
	}

	var id int64
	if err := db.conn.QueryRow(`SELECT id FROM provider_resources WHERE repository_id = ? AND name = ? AND kind = ?`, r.RepositoryID, r.Name, r.Kind).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (db *DB) InsertProviderAttribute(a *ProviderAttribute) error {
	_, err := db.conn.Exec(`
		INSERT INTO provider_resource_attributes (
			resource_id, name, type, required, optional, computed, force_new, sensitive, deprecated, description,
			conflicts_with, exactly_one_of, at_least_one_of, max_items, min_items, elem_type, elem_summary,
			nested_block, validation, diff_suppress, default_value, state_func, set_func, elem_schema_json,
			type_details, required_with)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(resource_id, name) DO UPDATE SET
			type = excluded.type,
			required = excluded.required,
			optional = excluded.optional,
			computed = excluded.computed,
			force_new = excluded.force_new,
			sensitive = excluded.sensitive,
			deprecated = excluded.deprecated,
			description = excluded.description,
			conflicts_with = excluded.conflicts_with,
			exactly_one_of = excluded.exactly_one_of,
			at_least_one_of = excluded.at_least_one_of,
			max_items = excluded.max_items,
			min_items = excluded.min_items,
			elem_type = excluded.elem_type,
			elem_summary = excluded.elem_summary,
			nested_block = excluded.nested_block,
			validation = excluded.validation,
			diff_suppress = excluded.diff_suppress,
			default_value = excluded.default_value,
			state_func = excluded.state_func,
			set_func = excluded.set_func,
			elem_schema_json = excluded.elem_schema_json,
			type_details = excluded.type_details,
			required_with = excluded.required_with
	`, a.ResourceID, a.Name, a.Type, a.Required, a.Optional, a.Computed, a.ForceNew, a.Sensitive, a.Deprecated, a.Description,
		a.ConflictsWith, a.ExactlyOneOf, a.AtLeastOneOf, a.MaxItems, a.MinItems, a.ElemType, a.ElemSummary, a.NestedBlock,
		a.Validation, a.DiffSuppress, a.DefaultValue, a.StateFunc, a.SetFunc, a.ElemSchemaJSON, a.TypeDetails, a.RequiredWith)
	return err
}

func (db *DB) UpsertProviderResourceSource(resourceID int64, functionName, filePath, functionSnippet, schemaSnippet, customizeDiff, timeouts, stateUpgraders, importer string) error {
	_, err := db.conn.Exec(`
		INSERT INTO provider_resource_sources (resource_id, function_name, file_path, function_snippet, schema_snippet,
			customize_diff_snippet, timeouts_json, state_upgraders, importer_snippet)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(resource_id) DO UPDATE SET
			function_name = excluded.function_name,
			file_path = excluded.file_path,
			function_snippet = excluded.function_snippet,
			schema_snippet = excluded.schema_snippet,
			customize_diff_snippet = excluded.customize_diff_snippet,
			timeouts_json = excluded.timeouts_json,
			state_upgraders = excluded.state_upgraders,
			importer_snippet = excluded.importer_snippet
	`, resourceID, nullIfEmpty(functionName), nullIfEmpty(filePath), nullIfEmpty(functionSnippet), nullIfEmpty(schemaSnippet),
		nullIfEmpty(customizeDiff), nullIfEmpty(timeouts), nullIfEmpty(stateUpgraders), nullIfEmpty(importer))
	return err
}

func (db *DB) GetProviderResourceSource(resourceID int64) (*ProviderResourceSource, error) {
	var src ProviderResourceSource
	err := db.conn.QueryRow(`
		SELECT id, resource_id, function_name, file_path, function_snippet, schema_snippet,
			customize_diff_snippet, timeouts_json, state_upgraders, importer_snippet
		FROM provider_resource_sources
		WHERE resource_id = ?
	`, resourceID).Scan(&src.ID, &src.ResourceID, &src.FunctionName, &src.FilePath, &src.FunctionSnippet, &src.SchemaSnippet,
		&src.CustomizeDiffSnippet, &src.TimeoutsJSON, &src.StateUpgraders, &src.ImporterSnippet)
	if err != nil {
		return nil, err
	}
	return &src, nil
}

func (db *DB) UpsertProviderRelease(r *ProviderRelease) (int64, bool, error) {
	var existingID int64
	err := db.conn.QueryRow(`
		SELECT id FROM provider_releases WHERE repository_id = ? AND version = ?
	`, r.RepositoryID, r.Version).Scan(&existingID)
	isNew := err != nil

	_, err = db.conn.Exec(`
		INSERT INTO provider_releases (
			repository_id, version, tag, previous_version, previous_tag,
			commit_sha, previous_commit_sha, release_date, comparison_url
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repository_id, version) DO UPDATE SET
			tag = excluded.tag,
			previous_version = excluded.previous_version,
			previous_tag = excluded.previous_tag,
			commit_sha = excluded.commit_sha,
			previous_commit_sha = excluded.previous_commit_sha,
			release_date = excluded.release_date,
			comparison_url = excluded.comparison_url
	`, r.RepositoryID, r.Version, r.Tag, r.PreviousVersion, r.PreviousTag, r.CommitSHA, r.PreviousCommitSHA, r.ReleaseDate, r.ComparisonURL)
	if err != nil {
		return 0, false, err
	}

	var id int64
	if err := db.conn.QueryRow(`
		SELECT id FROM provider_releases WHERE repository_id = ? AND version = ?
	`, r.RepositoryID, r.Version).Scan(&id); err != nil {
		return 0, false, err
	}

	return id, isNew, nil
}

func (db *DB) ReplaceReleaseEntries(releaseID int64, entries []ProviderReleaseEntry) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM provider_release_entries WHERE release_id = ?`, releaseID); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO provider_release_entries (
			release_id, section, entry_key, title, details,
			resource_name, identifier, change_type, order_index
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range entries {
		if entry.EntryKey == "" {
			continue
		}
		if _, err := stmt.Exec(
			releaseID,
			entry.Section,
			entry.EntryKey,
			entry.Title,
			entry.Details,
			entry.ResourceName,
			entry.Identifier,
			entry.ChangeType,
			entry.OrderIndex,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) GetLatestProviderRelease(repositoryID int64) (*ProviderRelease, error) {
	var r ProviderRelease
	err := db.conn.QueryRow(`
		SELECT id, repository_id, version, tag, previous_version, previous_tag,
			commit_sha, previous_commit_sha, release_date, comparison_url, created_at
		FROM provider_releases
		WHERE repository_id = ?
		ORDER BY
			CASE WHEN release_date IS NULL OR release_date = '' THEN 1 ELSE 0 END,
			release_date DESC,
			created_at DESC
		LIMIT 1
	`, repositoryID).Scan(&r.ID, &r.RepositoryID, &r.Version, &r.Tag, &r.PreviousVersion, &r.PreviousTag, &r.CommitSHA, &r.PreviousCommitSHA, &r.ReleaseDate, &r.ComparisonURL, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (db *DB) GetProviderReleaseByVersion(repositoryID int64, version string) (*ProviderRelease, error) {
	var r ProviderRelease
	err := db.conn.QueryRow(`
		SELECT id, repository_id, version, tag, previous_version, previous_tag,
			commit_sha, previous_commit_sha, release_date, comparison_url, created_at
		FROM provider_releases
		WHERE repository_id = ? AND version = ?
	`, repositoryID, version).Scan(&r.ID, &r.RepositoryID, &r.Version, &r.Tag, &r.PreviousVersion, &r.PreviousTag, &r.CommitSHA, &r.PreviousCommitSHA, &r.ReleaseDate, &r.ComparisonURL, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (db *DB) GetProviderReleaseByTag(repositoryID int64, tag string) (*ProviderRelease, error) {
	var r ProviderRelease
	err := db.conn.QueryRow(`
        SELECT id, repository_id, version, tag, previous_version, previous_tag,
            commit_sha, previous_commit_sha, release_date, comparison_url, created_at
        FROM provider_releases
        WHERE repository_id = ? AND tag = ?
    `, repositoryID, tag).Scan(&r.ID, &r.RepositoryID, &r.Version, &r.Tag, &r.PreviousVersion, &r.PreviousTag, &r.CommitSHA, &r.PreviousCommitSHA, &r.ReleaseDate, &r.ComparisonURL, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (db *DB) GetProviderReleaseEntries(releaseID int64) ([]ProviderReleaseEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, release_id, section, entry_key, title, details,
			resource_name, identifier, change_type, order_index
		FROM provider_release_entries
		WHERE release_id = ?
		ORDER BY order_index, id
	`, releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []ProviderReleaseEntry
	for rows.Next() {
		var entry ProviderReleaseEntry
		if err := rows.Scan(&entry.ID, &entry.ReleaseID, &entry.Section, &entry.EntryKey, &entry.Title, &entry.Details, &entry.ResourceName, &entry.Identifier, &entry.ChangeType, &entry.OrderIndex); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (db *DB) GetProviderReleaseEntryByKey(releaseID int64, entryKey string) (*ProviderReleaseEntry, error) {
	var entry ProviderReleaseEntry
	err := db.conn.QueryRow(`
		SELECT id, release_id, section, entry_key, title, details,
			resource_name, identifier, change_type, order_index
		FROM provider_release_entries
		WHERE release_id = ? AND entry_key = ?
	`, releaseID, entryKey).Scan(&entry.ID, &entry.ReleaseID, &entry.Section, &entry.EntryKey, &entry.Title, &entry.Details, &entry.ResourceName, &entry.Identifier, &entry.ChangeType, &entry.OrderIndex)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (db *DB) GetLatestReleaseWithEntries(repositoryID int64) (*ProviderRelease, []ProviderReleaseEntry, error) {
	release, err := db.GetLatestProviderRelease(repositoryID)
	if err != nil {
		return nil, nil, err
	}
	entries, err := db.GetProviderReleaseEntries(release.ID)
	if err != nil {
		return nil, nil, err
	}
	return release, entries, nil
}

func (db *DB) GetReleaseWithEntriesByVersion(repositoryID int64, version string) (*ProviderRelease, []ProviderReleaseEntry, error) {
	release, err := db.GetProviderReleaseByVersion(repositoryID, version)
	if err != nil {
		return nil, nil, err
	}
	entries, err := db.GetProviderReleaseEntries(release.ID)
	if err != nil {
		return nil, nil, err
	}
	return release, entries, nil
}

func (db *DB) GetReleaseWithEntriesByTag(repositoryID int64, tag string) (*ProviderRelease, []ProviderReleaseEntry, error) {
	release, err := db.GetProviderReleaseByTag(repositoryID, tag)
	if err != nil {
		return nil, nil, err
	}
	entries, err := db.GetProviderReleaseEntries(release.ID)
	if err != nil {
		return nil, nil, err
	}
	return release, entries, nil
}

func (db *DB) ListProviderResources(kind string, limit int) ([]ProviderResource, error) {
	query := `
		SELECT id, repository_id, service_id, name, display_name, kind, file_path, description, deprecation_message, version_added, version_removed, breaking_changes, api_version
		FROM provider_resources`
	var args []any
	if kind != "" {
		query += " WHERE kind = ?"
		args = append(args, kind)
	}
	query += " ORDER BY name"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []ProviderResource
	for rows.Next() {
		var r ProviderResource
		if err := rows.Scan(&r.ID, &r.RepositoryID, &r.ServiceID, &r.Name, &r.DisplayName, &r.Kind, &r.FilePath, &r.Description, &r.DeprecationMessage, &r.VersionAdded, &r.VersionRemoved, &r.BreakingChanges, &r.APIVersion); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

func (db *DB) SearchProviderResources(query string, limit int) ([]ProviderResource, error) {
	rows, err := db.conn.Query(`
		SELECT pr.id, pr.repository_id, pr.service_id, pr.name, pr.display_name, pr.kind, pr.file_path, pr.description, pr.deprecation_message, pr.version_added, pr.version_removed, pr.breaking_changes, pr.api_version
		FROM provider_resources pr
		JOIN provider_resources_fts ON provider_resources_fts.rowid = pr.id
		WHERE provider_resources_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, escapeFTS5(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []ProviderResource
	for rows.Next() {
		var r ProviderResource
		if err := rows.Scan(&r.ID, &r.RepositoryID, &r.ServiceID, &r.Name, &r.DisplayName, &r.Kind, &r.FilePath, &r.Description, &r.DeprecationMessage, &r.VersionAdded, &r.VersionRemoved, &r.BreakingChanges, &r.APIVersion); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

func (db *DB) GetProviderResource(name string) (*ProviderResource, error) {
	var r ProviderResource
	// When a name exists as both resource and data_source, prefer the resource
	err := db.conn.QueryRow(`
		SELECT id, repository_id, service_id, name, display_name, kind, file_path, description, deprecation_message, version_added, version_removed, breaking_changes, api_version
		FROM provider_resources
		WHERE name = ?
		ORDER BY CASE kind WHEN 'resource' THEN 0 WHEN 'data_source' THEN 1 ELSE 2 END
		LIMIT 1
	`, name).Scan(&r.ID, &r.RepositoryID, &r.ServiceID, &r.Name, &r.DisplayName, &r.Kind, &r.FilePath, &r.Description, &r.DeprecationMessage, &r.VersionAdded, &r.VersionRemoved, &r.BreakingChanges, &r.APIVersion)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (db *DB) GetProviderResourceAttributes(resourceID int64) ([]ProviderAttribute, error) {
	rows, err := db.conn.Query(`
		SELECT id, resource_id, name, type, required, optional, computed, force_new, sensitive, deprecated, description,
			conflicts_with, exactly_one_of, at_least_one_of, max_items, min_items, elem_type, elem_summary,
			nested_block, validation, diff_suppress, default_value, state_func, set_func, elem_schema_json,
			type_details, required_with
		FROM provider_resource_attributes
		WHERE resource_id = ?
		ORDER BY name
	`, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attrs []ProviderAttribute
	for rows.Next() {
		var a ProviderAttribute
		if err := rows.Scan(&a.ID, &a.ResourceID, &a.Name, &a.Type, &a.Required, &a.Optional, &a.Computed, &a.ForceNew, &a.Sensitive, &a.Deprecated,
			&a.Description, &a.ConflictsWith, &a.ExactlyOneOf, &a.AtLeastOneOf, &a.MaxItems, &a.MinItems, &a.ElemType, &a.ElemSummary,
			&a.NestedBlock, &a.Validation, &a.DiffSuppress, &a.DefaultValue, &a.StateFunc, &a.SetFunc, &a.ElemSchemaJSON,
			&a.TypeDetails, &a.RequiredWith); err != nil {
			return nil, err
		}
		attrs = append(attrs, a)
	}
	return attrs, rows.Err()
}

func (db *DB) SearchProviderAttributes(filters AttributeSearchFilters) ([]ProviderAttributeSearchResult, error) {
	if filters.Limit <= 0 {
		filters.Limit = 20
	}

	var builder strings.Builder
	builder.WriteString(`
		SELECT
			a.id, a.resource_id, a.name, a.type, a.required, a.optional, a.computed, a.force_new, a.sensitive,
			a.deprecated, a.description, a.conflicts_with, a.exactly_one_of, a.at_least_one_of, a.max_items,
			a.min_items, a.elem_type, a.elem_summary, a.nested_block, a.validation, a.diff_suppress,
			a.default_value, a.state_func, a.set_func, a.elem_schema_json, a.type_details, a.required_with,
			r.name, r.kind, r.file_path
		FROM provider_resource_attributes a
		JOIN provider_resources r ON r.id = a.resource_id
		WHERE 1=1
	`)

	var args []any
	lowerLike := func(val string) string {
		return "%" + strings.ToLower(val) + "%"
	}

	if filters.NameContains != "" {
		builder.WriteString(" AND LOWER(a.name) LIKE ?")
		args = append(args, lowerLike(filters.NameContains))
	}
	if filters.ResourcePrefix != "" {
		builder.WriteString(" AND r.name LIKE ?")
		args = append(args, filters.ResourcePrefix+"%")
	}
	for _, flag := range filters.Flags {
		switch strings.ToLower(flag) {
		case "required":
			builder.WriteString(" AND a.required = 1")
		case "optional":
			builder.WriteString(" AND a.optional = 1")
		case "computed":
			builder.WriteString(" AND a.computed = 1")
		case "force_new":
			builder.WriteString(" AND a.force_new = 1")
		case "sensitive":
			builder.WriteString(" AND a.sensitive = 1")
		case "deprecated":
			builder.WriteString(" AND a.deprecated IS NOT NULL AND a.deprecated <> ''")
		case "nested":
			builder.WriteString(" AND a.nested_block = 1")
		}
	}
	if filters.ConflictsWith != "" {
		builder.WriteString(" AND LOWER(COALESCE(a.conflicts_with, '')) LIKE ?")
		args = append(args, lowerLike(filters.ConflictsWith))
	}
	if filters.DescriptionQuery != "" {
		builder.WriteString(" AND LOWER(COALESCE(a.description, a.elem_summary, '')) LIKE ?")
		args = append(args, lowerLike(filters.DescriptionQuery))
	}
	if filters.HasValidation {
		builder.WriteString(" AND a.validation IS NOT NULL AND a.validation <> ''")
	}
	if filters.ValidationContains != "" {
		builder.WriteString(" AND LOWER(COALESCE(a.validation, '')) LIKE ?")
		args = append(args, lowerLike(filters.ValidationContains))
	}
	if filters.HasDiffSuppress {
		builder.WriteString(" AND a.diff_suppress IS NOT NULL AND a.diff_suppress <> ''")
	}
	if filters.DiffSuppressContains != "" {
		builder.WriteString(" AND LOWER(COALESCE(a.diff_suppress, '')) LIKE ?")
		args = append(args, lowerLike(filters.DiffSuppressContains))
	}

	builder.WriteString(" ORDER BY r.name, a.name LIMIT ?")
	args = append(args, filters.Limit)

	rows, err := db.conn.Query(builder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderAttributeSearchResult
	for rows.Next() {
		var res ProviderAttributeSearchResult
		if err := rows.Scan(
			&res.Attribute.ID,
			&res.Attribute.ResourceID,
			&res.Attribute.Name,
			&res.Attribute.Type,
			&res.Attribute.Required,
			&res.Attribute.Optional,
			&res.Attribute.Computed,
			&res.Attribute.ForceNew,
			&res.Attribute.Sensitive,
			&res.Attribute.Deprecated,
			&res.Attribute.Description,
			&res.Attribute.ConflictsWith,
			&res.Attribute.ExactlyOneOf,
			&res.Attribute.AtLeastOneOf,
			&res.Attribute.MaxItems,
			&res.Attribute.MinItems,
			&res.Attribute.ElemType,
			&res.Attribute.ElemSummary,
			&res.Attribute.NestedBlock,
			&res.Attribute.Validation,
			&res.Attribute.DiffSuppress,
			&res.Attribute.DefaultValue,
			&res.Attribute.StateFunc,
			&res.Attribute.SetFunc,
			&res.Attribute.ElemSchemaJSON,
			&res.Attribute.TypeDetails,
			&res.Attribute.RequiredWith,
			&res.ResourceName,
			&res.ResourceKind,
			&res.ResourceFilePath,
		); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (db *DB) GetParseCacheEntry(filePath string) (*ParseCacheEntry, error) {
	var entry ParseCacheEntry
	err := db.conn.QueryRow(`
		SELECT file_path, content_hash, parsed_at, resource_count, attribute_count
		FROM parse_cache
		WHERE file_path = ?
	`, filePath).Scan(&entry.FilePath, &entry.ContentHash, &entry.ParsedAt, &entry.ResourceCount, &entry.AttributeCount)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (db *DB) UpsertParseCacheEntry(entry *ParseCacheEntry) error {
	_, err := db.conn.Exec(`
		INSERT INTO parse_cache (file_path, content_hash, resource_count, attribute_count)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET
			content_hash = excluded.content_hash,
			parsed_at = CURRENT_TIMESTAMP,
			resource_count = excluded.resource_count,
			attribute_count = excluded.attribute_count
	`, entry.FilePath, entry.ContentHash, entry.ResourceCount, entry.AttributeCount)
	return err
}
