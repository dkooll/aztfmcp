package mcp

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/dkooll/aztfmcp/internal/formatter"
)

func stripFrontMatter(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) == 3 {
			return strings.TrimSpace(parts[2])
		}
	}
	return content
}

func extractMarkdownSection(content, section string) (string, bool) {
	section = strings.TrimSpace(section)
	if section == "" {
		return strings.TrimSpace(content), true
	}

	lines := strings.Split(content, "\n")
	target := strings.ToLower(section)
	var builder strings.Builder
	found := false
	capturing := false
	currentLevel := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			title := strings.TrimSpace(trimmed[level:])
			if capturing && level <= currentLevel {
				break
			}
			if strings.ToLower(title) == target {
				found = true
				capturing = true
				currentLevel = level
				builder.WriteString(line)
				builder.WriteString("\n")
				continue
			}
			if capturing && level <= currentLevel {
				break
			}
		}
		if capturing {
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}

	if found {
		return strings.TrimSpace(builder.String()), true
	}

	return strings.TrimSpace(content), false
}

func toCamelCase(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '/'
	})
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			builder.WriteString(strings.ToLower(part[1:]))
		}
	}
	return builder.String()
}

func parseTestFunctions(source string, prefixes []string) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", source, parser.ParseComments)
	if err != nil {
		return nil
	}

	var tests []string
	seen := make(map[string]struct{})
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				if _, exists := seen[name]; !exists {
					seen[name] = struct{}{}
					tests = append(tests, name)
				}
				break
			}
		}
	}

	sort.Strings(tests)
	return tests
}

func parseFeatureFlags(source string) []formatter.FeatureFlagInfo {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", source, parser.ParseComments)
	if err != nil {
		return nil
	}

	var infos []formatter.FeatureFlagInfo
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for idx, name := range valSpec.Names {
				if name.Name != "Features" || idx >= len(valSpec.Values) {
					continue
				}
				lit := compositeLiteral(valSpec.Values[idx])
				if lit == nil {
					continue
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key := strings.Trim(unwrapQuotes(exprString(fset, kv.Key)), "\"`")
					flagLit := compositeLiteral(kv.Value)
					if flagLit == nil {
						continue
					}
					info := formatter.FeatureFlagInfo{Key: key}
					for _, field := range flagLit.Elts {
						fieldKV, ok := field.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						fieldName := identName(fieldKV.Key)
						switch fieldName {
						case "Description":
							info.Description = unwrapQuotes(exprString(fset, fieldKV.Value))
						case "Default":
							info.Default = exprString(fset, fieldKV.Value)
						case "Stage":
							info.Stage = exprString(fset, fieldKV.Value)
						case "DisabledFor":
							info.DisabledFor = stringList(fieldKV.Value, fset)
						}
					}
					infos = append(infos, info)
				}
				return infos
			}
		}
	}

	return infos
}

func parseResourceBehaviors(schemaSnippet string) formatter.ResourceBehaviorInfo {
	info := formatter.ResourceBehaviorInfo{}
	if strings.TrimSpace(schemaSnippet) == "" {
		return info
	}

	fset := token.NewFileSet()
	expr, err := parser.ParseExpr(schemaSnippet)
	if err != nil {
		info.Notes = append(info.Notes, "Schema snippet could not be parsed; showing raw source.")
		info.TimeoutsRaw = strings.TrimSpace(schemaSnippet)
		return info
	}

	lit := compositeLiteral(expr)
	if lit == nil {
		info.Notes = append(info.Notes, "Schema snippet is not a composite literal; showing raw source.")
		info.TimeoutsRaw = strings.TrimSpace(schemaSnippet)
		return info
	}

	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		field := identName(kv.Key)
		switch field {
		case "Timeouts":
			details, raw := parseTimeouts(kv.Value)
			info.Timeouts = details
			info.TimeoutsRaw = raw
		case "CustomizeDiff":
			info.CustomizeDiff = append(info.CustomizeDiff, strings.TrimSpace(exprString(fset, kv.Value)))
		case "Importer":
			info.Importer = strings.TrimSpace(exprString(fset, kv.Value))
		case "DeprecationMessage":
			info.Notes = append(info.Notes, "Deprecation message overrides schema to discourage new usage.")
		case "CreateBeforeDestroy":
			info.Notes = append(info.Notes, "Sets CreateBeforeDestroy for updates.")
		case "SchemaVersion":
			info.Notes = append(info.Notes, "Includes SchemaVersion for state upgrades.")
		}
	}

	return info
}

func parseTimeouts(expr ast.Expr) ([]formatter.TimeoutDetail, string) {
	fset := token.NewFileSet()
	lit := compositeLiteral(expr)
	if lit == nil {
		return nil, strings.TrimSpace(exprString(fset, expr))
	}

	var details []formatter.TimeoutDetail
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name := identName(kv.Key)
		if name == "" {
			name = strings.TrimSpace(exprString(fset, kv.Key))
		}
		details = append(details, formatter.TimeoutDetail{
			Name:  name,
			Value: strings.TrimSpace(exprString(fset, kv.Value)),
		})
	}

	sort.Slice(details, func(i, j int) bool {
		return details[i].Name < details[j].Name
	})

	return details, ""
}

func compositeLiteral(expr ast.Expr) *ast.CompositeLit {
	switch v := expr.(type) {
	case *ast.CompositeLit:
		return v
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			if cl, ok := v.X.(*ast.CompositeLit); ok {
				return cl
			}
		}
	case *ast.ParenExpr:
		return compositeLiteral(v.X)
	}
	return nil
}

func identName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		prefix := identName(v.X)
		if prefix != "" {
			return prefix + "." + v.Sel.Name
		}
		return v.Sel.Name
	default:
		return ""
	}
}

func exprString(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return ""
	}
	return buf.String()
}

func unwrapQuotes(value string) string {
	if value == "" {
		return ""
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func stringList(expr ast.Expr, fset *token.FileSet) []string {
	lit := compositeLiteral(expr)
	if lit == nil {
		return []string{strings.TrimSpace(exprString(fset, expr))}
	}

	var items []string
	for _, elt := range lit.Elts {
		items = append(items, unwrapQuotes(strings.TrimSpace(exprString(fset, elt))))
	}
	return items
}
