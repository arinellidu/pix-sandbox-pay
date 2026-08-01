package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate applies every migration the database has not seen yet, in filename
// order. The applied count lives in SQLite's own `user_version`, so no
// bookkeeping table is needed.
//
// Each file runs inside a transaction together with the version bump: a failed
// migration leaves the database exactly as it was.
func migrate(db *sql.DB) error {
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(files) {
		return fmt.Errorf("database is at schema version %d but only %d migrations exist; "+
			"this binary is older than the data", version, len(files))
	}

	for i := version; i < len(files); i++ {
		statements, err := migrationsFS.ReadFile(files[i])
		if err != nil {
			return fmt.Errorf("read %s: %w", files[i], err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin %s: %w", files[i], err)
		}
		if _, err := tx.Exec(string(statements)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", files[i], err)
		}
		// PRAGMA takes no bind parameters; the value is a loop counter.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("bump version after %s: %w", files[i], err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", files[i], err)
		}
	}
	return nil
}
