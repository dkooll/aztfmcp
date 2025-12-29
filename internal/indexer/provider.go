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

	// Parse and store service metadata
	servicesByName, err := s.parseServiceMetadata(repositoryID, goFiles)
	if err != nil {
		log.Printf("Warning: failed to parse service metadata: %v", err)
	}

	parser := newProviderParser(goFiles)
	parsedResources := parser.Parse()
	if len(parsedResources) == 0 {
		return fmt.Errorf("no provider resources or data sources discovered in %s", repo.Name)
	}

	for _, resource := range parsedResources {
		resource.resource.RepositoryID = repositoryID

		// Link resource to service if file path indicates which service it belongs to
		if resource.resource.FilePath.Valid {
			if serviceName := extractServiceNameFromPath(resource.resource.FilePath.String); serviceName != "" {
				if serviceID, ok := servicesByName[serviceName]; ok {
					resource.resource.ServiceID = sql.NullInt64{Int64: serviceID, Valid: true}
				}
			}
		}

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
	parser         *providerParser
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
	files      []providerGoFile
	funcByName map[string]providerGoFile
}

func newProviderParser(files []providerGoFile) *providerParser {
	funcByName := make(map[string]providerGoFile)
	for _, f := range files {
		for _, decl := range f.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name != nil {
				// prefer first-seen definition; same-file lookups are handled separately
				if _, exists := funcByName[fn.Name.Name]; !exists {
					funcByName[fn.Name.Name] = f
				}
			}
		}
	}
	return &providerParser{files: files, funcByName: funcByName}
}

func (p *providerParser) Parse() []parsedProviderResource {
	funcs := p.collectResourceFunctions()
	registrations := p.collectResourceRegistrations()

	var parsed []parsedProviderResource
	for _, reg := range registrations {
		// Debug specific resources
		if reg.TypeName == "azurerm_resource_group" || reg.TypeName == "azurerm_virtual_network" {
			log.Printf("DEBUG: Processing %s (kind: %s, func: %s)", reg.TypeName, reg.Kind, reg.FuncName)
		}

		// Skip typed resources without function definitions (they use struct methods)
		if reg.FuncName == "" {
			// Create minimal resource entry for typed resources
			parsed = append(parsed, parsedProviderResource{
				resource: database.ProviderResource{
					Name:        reg.TypeName,
					DisplayName: sql.NullString{String: displayNameFromResource(reg.TypeName), Valid: true},
					Kind:        reg.Kind,
				},
				attributes: []database.ProviderAttribute{},
				source:     nil,
			})
			continue
		}

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
	seen := make(map[string]struct{}) // dedupe only identical name+kind
	var registrations []resourceRegistration

	// Collect untyped (legacy) registrations: map[string]*pluginsdk.Resource
	untypedRegs := p.collectUntypedRegistrations(seen)
	registrations = append(registrations, untypedRegs...)

	// Collect typed (modern) registrations: []sdk.Resource
	typedRegs := p.collectTypedRegistrations(seen)
	registrations = append(registrations, typedRegs...)

	return registrations
}

func (p *providerParser) collectUntypedRegistrations(seen map[string]struct{}) []resourceRegistration {
	var registrations []resourceRegistration
	mapCount := 0

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

			mapValueType := exprToString(file.fset, mapType.Value)
			if strings.HasSuffix(mapValueType, ".Resource") {
				mapCount++
				if mapCount <= 3 {
					log.Printf("DEBUG: Found untyped resource map in %s with type %s", file.repositoryFile.FilePath, mapValueType)
				}
			}

			if !strings.HasSuffix(mapValueType, ".Resource") {
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

				reg := resourceRegistration{
					TypeName: name,
					FuncName: funcName,
					Kind:     inferRegistrationKind(funcName),
				}

				key := fmt.Sprintf("%s|%s", reg.TypeName, reg.Kind)
				if _, exists := seen[key]; exists {
					return true
				}

				seen[key] = struct{}{}
				registrations = append(registrations, reg)
			}

			return true
		})
	}

	return registrations
}

func (p *providerParser) collectTypedRegistrations(seen map[string]struct{}) []resourceRegistration {
	var registrations []resourceRegistration

	for _, file := range p.files {
		ast.Inspect(file.file, func(node ast.Node) bool {
			fn, ok := node.(*ast.FuncDecl)
			if !ok || fn.Name == nil || fn.Type == nil || fn.Type.Results == nil {
				return true
			}

			methodName := fn.Name.Name
			if methodName != "Resources" && methodName != "DataSources" {
				return true
			}

			if len(fn.Type.Results.List) == 0 {
				return true
			}

			returnType := exprToString(file.fset, fn.Type.Results.List[0].Type)
			if !strings.Contains(returnType, "[]") || !strings.Contains(returnType, ".Resource") {
				return true
			}

			if fn.Body != nil {
				for _, stmt := range fn.Body.List {
					if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
						if lit, ok := ret.Results[0].(*ast.CompositeLit); ok {
							for _, elt := range lit.Elts {
								resourceType := extractTypedResourceName(elt)
								if resourceType != "" {
									kind := "resource"
									if methodName == "DataSources" {
										kind = "data_source"
									}

									key := fmt.Sprintf("%s|%s", resourceType, kind)
									if _, exists := seen[key]; !exists {
										seen[key] = struct{}{}
										registrations = append(registrations, resourceRegistration{
											TypeName: resourceType,
											FuncName: "",
											Kind:     kind,
										})
									}
								}
							}
						}
					}
				}
			}

			return true
		})
	}

	if len(registrations) > 0 {
		log.Printf("DEBUG: Found %d typed resource registrations", len(registrations))
	}

	return registrations
}

func extractTypedResourceName(expr ast.Expr) string {
	// Handle CompositeLit like AvailabilitySetResource{}
	if lit, ok := expr.(*ast.CompositeLit); ok {
		if ident, ok := lit.Type.(*ast.Ident); ok {
			return structNameToResourceName(ident.Name)
		}
	}

	// Handle bare identifiers
	if ident, ok := expr.(*ast.Ident); ok {
		return structNameToResourceName(ident.Name)
	}

	return ""
}

func structNameToResourceName(structName string) string {
	// Convert "AvailabilitySetResource" to "azurerm_availability_set"
	// Convert "VirtualNetworkDataSource" to "azurerm_virtual_network"

	structName = strings.TrimSuffix(structName, "Resource")
	structName = strings.TrimSuffix(structName, "DataSource")
	structName = strings.TrimSuffix(structName, "Action")
	structName = strings.TrimSuffix(structName, "List")
	structName = strings.TrimSuffix(structName, "Ephemeral")

	if structName == "" {
		return ""
	}

	// Convert PascalCase to snake_case
	var result strings.Builder
	for i, r := range structName {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}

	return "azurerm_" + strings.ToLower(result.String())
}

func returnsResourceType(expr ast.Expr, fset *token.FileSet) bool {
	typeString := exprToString(fset, expr)
	return strings.HasSuffix(typeString, ".Resource")
}

func extractResourceLiteral(body *ast.BlockStmt) *ast.CompositeLit {
	assigned := make(map[string]*ast.CompositeLit)

	for _, stmt := range body.List {
		if assign, ok := stmt.(*ast.AssignStmt); ok && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
			if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
				if lit := schemaLiteral(assign.Rhs[0]); lit != nil {
					assigned[ident.Name] = lit
				}
			}
		}

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
		case *ast.Ident:
			if lit, ok := assigned[expr.Name]; ok {
				return lit
			}
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
		APIVersion:  nullString(extractAPIVersionFromFile(fn.file)),
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
			schemaAttrs := parseSchemaAttributes(fn.file, kv.Value)
			attrs = append(attrs, schemaAttrs...)
		}
	}

	resource.BreakingChanges = nullString(summarizeBreakingAttributes(attrs))
	return parsedProviderResource{resource: resource, attributes: attrs, source: fn}, nil
}

func parseSchemaAttributes(file providerGoFile, expr ast.Expr) []database.ProviderAttribute {
	lit := schemaLiteral(expr)

	if lit == nil {
		if callExpr, ok := expr.(*ast.CallExpr); ok {
			funcName := functionNameFromExpr(callExpr)
			if funcName != "" {
				log.Printf("DEBUG: Attempting to resolve schema function: %s in file %s", funcName, file.repositoryFile.FilePath)
				lit = findSchemaFunctionReturn(file, funcName)
				if lit != nil {
					log.Printf("DEBUG: Successfully resolved schema function: %s", funcName)
				} else {
					log.Printf("DEBUG: Failed to resolve schema function: %s", funcName)
				}
			}
		}
	}

	if lit == nil {
		return nil
	}

	var attrs []database.ProviderAttribute
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		name := literalStringValue(file.fset, kv.Key)
		if name == "" {
			continue
		}

		schema := schemaLiteral(kv.Value)
		if schema == nil {
			attrs = append(attrs, database.ProviderAttribute{Name: name})
			continue
		}

		attr := buildAttributeFromSchema(file.fset, name, schema)
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

// findSchemaFunctionReturn finds a function by name in the file and extracts its return value
func findSchemaFunctionReturn(file providerGoFile, funcName string) *ast.CompositeLit {
	// Prefer same-file definition first
	if lit := findSchemaFunctionReturnInFile(file, funcName); lit != nil {
		return lit
	}

	// Fallback: look in other files via parser registry (if available)
	if parser := file.parser; parser != nil {
		if other, ok := parser.funcByName[funcName]; ok && other.repositoryFile.FilePath != file.repositoryFile.FilePath {
			if lit := findSchemaFunctionReturnInFile(other, funcName); lit != nil {
				return lit
			}
		}
	}

	return nil
}

func findSchemaFunctionReturnInFile(file providerGoFile, funcName string) *ast.CompositeLit {
	for _, decl := range file.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != funcName {
			continue
		}

		assigned := make(map[string]*ast.CompositeLit)

		if fn.Body != nil {
			for _, stmt := range fn.Body.List {
				if assign, ok := stmt.(*ast.AssignStmt); ok && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
					if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
						if lit := schemaLiteral(assign.Rhs[0]); lit != nil {
							assigned[ident.Name] = lit
						}
					}
				}

				ret, ok := stmt.(*ast.ReturnStmt)
				if !ok || len(ret.Results) == 0 {
					continue
				}

				if lit := schemaLiteral(ret.Results[0]); lit != nil {
					return lit
				}

				if ident, ok := ret.Results[0].(*ast.Ident); ok {
					if lit := assigned[ident.Name]; lit != nil {
						return lit
					}
				}
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
	if strings.Contains(lower, "action") {
		return "action"
	}
	if strings.Contains(lower, "list") && !strings.Contains(lower, "datasource") {
		return "list"
	}
	if strings.Contains(lower, "ephemeral") {
		return "ephemeral"
	}
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

func extractAPIVersionFromFile(file providerGoFile) string {
	// Parse imports to find go-azure-sdk imports with API versions
	// Example: "github.com/hashicorp/go-azure-sdk/resource-manager/compute/2024-03-01/virtualmachines"

	for _, imp := range file.file.Imports {
		if imp.Path == nil {
			continue
		}

		path := strings.Trim(imp.Path.Value, `"`)
		if !strings.Contains(path, "go-azure-sdk/resource-manager") {
			continue
		}

		// Extract version from path like: .../compute/2024-03-01/virtualmachines
		for part := range strings.SplitSeq(path, "/") {
			// Check if it matches YYYY-MM-DD pattern
			if len(part) == 10 && part[4] == '-' && part[7] == '-' {
				return part
			}
		}
	}

	return ""
}

// parseServiceMetadata extracts service registration metadata from registration.go files
// Returns a map of directory names (e.g., "compute") to service IDs
func (s *Syncer) parseServiceMetadata(repositoryID int64, goFiles []providerGoFile) (map[string]int64, error) {
	servicesByDirName := make(map[string]int64)

	for _, goFile := range goFiles {
		if !strings.HasSuffix(goFile.repositoryFile.FilePath, "registration.go") {
			continue
		}

		serviceName := extractServiceNameValue(goFile.file)
		if serviceName == "" {
			continue
		}

		service := database.ProviderService{
			RepositoryID:      repositoryID,
			Name:              serviceName,
			FilePath:          sql.NullString{String: goFile.repositoryFile.FilePath, Valid: true},
			WebsiteCategories: sql.NullString{String: extractWebsiteCategories(goFile.file), Valid: true},
			GitHubLabel:       sql.NullString{String: extractGitHubLabel(goFile.file), Valid: true},
		}

		serviceID, err := s.db.InsertProviderService(&service)
		if err != nil {
			log.Printf("Warning: failed to persist service %s: %v", serviceName, err)
			continue
		}

		// Extract directory name from file path (e.g., "internal/services/compute/registration.go" -> "compute")
		dirName := extractServiceNameFromPath(goFile.repositoryFile.FilePath)
		if dirName != "" {
			servicesByDirName[dirName] = serviceID
		}
	}

	return servicesByDirName, nil
}

// extractServiceNameValue finds the return value of func (r Registration) Name() string
func extractServiceNameValue(file *ast.File) string {
	var serviceName string
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "Name" {
			return true
		}

		if fn.Body != nil {
			for _, stmt := range fn.Body.List {
				if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
					if lit, ok := ret.Results[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						serviceName = strings.Trim(lit.Value, `"`)
						return false
					}
				}
			}
		}
		return true
	})
	return serviceName
}

// extractWebsiteCategories finds the return value of func (r Registration) WebsiteCategories() []string
func extractWebsiteCategories(file *ast.File) string {
	var categories []string
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "WebsiteCategories" {
			return true
		}

		if fn.Body != nil {
			for _, stmt := range fn.Body.List {
				if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
					if comp, ok := ret.Results[0].(*ast.CompositeLit); ok {
						for _, elt := range comp.Elts {
							if lit, ok := elt.(*ast.BasicLit); ok && lit.Kind == token.STRING {
								categories = append(categories, strings.Trim(lit.Value, `"`))
							}
						}
						return false
					}
				}
			}
		}
		return true
	})
	return strings.Join(categories, ",")
}

// extractGitHubLabel finds the return value of func (r Registration) AssociatedGitHubLabel() string
func extractGitHubLabel(file *ast.File) string {
	var label string
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "AssociatedGitHubLabel" {
			return true
		}

		if fn.Body != nil {
			for _, stmt := range fn.Body.List {
				if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
					if lit, ok := ret.Results[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						label = strings.Trim(lit.Value, `"`)
						return false
					}
				}
			}
		}
		return true
	})
	return label
}

// extractServiceNameFromPath extracts the service name from a file path like "internal/services/compute/..."
func extractServiceNameFromPath(filePath string) string {
	parts := strings.Split(filePath, "/")
	for i, part := range parts {
		if part == "services" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
