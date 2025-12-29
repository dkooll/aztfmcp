package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/indexer"
	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestHandleMessageUnknownMethod(t *testing.T) {
	var buf bytes.Buffer
	s := NewServer("test.db", "", "org", "repo")
	s.writer = &buf

	s.handleMessage(Message{JSONRPC: "2.0", Method: "nope", ID: 1})

	got := decodeMessage(t, buf.String())
	if got.Error == nil || got.Error.Code != -32601 {
		t.Fatalf("expected method not found error, got %+v", got)
	}
}

func TestHandleInitializeAndToolsList(t *testing.T) {
	var buf bytes.Buffer
	s := NewServer("test.db", "", "org", "repo")
	s.writer = &buf

	s.handleMessage(Message{JSONRPC: "2.0", Method: "initialize", ID: 1})
	initResp := decodeMessage(t, buf.String())
	if initResp.Result == nil {
		t.Fatalf("expected initialize response, got %+v", initResp)
	}

	buf.Reset()
	s.handleMessage(Message{JSONRPC: "2.0", Method: "tools/list", ID: 2})
	toolsResp := decodeMessage(t, buf.String())
	result, ok := toolsResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", toolsResp.Result)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected non-empty tools list, got %#v", result)
	}
}

func TestStartSyncJobCompletes(t *testing.T) {
	s := NewServer("test.db", "", "org", "repo")

	done := make(chan struct{})
	job := s.startSyncJob("test", func() (*indexer.SyncProgress, error) {
		close(done)
		return &indexer.SyncProgress{UpdatedRepos: []string{"repo"}}, nil
	})

	if job.Status != "running" {
		t.Fatalf("expected running status immediately, got %s", job.Status)
	}

	waitForStatus(t, s, job.ID, "completed")
}

func TestStartSyncJobError(t *testing.T) {
	s := NewServer("test.db", "", "org", "repo")

	job := s.startSyncJob("test-error", func() (*indexer.SyncProgress, error) {
		return nil, fmt.Errorf("boom")
	})

	waitForStatus(t, s, job.ID, "failed")
	j, ok := s.getJob(job.ID)
	if !ok || j.Error == "" {
		t.Fatalf("expected error recorded for job %s", job.ID)
	}
}

func TestRunLoopStopsWithContext(t *testing.T) {
	s := NewServer("test.db", "", "org", "repo")
	var out bytes.Buffer
	s.writer = &out

	r, w := io.Pipe()
	go func() {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","method":"initialize","id":1}`)
		w.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.Run(ctx, r, &out); err != nil && err != context.DeadlineExceeded {
		t.Fatalf("expected context cancellation to stop loop, got %v", err)
	}

	if out.Len() == 0 {
		t.Fatalf("expected one response to be written")
	}
}

func decodeMessage(t *testing.T, data string) Message {
	t.Helper()
	var msg Message
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		t.Fatalf("failed to decode message: %v\npayload: %s", err, data)
	}
	return msg
}

func waitForStatus(t *testing.T, s *Server, jobID, expected string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := s.getJob(jobID); ok && job.Status == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %s", jobID, expected)
}

func TestHandleSyncStatusAndJobList(t *testing.T) {
	s := NewServer("test.db", "", "org", "repo")

	done := make(chan struct{})
	job := s.startSyncJob("test", func() (*indexer.SyncProgress, error) {
		close(done)
		return &indexer.SyncProgress{UpdatedRepos: []string{"repo"}}, nil
	})
	waitForStatus(t, s, job.ID, "completed")

	// By specific job id
	resp := s.handleSyncStatus(map[string]any{"job_id": job.ID})
	content, ok := resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 || !strings.Contains(content[0].Text, job.ID) {
		t.Fatalf("expected job detail output, got %#v", resp)
	}

	// List all jobs
	resp = s.handleSyncStatus(map[string]any{})
	content = resp["content"].([]ContentBlock)
	if !ok || len(content) == 0 || !strings.Contains(content[0].Text, "test") {
		t.Fatalf("expected job list output, got %#v", resp)
	}
}

func TestHandleToolsCallKeyHandlers(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "path/to/resource.go")
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:       "name",
		Required:   true,
		Validation: sql.NullString{String: "ValidateDiagFunc", Valid: true},
	})
	if err := db.UpsertProviderResourceSource(
		res.ID,
		"resourceExample",
		"path/to/resource.go",
		"func resourceExample() *schema.Resource { return nil }",
		"map[string]*schema.Schema{\"name\": {Required: true}}",
		"",
		"",
		"",
		"",
	); err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	testutil.InsertFile(t, db, repo.ID, "examples/basic/main.tf", "terraform", "line1\nline2\nline3\n")
	testutil.InsertFile(t, db, repo.ID, "examples/basic/variables.tf", "terraform", "variable \"name\" {}")
	testutil.InsertFile(t, db, repo.ID, "internal/example/resource_test.go", "go", `package example
import "testing"
func TestAccAzureRMExample_basic(t *testing.T) {}
`)
	testutil.InsertFile(t, db, repo.ID, "internal/features/config/features.go", "go", `package features
var Features = map[string]struct{
	Description string
	Default     bool
}{
	"flag_one": {Description: "first", Default: true},
	"flag_two": {Description: "second", Default: false},
}`)

	s := NewServer("test.db", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db
	s.syncer = &fakeSyncer{
		compareResult: &indexer.GitHubCompareResult{
			Files: []indexer.GitHubCompareFile{{Filename: "path/to/resource.go", Patch: "diff"}},
		},
	}

	tests := []struct {
		name        string
		args        map[string]any
		expectMatch string
	}{
		{
			name:        "get_file_content",
			args:        map[string]any{"file_path": "missing.tf"},
			expectMatch: "not found",
		},
		{
			name:        "get_file_content",
			args:        map[string]any{"file_path": "examples/basic/main.tf", "start_line": -5, "end_line": -1},
			expectMatch: "line1",
		},
		{
			name:        "get_file_content",
			args:        map[string]any{"file_path": "examples/basic/main.tf", "summary": true},
			expectMatch: "Size",
		},
		{
			name:        "get_file_content",
			args:        map[string]any{"file_path": "examples/basic/main.tf", "start_line": 2, "end_line": 2},
			expectMatch: "line2",
		},
		{
			name:        "get_file_content",
			args:        map[string]any{"file_path": "examples/basic/main.tf", "start_line": 5, "end_line": 2},
			expectMatch: "line",
		},
		{
			name:        "get_schema_source",
			args:        map[string]any{"name": "azurerm_example", "section": "schema"},
			expectMatch: "name",
		},
		{
			name:        "get_schema_source",
			args:        map[string]any{"name": "azurerm_example", "section": "function", "max_lines": 1},
			expectMatch: "func",
		},
		{
			name:        "search_validations",
			args:        map[string]any{"contains": "ValidateDiagFunc", "limit": 5},
			expectMatch: "name",
		},
		{
			name:        "compare_resources",
			args:        map[string]any{"resource_a": "azurerm_example", "resource_b": "azurerm_example"},
			expectMatch: "azurerm_example",
		},
		{
			name:        "get_example",
			args:        map[string]any{"path": "basic"},
			expectMatch: "main.tf",
		},
		{
			name:        "get_resource_behaviors",
			args:        map[string]any{"name": "azurerm_example"},
			expectMatch: "azurerm_example",
		},
		{
			name:        "list_resource_tests",
			args:        map[string]any{"name": "azurerm_example"},
			expectMatch: "TestAcc",
		},
		{
			name:        "list_feature_flags",
			args:        map[string]any{},
			expectMatch: "flag_one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			s.writer = &buf
			s.handleToolsCall(Message{
				JSONRPC: "2.0",
				ID:      99,
				Params: map[string]any{
					"name":      tt.name,
					"arguments": tt.args,
				},
			})
			msg := decodeMessage(t, buf.String())
			if msg.Error != nil {
				t.Fatalf("unexpected jsonrpc error for %s: %+v", tt.name, msg.Error)
			}
			result, ok := msg.Result.(map[string]any)
			if !ok {
				t.Fatalf("expected map result, got %#v", msg.Result)
			}
			content, ok := result["content"].([]any)
			if !ok || len(content) == 0 {
				t.Fatalf("expected content for %s, got %#v", tt.name, result)
			}
			text, _ := content[0].(map[string]any)["text"].(string)
			if tt.expectMatch != "" && !strings.Contains(text, tt.expectMatch) {
				t.Fatalf("expected %q in response for %s, got %s", tt.expectMatch, tt.name, text)
			}
		})
	}
}

func TestRunIntegrationToolsFlow(t *testing.T) {
	// Seed an in-memory DB with a resource, attributes, and file content.
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_example", "resource", "internal/services/example/resource.go")
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:        "name",
		Description: sql.NullString{String: "example name", Valid: true},
		Required:    true,
	})
	testutil.InsertFile(t, db, repo.ID, "internal/services/example/resource.go", "go", "package example\n// example")

	// Wire a fake syncer that never hits GitHub.
	s := NewServer("test.db", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db
	s.syncer = &fakeSyncer{}

	var out bytes.Buffer
	inputR, inputW := io.Pipe()

	// Send initialize + three tool calls (list_resources, get_resource_schema, search_code).
	go func() {
		defer inputW.Close()
		fmt.Fprintln(inputW, `{"jsonrpc":"2.0","method":"initialize","id":1}`)
		fmt.Fprintln(inputW, `{"jsonrpc":"2.0","method":"tools/call","id":2,"params":{"name":"list_resources","arguments":{"limit":5}}}`)
		fmt.Fprintln(inputW, `{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"get_resource_schema","arguments":{"name":"azurerm_example"}}}`)
		fmt.Fprintln(inputW, `{"jsonrpc":"2.0","method":"tools/call","id":4,"params":{"name":"search_code","arguments":{"query":"example"}}}`)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.Run(ctx, inputR, &out); err != nil && err != context.Canceled && err != io.EOF {
		t.Fatalf("Run returned error: %v", err)
	}

	output := out.String()
	// Basic assertions that each call produced a result section.
	if !strings.Contains(output, `"id":2`) || !strings.Contains(output, "azurerm_example") {
		t.Fatalf("expected list_resources response to include resource, output: %s", output)
	}
	if !strings.Contains(output, `"id":3`) || !strings.Contains(output, "example name") {
		t.Fatalf("expected get_resource_schema response, output: %s", output)
	}
	if !strings.Contains(output, `"id":4`) || !strings.Contains(output, "example") {
		t.Fatalf("expected search_code response, output: %s", output)
	}
}

func TestHandleListResources(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	testutil.InsertResource(t, db, repo.ID, "azurerm_virtual_network", "resource", "internal/services/network/vnet.go")
	testutil.InsertResource(t, db, repo.ID, "azurerm_subnet", "resource", "internal/services/network/subnet.go")
	testutil.InsertResource(t, db, repo.ID, "azurerm_virtual_network", "data_source", "internal/services/network/vnet_data.go")

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("list_all_with_default_limit", func(t *testing.T) {
		resp := s.handleListResources(nil)
		content := resp["content"].([]ContentBlock)
		if len(content) == 0 {
			t.Fatal("expected content blocks")
		}
		text := content[0].Text
		if !strings.Contains(text, "azurerm_virtual_network") || !strings.Contains(text, "azurerm_subnet") {
			t.Fatalf("expected resource list, got %q", text)
		}
	})

	t.Run("filter_by_kind_resource", func(t *testing.T) {
		resp := s.handleListResources(map[string]any{"kind": "resource"})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if !strings.Contains(text, "azurerm_virtual_network") {
			t.Fatalf("expected resources in list, got %q", text)
		}
	})

	t.Run("compact_mode", func(t *testing.T) {
		resp := s.handleListResources(map[string]any{"compact": true})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if len(text) == 0 {
			t.Fatal("expected compact list output")
		}
	})

	t.Run("invalid_kind_returns_error", func(t *testing.T) {
		resp := s.handleListResources(map[string]any{"kind": "invalid"})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "kind must be") {
			t.Fatalf("expected kind error, got %s", content[0].Text)
		}
	})
}

func TestHandleGetResourceSchema(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res := testutil.InsertResource(t, db, repo.ID, "azurerm_virtual_network", "resource", "internal/services/network/vnet.go")

	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:     "name",
		Type:     sql.NullString{String: "String", Valid: true},
		Required: true,
	})
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:     "location",
		Type:     sql.NullString{String: "String", Valid: true},
		Required: true,
		ForceNew: true,
	})
	testutil.InsertAttribute(t, db, res.ID, database.ProviderAttribute{
		Name:     "address_space",
		Type:     sql.NullString{String: "List", Valid: true},
		Required: true,
	})

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("get_full_schema", func(t *testing.T) {
		resp := s.handleGetResourceSchema(map[string]any{"name": "azurerm_virtual_network"})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if !strings.Contains(text, "name") || !strings.Contains(text, "location") || !strings.Contains(text, "address_space") {
			t.Fatalf("expected full schema with all attributes, got %q", text)
		}
	})

	t.Run("filter_by_flags", func(t *testing.T) {
		resp := s.handleGetResourceSchema(map[string]any{
			"name":  "azurerm_virtual_network",
			"flags": []string{"force_new"},
		})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if !strings.Contains(text, "location") {
			t.Fatalf("expected schema with force_new attributes, got %q", text)
		}
	})

	t.Run("resource_not_found", func(t *testing.T) {
		resp := s.handleGetResourceSchema(map[string]any{"name": "azurerm_nonexistent"})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "not found") {
			t.Fatalf("expected not found error, got %s", content[0].Text)
		}
	})

	t.Run("missing_name_parameter", func(t *testing.T) {
		resp := s.handleGetResourceSchema(map[string]any{})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "name is required") {
			t.Fatalf("expected name required error, got %s", content[0].Text)
		}
	})
}

func TestHandleSearchResourceAttributes(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	res1 := testutil.InsertResource(t, db, repo.ID, "azurerm_virtual_network", "resource", "internal/services/network/vnet.go")
	res2 := testutil.InsertResource(t, db, repo.ID, "azurerm_subnet", "resource", "internal/services/network/subnet.go")

	testutil.InsertAttribute(t, db, res1.ID, database.ProviderAttribute{
		Name:     "subnet_id",
		Type:     sql.NullString{String: "String", Valid: true},
		Required: true,
	})
	testutil.InsertAttribute(t, db, res2.ID, database.ProviderAttribute{
		Name:     "subnet_id",
		Type:     sql.NullString{String: "String", Valid: true},
		Optional: true,
	})
	testutil.InsertAttribute(t, db, res2.ID, database.ProviderAttribute{
		Name:      "name",
		Type:      sql.NullString{String: "String", Valid: true},
		Required:  true,
		Sensitive: true,
	})

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("search_by_name", func(t *testing.T) {
		resp := s.handleSearchResourceAttributes(map[string]any{
			"name_contains": "subnet_id",
		})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if !strings.Contains(text, "subnet_id") {
			t.Fatalf("expected attributes with subnet_id, got %q", text)
		}
	})

	t.Run("filter_by_flags", func(t *testing.T) {
		resp := s.handleSearchResourceAttributes(map[string]any{
			"flags": []string{"sensitive"},
		})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if !strings.Contains(text, "name") {
			t.Fatalf("expected sensitive attributes, got %q", text)
		}
	})
}

func TestHandleSearchCode(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")
	testutil.InsertFile(t, db, repo.ID, "internal/services/network/vnet.go", "go", `package network
func ValidateVirtualNetwork() {
	// validation logic
}`)
	testutil.InsertFile(t, db, repo.ID, "internal/services/compute/vm.go", "go", `package compute
func ValidateVM() {
	// vm validation
}`)

	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db

	t.Run("basic_search", func(t *testing.T) {
		resp := s.handleSearchCode(map[string]any{
			"query": "validation",
		})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		// Should find at least one file with "validation" in it
		if strings.Contains(text, "No code matches found") {
			t.Fatalf("expected search results, got %q", text)
		}
	})

	t.Run("search_with_path_prefix", func(t *testing.T) {
		resp := s.handleSearchCode(map[string]any{
			"query":       "validation",
			"path_prefix": "internal/services/network",
		})
		content := resp["content"].([]ContentBlock)
		text := content[0].Text
		if strings.Contains(text, "vm.go") {
			t.Fatalf("expected only network files, got %q", text)
		}
	})

	t.Run("unsupported_filters_return_error", func(t *testing.T) {
		resp := s.handleSearchCode(map[string]any{
			"query": "Validate",
			"kind":  "resource",
		})
		content := resp["content"].([]ContentBlock)
		if !strings.Contains(content[0].Text, "not supported") {
			t.Fatalf("expected unsupported filter error, got %s", content[0].Text)
		}
	})
}

func TestHandleSyncProvider(t *testing.T) {
	db := testutil.NewTestDB(t)
	s := NewServer("", "", "hashicorp", "terraform-provider-azurerm")
	s.db = db
	s.syncer = &fakeSyncer{
		fullProgress: &indexer.SyncProgress{
			UpdatedRepos: []string{"terraform-provider-azurerm"},
		},
	}

	resp := s.handleSyncProvider()
	content := resp["content"].([]map[string]any)
	if len(content) == 0 {
		t.Fatal("expected sync response")
	}
	text := content[0]["text"].(string)
	if !strings.Contains(text, "Job ID") || !strings.Contains(text, "Full sync started") {
		t.Fatalf("expected job started message, got %q", text)
	}

	// Verify job was created
	jobs := s.listJobs()
	if len(jobs) == 0 {
		t.Fatal("expected sync job to be created")
	}
}
