package store

import (
	"database/sql"
	"fmt"
)

// Migrations bring an EXISTING database up to the current schema.
//
// schema.sql alone cannot do this. Every statement in it is
// `CREATE ... IF NOT EXISTS`, which is exactly right for a fresh database and
// a complete no-op for a table that already exists — so a column added to
// schema.sql never reaches a database that already has that table. The failure
// is loud but late: the service starts, applies the schema, and dies on the
// first index that names the missing column. It was found that way, on deploy.
//
// These run BEFORE schema.sql so that the indexes it creates can rely on the
// columns being there.
func migrate(db *sql.DB) error {
	if err := addMissingColumns(db); err != nil {
		return err
	}
	return rebuildTruthsForPhases(db)
}

// addedColumn is one column added to a table after it first shipped.
type addedColumn struct {
	table      string
	column     string
	definition string
}

// addedColumns is append-only. A column listed here is added to an existing
// table if absent, and ignored on a fresh database where schema.sql already
// created it.
var addedColumns = []addedColumn{
	{"sessions", "current_phase", "INTEGER NOT NULL DEFAULT 1"},
	{"sessions", "phase_count", "INTEGER NOT NULL DEFAULT 3"},
	{"scripted_lines", "phase", "INTEGER NOT NULL DEFAULT 1"},
	{"monitor_cursors", "last_phase", "INTEGER NOT NULL DEFAULT 0"},
}

func addMissingColumns(db *sql.DB) error {
	for _, c := range addedColumns {
		exists, err := tableExists(db, c.table)
		if err != nil {
			return err
		}
		if !exists {
			// Fresh database: schema.sql will create it with the column.
			continue
		}
		has, err := columnExists(db, c.table, c.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, c.table, c.column, c.definition)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("add %s.%s: %w", c.table, c.column, err)
		}
	}
	return nil
}

// rebuildTruthsForPhases widens the ground-truth table's primary key from
// (student, goal) to (student, goal, phase).
//
// This one cannot be an ALTER: SQLite will not change a primary key, so the
// table is rebuilt and its rows carried over as phase 1 — which is what they
// were, a single frozen snapshot taken at the start of a lesson.
func rebuildTruthsForPhases(db *sql.DB) error {
	exists, err := tableExists(db, "truths")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	has, err := columnExists(db, "truths", "phase")
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE truths_migrating (
		   student_id TEXT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
		   goal_id    TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
		   phase      INTEGER NOT NULL,
		   state      TEXT NOT NULL,
		   PRIMARY KEY (student_id, goal_id, phase)
		 )`,
		// Existing rows were the whole truth of a session that never moved, so
		// they are that session's phase 1.
		`INSERT INTO truths_migrating (student_id, goal_id, phase, state)
		   SELECT student_id, goal_id, 1, state FROM truths`,
		`DROP TABLE truths`,
		`ALTER TABLE truths_migrating RENAME TO truths`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("rebuild truths: %w", err)
		}
	}
	return tx.Commit()
}

func tableExists(db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up table %s: %w", table, err)
	}
	return true, nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, fmt.Errorf("read columns of %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name, ctyp string
			notNull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &ctyp, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
