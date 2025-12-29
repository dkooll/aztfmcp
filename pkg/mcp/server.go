// Package mcp provides the JSON-RPC server for the AzureRM provider coordination protocol.
package mcp

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/formatter"
	"github.com/dkooll/aztfmcp/internal/indexer"
	"github.com/dkooll/aztfmcp/internal/util"
)

type Message struct {
	JSONRPC string    `json:"jsonrpc"`
	Method  string    `json:"method,omitempty"`
	Params  any       `json:"params,omitempty"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ToolCallParams struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type Syncer interface {
	SyncAll() (*indexer.SyncProgress, error)
	SyncUpdates() (*indexer.SyncProgress, error)
	CompareTags(baseTag, headTag string) (*indexer.GitHubCompareResult, error)
}

type Server struct {
	db        *database.DB
	syncer    Syncer
	writer    io.Writer
	jobs      map[string]*SyncJob
	jobsMutex sync.RWMutex
	dbPath    string
	token     string
	org       string
	repo      string
	dbMutex   sync.Mutex
}

func NewServer(dbPath, token, org, repo string) *Server {
	return &Server{
		dbPath: dbPath,
		token:  token,
		org:    org,
		repo:   repo,
		jobs:   make(map[string]*SyncJob),
	}
}

func (s *Server) repoShortName() string {
	if strings.Contains(s.repo, "/") {
		parts := strings.SplitN(s.repo, "/", 2)
		return parts[1]
	}
	return s.repo
}

func (s *Server) releaseSummaryIfUpdated(updated []string) string {
	short := strings.ToLower(s.repoShortName())
	for _, name := range updated {
		if strings.ToLower(name) == short {
			return s.latestReleaseSummaryText()
		}
	}
	return ""
}

func (s *Server) latestReleaseSummaryText() string {
	if s.db == nil {
		return ""
	}
	repo, err := s.primaryRepository()
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("Warning: unable to load repository metadata for release summary: %v", err)
		}
		return ""
	}
	release, entries, err := s.db.GetLatestReleaseWithEntries(repo.ID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("Warning: failed to load latest release summary: %v", err)
		}
		return ""
	}
	fullName := repo.FullName
	if fullName == "" {
		fullName = repo.Name
	}
	return formatter.ReleaseSummary(fullName, release, entries)
}

type SyncJob struct {
	ID          string
	Type        string
	Status      string
	StartedAt   time.Time
	CompletedAt *time.Time
	Progress    *indexer.SyncProgress
	Error       string
}

func (s *Server) ensureDB() error {
	s.dbMutex.Lock()
	defer s.dbMutex.Unlock()

	if s.db != nil {
		return nil
	}

	log.Printf("Initializing database at: %s", s.dbPath)
	db, err := database.New(s.dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	s.db = db
	s.syncer = indexer.NewSyncer(db, s.token, s.org, s.repo)
	log.Println("Database initialized successfully")

	return nil
}

func (s *Server) Run(ctx context.Context, r io.Reader, w io.Writer) error {
	s.writer = w
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		log.Printf("Received: %s", line)

		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			s.sendError(-32700, "Parse error", nil)
			continue
		}

		s.handleMessage(msg)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}

func (s *Server) handleMessage(msg Message) {
	log.Printf("Handling method: %s", msg.Method)

	switch msg.Method {
	case "initialize":
		s.handleInitialize(msg)
	case "initialized", "notifications/initialized":
		log.Println("Client initialized")
		return
	case "tools/list":
		s.handleToolsList(msg)
	case "tools/call":
		s.handleToolsCall(msg)
	case "notifications/cancelled":
		log.Println("Request cancelled")
		return
	default:
		s.sendError(-32601, "Method not found", msg.ID)
	}
}

func (s *Server) handleInitialize(msg Message) {
	response := Message{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]any{
				"name":    "az-cn-azurerm",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		},
	}
	s.sendResponse(response)
}

func (s *Server) handleToolsList(msg Message) {
	tools := []map[string]any{
		{
			"name":        "sync_provider",
			"description": "Sync the terraform-provider-azurerm repository from GitHub into the local SQLite index",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "sync_updates_provider",
			"description": "Incrementally sync the provider (fetches GitHub updates only)",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "sync_status",
			"description": "Show status for running or completed sync jobs",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"job_id": map[string]any{
						"type":        "string",
						"description": "Optional job ID to inspect",
					},
				},
			},
		},
		{
			"name":        "get_release_summary",
			"description": "Render the latest or specified release summary for the provider",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"version": map[string]any{
						"type":        "string",
						"description": "Optional provider version (e.g. 4.52.0). Defaults to the latest synced release.",
					},
					"fields": map[string]any{
						"type":        "array",
						"description": "Optional fields to include (e.g., header, entries)",
						"items": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
		{
			"name":        "get_release_snippet",
			"description": "Show the code diff snippet associated with a release entry",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"version": map[string]any{
						"type":        "string",
						"description": "Release version to inspect (e.g. 4.52.0)",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "Resource name or text excerpt from the release entry",
					},
					"max_context_lines": map[string]any{
						"type":        "integer",
						"description": "Optional limit for diff lines (default 24)",
					},
					"fields": map[string]any{
						"type":        "array",
						"description": "Optional fields to include: header, file, diff, compare_url",
						"items": map[string]any{
							"type": "string",
						},
					},
				},
				"required": []string{"version", "query"},
			},
		},
		{
			"name":        "backfill_release",
			"description": "Parse and store a specific release from CHANGELOG without a full sync",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"version": map[string]any{
						"type":        "string",
						"description": "Target version (e.g. 4.48.0 or v4.48.0)",
					},
				},
				"required": []string{"version"},
			},
		},
		{
			"name":        "list_resources",
			"description": "List parsed AzureRM resources and data sources (from Go schemas)",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"description": "Optional filter: resource | data_source",
					},
					"compact": map[string]any{
						"type":        "boolean",
						"description": "Return a compact list (names/paths only)",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Optional maximum results",
					},
				},
			},
		},
		{
			"name":        "search_resources",
			"description": "Search resource/data source names and descriptions (FTS-backed)",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (supports boolean operators)",
					},
					"compact": map[string]any{
						"type":        "boolean",
						"description": "Return a compact list (names/paths only)",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Optional result cap (default 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "get_resource_schema",
			"description": "Show schema, breaking properties, and nested blocks for a provider resource/data source",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Resource or data source name (e.g., azurerm_virtual_network)",
					},
					"attributes": map[string]any{
						"type":        "array",
						"description": "Optional list of attribute name filters (substring match)",
						"items": map[string]any{
							"type": "string",
						},
					},
					"flags": map[string]any{
						"type":        "array",
						"description": "Require attributes to include these flags (required, optional, computed, force_new, sensitive, deprecated, nested)",
						"items": map[string]any{
							"type": "string",
						},
					},
					"nested_only": map[string]any{
						"type":        "boolean",
						"description": "Only include nested block definitions",
					},
					"max_rows": map[string]any{
						"type":        "number",
						"description": "Limit the number of attributes returned (default 50, use -1 for all)",
					},
					"compact": map[string]any{
						"type":        "boolean",
						"description": "Emit a compact bullet list instead of the full table",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "search_resource_attributes",
			"description": "Search provider attributes across all resources with name/flag filters",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name_contains": map[string]any{
						"type":        "string",
						"description": "Substring applied to attribute names",
					},
					"resource_prefix": map[string]any{
						"type":        "string",
						"description": "Only include resources starting with this prefix",
					},
					"flags": map[string]any{
						"type":        "array",
						"description": "Attributes must include every listed flag (required, optional, computed, force_new, sensitive, deprecated, nested)",
						"items": map[string]any{
							"type": "string",
						},
					},
					"conflicts_with": map[string]any{
						"type":        "string",
						"description": "Only show attributes that conflict with this name",
					},
					"description_query": map[string]any{
						"type":        "string",
						"description": "Substring applied to attribute descriptions",
					},
					"compact": map[string]any{
						"type":        "boolean",
						"description": "Return a compact list (resource.attribute only)",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of matches (default 20)",
					},
				},
			},
		},
		{
			"name":        "get_schema_source",
			"description": "Return the Go definition for a provider resource/data source",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Resource or data source name (e.g., azurerm_virtual_network)",
					},
					"section": map[string]any{
						"type":        "string",
						"description": "Snippet to return: schema | function (default schema)",
					},
					"max_lines": map[string]any{
						"type":        "number",
						"description": "Trim response to this number of lines (0 = unlimited)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "search_code",
			"description": "Search across the provider Go files for text or identifiers",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Text or identifier to search for",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Optional maximum matches (default 20)",
					},
					"path_prefix": map[string]any{
						"type":        "string",
						"description": "Restrict matches to files under this relative path",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "get_file_content",
			"description": "Fetch the content of any file inside terraform-provider-azurerm",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative path (e.g., internal/services/network/virtual_network_resource.go)",
					},
					"start_line": map[string]any{
						"type":        "number",
						"description": "Optional starting line number (1-based)",
					},
					"end_line": map[string]any{
						"type":        "number",
						"description": "Optional ending line number (inclusive, 0 for default window, -1 for full file)",
					},
					"summary": map[string]any{
						"type":        "boolean",
						"description": "Only return file metadata and line window info, omit content",
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			"name":        "get_resource_docs",
			"description": "Show the rendered markdown documentation for a provider resource or data source",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Resource or data source name (e.g., azurerm_virtual_network)",
					},
					"section": map[string]any{
						"type":        "string",
						"description": "Optional markdown section heading to extract (e.g., Example Usage)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "list_resource_tests",
			"description": "List acceptance tests that cover a provider resource or data source",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Resource or data source name (e.g., azurerm_virtual_network)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "list_feature_flags",
			"description": "Enumerate provider feature flags defined in internal/features/config/features.go",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "search_validations",
			"description": "Find schema attributes that use specific validation or diff-suppress functions",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"contains": map[string]any{
						"type":        "string",
						"description": "Substring to match inside the validation function expression",
					},
					"resource_prefix": map[string]any{
						"type":        "string",
						"description": "Optional resource name prefix filter (e.g., azurerm_virtual)",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of matches (default 20)",
					},
				},
			},
		},
		{
			"name":        "get_resource_behaviors",
			"description": "Summarize advanced schema behaviours (timeouts, CustomizeDiff, importer) for a resource/data source",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Resource or data source name (e.g., azurerm_virtual_network)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "get_example",
			"description": "Fetch the files for an example scenario under the provider's examples directory",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Relative path under examples/ (e.g., virtual_machine/basic)",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "analyze_update_behavior",
			"description": "Analyzes whether changing a specific attribute requires resource recreation or supports in-place updates",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_name": map[string]any{
						"type":        "string",
						"description": "Resource name (e.g., azurerm_virtual_network)",
					},
					"attribute_path": map[string]any{
						"type":        "string",
						"description": "Attribute path (e.g., address_space)",
					},
				},
				"required": []string{"resource_name", "attribute_path"},
			},
		},
		{
			"name":        "compare_resources",
			"description": "Compare schemas, attributes, and behaviors between two provider resources",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_a": map[string]any{
						"type":        "string",
						"description": "First resource name",
					},
					"resource_b": map[string]any{
						"type":        "string",
						"description": "Second resource name",
					},
					"max_names": map[string]any{
						"type":        "number",
						"description": "Maximum attribute names to list per section (default 30, use -1 for all)",
					},
				},
				"required": []string{"resource_a", "resource_b"},
			},
		},
		{
			"name":        "find_similar_resources",
			"description": "Find provider resources with similar schemas based on attribute similarity",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_name": map[string]any{
						"type":        "string",
						"description": "Target resource name",
					},
					"similarity_threshold": map[string]any{
						"type":        "number",
						"description": "Minimum similarity score (0.0-1.0, default 0.7)",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of results (default 5)",
					},
				},
				"required": []string{"resource_name"},
			},
		},
		{
			"name":        "explain_breaking_change",
			"description": "Explains why a specific attribute causes breaking changes and suggests migration paths",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_name": map[string]any{
						"type":        "string",
						"description": "Resource name",
					},
					"attribute_name": map[string]any{
						"type":        "string",
						"description": "Attribute name",
					},
				},
				"required": []string{"resource_name", "attribute_name"},
			},
		},
		{
			"name":        "suggest_validation_improvements",
			"description": "Analyzes resource schema and suggests missing or weak validations",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_name": map[string]any{
						"type":        "string",
						"description": "Resource name to analyze",
					},
				},
				"required": []string{"resource_name"},
			},
		},
		{
			"name":        "trace_attribute_dependencies",
			"description": "Traces all dependencies and constraints for a specific attribute (ConflictsWith, RequiredWith, ExactlyOneOf, etc.)",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_name": map[string]any{
						"type":        "string",
						"description": "Resource name",
					},
					"attribute_name": map[string]any{
						"type":        "string",
						"description": "Attribute name",
					},
				},
				"required": []string{"resource_name", "attribute_name"},
			},
		},
	}

	response := Message{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"tools": tools,
		},
	}
	s.sendResponse(response)
}

func (s *Server) handleToolsCall(msg Message) {
	paramsBytes, err := json.Marshal(msg.Params)
	if err != nil {
		s.sendError(-32602, "Invalid params", msg.ID)
		return
	}

	var params ToolCallParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		s.sendError(-32602, "Invalid params", msg.ID)
		return
	}

	log.Printf("Tool call: %s", params.Name)

	var result any
	switch params.Name {
	case "sync_provider":
		result = s.handleSyncProvider()
	case "sync_updates_provider":
		result = s.handleSyncProviderUpdates()
	case "sync_status":
		result = s.handleSyncStatus(params.Arguments)
	case "get_release_summary":
		result = s.handleGetReleaseSummary(params.Arguments)
	case "get_release_snippet":
		result = s.handleGetReleaseSnippet(params.Arguments)
	case "backfill_release":
		result = s.handleBackfillRelease(params.Arguments)
	case "list_resources":
		result = s.handleListResources(params.Arguments)
	case "search_resources":
		result = s.handleSearchResources(params.Arguments)
	case "get_resource_schema":
		result = s.handleGetResourceSchema(params.Arguments)
	case "search_resource_attributes":
		result = s.handleSearchResourceAttributes(params.Arguments)
	case "get_schema_source":
		result = s.handleGetSchemaSource(params.Arguments)
	case "search_code":
		result = s.handleSearchCode(params.Arguments)
	case "get_file_content":
		result = s.handleGetFileContent(params.Arguments)
	case "get_resource_docs":
		result = s.handleGetResourceDocs(params.Arguments)
	case "list_resource_tests":
		result = s.handleListResourceTests(params.Arguments)
	case "list_feature_flags":
		result = s.handleListFeatureFlags()
	case "search_validations":
		result = s.handleSearchValidations(params.Arguments)
	case "get_resource_behaviors":
		result = s.handleGetResourceBehaviors(params.Arguments)
	case "get_example":
		result = s.handleGetExample(params.Arguments)
	case "analyze_update_behavior":
		result = s.handleAnalyzeUpdateBehavior(params.Arguments)
	case "compare_resources":
		result = s.handleCompareResources(params.Arguments)
	case "find_similar_resources":
		result = s.handleFindSimilarResources(params.Arguments)
	case "explain_breaking_change":
		result = s.handleExplainBreakingChange(params.Arguments)
	case "suggest_validation_improvements":
		result = s.handleSuggestValidationImprovements(params.Arguments)
	case "trace_attribute_dependencies":
		result = s.handleTraceAttributeDependencies(params.Arguments)
	default:
		s.sendError(-32601, "Tool not found", msg.ID)
		return
	}

	response := Message{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  result,
	}
	s.sendResponse(response)
}

func (s *Server) handleSyncProvider() map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	job := s.startSyncJob("full_sync", func() (*indexer.SyncProgress, error) {
		log.Println("Starting full repository sync (async job)...")
		return s.syncer.SyncAll()
	})

	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": fmt.Sprintf("Full sync started.\nJob ID: %s\nUse `sync_status` with this job ID to monitor progress.", job.ID),
			},
		},
	}
}

func (s *Server) handleSyncProviderUpdates() map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	log.Println("Starting incremental repository sync (updates only)...")

	progress, err := s.syncer.SyncUpdates()
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Sync failed: %v", err))
	}

	text := formatter.IncrementalSyncProgress(
		progress.TotalRepos,
		len(progress.UpdatedRepos),
		progress.SkippedRepos,
		progress.UpdatedRepos,
		progress.Errors,
	)

	if summary := s.releaseSummaryIfUpdated(progress.UpdatedRepos); summary != "" {
		if strings.TrimSpace(text) != "" {
			text = strings.TrimSpace(text) + "\n\n" + summary
		} else {
			text = summary
		}
	}

	return SuccessResponse(text)
}

func (s *Server) handleSyncStatus(args any) map[string]any {
	statusArgs, err := UnmarshalArgs[struct {
		JobID string `json:"job_id"`
	}](args)
	if err != nil {
		return ErrorResponse("Error: Invalid parameters")
	}

	if statusArgs.JobID != "" {
		job, ok := s.getJob(statusArgs.JobID)
		if !ok {
			return ErrorResponse(fmt.Sprintf("Job '%s' not found", statusArgs.JobID))
		}

		text := s.formatJobDetails(job)
		return SuccessResponse(text)
	}

	jobs := s.listJobs()
	text := s.formatJobList(jobs)
	return SuccessResponse(text)
}

func (s *Server) handleListResources(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Kind    string `json:"kind"`
		Limit   int    `json:"limit"`
		Compact bool   `json:"compact"`
	}](args)
	if err != nil {
		params = struct {
			Kind    string `json:"kind"`
			Limit   int    `json:"limit"`
			Compact bool   `json:"compact"`
		}{}
	}

	kind := strings.TrimSpace(strings.ToLower(params.Kind))
	if kind != "" && kind != "resource" && kind != "data_source" {
		return ErrorResponse("kind must be 'resource' or 'data_source'")
	}

	limit := params.Limit
	if limit == 0 {
		limit = 50 // default cap to avoid large responses
	} else if limit < 0 {
		limit = 0 // negative keeps legacy “no limit” behavior
	}

	resources, err := s.db.ListProviderResources(kind, limit)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to load provider resources: %v", err))
	}

	text := formatter.ProviderResourceList(resources)
	if params.Compact {
		text = formatter.ProviderResourceListCompact(resources)
	}
	return SuccessResponse(text)
}

func (s *Server) handleSearchResources(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
		Compact bool   `json:"compact"`
	}](args)
	if err != nil || strings.TrimSpace(params.Query) == "" {
		return ErrorResponse("query is required")
	}

	if params.Limit == 0 {
		params.Limit = 10
	}

	resources, err := s.db.SearchProviderResources(params.Query, params.Limit)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Search failed: %v", err))
	}

	text := formatter.ProviderResourceList(resources)
	if params.Compact {
		text = formatter.ProviderResourceListCompact(resources)
	}
	return SuccessResponse(text)
}

func (s *Server) handleGetResourceSchema(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Name       string   `json:"name"`
		Attributes []string `json:"attributes"`
		Flags      []string `json:"flags"`
		NestedOnly bool     `json:"nested_only"`
		MaxRows    int      `json:"max_rows"`
		Compact    bool     `json:"compact"`
	}](args)
	if err != nil || strings.TrimSpace(params.Name) == "" {
		return ErrorResponse("name is required")
	}

	if params.MaxRows == 0 {
		params.MaxRows = 50 // default cap for readability
	} else if params.MaxRows < 0 {
		params.MaxRows = 0 // no cap
	}

	resourceName := strings.TrimSpace(params.Name)
	resource, err := s.db.GetProviderResource(resourceName)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Resource '%s' not found", resourceName))
	}

	attrs, err := s.db.GetProviderResourceAttributes(resource.ID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to load schema for %s: %v", resourceName, err))
	}

	filtered, summary := filterProviderAttributes(
		attrs,
		params.Attributes,
		params.Flags,
		params.NestedOnly,
		params.MaxRows,
	)

	opts := formatter.SchemaRenderOptions{
		FilterSummary: summary,
		Compact:       params.Compact,
		Filtered:      len(params.Attributes) > 0 || len(params.Flags) > 0 || params.NestedOnly || params.MaxRows > 0,
	}

	text := formatter.ProviderResourceDetail(resource, filtered, opts)
	return SuccessResponse(text)
}

func (s *Server) handleSearchResourceAttributes(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		NameContains     string   `json:"name_contains"`
		ResourcePrefix   string   `json:"resource_prefix"`
		Flags            []string `json:"flags"`
		ConflictsWith    string   `json:"conflicts_with"`
		DescriptionQuery string   `json:"description_query"`
		Compact          bool     `json:"compact"`
		Limit            int      `json:"limit"`
	}](args)
	if err != nil {
		return ErrorResponse("Error: invalid filter parameters")
	}

	if params.Limit == 0 {
		params.Limit = 20
	} else if params.Limit < 0 {
		params.Limit = 0 // no limit
	}

	results, err := s.db.SearchProviderAttributes(database.AttributeSearchFilters{
		NameContains:     strings.TrimSpace(params.NameContains),
		ResourcePrefix:   strings.TrimSpace(params.ResourcePrefix),
		Flags:            normalizeFilters(params.Flags),
		ConflictsWith:    strings.TrimSpace(params.ConflictsWith),
		DescriptionQuery: strings.TrimSpace(params.DescriptionQuery),
		Limit:            params.Limit,
	})
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Attribute search failed: %v", err))
	}

	text := formatter.ProviderAttributeSearch(results)
	if params.Compact {
		text = formatter.ProviderAttributeSearchCompact(results)
	}
	return SuccessResponse(text)
}

func filterProviderAttributes(attrs []database.ProviderAttribute, nameFilters, flagFilters []string, nestedOnly bool, maxRows int) ([]database.ProviderAttribute, string) {
	cleanNames := normalizeFilters(nameFilters)
	cleanFlags := normalizeFilters(flagFilters)
	nameMatchers := toLower(cleanNames)
	flagMatchers := toLower(cleanFlags)

	var filtered []database.ProviderAttribute
	for _, attr := range attrs {
		if nestedOnly && !attr.NestedBlock {
			continue
		}
		if len(nameMatchers) > 0 && !attributeNameMatch(attr.Name, nameMatchers) {
			continue
		}
		if len(flagMatchers) > 0 && !attributeHasFlags(attr, flagMatchers) {
			continue
		}
		filtered = append(filtered, attr)
		if maxRows > 0 && len(filtered) >= maxRows {
			break
		}
	}

	var summary []string
	if len(cleanNames) > 0 {
		summary = append(summary, fmt.Sprintf("names~%s", strings.Join(cleanNames, ",")))
	}
	if len(cleanFlags) > 0 {
		summary = append(summary, fmt.Sprintf("flags=%s", strings.Join(cleanFlags, "+")))
	}
	if nestedOnly {
		summary = append(summary, "nested_only")
	}
	if maxRows > 0 {
		summary = append(summary, fmt.Sprintf("max_rows=%d", maxRows))
	}

	return filtered, strings.Join(summary, ", ")
}

func (s *Server) handleGetSchemaSource(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Name     string `json:"name"`
		Section  string `json:"section"`
		MaxLines int    `json:"max_lines"`
	}](args)
	if err != nil || strings.TrimSpace(params.Name) == "" {
		return ErrorResponse("name is required")
	}

	section := strings.ToLower(strings.TrimSpace(params.Section))
	if section == "" {
		section = "schema"
	}
	if section != "schema" && section != "function" {
		return ErrorResponse("section must be 'schema' or 'function'")
	}

	resource, err := s.db.GetProviderResource(strings.TrimSpace(params.Name))
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Resource '%s' not found", params.Name))
	}

	src, err := s.db.GetProviderResourceSource(resource.ID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Source snippet for '%s' not available yet. Try running sync_provider.", params.Name))
	}

	snippet := ""
	switch section {
	case "function":
		if src.FunctionSnippet.Valid {
			snippet = src.FunctionSnippet.String
		}
	default:
		if src.SchemaSnippet.Valid {
			snippet = src.SchemaSnippet.String
		}
	}
	if snippet == "" && src.FunctionSnippet.Valid {
		snippet = src.FunctionSnippet.String
		section = "function"
	}

	snippet, truncated := trimSnippet(snippet, params.MaxLines)
	filePath := src.FilePath.String
	if filePath == "" && resource.FilePath.Valid {
		filePath = resource.FilePath.String
	}

	functionName := src.FunctionName.String
	text := formatter.ProviderSchemaSource(resource.Name, section, filePath, functionName, snippet, truncated)
	return SuccessResponse(text)
}

func trimSnippet(snippet string, maxLines int) (string, bool) {
	if maxLines <= 0 {
		return snippet, false
	}
	lines := strings.Split(snippet, "\n")
	if len(lines) <= maxLines {
		return snippet, false
	}
	return strings.Join(lines[:maxLines], "\n"), true
}

func extractLineWindow(content string, startLine, endLine int) (string, int, int, int) {
	total := lineCount(content)
	if startLine <= 0 && endLine <= 0 {
		return content, 0, 0, total
	}
	if total == 0 {
		return "", 0, 0, 0
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine > total {
		endLine = total
	}
	if startLine > endLine {
		startLine = endLine
	}
	lines := strings.Split(content, "\n")
	snippet := strings.Join(lines[startLine-1:endLine], "\n")
	return snippet, startLine, endLine, total
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func (s *Server) handleSearchCode(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	searchArgs, err := UnmarshalArgs[struct {
		Query      string   `json:"query"`
		Limit      int      `json:"limit"`
		Kind       string   `json:"kind"`
		TypePrefix string   `json:"type_prefix"`
		Has        []string `json:"has"`
		PathPrefix string   `json:"path_prefix"`
	}](args)
	if err != nil {
		return ErrorResponse("Error: Invalid search query")
	}

	if searchArgs.Limit == 0 {
		searchArgs.Limit = 20
	}

	if strings.TrimSpace(searchArgs.Kind) != "" || strings.TrimSpace(searchArgs.TypePrefix) != "" || len(searchArgs.Has) > 0 {
		return ErrorResponse("kind/type_prefix/has filters are not supported for provider code search")
	}

	variants := util.ExpandQueryVariants(searchArgs.Query)
	if len(variants) == 0 {
		variants = []string{searchArgs.Query}
	}

	seen := make(map[int64]struct{})
	var merged []database.RepositoryFile
	var files []database.RepositoryFile
	if len(variants) == 1 {
		files, _ = s.db.SearchFiles(variants[0], searchArgs.Limit)
	} else {
		parts := make([]string, 0, len(variants))
		for _, v := range variants {
			escaped := strings.ReplaceAll(v, "\"", "\"\"")
			parts = append(parts, fmt.Sprintf("\"%s\"", escaped))
		}
		match := strings.Join(parts, " OR ")
		files, _ = s.db.SearchFilesFTS(match, searchArgs.Limit)
	}

	pathPrefix := strings.TrimSpace(searchArgs.PathPrefix)

	for _, f := range files {
		if _, ok := seen[f.ID]; ok {
			continue
		}
		if pathPrefix != "" && !strings.HasPrefix(f.FilePath, pathPrefix) {
			continue
		}
		seen[f.ID] = struct{}{}
		merged = append(merged, f)
		if searchArgs.Limit > 0 && len(merged) >= searchArgs.Limit {
			break
		}
	}

	getRepositoryName := func(repositoryID int64) string {
		repo, err := s.db.GetRepositoryByID(repositoryID)
		if err == nil {
			return repo.Name
		}
		return "unknown"
	}

	text := formatter.CodeSearchResults(searchArgs.Query, merged, getRepositoryName)
	return SuccessResponse(text)
}

func (s *Server) handleGetFileContent(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	fileArgs, err := UnmarshalArgs[struct {
		Repository string `json:"repository"`
		FilePath   string `json:"file_path"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		Summary    bool   `json:"summary"`
	}](args)
	if err != nil {
		return ErrorResponse("Error: Invalid parameters")
	}

	repoName := strings.TrimSpace(fileArgs.Repository)
	if repoName == "" {
		repoName = s.repo
	}

	repo, err := s.resolveRepository(repoName)
	if err != nil {
		repositories, listErr := s.db.ListRepositories()
		if listErr == nil && len(repositories) > 0 {
			repo = &repositories[0]
		} else {
			target := repoName
			if strings.TrimSpace(target) == "" {
				target = "(not specified)"
			}
			return ErrorResponse(fmt.Sprintf("Repository '%s' not found", target))
		}
	}
	file, err := s.db.GetFile(repo.Name, fileArgs.FilePath)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("File '%s' not found in repository '%s'", fileArgs.FilePath, repo.Name))
	}

	startLine := fileArgs.StartLine
	endLine := fileArgs.EndLine
	if startLine == 0 && endLine == 0 {
		startLine = 1
		endLine = 200 // default window to avoid dumping entire files
	}
	if startLine < 0 {
		startLine = 1
	}
	if endLine < 0 {
		endLine = 0 // treat as full file
	}

	snippet, startLine, endLine, totalLines := extractLineWindow(file.Content, startLine, endLine)
	text := formatter.FileContent(repo.Name, file.FilePath, file.FileType, file.SizeBytes, snippet, startLine, endLine, totalLines, !fileArgs.Summary)
	return SuccessResponse(text)
}

func (s *Server) handleGetResourceDocs(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Name    string `json:"name"`
		Section string `json:"section"`
	}](args)
	if err != nil || strings.TrimSpace(params.Name) == "" {
		return ErrorResponse("name is required")
	}

	resource, err := s.db.GetProviderResource(strings.TrimSpace(params.Name))
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Resource '%s' not found", strings.TrimSpace(params.Name)))
	}

	repo, err := s.db.GetRepositoryByID(resource.RepositoryID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to load repository metadata for resource '%s': %v", resource.Name, err))
	}

	files, err := s.db.GetRepositoryFiles(repo.ID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to load repository files: %v", err))
	}

	docSuffix := strings.TrimPrefix(resource.Name, "azurerm_")
	docFile := findDocumentationFile(files, docSuffix, resource.Kind)
	if docFile == nil {
		return ErrorResponse(fmt.Sprintf("Documentation not found for '%s'. Ensure the repository sync is up-to-date.", resource.Name))
	}

	content := stripFrontMatter(docFile.Content)
	sectionText, sectionFound := extractMarkdownSection(content, params.Section)

	text := formatter.ResourceDocs(resource.Name, resource.Kind, docFile.FilePath, params.Section, sectionFound, sectionText)
	return SuccessResponse(text)
}

func (s *Server) handleListResourceTests(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Name string `json:"name"`
	}](args)
	if err != nil || strings.TrimSpace(params.Name) == "" {
		return ErrorResponse("name is required")
	}

	resource, err := s.db.GetProviderResource(strings.TrimSpace(params.Name))
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Resource '%s' not found", strings.TrimSpace(params.Name)))
	}

	repo, err := s.db.GetRepositoryByID(resource.RepositoryID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to load repository metadata for resource '%s': %v", resource.Name, err))
	}

	files, err := s.db.GetRepositoryFiles(repo.ID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to load repository files: %v", err))
	}

	shortName := strings.TrimPrefix(resource.Name, "azurerm_")
	camel := toCamelCase(shortName)
	var prefixes []string
	if resource.Kind == "data_source" {
		prefixes = []string{
			"TestAccDataSourceAzureRM" + camel,
			"TestAccDataSourceAzureRm" + camel,
		}
	} else {
		prefixes = []string{
			"TestAccAzureRM" + camel,
			"TestAccAzAPI" + camel,
		}
	}

	var matches []formatter.ResourceTestFile
	for i := range files {
		file := files[i]
		if !strings.HasSuffix(file.FileName, "_test.go") {
			continue
		}
		tests := parseTestFunctions(file.Content, prefixes)
		if len(tests) == 0 {
			continue
		}
		matches = append(matches, formatter.ResourceTestFile{
			FilePath: file.FilePath,
			Tests:    tests,
		})
	}

	text := formatter.ResourceTestOverview(resource.Name, resource.Kind, matches)
	return SuccessResponse(text)
}

func (s *Server) handleListFeatureFlags() map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	repo, err := s.defaultRepository()
	if err != nil {
		return ErrorResponse(fmt.Sprintf("No provider repository data available. Run sync_provider first. Error: %v", err))
	}

	file, err := s.db.GetFile(repo.Name, "internal/features/config/features.go")
	if err != nil {
		return ErrorResponse("Feature configuration file not found. Ensure the repository sync includes internal/features/config/features.go.")
	}

	flags := parseFeatureFlags(file.Content)
	text := formatter.FeatureFlagList(flags)
	return SuccessResponse(text)
}

func (s *Server) handleSearchValidations(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Contains       string `json:"contains"`
		ResourcePrefix string `json:"resource_prefix"`
		Limit          int    `json:"limit"`
	}](args)
	if err != nil {
		return ErrorResponse("Error: Invalid parameters")
	}

	filters := database.AttributeSearchFilters{
		ResourcePrefix:     strings.TrimSpace(params.ResourcePrefix),
		ValidationContains: strings.TrimSpace(strings.ToLower(params.Contains)),
		Limit:              params.Limit,
		HasValidation:      true,
	}

	results, err := s.db.SearchProviderAttributes(filters)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to search provider attributes: %v", err))
	}

	text := formatter.ProviderAttributeSearch(results)
	return SuccessResponse(text)
}

func (s *Server) handleGetResourceBehaviors(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Name string `json:"name"`
	}](args)
	if err != nil || strings.TrimSpace(params.Name) == "" {
		return ErrorResponse("name is required")
	}

	resource, err := s.db.GetProviderResource(strings.TrimSpace(params.Name))
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Resource '%s' not found", strings.TrimSpace(params.Name)))
	}

	src, err := s.db.GetProviderResourceSource(resource.ID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Source snippet for '%s' not available yet. Try running sync_provider.", resource.Name))
	}

	snippet := ""
	if src.SchemaSnippet.Valid && src.SchemaSnippet.String != "" {
		snippet = src.SchemaSnippet.String
	} else if src.FunctionSnippet.Valid {
		snippet = src.FunctionSnippet.String
	}
	if strings.TrimSpace(snippet) == "" {
		return ErrorResponse(fmt.Sprintf("Schema snippet for '%s' not available yet. Try running sync_provider.", resource.Name))
	}

	info := parseResourceBehaviors(snippet)
	if src.FilePath.Valid {
		info.FilePath = src.FilePath.String
	}
	if src.FunctionName.Valid {
		info.FunctionName = src.FunctionName.String
	}

	text := formatter.ResourceBehaviors(resource.Name, resource.Kind, info)
	return SuccessResponse(text)
}

func (s *Server) handleGetExample(args any) map[string]any {
	if err := s.ensureDB(); err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to initialize database: %v", err))
	}

	params, err := UnmarshalArgs[struct {
		Path string `json:"path"`
	}](args)
	if err != nil || strings.TrimSpace(params.Path) == "" {
		return ErrorResponse("path is required")
	}

	repo, err := s.defaultRepository()
	if err != nil {
		return ErrorResponse(fmt.Sprintf("No provider repository data available. Run sync_provider first. Error: %v", err))
	}

	files, err := s.db.GetRepositoryFiles(repo.ID)
	if err != nil {
		return ErrorResponse(fmt.Sprintf("Failed to load repository files: %v", err))
	}

	normalized := strings.Trim(strings.TrimSpace(params.Path), "/")
	if normalized == "" {
		return ErrorResponse("path is required")
	}

	prefix := "examples/" + normalized
	var exampleFiles []formatter.ExampleFile
	for i := range files {
		file := files[i]
		if strings.HasPrefix(file.FilePath, prefix) {
			exampleFiles = append(exampleFiles, formatter.ExampleFile{
				FileName: file.FileName,
				FilePath: file.FilePath,
				Content:  file.Content,
			})
		}
	}

	if len(exampleFiles) == 0 {
		return ErrorResponse(fmt.Sprintf("Example '%s' not found. Verify the path under examples/.", normalized))
	}

	text := formatter.ExampleDirectory(prefix, exampleFiles)
	return SuccessResponse(text)
}

func findDocumentationFile(files []database.RepositoryFile, suffix string, kind string) *database.RepositoryFile {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return nil
	}

	// Determine which patterns to try based on resource kind
	var searchPatterns []struct {
		prefix string
		suffix string
	}
	if strings.TrimSpace(kind) == "data_source" {
		// For data sources, prioritize d/ paths
		searchPatterns = []struct {
			prefix string
			suffix string
		}{
			{"website/docs/d/", suffix + ".html.markdown"},
			{"docs/data-sources/", suffix + ".md"},
			{"website/docs/r/", suffix + ".html.markdown"},
			{"docs/resources/", suffix + ".md"},
		}
	} else {
		// For resources, prioritize r/ paths
		searchPatterns = []struct {
			prefix string
			suffix string
		}{
			{"website/docs/r/", suffix + ".html.markdown"},
			{"docs/resources/", suffix + ".md"},
			{"website/docs/d/", suffix + ".html.markdown"},
			{"docs/data-sources/", suffix + ".md"},
		}
	}

	// Search for documentation file
	for _, pattern := range searchPatterns {
		for i := range files {
			f := &files[i]
			expectedPath := pattern.prefix + pattern.suffix
			if f.FilePath == expectedPath {
				return f
			}
		}
	}

	// Fallback: search any docs/ path
	for i := range files {
		f := &files[i]
		if strings.Contains(f.FilePath, "docs/") &&
			(strings.HasSuffix(f.FilePath, suffix+".md") || strings.HasSuffix(f.FilePath, suffix+".html.markdown")) {
			return f
		}
	}

	return nil
}

func (s *Server) defaultRepository() (*database.Repository, error) {
	if strings.TrimSpace(s.repo) != "" {
		if m, err := s.db.GetRepository(strings.TrimSpace(s.repo)); err == nil {
			return m, nil
		}
	}
	repositories, err := s.db.ListRepositories()
	if err != nil {
		return nil, err
	}
	if len(repositories) == 0 {
		return nil, fmt.Errorf("no provider repository indexed")
	}
	return &repositories[0], nil
}

func normalizeFilters(items []string) []string {
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func toLower(items []string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = strings.ToLower(item)
	}
	return out
}

func attributeNameMatch(name string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	lowerName := strings.ToLower(name)
	for _, filter := range filters {
		if strings.Contains(lowerName, filter) {
			return true
		}
	}
	return false
}

func attributeHasFlags(attr database.ProviderAttribute, flags []string) bool {
	if len(flags) == 0 {
		return true
	}
	for _, flag := range flags {
		if !attributeHasFlag(attr, flag) {
			return false
		}
	}
	return true
}

func attributeHasFlag(attr database.ProviderAttribute, flag string) bool {
	switch strings.ToLower(flag) {
	case "required":
		return attr.Required
	case "optional":
		return attr.Optional
	case "computed":
		return attr.Computed
	case "force_new":
		return attr.ForceNew
	case "sensitive":
		return attr.Sensitive
	case "deprecated":
		return attr.Deprecated.Valid && attr.Deprecated.String != ""
	case "nested":
		return attr.NestedBlock
	default:
		return false
	}
}

func (s *Server) startSyncJob(jobType string, runner func() (*indexer.SyncProgress, error)) *SyncJob {
	jobID := fmt.Sprintf("%s-%d", jobType, time.Now().UnixNano())
	job := &SyncJob{
		ID:        jobID,
		Type:      jobType,
		Status:    "running",
		StartedAt: time.Now(),
	}

	s.jobsMutex.Lock()
	s.jobs[jobID] = job
	s.jobsMutex.Unlock()

	go func() {
		headline := fmt.Sprintf("Sync job %s (%s)", jobID, jobType)
		defer func() {
			if r := recover(); r != nil {
				errMsg := fmt.Sprintf("panic: %v", r)
				log.Printf("%s panicked: %v", headline, r)
				s.completeJobWithError(jobID, errMsg)
			}
		}()

		progress, err := runner()
		if err != nil {
			log.Printf("%s failed: %v", headline, err)
			s.completeJobWithError(jobID, err.Error())
			return
		}

		log.Printf("%s completed", headline)
		s.completeJobWithSuccess(jobID, progress)
	}()

	return job
}

func (s *Server) completeJobWithError(jobID, errMsg string) {
	now := time.Now()
	s.jobsMutex.Lock()
	if job, ok := s.jobs[jobID]; ok {
		job.Status = "failed"
		job.Error = errMsg
		job.CompletedAt = &now
	}
	s.jobsMutex.Unlock()
}

func (s *Server) completeJobWithSuccess(jobID string, progress *indexer.SyncProgress) {
	now := time.Now()
	s.jobsMutex.Lock()
	if job, ok := s.jobs[jobID]; ok {
		job.Status = "completed"
		job.Progress = progress
		job.CompletedAt = &now
	}
	s.jobsMutex.Unlock()
}

func (s *Server) getJob(jobID string) (*SyncJob, bool) {
	s.jobsMutex.RLock()
	defer s.jobsMutex.RUnlock()
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *Server) listJobs() []*SyncJob {
	s.jobsMutex.RLock()
	defer s.jobsMutex.RUnlock()
	jobs := make([]*SyncJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].StartedAt.After(jobs[j].StartedAt)
	})
	return jobs
}

func (s *Server) formatJobDetails(job *SyncJob) string {
	progressText := ""
	if job.Progress != nil {
		progressText = formatter.SyncProgress(job.Progress)
	}

	return formatter.JobDetails(
		job.ID,
		job.Type,
		job.Status,
		job.StartedAt,
		job.CompletedAt,
		job.Error,
		progressText,
	)
}

func (s *Server) formatJobList(jobs []*SyncJob) string {
	jobInfos := make([]formatter.JobInfo, len(jobs))
	for i, job := range jobs {
		jobInfos[i] = formatter.JobInfo{
			ID:          job.ID,
			Type:        job.Type,
			Status:      job.Status,
			StartedAt:   job.StartedAt,
			CompletedAt: job.CompletedAt,
		}
	}
	return formatter.JobList(jobInfos)
}

func (s *Server) sendResponse(response Message) {
	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal response: %v", err)
		return
	}

	if s.writer == nil {
		log.Printf("No writer configured, dropping response: %s", string(data))
		return
	}

	if _, err := fmt.Fprintln(s.writer, string(data)); err != nil {
		log.Printf("Failed to write response: %v", err)
		return
	}
	log.Printf("Sent: %s", string(data))
}

func (s *Server) sendError(code int, message string, id any) {
	response := Message{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	s.sendResponse(response)
}

func (s *Server) resolveRepository(nameOrAlias string) (*database.Repository, error) {
	if m, err := s.db.GetRepository(nameOrAlias); err == nil {
		return m, nil
	}
	repos, err := s.db.SearchRepositories(nameOrAlias, 1)
	if err == nil && len(repos) > 0 {
		m := repos[0]
		return &m, nil
	}
	return nil, fmt.Errorf("repository not found for '%s'", nameOrAlias)
}
