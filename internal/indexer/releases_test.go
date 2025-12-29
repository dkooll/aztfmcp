package indexer

import (
	"strings"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"V1.2.3", "1.2.3"},
		{"", ""},
		{"unreleased", ""},
		{"Unreleased", ""},
		{"UNRELEASED", ""},
		{"1.2.3-alpha.1", "1.2.3-alpha.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeVersion(tt.input)
			if got != tt.want {
				t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEnsureTagPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.2.3", "v1.2.3"},
		{"v1.2.3", "v1.2.3"},
		{"V1.2.3", "v1.2.3"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ensureTagPrefix(tt.input)
			if got != tt.want {
				t.Errorf("ensureTagPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeTagName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.2.3", "v1.2.3"},
		{"V1.2.3", "v1.2.3"},
		{"1.2.3", "v1.2.3"},
		{"", ""},
		{"v1.2.3-BETA", "v1.2.3-beta"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeTagName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeTagName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2024-01-15", "2024-01-15"},
		{"January 15, 2024", "2024-01-15"},
		{"Jan 15, 2024", "2024-01-15"},
		{"15 January 2024", "2024-01-15"},
		{"", ""},
		{"invalid", "invalid"}, // returns raw if not parseable
		{"  2024-01-15  ", "2024-01-15"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeDate(tt.input)
			if got != tt.want {
				t.Errorf("normalizeDate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsListEntry(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"- Item", true},
		{"* Item", true},
		{"-Item", true},
		{"*Item", true},
		{"  - Indented", true},
		{"Item without marker", false},
		{"", false},
		{"-- Double dash", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isListEntry(tt.input)
			if got != tt.want {
				t.Errorf("isListEntry(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCleanBulletText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"dash with space", "- Item", "Item"},
		{"asterisk with space", "* Item", "Item"},
		{"dash without space", "-Item", "Item"},
		{"markdown link", "- [text](http://url)", "text"},
		{"backticks", "- `code`", "code"},
		{"multiple links", "- [a](url1) and [b](url2)", "a and b"},
		{"mixed", "- [link](url) `code` text", "link code text"},
		{"whitespace", "  - item  ", "item"},
		{"empty after cleaning", "- ``", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanBulletText(tt.input)
			if got != tt.want {
				t.Errorf("cleanBulletText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Bug Fixes", "bug-fixes"},
		{"Bug Fixes!", "bug-fixes"},
		{"123", "123"},
		{"!!!", "item"},
		{"", "item"},
		{"Bug FIXES", "bug-fixes"},
		{"bug_fixes", "bug-fixes"},
		{"  spaces  ", "spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestChangeTypeForSection(t *testing.T) {
	tests := []struct {
		section string
		text    string
		want    string
	}{
		{"Features", "some feature", "feature"},
		{"Features", "new resource azurerm_foo", "new_resource"},
		{"features", "New Resource azurerm_bar", "new_resource"},
		{"Features", "**New List Resource:** azurerm_private_dns_zone", "new_list_resource"},
		{"Features", "**New Action:** azurerm_cdn_front_door_cache_purge", "new_action"},
		{"Features", "**New Ephemeral:** azurerm_key_vault_secret", "new_ephemeral"},
		{"Features", "**New Data Source:** azurerm_virtual_network", "new_data_source"},
		{"Enhancements", "enhancement", "enhancement"},
		{"Improvements", "improvement", "enhancement"},
		{"Enhancements", "dependencies: storage - update to API version 2024-11-01", "dependency_update"},
		{"Bug Fixes", "fix something", "bugfix"},
		{"bugfixes", "fix", "bugfix"},
		{"bugs", "fix", "bugfix"},
		{"Breaking Changes", "breaking", "breaking_change"},
		{"Security", "security fix", "security"},
		{"Deprecations", "azurerm_foo is deprecated", "deprecation"},
		{"Notes", "**NOTE:** service retired", "deprecation"},
		{"Unknown", "text", ""},
		{"Other", "text", ""},
	}

	for _, tt := range tests {
		t.Run(tt.section+"/"+tt.text, func(t *testing.T) {
			got := changeTypeForSection(tt.section, tt.text)
			if got != tt.want {
				t.Errorf("changeTypeForSection(%q, %q) = %q, want %q", tt.section, tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"## 1.0.0 (2024-01-15)", "2024-01-15"},
		{"## 1.0.0 (January 15, 2024)", "2024-01-15"},
		{"## 1.0.0", ""},
		{"## 1.0.0 (unreleased)", ""},
		{"## (old) (2024-01-15)", "2024-01-15"}, // uses last parens
		{"## 1.0.0 ()", ""},
		{"## 1.0.0 (invalid-date)", "invalid-date"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractDate(tt.input)
			if got != tt.want {
				t.Errorf("extractDate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseReleaseHeading(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVer string
		wantTag string
		wantOK  bool
	}{
		{
			name:    "bracket format",
			input:   "## [1.2.3] (2024-01-15)",
			wantVer: "1.2.3",
			wantTag: "v1.2.3",
			wantOK:  true,
		},
		{
			name:    "space format",
			input:   "## 1.2.3 (2024-01-15)",
			wantVer: "1.2.3",
			wantTag: "v1.2.3",
			wantOK:  true,
		},
		{
			name:    "with v prefix",
			input:   "## v1.2.3",
			wantVer: "1.2.3",
			wantTag: "v1.2.3",
			wantOK:  true,
		},
		{
			name:   "unreleased",
			input:  "## Unreleased",
			wantOK: false,
		},
		{
			name:   "empty after ##",
			input:  "##",
			wantOK: false,
		},
		{
			name:   "just whitespace",
			input:  "##   ",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel, ok := parseReleaseHeading(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseReleaseHeading(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				return
			}
			if !ok {
				return
			}
			if rel.Version != tt.wantVer {
				t.Errorf("Version = %q, want %q", rel.Version, tt.wantVer)
			}
			if rel.Tag != tt.wantTag {
				t.Errorf("Tag = %q, want %q", rel.Tag, tt.wantTag)
			}
		})
	}
}

func TestParseChangelogReleases(t *testing.T) {
	t.Run("multiple releases", func(t *testing.T) {
		content := `# Changelog

## 2.0.0 (2024-02-01)

### Features
- New feature one
- New feature two

### Bug Fixes
- Fix something

## 1.0.0 (2024-01-01)

### Features
- Initial release
`
		releases := parseChangelogReleases(content)
		if len(releases) != 2 {
			t.Fatalf("expected 2 releases, got %d", len(releases))
		}

		if releases[0].Version != "2.0.0" {
			t.Errorf("first release version = %q, want 2.0.0", releases[0].Version)
		}
		if releases[1].Version != "1.0.0" {
			t.Errorf("second release version = %q, want 1.0.0", releases[1].Version)
		}
	})

	t.Run("unreleased section ignored", func(t *testing.T) {
		content := `# Changelog

## Unreleased

### Features
- Upcoming feature

## 1.0.0 (2024-01-01)

### Features
- Released feature
`
		releases := parseChangelogReleases(content)
		if len(releases) != 1 {
			t.Fatalf("expected 1 release, got %d", len(releases))
		}
		if releases[0].Version != "1.0.0" {
			t.Errorf("version = %q, want 1.0.0", releases[0].Version)
		}
	})

	t.Run("max history limit", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString("# Changelog\n\n")
		for i := 50; i >= 1; i-- {
			sb.WriteString("## " + string(rune('0'+i%10)) + ".0.0\n\n")
			sb.WriteString("### Features\n- Feature\n\n")
		}

		releases := parseChangelogReleases(sb.String())
		if len(releases) > maxReleaseHistory {
			t.Errorf("expected max %d releases, got %d", maxReleaseHistory, len(releases))
		}
	})

	t.Run("empty changelog", func(t *testing.T) {
		releases := parseChangelogReleases("")
		if len(releases) != 0 {
			t.Errorf("expected 0 releases from empty changelog, got %d", len(releases))
		}
	})

	t.Run("list without section creates Other", func(t *testing.T) {
		content := `## 1.0.0

- Orphan item without section
`
		releases := parseChangelogReleases(content)
		if len(releases) != 1 {
			t.Fatalf("expected 1 release, got %d", len(releases))
		}
		if len(releases[0].Sections) != 1 {
			t.Fatalf("expected 1 section, got %d", len(releases[0].Sections))
		}
		if releases[0].Sections[0].Name != "Other" {
			t.Errorf("section name = %q, want Other", releases[0].Sections[0].Name)
		}
	})
}

func TestBuildReleaseEntries(t *testing.T) {
	t.Run("basic entries", func(t *testing.T) {
		rel := parsedRelease{
			Version: "1.0.0",
			Tag:     "v1.0.0",
			Sections: []*parsedSection{
				{
					Name:    "Features",
					Entries: []string{"azurerm_resource_group - new resource"},
				},
				{
					Name:    "Bug Fixes",
					Entries: []string{"Fix for azurerm_virtual_network"},
				},
			},
		}

		entries := buildReleaseEntries(rel)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}

		// First entry
		if entries[0].Section != "Features" {
			t.Errorf("entry[0].Section = %q, want Features", entries[0].Section)
		}
		if !entries[0].Identifier.Valid || entries[0].Identifier.String != "azurerm_resource_group" {
			t.Errorf("entry[0].Identifier = %+v, want azurerm_resource_group", entries[0].Identifier)
		}
		if entries[0].OrderIndex != 0 {
			t.Errorf("entry[0].OrderIndex = %d, want 0", entries[0].OrderIndex)
		}

		// Second entry
		if entries[1].Section != "Bug Fixes" {
			t.Errorf("entry[1].Section = %q, want Bug Fixes", entries[1].Section)
		}
		if !entries[1].Identifier.Valid || entries[1].Identifier.String != "azurerm_virtual_network" {
			t.Errorf("entry[1].Identifier = %+v, want azurerm_virtual_network", entries[1].Identifier)
		}
		if entries[1].OrderIndex != 1 {
			t.Errorf("entry[1].OrderIndex = %d, want 1", entries[1].OrderIndex)
		}
	})

	t.Run("no resource name", func(t *testing.T) {
		rel := parsedRelease{
			Version: "1.0.0",
			Tag:     "v1.0.0",
			Sections: []*parsedSection{
				{
					Name:    "Features",
					Entries: []string{"Generic feature without resource"},
				},
			},
		}

		entries := buildReleaseEntries(rel)
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		if entries[0].Identifier.Valid {
			t.Errorf("expected no identifier, got %+v", entries[0].Identifier)
		}
	})

	t.Run("empty sections", func(t *testing.T) {
		rel := parsedRelease{
			Version:  "1.0.0",
			Tag:      "v1.0.0",
			Sections: []*parsedSection{},
		}

		entries := buildReleaseEntries(rel)
		if len(entries) != 0 {
			t.Errorf("expected 0 entries for empty sections, got %d", len(entries))
		}
	})

	t.Run("nil section", func(t *testing.T) {
		rel := parsedRelease{
			Version: "1.0.0",
			Tag:     "v1.0.0",
			Sections: []*parsedSection{
				nil,
				{Name: "Features", Entries: []string{"item"}},
			},
		}

		entries := buildReleaseEntries(rel)
		if len(entries) != 1 {
			t.Errorf("expected 1 entry (nil section skipped), got %d", len(entries))
		}
	})

	t.Run("entry key format", func(t *testing.T) {
		rel := parsedRelease{
			Version: "1.2.3",
			Tag:     "v1.2.3",
			Sections: []*parsedSection{
				{Name: "Bug Fixes", Entries: []string{"first", "second"}},
			},
		}

		entries := buildReleaseEntries(rel)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}

		// Entry keys should be formatted like "bug-fixes-1-2-3-000"
		if !strings.HasPrefix(entries[0].EntryKey, "bug-fixes-") {
			t.Errorf("entry key %q should start with 'bug-fixes-'", entries[0].EntryKey)
		}
		if !strings.HasSuffix(entries[0].EntryKey, "-000") {
			t.Errorf("entry key %q should end with '-000'", entries[0].EntryKey)
		}
		if !strings.HasSuffix(entries[1].EntryKey, "-001") {
			t.Errorf("entry key %q should end with '-001'", entries[1].EntryKey)
		}
	})
}

func TestMakeNullString(t *testing.T) {
	tests := []struct {
		input     string
		wantValid bool
		wantStr   string
	}{
		{"hello", true, "hello"},
		{"", false, ""},
		{"  ", false, ""},
		{"  trimmed  ", true, "trimmed"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := makeNullString(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("makeNullString(%q).Valid = %v, want %v", tt.input, got.Valid, tt.wantValid)
			}
			if got.Valid && got.String != tt.wantStr {
				t.Errorf("makeNullString(%q).String = %q, want %q", tt.input, got.String, tt.wantStr)
			}
		})
	}
}

func TestIsAllCapsHeader(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"FEATURES:", true},
		{"ENHANCEMENTS:", true},
		{"BUG FIXES:", true},
		{"BREAKING CHANGES:", true},
		{"NOTE:", true},
		{"features:", false},
		{"Features:", false},
		{"FEATURES", false},
		{"FEATURES: extra text", false},
		{"123:", false},
		{"", false},
		{":", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isAllCapsHeader(tt.input)
			if got != tt.want {
				t.Errorf("isAllCapsHeader(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseChangelogWithAllCapsHeaders(t *testing.T) {
	changelog := `# Changelog

## 4.57.0 (December 18, 2025)

FEATURES:

* **New Resource:** azurerm_automation_runtime_environment
* **New List Resource:** azurerm_private_dns_zone

ENHANCEMENTS:

* dependencies: update to API version 2025-05-25
* azurerm_kubernetes_cluster - support for node_provisioning_profile

BUG FIXES:

* azurerm_data_factory - fix ID parsing errors
`

	releases := parseChangelogReleases(changelog)
	if len(releases) == 0 {
		t.Fatal("Expected at least one release, got none")
	}

	rel := releases[0]
	if rel.Version != "4.57.0" {
		t.Errorf("Version = %q, want %q", rel.Version, "4.57.0")
	}

	if len(rel.Sections) != 3 {
		t.Fatalf("Expected 3 sections, got %d", len(rel.Sections))
	}

	// Check section names
	expectedSections := []string{"FEATURES", "ENHANCEMENTS", "BUG FIXES"}
	for i, expected := range expectedSections {
		if rel.Sections[i].Name != expected {
			t.Errorf("Section %d name = %q, want %q", i, rel.Sections[i].Name, expected)
		}
	}

	// Check entries count
	if len(rel.Sections[0].Entries) != 2 {
		t.Errorf("FEATURES section has %d entries, want 2", len(rel.Sections[0].Entries))
	}
	if len(rel.Sections[1].Entries) != 2 {
		t.Errorf("ENHANCEMENTS section has %d entries, want 2", len(rel.Sections[1].Entries))
	}
	if len(rel.Sections[2].Entries) != 1 {
		t.Errorf("BUG FIXES section has %d entries, want 1", len(rel.Sections[2].Entries))
	}
}
