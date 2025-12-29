package mcp

import "github.com/dkooll/aztfmcp/internal/indexer"

type fakeSyncer struct {
	fullProgress   *indexer.SyncProgress
	updateProgress *indexer.SyncProgress
	err            error
	compareResult  *indexer.GitHubCompareResult
	compareErr     error
}

// Compile-time check: fakeSyncer implements the syncer interface used by Server.
var _ Syncer = (*fakeSyncer)(nil)

func (f *fakeSyncer) SyncAll() (*indexer.SyncProgress, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.fullProgress != nil {
		return f.fullProgress, nil
	}
	return &indexer.SyncProgress{}, nil
}

func (f *fakeSyncer) SyncUpdates() (*indexer.SyncProgress, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.updateProgress != nil {
		return f.updateProgress, nil
	}
	return &indexer.SyncProgress{}, nil
}

func (f *fakeSyncer) CompareTags(_, _ string) (*indexer.GitHubCompareResult, error) {
	if f.compareErr != nil {
		return nil, f.compareErr
	}
	return f.compareResult, nil
}
