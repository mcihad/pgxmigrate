package migrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitMigration(t *testing.T) {
	up, down, err := splitMigration("----------UP----------\nCREATE TABLE users(id bigint);\n----------DOWN----------\nDROP TABLE users;")
	if err != nil {
		t.Fatalf("splitMigration returned error: %v", err)
	}
	if up != "CREATE TABLE users(id bigint);" {
		t.Fatalf("unexpected up sql: %q", up)
	}
	if down != "DROP TABLE users;" {
		t.Fatalf("unexpected down sql: %q", down)
	}
}

func TestSplitMigrationRequiresMarkers(t *testing.T) {
	if _, _, err := splitMigration("CREATE TABLE users(id bigint);"); err == nil {
		t.Fatal("expected marker error")
	}
}

func TestSlugify(t *testing.T) {
	got := slugify("Create Users Table!")
	if got != "create_users_table" {
		t.Fatalf("unexpected slug: %q", got)
	}
}

func TestLoadFilesSortsAndIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"20260102030405_second.sql": upMarker + "\nSELECT 2;\n" + downMarker + "\nSELECT -2;",
		"20250102030405_first.sql":  upMarker + "\nSELECT 1;\n" + downMarker + "\nSELECT -1;",
		"notes.txt":                 "ignore me",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	migrations, err := New(nil, dir).loadFiles()
	if err != nil {
		t.Fatalf("loadFiles returned error: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(migrations))
	}
	if migrations[0].Name != "first" || migrations[1].Name != "second" {
		t.Fatalf("migrations not sorted: %#v", migrations)
	}
}

func TestValidateFilesReportsInvalidSQLFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad_name.sql"), []byte("SELECT 1;"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	issues, err := New(nil, dir).Validate(t.Context())
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %#v", len(issues), issues)
	}
}

func TestValidateFilesReportsEmptySections(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20250102030405_empty.sql"), []byte(upMarker+"\n\n"+downMarker+"\n"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	issues, err := New(nil, dir).Validate(t.Context())
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %#v", len(issues), issues)
	}
}
