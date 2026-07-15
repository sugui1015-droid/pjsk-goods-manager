package testdb_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// These are static guards, not database tests: they run on every `go test ./...`
// and need no database.
//
// They exist because test fixtures used to load the real backend/.env and
// connect to DATABASE_URL, which pointed at the production database. That put
// an unregistered admin_auth_audit_events table into production and let the
// rate-limit tests' ghost accounts write 208 audit rows there. Nothing but a
// mechanical check stops that from being reintroduced.
//
// The checks parse Go source rather than grepping text, so comments and this
// file's own string literals cannot trip them.

// backendDir resolves <repo>/backend from this source file, independent of the
// working directory.
func backendDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate this source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

// allowedEnvFileLoaders may legitimately load .env: production config only.
var allowedEnvFileLoaders = map[string]bool{
	filepath.Join("internal", "config", "config.go"): true,
}

// forbiddenEnvInTests must never be read by a _test.go file. Production code may
// use DATABASE_URL freely; tests must use PJSK_TEST_DATABASE_ADMIN_DSN instead.
var forbiddenEnvInTests = []string{"DATABASE_URL"}

func walkGoFiles(t *testing.T, root string, fn func(path string, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "bin" || base == ".cache" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			t.Errorf("parse %s: %v", path, perr)
			return nil
		}
		fn(path, parsed, fset)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// TestNoTestLoadsDotEnv fails if any _test.go file loads a .env file. That is
// how tests reached the production database.
func TestNoTestLoadsDotEnv(t *testing.T) {
	root := backendDir(t)
	walkGoFiles(t, root, func(path string, file *ast.File, fset *token.FileSet) {
		rel, _ := filepath.Rel(root, path)
		isTest := strings.HasSuffix(path, "_test.go")
		if !isTest && allowedEnvFileLoaders[rel] {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "godotenv" {
				return true
			}
			if !isTest {
				return true // production code outside config.go is checked elsewhere
			}
			t.Errorf("%s:%d: test files must never call godotenv.%s — loading the real .env is how tests reached the production database; use testdb.New instead",
				rel, fset.Position(call.Pos()).Line, sel.Sel.Name)
			return true
		})
	})
}

// TestNoTestReadsDatabaseURL fails if any _test.go file reads DATABASE_URL,
// which in this repository resolves to the production database.
func TestNoTestReadsDatabaseURL(t *testing.T) {
	root := backendDir(t)
	walkGoFiles(t, root, func(path string, file *ast.File, fset *token.FileSet) {
		if !strings.HasSuffix(path, "_test.go") {
			return
		}
		rel, _ := filepath.Rel(root, path)
		// This guard file names the forbidden variable as test data.
		if strings.HasSuffix(rel, filepath.Join("internal", "testdb", "no_production_access_test.go")) {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "os" {
				return true
			}
			if sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv" {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				for _, forbidden := range forbiddenEnvInTests {
					if value == forbidden {
						t.Errorf("%s:%d: test files must never read %s (it points at the production database); use %s via testdb instead",
							rel, fset.Position(call.Pos()).Line, forbidden, AdminDSNEnvVarName)
					}
				}
			}
			return true
		})
	})
}

// AdminDSNEnvVarName is duplicated as a plain string so this guard does not
// import the package it guards.
const AdminDSNEnvVarName = "PJSK_TEST_DATABASE_ADMIN_DSN"

// riskyStatements are schema/data statements that must live in migrations, not
// in a test. A test that carries its own copy of a migration's DDL is exactly
// what created the unregistered production table.
var riskyStatements = []string{
	"alter table users",
	"alter table payments",
	"create table if not exists admin_auth_audit_events",
	"drop constraint if exists payments_status_check",
	"drop constraint if exists payments_fee_amount_check",
	"drop constraint if exists payments_payable_amount_check",
	"update payments",
}

// TestNoTestEmbedsProductionSchemaDDL fails if a _test.go file contains a string
// literal with DDL/DML against real business tables. Only string literals are
// inspected, so comments cannot trip it.
func TestNoTestEmbedsProductionSchemaDDL(t *testing.T) {
	root := backendDir(t)
	walkGoFiles(t, root, func(path string, file *ast.File, fset *token.FileSet) {
		if !strings.HasSuffix(path, "_test.go") {
			return
		}
		rel, _ := filepath.Rel(root, path)
		// This guard file lists the forbidden statements as test data.
		if strings.HasSuffix(rel, filepath.Join("internal", "testdb", "no_production_access_test.go")) {
			return
		}
		// The migration runner's own tests build synthetic migrations on purpose;
		// they run against their own throwaway database and never touch real tables.
		if strings.HasPrefix(rel, filepath.Join("internal", "database")) {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			normalized := strings.ToLower(strings.Join(strings.Fields(value), " "))
			for _, risky := range riskyStatements {
				if strings.Contains(normalized, risky) {
					t.Errorf("%s:%d: test files must not embed %q — migrations own the schema; a copied CREATE TABLE is what put an unregistered table into production",
						rel, fset.Position(lit.Pos()).Line, risky)
				}
			}
			return true
		})
	})
}

// TestNoProductionCodeImportsTestdb keeps this package out of the shipped
// binary: only _test.go files may import it.
func TestNoProductionCodeImportsTestdb(t *testing.T) {
	root := backendDir(t)
	walkGoFiles(t, root, func(path string, file *ast.File, fset *token.FileSet) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, filepath.Join("internal", "testdb")) {
			return
		}
		for _, imp := range file.Imports {
			value, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if strings.HasSuffix(value, "/internal/testdb") {
				t.Errorf("%s:%d: production code must not import internal/testdb; it would pull test-only helpers into the backend binary",
					rel, fset.Position(imp.Pos()).Line)
			}
		}
	})
}
