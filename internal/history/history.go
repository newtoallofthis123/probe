package history

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Entry represents a single history record (without full results).
type Entry struct {
	ID        int64
	Query     string
	Timestamp time.Time
	Dir       string
}

// DBPath returns the default database path using os.UserConfigDir().
func DBPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "probe", "probe.db"), nil
}

// Open opens (or creates) the history database. Parent dirs are created.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", path)
}

// Init creates the history table if it doesn't exist.
func Init(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			query TEXT NOT NULL,
			results TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			dir TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_history_dir ON history(dir);
		CREATE INDEX IF NOT EXISTS idx_history_timestamp ON history(timestamp);
	`)
	return err
}

// Insert stores a query and its results.
func Insert(db *sql.DB, query, results, dir string) error {
	_, err := db.Exec(
		"INSERT INTO history (query, results, timestamp, dir) VALUES (?, ?, ?, ?)",
		query, results, time.Now().Unix(), dir,
	)
	return err
}

// ListByDir returns entries for a directory, most recent first.
func ListByDir(db *sql.DB, dir string) ([]Entry, error) {
	return queryEntries(db, "SELECT id, query, timestamp, dir FROM history WHERE dir = ? ORDER BY timestamp DESC", dir)
}

// ListAll returns all entries, most recent first.
func ListAll(db *sql.DB) ([]Entry, error) {
	return queryEntries(db, "SELECT id, query, timestamp, dir FROM history ORDER BY timestamp DESC")
}

// GetByID returns the stored results for a history entry.
func GetByID(db *sql.DB, id int64) (string, error) {
	var results string
	err := db.QueryRow("SELECT results FROM history WHERE id = ?", id).Scan(&results)
	return results, err
}

func queryEntries(db *sql.DB, query string, args ...any) ([]Entry, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var ts int64
		if err := rows.Scan(&e.ID, &e.Query, &ts, &e.Dir); err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(ts, 0)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
