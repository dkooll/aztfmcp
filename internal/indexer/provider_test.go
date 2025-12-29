package indexer

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/dkooll/aztfmcp/internal/database"
	"github.com/dkooll/aztfmcp/internal/testutil"
)

func TestParseGoFile(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantErr   bool
		wantDecls int
	}{
		{
			name:      "valid go file",
			content:   "package main\n\nfunc main() {}",
			wantErr:   false,
			wantDecls: 1,
		},
		{
			name:      "empty file",
			content:   "package main",
			wantErr:   false,
			wantDecls: 0,
		},
		{
			name:    "invalid syntax",
			content: "package main\n\nfunc {",
			wantErr: true,
		},
		{
			name:      "file with comments only",
			content:   "package main\n\n// This is a comment",
			wantErr:   false,
			wantDecls: 0,
		},
		{
			name:      "file with imports",
			content:   "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }",
			wantErr:   false,
			wantDecls: 2, // import + func
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := database.RepositoryFile{
				FileName: "test.go",
				FilePath: "test.go",
				Content:  tt.content,
			}

			result, err := parseGoFile(file)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result.file.Decls) != tt.wantDecls {
				t.Errorf("expected %d declarations, got %d", tt.wantDecls, len(result.file.Decls))
			}
		})
	}
}

func TestInferRegistrationKind(t *testing.T) {
	tests := []struct {
		funcName string
		want     string
	}{
		{"resourceVirtualNetwork", "resource"},
		{"dataSourceVirtualNetwork", "data_source"},
		{"DataSourceSubnet", "data_source"},
		{"resourceGroupDataSource", "data_source"},
		{"resourceGroup", "resource"},
		{"", "resource"},
		{"DATASOURCE", "data_source"},
		{"cdnFrontDoorCachePurgeAction", "action"},
		{"mssqlExecuteJobAction", "action"},
		{"privateDnsZoneListResource", "list"},
		{"networkInterfaceList", "list"},
		{"keyVaultSecretEphemeral", "ephemeral"},
		{"keyVaultCertificateEphemeralResource", "ephemeral"},
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			got := inferRegistrationKind(tt.funcName)
			if got != tt.want {
				t.Errorf("inferRegistrationKind(%q) = %q, want %q", tt.funcName, got, tt.want)
			}
		})
	}
}

func TestDisplayNameFromResource(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"azurerm_resource_group", "Resource Group"},
		{"azurerm_virtual_network", "Virtual Network"},
		{"azurerm_storage_account", "Storage Account"},
		{"resource_group", "Resource Group"},
		{"azurerm_vm", "Vm"},
		{"azurerm_", ""},
		{"", ""},
		{"azurerm_storage_account_v2", "Storage Account V2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := displayNameFromResource(tt.name)
			if got != tt.want {
				t.Errorf("displayNameFromResource(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestBoolValue(t *testing.T) {
	fset := token.NewFileSet()

	tests := []struct {
		name string
		expr string
		want bool
	}{
		{"true", "true", true},
		{"false", "false", false},
		{"other ident", "something", false},
		{"number", "123", false},
		{"string", `"true"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}
			_ = fset

			got := boolValue(expr)
			if got != tt.want {
				t.Errorf("boolValue(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestIntValue(t *testing.T) {
	tests := []struct {
		name   string
		expr   string
		want   int
		wantOK bool
	}{
		{"positive int", "42", 42, true},
		{"zero", "0", 0, true},
		{"negative", "-5", 0, false}, // unary expr, not basic lit
		{"string", `"42"`, 0, false},
		{"float", "3.14", 0, false},
		{"ident", "foo", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}

			got, ok := intValue(expr)
			if ok != tt.wantOK {
				t.Errorf("intValue(%q) ok = %v, want %v", tt.expr, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("intValue(%q) = %d, want %d", tt.expr, got, tt.want)
			}
		})
	}
}

func TestLiteralStringValue(t *testing.T) {
	fset := token.NewFileSet()

	tests := []struct {
		name string
		expr string
		want string
	}{
		{"basic string", `"hello"`, "hello"},
		{"empty string", `""`, ""},
		{"nil ident", "nil", ""},
		{"other ident", "foo", "foo"},
		{"raw string", "`raw`", "raw"},
		{"number", "123", "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}

			got := literalStringValue(fset, expr)
			if got != tt.want {
				t.Errorf("literalStringValue(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestStringListValue(t *testing.T) {
	fset := token.NewFileSet()

	tests := []struct {
		name string
		expr string
		want string
	}{
		{"single element", `[]string{"one"}`, "one"},
		{"multiple elements", `[]string{"one", "two", "three"}`, "one, two, three"},
		{"empty list", `[]string{}`, ""},
		{"not a list", `"single"`, `"single"`},
		{"ident", `items`, "items"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}

			got := stringListValue(fset, expr)
			if got != tt.want {
				t.Errorf("stringListValue(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestParseProviderRepositoryStoresResources(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := testutil.InsertRepository(t, db, "terraform-provider-azurerm")

	const content = `
package provider

import "schema"

func Provider() *schema.Provider {
	return &schema.Provider{
		ResourcesMap: map[string]*schema.Resource{
			"azurerm_example":        resourceExample(),
			"azurerm_example_nested": resourceExample(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"azurerm_example_data": dataSourceExample(),
		},
	}
}

func resourceExample() *schema.Resource {
	return &schema.Resource{
		Description:         "Example resource",
		DeprecationMessage:  "deprecated soon",
		Schema:              buildSchema(),
		CustomizeDiff:       customDiff,
		Timeouts:            &schema.ResourceTimeout{Create: "30m"},
		Importer:            &schema.ResourceImporter{StateContext: schema.ImportStatePassthroughContext},
		StateUpgraders:      []schema.StateUpgrader{{Type: schema.TypeString}},
		SchemaVersion:       1,
		MigrateState:        migrateState,
	}
}

func dataSourceExample() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			"lookup": {Type: schema.TypeString, Required: true},
		},
	}
}

func buildSchema() map[string]*schema.Schema {
	return map[string]*schema.Schema{
		"name": {
			Type:          schema.TypeString,
			Required:      true,
			ForceNew:      true,
			Description:   "name desc",
			ConflictsWith: []string{"other"},
			ExactlyOneOf:  []string{"a", "b"},
			AtLeastOneOf:  []string{"c"},
			MaxItems:      1,
			MinItems:      0,
			Sensitive:     true,
			Deprecated:    "use_other",
		},
		"nested": {
			Type:     schema.TypeList,
			Optional: true,
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					"inner": {Type: schema.TypeString, Optional: true},
				},
			},
		},
		"count": {
			Type:             schema.TypeInt,
			Optional:         true,
			ValidateFunc:     validateCount,
			DiffSuppressFunc: suppressDiff,
		},
	}
}

var (
	customDiff    = func() {}
	validateCount = func(i interface{}, k string) (warns []string, errs []error) { return }
	suppressDiff  = func(k, old, new string, d interface{}) bool { return false }
	migrateState  = func(i interface{}, meta interface{}) (interface{}, error) { return i, nil }
)
`

	testutil.InsertFile(t, db, repo.ID, "provider/provider.go", "go", content)

	s := &Syncer{db: db}
	if err := s.parseProviderRepository(repo.ID, GitHubRepo{Name: repo.Name}); err != nil {
		t.Fatalf("parseProviderRepository: %v", err)
	}

	resources, err := db.ListProviderResources("", 0)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources (2 resources + 1 data source), got %d", len(resources))
	}

	var example database.ProviderResource
	for _, r := range resources {
		if r.Name == "azurerm_example" {
			example = r
		}
		if r.Kind == "data_source" && r.Name == "azurerm_example_data" && !strings.Contains(r.DisplayName.String, "Data") {
			t.Fatalf("expected data source display name, got %s", r.DisplayName.String)
		}
	}
	if example.Name == "" {
		t.Fatalf("expected azurerm_example to be parsed")
	}
	if example.DisplayName.String != "Example" {
		t.Fatalf("unexpected display name: %s", example.DisplayName.String)
	}
	if example.BreakingChanges.String == "" {
		t.Fatalf("expected breaking changes summary for force_new/conflicts")
	}

	attrs, err := db.GetProviderResourceAttributes(example.ID)
	if err != nil {
		t.Fatalf("get attributes: %v", err)
	}
	if len(attrs) != 3 {
		t.Fatalf("expected 3 attributes, got %d", len(attrs))
	}
	var nested database.ProviderAttribute
	for _, a := range attrs {
		if a.Name == "name" && !a.Required {
			t.Fatalf("expected required attribute 'name'")
		}
		if a.Name == "count" && a.Validation.String == "" {
			t.Fatalf("expected validation on count attribute")
		}
		if a.NestedBlock {
			nested = a
		}
	}
	if nested.Name != "nested" {
		t.Fatalf("expected nested attribute to be marked, got %s", nested.Name)
	}

	source, err := db.GetProviderResourceSource(example.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if !strings.Contains(source.FunctionSnippet.String, "resourceExample") {
		t.Fatalf("expected function snippet to include resourceExample")
	}
	if source.CustomizeDiffSnippet.String == "" {
		t.Fatalf("expected customize diff snippet to be captured")
	}
	if source.SchemaSnippet.String == "" {
		t.Fatalf("expected schema snippet to be captured, got empty")
	}
}

func TestIdentName(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"simple ident", "foo", "foo"},
		{"number", "123", ""},
		{"string", `"foo"`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}

			got := identName(expr)
			if got != tt.want {
				t.Errorf("identName(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestExprToString(t *testing.T) {
	fset := token.NewFileSet()

	tests := []struct {
		name string
		expr string
		want string
	}{
		{"ident", "foo", "foo"},
		{"call", "foo()", "foo()"},
		{"selector", "pkg.Foo", "pkg.Foo"},
		{"binary", "a + b", "a + b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}

			got := exprToString(fset, expr)
			if got != tt.want {
				t.Errorf("exprToString(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}

	t.Run("nil expr", func(t *testing.T) {
		got := exprToString(fset, nil)
		if got != "" {
			t.Errorf("exprToString(nil) = %q, want empty string", got)
		}
	})
}

func TestNullString(t *testing.T) {
	tests := []struct {
		input string
		want  sql.NullString
	}{
		{"hello", sql.NullString{String: "hello", Valid: true}},
		{"", sql.NullString{}},
		{"  ", sql.NullString{}},
		{"  trimmed  ", sql.NullString{String: "trimmed", Valid: true}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := nullString(tt.input)
			if got != tt.want {
				t.Errorf("nullString(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSummarizeBreakingAttributes(t *testing.T) {
	tests := []struct {
		name  string
		attrs []database.ProviderAttribute
		want  string
	}{
		{
			name:  "empty",
			attrs: nil,
			want:  "",
		},
		{
			name: "force new only",
			attrs: []database.ProviderAttribute{
				{Name: "location", ForceNew: true},
				{Name: "name", ForceNew: true},
			},
			want: "ForceNew attributes: location, name",
		},
		{
			name: "conflicts only",
			attrs: []database.ProviderAttribute{
				{Name: "a", ConflictsWith: sql.NullString{String: "b", Valid: true}},
			},
			want: "Conflicts: a ↔ b",
		},
		{
			name: "exactly one of only",
			attrs: []database.ProviderAttribute{
				{Name: "x", ExactlyOneOf: sql.NullString{String: "y", Valid: true}},
			},
			want: "Mutually exclusive: x ↔ y",
		},
		{
			name: "all types",
			attrs: []database.ProviderAttribute{
				{Name: "location", ForceNew: true},
				{Name: "a", ConflictsWith: sql.NullString{String: "b", Valid: true}},
				{Name: "x", ExactlyOneOf: sql.NullString{String: "y", Valid: true}},
			},
			want: "ForceNew attributes: location\nConflicts: a ↔ b\nMutually exclusive: x ↔ y",
		},
		{
			name: "invalid null strings ignored",
			attrs: []database.ProviderAttribute{
				{Name: "a", ConflictsWith: sql.NullString{Valid: false}},
				{Name: "b", ExactlyOneOf: sql.NullString{Valid: false}},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeBreakingAttributes(tt.attrs)
			if got != tt.want {
				t.Errorf("summarizeBreakingAttributes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFunctionNameFromExpr(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"simple call", "foo()", "foo"},
		{"method call", "pkg.Method()", "Method"},
		{"not a call", "foo", ""},
		{"nested call", "outer(inner())", "outer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}

			got := functionNameFromExpr(expr)
			if got != tt.want {
				t.Errorf("functionNameFromExpr(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestSchemaLiteral(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantNil bool
	}{
		{"composite literal", "Foo{}", false},
		{"address of literal", "&Foo{}", false},
		{"ident", "foo", true},
		{"call", "foo()", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expr: %v", err)
			}

			got := schemaLiteral(expr)
			if tt.wantNil && got != nil {
				t.Errorf("schemaLiteral(%q) = %v, want nil", tt.expr, got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("schemaLiteral(%q) = nil, want non-nil", tt.expr)
			}
		})
	}
}

func TestExtractSchemaExpr(t *testing.T) {
	src := `&schema.Resource{
		Schema: map[string]*schema.Schema{},
		Description: "test",
	}`

	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	lit := schemaLiteral(expr)
	if lit == nil {
		t.Fatal("expected composite literal")
	}

	schemaExpr := extractSchemaExpr(lit)
	if schemaExpr == nil {
		t.Error("expected Schema field to be extracted")
	}
}

func TestExtractFieldExpr(t *testing.T) {
	src := `&schema.Resource{
		Schema: map[string]*schema.Schema{},
		CustomizeDiff: customDiffFunc,
		Timeouts: &schema.ResourceTimeout{},
	}`

	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	lit := schemaLiteral(expr)
	if lit == nil {
		t.Fatal("expected composite literal")
	}

	tests := []struct {
		field   string
		wantNil bool
	}{
		{"Schema", false},
		{"CustomizeDiff", false},
		{"Timeouts", false},
		{"NonExistent", true},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := extractFieldExpr(lit, tt.field)
			if tt.wantNil && got != nil {
				t.Errorf("extractFieldExpr(%q) = %v, want nil", tt.field, got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("extractFieldExpr(%q) = nil, want non-nil", tt.field)
			}
		})
	}
}

func TestBuildAttributeFromSchema(t *testing.T) {
	fset := token.NewFileSet()

	src := `&schema.Schema{
		Type:        schema.TypeString,
		Required:    true,
		Description: "The name",
		ForceNew:    true,
		Sensitive:   false,
		MaxItems:    5,
	}`

	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	lit := schemaLiteral(expr)
	if lit == nil {
		t.Fatal("expected composite literal")
	}

	attr := buildAttributeFromSchema(fset, "test_attr", lit)

	if attr.Name != "test_attr" {
		t.Errorf("Name = %q, want %q", attr.Name, "test_attr")
	}
	if !attr.Required {
		t.Error("Required should be true")
	}
	if !attr.ForceNew {
		t.Error("ForceNew should be true")
	}
	if attr.Sensitive {
		t.Error("Sensitive should be false")
	}
	if !attr.Description.Valid || attr.Description.String != "The name" {
		t.Errorf("Description = %+v, want 'The name'", attr.Description)
	}
	if !attr.MaxItems.Valid || attr.MaxItems.Int64 != 5 {
		t.Errorf("MaxItems = %+v, want 5", attr.MaxItems)
	}
}

func TestReturnsResourceType(t *testing.T) {
	fset := token.NewFileSet()

	tests := []struct {
		name string
		typ  string
		want bool
	}{
		{"schema.Resource pointer", "*schema.Resource", true},
		{"pluginsdk.Resource pointer", "*pluginsdk.Resource", true},
		{"string", "string", false},
		{"error", "error", false},
		{"other struct", "*Foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\nfunc f() " + tt.typ + " { return nil }"
			f, err := parser.ParseFile(fset, "", src, 0)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			fn := f.Decls[0].(*ast.FuncDecl)
			got := returnsResourceType(fn.Type.Results.List[0].Type, fset)
			if got != tt.want {
				t.Errorf("returnsResourceType(%q) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

func TestNewProviderParser(t *testing.T) {
	fset := token.NewFileSet()

	src1 := `package main
func resourceOne() *schema.Resource { return nil }
func helper() string { return "" }`

	src2 := `package main
func resourceTwo() *schema.Resource { return nil }
func resourceOne() *schema.Resource { return nil }` // duplicate

	f1, _ := parser.ParseFile(fset, "one.go", src1, 0)
	f2, _ := parser.ParseFile(fset, "two.go", src2, 0)

	files := []providerGoFile{
		{file: f1, fset: fset, repositoryFile: database.RepositoryFile{FilePath: "one.go"}},
		{file: f2, fset: fset, repositoryFile: database.RepositoryFile{FilePath: "two.go"}},
	}

	p := newProviderParser(files)

	// Should have 3 unique function names (resourceOne from first file wins)
	if len(p.funcByName) != 3 {
		t.Errorf("expected 3 functions in index, got %d", len(p.funcByName))
	}

	// resourceOne should map to first file
	if f, ok := p.funcByName["resourceOne"]; !ok || f.repositoryFile.FilePath != "one.go" {
		t.Error("resourceOne should map to one.go (first seen)")
	}
}

func TestExtractElemSummary(t *testing.T) {
	fset := token.NewFileSet()

	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "schema type",
			expr: `&schema.Schema{Type: schema.TypeString}`,
			want: "Type=schema.TypeString",
		},
		{
			name: "simple ident",
			expr: `schema.TypeString`,
			want: "schema.TypeString",
		},
		{
			name: "multiple fields",
			expr: `&schema.Schema{Type: schema.TypeString, Required: true}`,
			want: "Type=schema.TypeString, Required=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			got := extractElemSummary(fset, expr)
			if !strings.Contains(got, "Type") && tt.name != "simple ident" {
				t.Errorf("extractElemSummary(%q) = %q, want to contain Type", tt.expr, got)
			}
		})
	}
}

func TestStructNameToResourceName(t *testing.T) {
	tests := []struct {
		structName string
		want       string
	}{
		{"AvailabilitySetResource", "azurerm_availability_set"},
		{"VirtualNetworkDataSource", "azurerm_virtual_network"},
		{"CdnFrontDoorCachePurgeAction", "azurerm_cdn_front_door_cache_purge"},
		{"PrivateDnsZoneList", "azurerm_private_dns_zone"},
		{"KeyVaultSecretEphemeral", "azurerm_key_vault_secret"},
		{"DiskResource", "azurerm_disk"},
		{"VMResource", "azurerm_v_m"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.structName, func(t *testing.T) {
			got := structNameToResourceName(tt.structName)
			if got != tt.want {
				t.Errorf("structNameToResourceName(%q) = %q, want %q", tt.structName, got, tt.want)
			}
		})
	}
}

func TestExtractServiceNameValue(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "compute_service",
			source: `package compute

type Registration struct{}

func (r Registration) Name() string {
	return "Compute"
}`,
			want: "Compute",
		},
		{
			name: "network_service",
			source: `package network

type Registration struct{}

func (r Registration) Name() string {
	return "Network"
}`,
			want: "Network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "registration.go", tt.source, 0)
			if err != nil {
				t.Fatalf("Failed to parse source: %v", err)
			}

			got := extractServiceNameValue(file)
			if got != tt.want {
				t.Errorf("extractServiceNameValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractWebsiteCategories(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "single_category",
			source: `package compute

func (r Registration) WebsiteCategories() []string {
	return []string{"Compute"}
}`,
			want: "Compute",
		},
		{
			name: "multiple_categories",
			source: `package network

func (r Registration) WebsiteCategories() []string {
	return []string{"Networking", "Virtual Networks"}
}`,
			want: "Networking,Virtual Networks",
		},
		{
			name: "empty_categories",
			source: `package foo

func (r Registration) WebsiteCategories() []string {
	return []string{}
}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "registration.go", tt.source, 0)
			if err != nil {
				t.Fatalf("Failed to parse source: %v", err)
			}

			got := extractWebsiteCategories(file)
			if got != tt.want {
				t.Errorf("extractWebsiteCategories() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractGitHubLabel(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "compute_label",
			source: `package compute

func (r Registration) AssociatedGitHubLabel() string {
	return "service/compute"
}`,
			want: "service/compute",
		},
		{
			name: "dynatrace_label",
			source: `package dynatrace

func (r Registration) AssociatedGitHubLabel() string {
	return "service/dynatrace"
}`,
			want: "service/dynatrace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "registration.go", tt.source, 0)
			if err != nil {
				t.Fatalf("Failed to parse source: %v", err)
			}

			got := extractGitHubLabel(file)
			if got != tt.want {
				t.Errorf("extractGitHubLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractServiceNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"internal/services/compute/virtual_machine.go", "compute"},
		{"internal/services/network/vnet_resource.go", "network"},
		{"internal/services/storage/account.go", "storage"},
		{"internal/provider/services.go", ""},
		{"some/other/path.go", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractServiceNameFromPath(tt.path)
			if got != tt.want {
				t.Errorf("extractServiceNameFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
