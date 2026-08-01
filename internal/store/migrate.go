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

	return normalizeChargeStamps(db)
}

// sortableTimeLen is the width of every stamp formatTime writes: a UTC
// RFC 3339 instant with a nine-digit fraction.
const sortableTimeLen = len("2006-01-02T15:04:05.000000000Z")

// normalizeChargeStamps rewrites charge timestamps persisted by builds that
// used trimmed RFC3339Nano into the fixed-width SortableTime layout, so
// ListCharges' ORDER BY stays chronological when old and new rows share a
// second. Idempotent and cheap: fixed-width UTC stamps are exactly
// sortableTimeLen bytes, so a normalized database selects nothing.
func normalizeChargeStamps(db *sql.DB) error {
	rows, err := db.Query(`SELECT txid, created_at, expires_at FROM charges
		WHERE length(created_at) <> ? OR length(expires_at) <> ?`,
		sortableTimeLen, sortableTimeLen)
	if err != nil {
		return fmt.Errorf("scan legacy stamps: %w", err)
	}
	defer rows.Close()

	type fix struct{ txid, created, expires string }
	var fixes []fix
	for rows.Next() {
		var txid, created, expires string
		if err := rows.Scan(&txid, &created, &expires); err != nil {
			return fmt.Errorf("scan legacy stamp: %w", err)
		}
		createdAt, err := parseTime(created)
		if err != nil {
			return err
		}
		expiresAt, err := parseTime(expires)
		if err != nil {
			return err
		}
		fixes = append(fixes, fix{txid, formatTime(createdAt), formatTime(expiresAt)})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan legacy stamps: %w", err)
	}
	if len(fixes) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin stamp normalization: %w", err)
	}
	defer tx.Rollback()
	for _, f := range fixes {
		if _, err := tx.Exec(`UPDATE charges SET created_at = ?, expires_at = ? WHERE txid = ?`,
			f.created, f.expires, f.txid); err != nil {
			return fmt.Errorf("normalize stamp for %s: %w", f.txid, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit stamp normalization: %w", err)
	}
	return nil
}
