package history

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func tempDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDBPath(t *testing.T) {
	p, err := DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, filepath.Join("probe", "probe.db")) {
		t.Errorf("unexpected path: %s", p)
	}
}

func TestOpenCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func TestInitCreatesTable(t *testing.T) {
	db := tempDB(t)
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='history'").Scan(&name)
	if err != nil {
		t.Fatal(err)
	}
	if name != "history" {
		t.Errorf("expected table 'history', got %q", name)
	}
}

func TestInsertAndListByDir(t *testing.T) {
	db := tempDB(t)
	// Use explicit timestamps to ensure ordering
	db.Exec("INSERT INTO history (query, results, timestamp, dir) VALUES (?, ?, ?, ?)", "q1", "r1", 1000, "/a")
	db.Exec("INSERT INTO history (query, results, timestamp, dir) VALUES (?, ?, ?, ?)", "q2", "r2", 2000, "/a")
	db.Exec("INSERT INTO history (query, results, timestamp, dir) VALUES (?, ?, ?, ?)", "q3", "r3", 3000, "/b")

	entries, err := ListByDir(db, "/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Most recent first
	if entries[0].Query != "q2" {
		t.Errorf("expected q2 first, got %s", entries[0].Query)
	}
}

func TestInsertAndListAll(t *testing.T) {
	db := tempDB(t)
	Insert(db, "q1", "r1", "/a")
	Insert(db, "q2", "r2", "/b")
	Insert(db, "q3", "r3", "/c")

	entries, err := ListAll(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestListEmptyDB(t *testing.T) {
	db := tempDB(t)
	entries, err := ListAll(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestInsertStoresResults(t *testing.T) {
	db := tempDB(t)
	Insert(db, "q1", "full result text here", "/a")

	var results string
	err := db.QueryRow("SELECT results FROM history WHERE query = 'q1'").Scan(&results)
	if err != nil {
		t.Fatal(err)
	}
	if results != "full result text here" {
		t.Errorf("unexpected results: %q", results)
	}
}

func TestEntryTimestampOrdering(t *testing.T) {
	db := tempDB(t)
	// Insert with explicit timestamps via raw SQL
	db.Exec("INSERT INTO history (query, results, timestamp, dir) VALUES (?, ?, ?, ?)", "old", "", 1000, "/a")
	db.Exec("INSERT INTO history (query, results, timestamp, dir) VALUES (?, ?, ?, ?)", "mid", "", 2000, "/a")
	db.Exec("INSERT INTO history (query, results, timestamp, dir) VALUES (?, ?, ?, ?)", "new", "", 3000, "/a")

	entries, _ := ListAll(db)
	if entries[0].Query != "new" || entries[2].Query != "old" {
		t.Errorf("wrong order: %v", entries)
	}
}

func TestGetByID(t *testing.T) {
	db := tempDB(t)
	Insert(db, "q1", "result one", "/a")
	Insert(db, "q2", "result two", "/a")

	entries, _ := ListAll(db)
	results, err := GetByID(db, entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if results != "result two" {
		t.Errorf("expected 'result two', got %q", results)
	}

	_, err = GetByID(db, 9999)
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestListByDirFiltersCorrectly(t *testing.T) {
	db := tempDB(t)
	Insert(db, "q1", "", "/x")
	Insert(db, "q2", "", "/y")
	Insert(db, "q3", "", "/z")

	for _, dir := range []string{"/x", "/y", "/z"} {
		entries, _ := ListByDir(db, dir)
		if len(entries) != 1 {
			t.Errorf("dir %s: expected 1 entry, got %d", dir, len(entries))
		}
	}
}
