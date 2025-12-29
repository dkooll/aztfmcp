package mcp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/indexer"
	"github.com/dkooll/aztfmcp/internal/testutil"
)

type fakeSyncerProgress struct {
	progress *indexer.SyncProgress
	err      error
}

func (f *fakeSyncerProgress) SyncAll() (*indexer.SyncProgress, error)     { return f.progress, f.err }
func (f *fakeSyncerProgress) SyncUpdates() (*indexer.SyncProgress, error) { return f.progress, f.err }
func (f *fakeSyncerProgress) CompareTags(baseTag, headTag string) (*indexer.GitHubCompareResult, error) {
	return nil, nil
}

func TestHandleSyncProviderUpdatesError(t *testing.T) {
	s := NewServer("test.db", "", "org", "repo")
	s.db = testutil.NewTestDB(t)
	s.syncer = &fakeSyncerProgress{err: fmt.Errorf("boom")}

	resp := s.handleSyncProviderUpdates()
	content := resp["content"].([]ContentBlock)
	if !strings.Contains(content[0].Text, "boom") {
		t.Fatalf("expected sync error, got %s", content[0].Text)
	}
}
