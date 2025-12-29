package mcp

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
)

func TestCalculateJaccardSimilarity(t *testing.T) {
	attrsA := []database.ProviderAttribute{{Name: "name"}, {Name: "location"}}
	attrsB := []database.ProviderAttribute{{Name: "name"}, {Name: "size"}}

	score := calculateJaccardSimilarity(attrsA, attrsB)
	if score <= 0.0 || score >= 1.0 {
		t.Fatalf("expected partial overlap, got %f", score)
	}
}

func TestFindCommonAndUniqueAttributes(t *testing.T) {
	attrsA := []database.ProviderAttribute{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	attrsB := []database.ProviderAttribute{{Name: "b"}, {Name: "d"}}

	common := findCommonAttributes(attrsA, attrsB)
	if len(common) != 1 || common[0] != "b" {
		t.Fatalf("unexpected common set: %v", common)
	}

	unique := findUniqueAttributes(attrsA, attrsB)
	if len(unique) != 2 {
		t.Fatalf("unexpected unique set: %v", unique)
	}
}

func TestParseConflictsList(t *testing.T) {
	conflicts := parseConflictsList("a, b , ,c")
	if len(conflicts) != 3 || conflicts[1] != "b" || conflicts[2] != "c" {
		t.Fatalf("unexpected conflicts parsing: %v", conflicts)
	}

	if got := parseConflictsList(""); len(got) != 0 {
		t.Fatalf("expected empty slice for empty input, got %v", got)
	}
}

func TestTrimStrings(t *testing.T) {
	values := []string{"a", "b", "c"}
	trimmed, truncated := trimStrings(values, 2)
	if !truncated {
		t.Fatalf("expected truncation flag")
	}
	if len(trimmed) != 2 || trimmed[1] != "b" {
		t.Fatalf("unexpected trimmed result: %v", trimmed)
	}

	unchanged, truncated2 := trimStrings(values, 0)
	if truncated2 || len(unchanged) != 3 {
		t.Fatalf("expected unchanged slice when limit <=0")
	}
}

func TestBuildDependencyGraph(t *testing.T) {
	attr := database.ProviderAttribute{
		Name:          "example",
		Required:      true,
		Computed:      true,
		ForceNew:      true,
		ConflictsWith: sql.NullString{String: "foo,bar", Valid: true},
		ExactlyOneOf:  sql.NullString{String: "one,two", Valid: true},
	}

	graph := buildDependencyGraph(attr)
	if graph == "" || graph[:9] != "Attribute" {
		t.Fatalf("graph should start with attribute header, got: %q", graph)
	}
	if !contains(graph, "Conflicts with:") || !contains(graph, "Mutually exclusive group") {
		t.Fatalf("graph missing dependency sections: %q", graph)
	}
}

// contains is a tiny helper to avoid importing strings repeatedly in tests.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
