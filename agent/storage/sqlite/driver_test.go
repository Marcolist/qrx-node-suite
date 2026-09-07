package sqlite_test

import (
	"database/sql"
	"testing"
	"time"

	_ "qrx-node-suite/agent/storage/sqlite"
)

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateInsertSelect(t *testing.T) {
	db := openMemDB(t)

	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT, weight REAL, active INTEGER, note TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	res, err := db.Exec(`INSERT INTO widgets (name, weight, active, note) VALUES (?, ?, ?, ?)`, "bolt", 1.5, true, nil)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if id != 1 {
		t.Errorf("LastInsertId = %d, want 1", id)
	}
	affected, err := res.RowsAffected()
	if err != nil || affected != 1 {
		t.Errorf("RowsAffected = %d, err %v, want 1", affected, err)
	}

	var name string
	var weight float64
	var active bool
	var note sql.NullString
	err = db.QueryRow(`SELECT name, weight, active, note FROM widgets WHERE id = ?`, id).Scan(&name, &weight, &active, &note)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if name != "bolt" || weight != 1.5 || !active || note.Valid {
		t.Errorf("got name=%q weight=%v active=%v note.Valid=%v, want bolt/1.5/true/false", name, weight, active, note.Valid)
	}
}

func TestTransactionRollback(t *testing.T) {
	db := openMemDB(t)
	if _, err := db.Exec(`CREATE TABLE counters (id INTEGER PRIMARY KEY, n INTEGER)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO counters (id, n) VALUES (1, 0)`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := tx.Exec(`UPDATE counters SET n = n + 1 WHERE id = 1`); err != nil {
		t.Fatalf("UPDATE in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT n FROM counters WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("SELECT after rollback: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d after rollback, want 0 (rollback should have discarded the update)", n)
	}
}

func TestTransactionCommit(t *testing.T) {
	db := openMemDB(t)
	if _, err := db.Exec(`CREATE TABLE counters (id INTEGER PRIMARY KEY, n INTEGER)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO counters (id, n) VALUES (1, 0)`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := tx.Exec(`UPDATE counters SET n = n + 5 WHERE id = 1`); err != nil {
		t.Fatalf("UPDATE in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT n FROM counters WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("SELECT after commit: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d after commit, want 5", n)
	}
}

func TestMultiRowQuery(t *testing.T) {
	db := openMemDB(t)
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, label TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	for _, label := range []string{"a", "b", "c"} {
		if _, err := db.Exec(`INSERT INTO items (label) VALUES (?)`, label); err != nil {
			t.Fatalf("insert %q: %v", label, err)
		}
	}
	rows, err := db.Query(`SELECT label FROM items ORDER BY id`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		got = append(got, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTimeRoundTrip(t *testing.T) {
	db := openMemDB(t)
	if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY, ts TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(`INSERT INTO events (ts) VALUES (?)`, now); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT ts FROM events`).Scan(&got); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, got)
	if err != nil {
		t.Fatalf("parse stored time %q: %v", got, err)
	}
	if !parsed.Equal(now) {
		t.Errorf("round-tripped time = %v, want %v", parsed, now)
	}
}

func TestMultiStatementExecRunsEveryStatement(t *testing.T) {
	// A migration file is exactly this shape: several statements in one
	// script, executed with no bound parameters. Prepare+Stmt.Exec would
	// silently run only the first (sqlite3_prepare_v2 stops at the first
	// statement) -- conn.Exec must route this through sqlite3_exec instead.
	db := openMemDB(t)
	_, err := db.Exec(`
		CREATE TABLE t1 (id INTEGER PRIMARY KEY);
		CREATE TABLE t2 (id INTEGER PRIMARY KEY);
		INSERT INTO t1 (id) VALUES (1);
		INSERT INTO t2 (id) VALUES (2);
	`)
	if err != nil {
		t.Fatalf("multi-statement Exec: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM t1`).Scan(&n); err != nil || n != 1 {
		t.Errorf("t1 count = %d, err %v, want 1 (t1 not created/populated)", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM t2`).Scan(&n); err != nil || n != 1 {
		t.Errorf("t2 count = %d, err %v, want 1 (second statement was dropped)", n, err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db := openMemDB(t)
	if _, err := db.Exec(`
		CREATE TABLE parents (id INTEGER PRIMARY KEY);
		CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parents(id));
	`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	_, err := db.Exec(`INSERT INTO children (parent_id) VALUES (999)`)
	if err == nil {
		t.Fatal("expected a foreign key violation, got nil error (PRAGMA foreign_keys not effective?)")
	}
}
