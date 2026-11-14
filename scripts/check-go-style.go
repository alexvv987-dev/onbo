//go:build ignore

// Run with: go run scripts/check-go-style.go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func main() {
	violations := 0
	for _, root := range []string{"cmd", "internal"} {
		walkErr := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, entryErr error) error {
			if entryErr != nil {
				return entryErr
			}
			if entry.IsDir() || !strings.HasSuffix(filePath, ".go") {
				return nil
			}
			fileSet := token.NewFileSet()
			parsedFile, err := parser.ParseFile(fileSet, filePath, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			importsEnd := 0
			for declarationIndex, declaration := range parsedFile.Decls {
				if general, ok := declaration.(*ast.GenDecl); ok && general.Tok == token.IMPORT {
					importsEnd = declarationIndex + 1
				}
			}
			for declarationIndex, declaration := range parsedFile.Decls {
				if general, ok := declaration.(*ast.GenDecl); ok && general.Tok == token.CONST {
					if !general.Lparen.IsValid() {
						fmt.Printf("%s: constants must use a block\n", fileSet.Position(general.Pos()))
						violations++
					}
					if declarationIndex != importsEnd {
						fmt.Printf("%s: const block must immediately follow imports\n", fileSet.Position(general.Pos()))
						violations++
					}
				}
			}
			ast.Inspect(parsedFile, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if !ok || identifier.Obj == nil || identifier.Obj.Pos() != identifier.Pos() || identifier.Name == "_" {
					return true
				}
				if utf8.RuneCountInString(identifier.Name) == 1 {
					fmt.Printf("%s: single-letter declaration %s\n", fileSet.Position(identifier.Pos()), identifier.Name)
					violations++
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			fmt.Fprintln(os.Stderr, walkErr)
			os.Exit(1)
		}
	}
	if violations != 0 {
		os.Exit(1)
	}
	fmt.Println("Go declarations: no single-letter names; constants use blocks")
}
