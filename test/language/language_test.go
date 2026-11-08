// Package language enforces ADR-054 (English as the project language) in Go
// sources: no French in string literals, which become error messages, logs
// and CLI output. Comments, documentation and non-Go files are out of scope.
package language

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// frenchWords are lowercase words that are French and not English. Words that
// exist in both languages (pour, sur, pas, est, on, a…) are left out on
// purpose to avoid false positives; accented letters catch most of the rest.
var frenchWords = map[string]bool{
	"de": true, "du": true, "des": true, "le": true, "les": true, "une": true,
	"avec": true, "dans": true, "vers": true,
	"erreur": true, "impossible": true, "introuvable": true, "invalide": true,
	"vide": true, "fichier": true, "valeur": true,
	"lecture": true, "ecriture": true, "echec": true, "initialisation": true,
	"compilation": true, "encodage": true, "lancement": true,
	"chargement": true, "demarrage": true, "creation": true,
	"suppression": true,
}

// allowed lists string literals that legitimately contain French, as
// "<path relative to the repository root>: <quoted literal>". Keep it empty
// unless a literal really must be French (e.g. a test of non-ASCII input).
var allowed = map[string]bool{}

var nonLetters = regexp.MustCompile(`[^\p{L}]+`)

// accented reports whether s contains a letter typical of French.
func accented(s string) bool {
	return strings.ContainsAny(s, "àâäçéèêëîïôöûùüÿœÀÂÇÉÈÊÎÔÛÙŒ")
}

// isFrench reports whether s contains an accented letter or a French word.
// Matching is case-sensitive on lowercase words, so a capitalised proper name
// is not flagged.
func isFrench(s string) bool {
	if accented(s) {
		return true
	}
	for _, w := range nonLetters.Split(s, -1) {
		if frenchWords[w] {
			return true
		}
	}
	return false
}

// generated reports whether src carries the standard "Code generated" header.
func generated(src []byte) bool {
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if strings.HasPrefix(line, "// Code generated") && strings.Contains(line, "DO NOT EDIT") {
			return true
		}
	}
	return false
}

// findings returns the French string literals of one Go source, as
// "<position>: <quoted literal>".
func findings(fset *token.FileSet, name string, src []byte) ([]string, error) {
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if s, err := strconv.Unquote(lit.Value); err == nil && isFrench(s) {
			out = append(out, fset.Position(lit.Pos()).String()+": "+lit.Value)
		}
		return true
	})
	return out, nil
}

func TestIsFrench(t *testing.T) {
	for _, s := range []string{
		"permissions de %s: %w",
		"compilation de %s: %w\n%s",
		"encodage de %s: %w",
		"initialisation de vault: %w",
		"échec",
	} {
		if !isFrench(s) {
			t.Errorf("%q not detected as French", s)
		}
	}
	for _, s := range []string{
		"",
		"setting permissions on %s: %w",
		"compiling %s: %w\n%s",
		"encoding %s: %w",
		"initialising vault: %w",
		"Vault is not initialised",
		"Genesis",
	} {
		if isFrench(s) {
			t.Errorf("%q wrongly detected as French", s)
		}
	}
}

// TestNoFrenchStrings walks every non-generated Go file of the repository
// (all modules, tests included) and fails on French string literals.
func TestNoFrenchStrings(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(root, "test", "language", "language_test.go")
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == ".bin" {
				return filepath.SkipDir
			}
			return nil
		}
		// This file holds French examples on purpose.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".pb.go") || path == self {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if generated(src) {
			return nil
		}
		found, err := findings(fset, path, src)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, f := range found {
			lit := f[strings.Index(f, ": ")+2:]
			if !allowed[rel+": "+lit] {
				t.Errorf("French string literal (ADR-054, English only; allowlist in %s): %s", filepath.Base(self), f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
