// Package sqlite implements a minimal database/sql/driver.Driver over the
// system libsqlite3 via cgo. It exists because this module takes no external
// Go dependencies (ADR-001) and this repository's build environment has no
// network access to the Go module proxy to fetch mattn/go-sqlite3 or
// modernc.org/sqlite. See README.md in this directory for exactly what it
// does and does not implement.
package sqlite

/*
#cgo pkg-config: sqlite3
#include <sqlite3.h>
#include <stdlib.h>
*/
import "C"

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"time"
	"unsafe"
)

func init() {
	sql.Register("sqlite3", &sqliteDriver{})
}

type sqliteDriver struct{}

// Open implements driver.Driver. name is a file path, or ":memory:" for an
// in-process database (each Open call to ":memory:" is a distinct database,
// matching stdlib sqlite3's semantics -- callers wanting a shared in-memory
// DB across connections should use "file::memory:?cache=shared", which this
// driver passes straight through to sqlite3_open_v2).
func (d *sqliteDriver) Open(name string) (driver.Conn, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	var db *C.sqlite3
	flags := C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX
	rc := C.sqlite3_open_v2(cname, &db, C.int(flags), nil)
	if rc != C.SQLITE_OK {
		msg := "unknown error"
		if db != nil {
			msg = C.GoString(C.sqlite3_errmsg(db))
		}
		if db != nil {
			C.sqlite3_close_v2(db)
		}
		return nil, fmt.Errorf("sqlite3_open_v2(%s): %s", name, msg)
	}

	c := &conn{db: db}
	// Sensible, safe defaults: foreign keys are opt-in in sqlite and this
	// project relies on them (see migrations); WAL improves concurrent
	// reader/writer behavior for an Agent that's both serving the API and
	// writing metrics.
	if err := c.execSQL("PRAGMA foreign_keys = ON;"); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.execSQL("PRAGMA journal_mode = WAL;"); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.execSQL("PRAGMA busy_timeout = 5000;"); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

type conn struct {
	db *C.sqlite3
}

func (c *conn) lastError() error {
	return errors.New(C.GoString(C.sqlite3_errmsg(c.db)))
}

// execSQL runs a statement with no parameters and no result rows, used for
// PRAGMAs and BEGIN/COMMIT/ROLLBACK.
func (c *conn) execSQL(query string) error {
	cquery := C.CString(query)
	defer C.free(unsafe.Pointer(cquery))
	var errmsg *C.char
	rc := C.sqlite3_exec(c.db, cquery, nil, nil, &errmsg)
	if rc != C.SQLITE_OK {
		msg := C.GoString(errmsg)
		C.sqlite3_free(unsafe.Pointer(errmsg))
		return errors.New(msg)
	}
	return nil
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	cquery := C.CString(query)
	defer C.free(unsafe.Pointer(cquery))
	var stmt *C.sqlite3_stmt
	rc := C.sqlite3_prepare_v2(c.db, cquery, -1, &stmt, nil)
	if rc != C.SQLITE_OK {
		return nil, c.lastError()
	}
	return &sqliteStmt{c: c, stmt: stmt}, nil
}

func (c *conn) Close() error {
	rc := C.sqlite3_close_v2(c.db)
	if rc != C.SQLITE_OK {
		return c.lastError()
	}
	return nil
}

func (c *conn) Begin() (driver.Tx, error) {
	if err := c.execSQL("BEGIN"); err != nil {
		return nil, err
	}
	return &sqliteTx{c: c}, nil
}

// Exec implements the (legacy, still-supported) driver.Execer interface.
// database/sql prefers it over Prepare+Stmt.Exec when there are no
// arguments. This matters because sqlite3_prepare_v2 -- what Prepare/Stmt.Exec
// use -- only compiles the FIRST statement in a query string; a
// multi-statement script (as every migration file is) would otherwise
// silently execute only its first statement. Routing no-arg execs through
// sqlite3_exec instead runs every statement in the script. Parameterized
// execs (len(args) > 0) return driver.ErrSkip so database/sql falls back to
// the normal single-statement Prepare+Stmt.Exec path.
func (c *conn) Exec(query string, args []driver.Value) (driver.Result, error) {
	if len(args) > 0 {
		return nil, driver.ErrSkip
	}
	if err := c.execSQL(query); err != nil {
		return nil, err
	}
	return sqliteResult{
		lastInsertID: int64(C.sqlite3_last_insert_rowid(c.db)),
		rowsAffected: int64(C.sqlite3_changes(c.db)),
	}, nil
}

type sqliteTx struct{ c *conn }

func (t *sqliteTx) Commit() error   { return t.c.execSQL("COMMIT") }
func (t *sqliteTx) Rollback() error { return t.c.execSQL("ROLLBACK") }

type sqliteStmt struct {
	c    *conn
	stmt *C.sqlite3_stmt
}

func (s *sqliteStmt) Close() error {
	rc := C.sqlite3_finalize(s.stmt)
	if rc != C.SQLITE_OK {
		return s.c.lastError()
	}
	return nil
}

func (s *sqliteStmt) NumInput() int {
	return int(C.sqlite3_bind_parameter_count(s.stmt))
}

func (s *sqliteStmt) bind(args []driver.Value) error {
	C.sqlite3_reset(s.stmt)
	C.sqlite3_clear_bindings(s.stmt)
	for i, v := range args {
		idx := C.int(i + 1)
		var rc C.int
		switch val := v.(type) {
		case nil:
			rc = C.sqlite3_bind_null(s.stmt, idx)
		case int64:
			rc = C.sqlite3_bind_int64(s.stmt, idx, C.sqlite3_int64(val))
		case float64:
			rc = C.sqlite3_bind_double(s.stmt, idx, C.double(val))
		case bool:
			n := C.sqlite3_int64(0)
			if val {
				n = 1
			}
			rc = C.sqlite3_bind_int64(s.stmt, idx, n)
		case []byte:
			if len(val) == 0 {
				rc = C.sqlite3_bind_zeroblob(s.stmt, idx, 0)
			} else {
				rc = C.sqlite3_bind_blob(s.stmt, idx, unsafe.Pointer(&val[0]), C.int(len(val)), C.SQLITE_TRANSIENT)
			}
		case string:
			cstr := C.CString(val)
			rc = C.sqlite3_bind_text(s.stmt, idx, cstr, C.int(len(val)), C.SQLITE_TRANSIENT)
			C.free(unsafe.Pointer(cstr))
		case time.Time:
			str := val.UTC().Format(time.RFC3339Nano)
			cstr := C.CString(str)
			rc = C.sqlite3_bind_text(s.stmt, idx, cstr, C.int(len(str)), C.SQLITE_TRANSIENT)
			C.free(unsafe.Pointer(cstr))
		default:
			return fmt.Errorf("sqlite: unsupported argument type %T at position %d", v, i)
		}
		if rc != C.SQLITE_OK {
			return s.c.lastError()
		}
	}
	return nil
}

func (s *sqliteStmt) Exec(args []driver.Value) (driver.Result, error) {
	if err := s.bind(args); err != nil {
		return nil, err
	}
	rc := C.sqlite3_step(s.stmt)
	if rc != C.SQLITE_DONE && rc != C.SQLITE_ROW {
		C.sqlite3_reset(s.stmt)
		return nil, s.c.lastError()
	}
	lastID := int64(C.sqlite3_last_insert_rowid(s.c.db))
	changes := int64(C.sqlite3_changes(s.c.db))
	C.sqlite3_reset(s.stmt)
	return sqliteResult{lastInsertID: lastID, rowsAffected: changes}, nil
}

func (s *sqliteStmt) Query(args []driver.Value) (driver.Rows, error) {
	if err := s.bind(args); err != nil {
		return nil, err
	}
	return &sqliteRows{stmt: s}, nil
}

type sqliteResult struct {
	lastInsertID int64
	rowsAffected int64
}

func (r sqliteResult) LastInsertId() (int64, error) { return r.lastInsertID, nil }
func (r sqliteResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

type sqliteRows struct {
	stmt *sqliteStmt
}

func (r *sqliteRows) Columns() []string {
	n := int(C.sqlite3_column_count(r.stmt.stmt))
	cols := make([]string, n)
	for i := 0; i < n; i++ {
		cols[i] = C.GoString(C.sqlite3_column_name(r.stmt.stmt, C.int(i)))
	}
	return cols
}

func (r *sqliteRows) Close() error {
	C.sqlite3_reset(r.stmt.stmt)
	return nil
}

func (r *sqliteRows) Next(dest []driver.Value) error {
	rc := C.sqlite3_step(r.stmt.stmt)
	if rc == C.SQLITE_DONE {
		return io.EOF
	}
	if rc != C.SQLITE_ROW {
		return r.stmt.c.lastError()
	}
	n := int(C.sqlite3_column_count(r.stmt.stmt))
	for i := 0; i < n; i++ {
		idx := C.int(i)
		switch C.sqlite3_column_type(r.stmt.stmt, idx) {
		case C.SQLITE_INTEGER:
			dest[i] = int64(C.sqlite3_column_int64(r.stmt.stmt, idx))
		case C.SQLITE_FLOAT:
			dest[i] = float64(C.sqlite3_column_double(r.stmt.stmt, idx))
		case C.SQLITE_TEXT:
			ptr := C.sqlite3_column_text(r.stmt.stmt, idx)
			n2 := C.sqlite3_column_bytes(r.stmt.stmt, idx)
			dest[i] = C.GoStringN((*C.char)(unsafe.Pointer(ptr)), n2)
		case C.SQLITE_BLOB:
			ptr := C.sqlite3_column_blob(r.stmt.stmt, idx)
			n2 := C.sqlite3_column_bytes(r.stmt.stmt, idx)
			if n2 == 0 {
				dest[i] = []byte{}
			} else {
				dest[i] = C.GoBytes(ptr, n2)
			}
		case C.SQLITE_NULL:
			dest[i] = nil
		}
	}
	return nil
}
