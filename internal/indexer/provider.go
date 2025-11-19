package indexer

import (
	"bytes"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/dkooll/aztfmcp/internal/database"
)

func (s *Syncer) parseProviderRepository(repositoryID int64, repo GitHubRepo) error {
	files, err := s.db.GetRepositoryFiles(repositoryID)
	if err != nil {
		return err
	}

	var goFiles []providerGoFile
	for _, file := range files {
		if !strings.HasSuffix(file.FileName, ".go") {
			continue
		}

		goFile, err := parseGoFile(file)
		if err != nil {
			log.Printf("Warning: failed to parse Go file %s: %v", file.FilePath, err)
			continue
		}
		goFiles = append(goFiles, goFile)
	}

	if len(goFiles) == 0 {
		return fmt.Errorf("no Go files discovered in %s", repo.Name)
	}

	parser := newProviderParser(goFiles)
	parsedResources := parser.Parse()
	if len(parsedResources) == 0 {
		return fmt.Errorf("no provider resources or data sources discovered in %s", repo.Name)
	}

	for _, resource := range parsedResources {
		resource.resource.RepositoryID = repositoryID

		resourceID, err := s.db.InsertProviderResource(&resource.resource)
		if err != nil {
			log.Printf("Warning: failed to persist provider resource %s: %v", resource.resource.Name, err)
			continue
		}

		for idx := range resource.attributes {
			attr := resource.attributes[idx]
			attr.ResourceID = resourceID
			if err := s.db.InsertProviderAttribute(&attr); err != nil {
				log.Printf("Warning: failed to persist attribute %s on %s: %v", attr.Name, resource.resource.Name, err)
			}
		}

		if resource.source != nil {
			if err := s.db.UpsertProviderResourceSource(
				resourceID,
				resource.source.name,
				resource.source.filePath,
				resource.source.functionSnippet(),
				resource.source.schemaSnippet(),
				resource.source.customizeDiffSnippet(),
				resource.source.timeoutsJSON(),
				resource.source.stateUpgradersSnippet(),
				resource.source.importerSnippet(),
			); err != nil {
				log.Printf("Warning: failed to store source snippet for %s: %v", resource.resource.Name, err)
			}
		}
	}

	log.Printf("Indexed %d provider definitions (resources + data sources)", len(parsedResources))
	return nil
}

type providerGoFile struct {
	repositoryFile database.RepositoryFile
	file           *ast.File
	fset           *token.FileSet
}

func parseGoFile(file database.RepositoryFile) (providerGoFile, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, file.FilePath, file.Content, parser.ParseComments)
	if err != nil {
		return providerGoFile{}, err
	}
	return providerGoFile{
		repositoryFile: file,
		file:           astFile,
		fset:           fset,
	}, nil
}

type parsedProviderResource struct {
	resource   database.ProviderResource
	attributes []database.ProviderAttribute
	source     *resourceFunc
}

type resourceFunc struct {
	name     string
	filePath string
	file     providerGoFile
	literal  *ast.CompositeLit
	decl     *ast.FuncDecl
}

func (f *resourceFunc) functionSnippet() string {
	if f == nil || f.decl == nil {
		return ""
	}
	return snippetFromRange(f.file, f.decl.Pos(), f.decl.End())
}

func (f *resourceFunc) schemaSnippet() string {
	if f == nil || f.literal == nil {
		return ""
	}

	schemaExpr := extractSchemaExpr(f.literal)
	if schemaExpr == nil {
		return exprToString(f.file.fset, f.literal)
	}
	return exprToString(f.file.fset, schemaExpr)
}

func (f *resourceFunc) customizeDiffSnippet() string {
	if f == nil || f.literal == nil {
		return ""
	}

	customizeDiffExpr := extractFieldExpr(f.literal, "CustomizeDiff")
	if customizeDiffExpr == nil {
		return ""
	}
	return exprToString(f.file.fset, customizeDiffExpr)
}

func (f *resourceFunc) timeoutsJSON() string {
	if f == nil || f.literal == nil {
		return ""
	}

	timeoutsExpr := extractFieldExpr(f.literal, "Timeouts")
	if timeoutsExpr == nil {
		return ""
	}
	return exprToString(f.file.fset, timeoutsExpr)
}

func (f *resourceFunc) stateUpgradersSnippet() string {
	if f == nil || f.literal == nil {
		return ""
	}

	stateUpgradersExpr := extractFieldExpr(f.literal, "StateUpgraders")
	if stateUpgradersExpr == nil {
		return ""
	}
	return exprToString(f.file.fset, stateUpgradersExpr)
}

func (f *resourceFunc) importerSnippet() string {
	if f == nil || f.literal == nil {
		return ""
	}

	importerExpr := extractFieldExpr(f.literal, "Importer")
	if importerExpr == nil {
		return ""
	}
	return exprToString(f.file.fset, importerExpr)
}

type resourceRegistration struct {
	TypeName string
	FuncName string
	Kind     string
}

type providerParser struct {
	files []providerGoFile
}

func newProviderParser(files []providerGoFile) *providerParser {
	return &providerParser{files: files}
}

func (p *providerParser) Parse() []parsedProviderResource {
	funcs := p.collectResourceFunctions()
	registrations := p.collectResourceRegistrations()

	var parsed []parsedProviderResource
	for _, reg := range registrations {
		fn := funcs[reg.FuncName]
		if fn == nil {
			log.Printf("Warning: registry entry %s -> %s missing function definition", reg.TypeName, reg.FuncName)
			continue
		}

		resource, err := buildParsedResource(reg, fn)
		if err != nil {
			log.Printf("Warning: failed to parse schema for %s: %v", reg.TypeName, err)
			continue
		}
		parsed = append(parsed, resource)
	}

	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].resource.Name < parsed[j].resource.Name
	})

	return parsed
}

func (p *providerParser) collectResourceFunctions() map[string]*resourceFunc {
	funcs := make(map[string]*resourceFunc)

	for _, file := range p.files {
		for _, decl := range file.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || fn.Body == nil || fn.Type == nil || fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
				continue
			}

			if !returnsResourceType(fn.Type.Results.List[0].Type, file.fset) {
				continue
			}

			lit := extractResourceLiteral(fn.Body)
			if lit == nil {
				continue
			}

			funcs[fn.Name.Name] = &resourceFunc{
				name:     fn.Name.Name,
				filePath: file.repositoryFile.FilePath,
				file:     file,
				literal:  lit,
				decl:     fn,
			}
		}
	}

	return funcs
}

func (p *providerParser) collectResourceRegistrations() []resourceRegistration {
	var registrations []resourceRegistration
	seen := make(map[string]struct{})

	for _, file := range p.files {
		ast.Inspect(file.file, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}

			mapType, ok := lit.Type.(*ast.MapType)
			if !ok {
				return true
			}

			if !strings.HasSuffix(exprToString(file.fset, mapType.Value), ".Resource") {
				return true
			}

			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}

				name := literalStringValue(file.fset, kv.Key)
				if name == "" {
					continue
				}

				funcName := functionNameFromExpr(kv.Value)
				if funcName == "" {
					continue
				}

				if _, exists := seen[name]; exists {
					continue
				}

				registrations = append(registrations, resourceRegistration{
					TypeName: name,
					FuncName: funcName,
					Kind:     inferRegistrationKind(funcName),
				})
				seen[name] = struct{}{}
			}

			return true
		})
	}

	return registrations
}

func returnsResourceType(expr ast.Expr, fset *token.FileSet) bool {
	typeString := exprToString(fset, expr)
	return strings.HasSuffix(typeString, ".Resource")
}

func extractResourceLiteral(body *ast.BlockStmt) *ast.CompositeLit {
	for _, stmt := range body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) == 0 {
			continue
		}

		switch expr := ret.Results[0].(type) {
		case *ast.UnaryExpr:
			if expr.Op == token.AND {
				if lit, ok := expr.X.(*ast.CompositeLit); ok {
					return lit
				}
			}
		case *ast.CompositeLit:
			return expr
		}
	}
	return nil
}

func buildParsedResource(reg resourceRegistration, fn *resourceFunc) (parsedProviderResource, error) {
	resource := database.ProviderResource{
		Name:        reg.TypeName,
		Kind:        reg.Kind,
		DisplayName: nullString(displayNameFromResource(reg.TypeName)),
		FilePath:    nullString(fn.filePath),
	}

	var attrs []database.ProviderAttribute

	for _, elt := range fn.literal.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key := identName(kv.Key)
		switch key {
		case "Description":
			resource.Description = nullString(literalStringValue(fn.file.fset, kv.Value))
		case "DeprecationMessage":
			resource.DeprecationMessage = nullString(literalStringValue(fn.file.fset, kv.Value))
		case "Schema":
			schemaAttrs := parseSchemaAttributes(fn.file.fset, kv.Value)
			attrs = append(attrs, schemaAttrs...)
		}
	}

	resource.BreakingChanges = nullString(summarizeBreakingAttributes(attrs))
	return parsedProviderResource{resource: resource, attributes: attrs, source: fn}, nil
}

func parseSchemaAttributes(fset *token.FileSet, expr ast.Expr) []database.ProviderAttribute {
	lit := schemaLiteral(expr)
	if lit == nil {
		return nil
	}

	var attrs []database.ProviderAttribute
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		name := literalStringValue(fset, kv.Key)
		if name == "" {
			continue
		}

		schema := schemaLiteral(kv.Value)
		if schema == nil {
			continue
		}

		attr := buildAttributeFromSchema(fset, name, schema)
		attrs = append(attrs, attr)
	}

	return attrs
}

func buildAttributeFromSchema(fset *token.FileSet, name string, schema *ast.CompositeLit) database.ProviderAttribute {
	attr := database.ProviderAttribute{
		Name: name,
	}

	for _, elt := range schema.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key := identName(kv.Key)
		switch key {
		case "Type":
			attr.Type = nullString(exprToString(fset, kv.Value))
		case "Required":
			attr.Required = boolValue(kv.Value)
		case "Optional":
			attr.Optional = boolValue(kv.Value)
		case "Computed":
			attr.Computed = boolValue(kv.Value)
		case "ForceNew":
			attr.ForceNew = boolValue(kv.Value)
		case "Sensitive":
			attr.Sensitive = boolValue(kv.Value)
		case "Deprecated":
			attr.Deprecated = nullString(literalStringValue(fset, kv.Value))
		case "Description":
			attr.Description = nullString(literalStringValue(fset, kv.Value))
		case "ConflictsWith":
			attr.ConflictsWith = nullString(stringListValue(fset, kv.Value))
		case "ExactlyOneOf":
			attr.ExactlyOneOf = nullString(stringListValue(fset, kv.Value))
		case "AtLeastOneOf":
			attr.AtLeastOneOf = nullString(stringListValue(fset, kv.Value))
		case "MaxItems":
			if v, ok := intValue(kv.Value); ok {
				attr.MaxItems = sql.NullInt64{Int64: int64(v), Valid: true}
			}
		case "MinItems":
			if v, ok := intValue(kv.Value); ok {
				attr.MinItems = sql.NullInt64{Int64: int64(v), Valid: true}
			}
		case "Elem":
			elemText := exprToString(fset, kv.Value)
			attr.ElemType = nullString(elemText)
			attr.ElemSummary = nullString(extractElemSummary(fset, kv.Value))
			if strings.Contains(elemText, ".Resource") {
				attr.NestedBlock = true
			}
		case "ValidateFunc", "ValidateDiagFunc":
			attr.Validation = nullString(exprToString(fset, kv.Value))
		case "DiffSuppressFunc":
			attr.DiffSuppress = nullString(exprToString(fset, kv.Value))
		}
	}

	return attr
}

func schemaLiteral(expr ast.Expr) *ast.CompositeLit {
	switch v := expr.(type) {
	case *ast.CompositeLit:
		return v
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			if lit, ok := v.X.(*ast.CompositeLit); ok {
				return lit
			}
		}
	}
	return nil
}

func snippetFromRange(file providerGoFile, start, end token.Pos) string {
	content := file.repositoryFile.Content
	startPos := file.fset.Position(start)
	endPos := file.fset.Position(end)
	if startPos.Offset < 0 {
		startPos.Offset = 0
	}
	if endPos.Offset > len(content) {
		endPos.Offset = len(content)
	}
	if endPos.Offset <= startPos.Offset {
		return ""
	}
	if startPos.Offset >= len(content) {
		return ""
	}
	return content[startPos.Offset:endPos.Offset]
}

func extractSchemaExpr(resourceLit *ast.CompositeLit) ast.Expr {
	for _, elt := range resourceLit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if identName(kv.Key) == "Schema" {
			return kv.Value
		}
	}
	return nil
}

// extractFieldExpr extracts any field by name from a resource composite literal
func extractFieldExpr(resourceLit *ast.CompositeLit, fieldName string) ast.Expr {
	for _, elt := range resourceLit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if identName(kv.Key) == fieldName {
			return kv.Value
		}
	}
	return nil
}

func literalStringValue(fset *token.FileSet, expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			text, err := strconv.Unquote(v.Value)
			if err == nil {
				return text
			}
		}
	case *ast.Ident:
		if v.Name == "nil" {
			return ""
		}
	}
	return strings.TrimSpace(exprToString(fset, expr))
}

func stringListValue(fset *token.FileSet, expr ast.Expr) string {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return strings.TrimSpace(exprToString(fset, expr))
	}

	var parts []string
	for _, elt := range lit.Elts {
		val := literalStringValue(fset, elt)
		if val != "" {
			parts = append(parts, val)
		}
	}
	return strings.Join(parts, ", ")
}

func intValue(expr ast.Expr) (int, bool) {
	basic, ok := expr.(*ast.BasicLit)
	if !ok || basic.Kind != token.INT {
		return 0, false
	}
	val, err := strconv.Atoi(basic.Value)
	if err != nil {
		return 0, false
	}
	return val, true
}

func boolValue(expr ast.Expr) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "true"
	}
	return false
}

func extractElemSummary(fset *token.FileSet, expr ast.Expr) string {
	lit := schemaLiteral(expr)
	if lit == nil {
		return exprToString(fset, expr)
	}

	var summary []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := identName(kv.Key)
		if key == "" {
			continue
		}
		val := literalStringValue(fset, kv.Value)
		if val == "" {
			val = exprToString(fset, kv.Value)
		}
		summary = append(summary, fmt.Sprintf("%s=%s", key, val))
	}

	return strings.Join(summary, ", ")
}

func identName(expr ast.Expr) string {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func functionNameFromExpr(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}

	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	default:
		return ""
	}
}

func inferRegistrationKind(funcName string) string {
	lower := strings.ToLower(funcName)
	if strings.Contains(lower, "datasource") {
		return "data_source"
	}
	return "resource"
}

func exprToString(fset *token.FileSet, expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return ""
	}
	return buf.String()
}

func displayNameFromResource(name string) string {
	trimmed := strings.TrimPrefix(name, "azurerm_")
	parts := strings.Split(trimmed, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func summarizeBreakingAttributes(attrs []database.ProviderAttribute) string {
	var forceNew []string
	var conflicts []string
	var exclusives []string

	for _, attr := range attrs {
		if attr.ForceNew {
			forceNew = append(forceNew, attr.Name)
		}
		if attr.ConflictsWith.Valid {
			conflicts = append(conflicts, fmt.Sprintf("%s ↔ %s", attr.Name, attr.ConflictsWith.String))
		}
		if attr.ExactlyOneOf.Valid {
			exclusives = append(exclusives, fmt.Sprintf("%s ↔ %s", attr.Name, attr.ExactlyOneOf.String))
		}
	}

	var sections []string
	if len(forceNew) > 0 {
		sections = append(sections, fmt.Sprintf("ForceNew attributes: %s", strings.Join(forceNew, ", ")))
	}
	if len(conflicts) > 0 {
		sections = append(sections, fmt.Sprintf("Conflicts: %s", strings.Join(conflicts, "; ")))
	}
	if len(exclusives) > 0 {
		sections = append(sections, fmt.Sprintf("Mutually exclusive: %s", strings.Join(exclusives, "; ")))
	}

	return strings.Join(sections, "\n")
}
