package database

const Schema = `
CREATE TABLE IF NOT EXISTS repositories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    full_name TEXT NOT NULL,
    description TEXT,
    repo_url TEXT NOT NULL,
    last_updated TEXT,
    synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    readme_content TEXT
);

CREATE TABLE IF NOT EXISTS repository_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL,
    file_name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    file_type TEXT,
    content TEXT NOT NULL,
    size_bytes INTEGER,
    FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
    UNIQUE(repository_id, file_path)
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_repositories_name ON repositories(name);
CREATE INDEX IF NOT EXISTS idx_repositories_full_name ON repositories(full_name);
CREATE INDEX IF NOT EXISTS idx_repository_files_repository_id ON repository_files(repository_id);
CREATE INDEX IF NOT EXISTS idx_repository_files_type ON repository_files(file_type);

-- FTS indexes for fast lookups across repository metadata
CREATE VIRTUAL TABLE IF NOT EXISTS repositories_fts USING fts5(
    name,
    description,
    readme_content,
    content='repositories',
    content_rowid='id'
);

CREATE VIRTUAL TABLE IF NOT EXISTS repository_files_fts USING fts5(
    file_name,
    file_path,
    content,
    content='repository_files',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS repositories_fts_insert AFTER INSERT ON repositories BEGIN
    INSERT INTO repositories_fts(rowid, name, description, readme_content)
    VALUES (new.id, new.name, new.description, new.readme_content);
END;

CREATE TRIGGER IF NOT EXISTS repositories_fts_update AFTER UPDATE ON repositories BEGIN
    UPDATE repositories_fts
    SET name = new.name,
        description = new.description,
        readme_content = new.readme_content
    WHERE rowid = new.id;
END;

CREATE TRIGGER IF NOT EXISTS repositories_fts_delete AFTER DELETE ON repositories BEGIN
    DELETE FROM repositories_fts WHERE rowid = old.id;
END;

-- Triggers to keep file FTS in sync
CREATE TRIGGER IF NOT EXISTS repository_files_fts_insert AFTER INSERT ON repository_files BEGIN
    INSERT INTO repository_files_fts(rowid, file_name, file_path, content)
    VALUES (new.id, new.file_name, new.file_path, new.content);
END;

CREATE TRIGGER IF NOT EXISTS repository_files_fts_update AFTER UPDATE ON repository_files BEGIN
    UPDATE repository_files_fts
    SET file_name = new.file_name,
        file_path = new.file_path,
        content = new.content
    WHERE rowid = new.id;
END;

CREATE TRIGGER IF NOT EXISTS repository_files_fts_delete AFTER DELETE ON repository_files BEGIN
    DELETE FROM repository_files_fts WHERE rowid = old.id;
END;

CREATE TABLE IF NOT EXISTS provider_resources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    display_name TEXT,
    kind TEXT NOT NULL,
    file_path TEXT,
    description TEXT,
    deprecation_message TEXT,
    version_added TEXT,
    version_removed TEXT,
    breaking_changes TEXT,
    FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
    UNIQUE(repository_id, name)
);

CREATE INDEX IF NOT EXISTS idx_provider_resources_name ON provider_resources(name);
CREATE INDEX IF NOT EXISTS idx_provider_resources_kind ON provider_resources(kind);

CREATE VIRTUAL TABLE IF NOT EXISTS provider_resources_fts USING fts5(
    name,
    description,
    breaking_changes,
    content='provider_resources',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS provider_resources_fts_insert AFTER INSERT ON provider_resources BEGIN
    INSERT INTO provider_resources_fts(rowid, name, description, breaking_changes)
    VALUES (new.id, new.name, new.description, new.breaking_changes);
END;

CREATE TRIGGER IF NOT EXISTS provider_resources_fts_update AFTER UPDATE ON provider_resources BEGIN
    UPDATE provider_resources_fts
    SET name = new.name,
        description = new.description,
        breaking_changes = new.breaking_changes
    WHERE rowid = new.id;
END;

CREATE TRIGGER IF NOT EXISTS provider_resources_fts_delete AFTER DELETE ON provider_resources BEGIN
    DELETE FROM provider_resources_fts WHERE rowid = old.id;
END;

CREATE TABLE IF NOT EXISTS provider_resource_attributes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    resource_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    type TEXT,
    required BOOLEAN DEFAULT 0,
    optional BOOLEAN DEFAULT 0,
    computed BOOLEAN DEFAULT 0,
    force_new BOOLEAN DEFAULT 0,
    sensitive BOOLEAN DEFAULT 0,
    deprecated TEXT,
    description TEXT,
    conflicts_with TEXT,
    exactly_one_of TEXT,
    at_least_one_of TEXT,
    max_items INTEGER,
    min_items INTEGER,
    elem_type TEXT,
    elem_summary TEXT,
    nested_block BOOLEAN DEFAULT 0,
    validation TEXT,
    diff_suppress TEXT,
    default_value TEXT,
    state_func TEXT,
    set_func TEXT,
    elem_schema_json TEXT,
    type_details TEXT,
    required_with TEXT,
    FOREIGN KEY (resource_id) REFERENCES provider_resources(id) ON DELETE CASCADE,
    UNIQUE(resource_id, name)
);

CREATE INDEX IF NOT EXISTS idx_provider_attr_resource ON provider_resource_attributes(resource_id);
CREATE INDEX IF NOT EXISTS idx_provider_attr_name_lower ON provider_resource_attributes(LOWER(name));
CREATE INDEX IF NOT EXISTS idx_provider_attr_force_new ON provider_resource_attributes(force_new) WHERE force_new = 1;
CREATE INDEX IF NOT EXISTS idx_provider_attr_required ON provider_resource_attributes(required) WHERE required = 1;
CREATE INDEX IF NOT EXISTS idx_provider_attr_sensitive ON provider_resource_attributes(sensitive) WHERE sensitive = 1;

CREATE TABLE IF NOT EXISTS provider_resource_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    resource_id INTEGER NOT NULL UNIQUE,
    function_name TEXT,
    file_path TEXT,
    function_snippet TEXT,
    schema_snippet TEXT,
    customize_diff_snippet TEXT,
    timeouts_json TEXT,
    state_upgraders TEXT,
    importer_snippet TEXT,
    FOREIGN KEY (resource_id) REFERENCES provider_resources(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_provider_sources_resource ON provider_resource_sources(resource_id);

CREATE TABLE IF NOT EXISTS provider_releases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL,
    version TEXT NOT NULL,
    tag TEXT NOT NULL,
    previous_version TEXT,
    previous_tag TEXT,
    commit_sha TEXT,
    previous_commit_sha TEXT,
    release_date TEXT,
    comparison_url TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
    UNIQUE(repository_id, version)
);

CREATE INDEX IF NOT EXISTS idx_provider_releases_repo ON provider_releases(repository_id);
CREATE INDEX IF NOT EXISTS idx_provider_releases_tag ON provider_releases(tag);

CREATE TABLE IF NOT EXISTS provider_release_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    release_id INTEGER NOT NULL,
    section TEXT NOT NULL,
    entry_key TEXT NOT NULL,
    title TEXT NOT NULL,
    details TEXT,
    resource_name TEXT,
    identifier TEXT,
    change_type TEXT,
    order_index INTEGER DEFAULT 0,
    FOREIGN KEY (release_id) REFERENCES provider_releases(id) ON DELETE CASCADE,
    UNIQUE(release_id, entry_key)
);

CREATE INDEX IF NOT EXISTS idx_release_entries_release ON provider_release_entries(release_id);
CREATE INDEX IF NOT EXISTS idx_release_entries_identifier ON provider_release_entries(identifier);

-- Parse cache for incremental parsing
CREATE TABLE IF NOT EXISTS parse_cache (
    file_path TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    parsed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resource_count INTEGER,
    attribute_count INTEGER
);
`
