package stripbodies

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func panicBody() *ast.BlockStmt {
	return &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{
		Fun:  ast.NewIdent("panic"),
		Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: strconv.Quote("unimplemented")}},
	}}}}
}

func stripGo(path string) (int, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return 0, fmt.Errorf("stripbodies: parse %s: %w", path, err)
	}
	stripped := 0
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if !ok || declaration.Body == nil {
			return true
		}
		declaration.Body = panicBody()
		stripped++
		return false
	})
	if stripped == 0 {
		return 0, nil
	}
	pruneImports(file)
	buffer := &bytes.Buffer{}
	config := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := config.Fprint(buffer, fset, file); err != nil {
		return 0, fmt.Errorf("stripbodies: print %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return stripped, os.WriteFile(path, buffer.Bytes(), info.Mode())
}

func pruneImports(file *ast.File) {
	used := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		if _, ok := node.(*ast.ImportSpec); ok {
			return false
		}
		if identifier, ok := node.(*ast.Ident); ok {
			used[identifier.Name] = true
		}
		return true
	})
	declarations := make([]ast.Decl, 0, len(file.Decls))
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			declarations = append(declarations, declaration)
			continue
		}
		kept := make([]ast.Spec, 0, len(gen.Specs))
		for _, spec := range gen.Specs {
			imported, ok := spec.(*ast.ImportSpec)
			if !ok {
				kept = append(kept, spec)
				continue
			}
			if importName(imported) == "" || used[importName(imported)] {
				kept = append(kept, spec)
			}
		}
		gen.Specs = kept
		if len(kept) > 0 {
			declarations = append(declarations, gen)
		}
	}
	file.Decls = declarations
	kept := make([]*ast.ImportSpec, 0, len(file.Imports))
	for _, imported := range file.Imports {
		if importName(imported) == "" || used[importName(imported)] {
			kept = append(kept, imported)
		}
	}
	file.Imports = kept
}

func importName(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		if spec.Name.Name == "_" || spec.Name.Name == "." {
			return ""
		}
		return spec.Name.Name
	}
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return ""
	}
	base := filepath.Base(path)
	if index := strings.IndexAny(base, "-."); index > 0 {
		base = base[:index]
	}
	return base
}
