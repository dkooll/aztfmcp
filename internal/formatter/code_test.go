package formatter

import (
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
)

func TestCodeSearchResultsAndExtractContext(t *testing.T) {
	content := "line1\nsearch me\nline3\nline4"
	ctx := ExtractCodeContext(content, "search")
	if ctx == "" || !strings.Contains(ctx, "→") {
		t.Fatalf("expected highlighted context, got %q", ctx)
	}

	files := []database.RepositoryFile{
		{RepositoryID: 1, FilePath: "path.go", Content: content},
	}
	out := CodeSearchResults("search", files, func(id int64) string { return "repo" })
	if out == "" || !containsStr(out, "path.go") {
		t.Fatalf("expected search results output, got %s", out)
	}
}

func TestFileContentSummary(t *testing.T) {
	out := FileContent("repo", "path/file.txt", "go", 10, "code", 0, 0, 0, false)
	if !containsStr(out, "path/file.txt") || containsStr(out, "code") {
		t.Fatalf("expected metadata without content when includeContent=false, got %s", out)
	}
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}
