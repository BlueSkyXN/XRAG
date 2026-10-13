// Package sqlite is a small prepared-statement adapter to the installed SQLite C library.
// It intentionally has no network-fetched Go dependencies. Linux and macOS need SQLite headers.
package sqlite

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>
static int bind_string(sqlite3_stmt *s, int i, const char *v, int n) {return sqlite3_bind_text(s,i,v,n,SQLITE_TRANSIENT);}
*/
import "C"
import (
	"context"
	"fmt"
	"path/filepath"
	"unsafe"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
)

type Row map[string]any
type Conn struct{ p *C.sqlite3 }
type DB struct {
	gate chan struct{}
	dir  string
	c    Conn
}

func Open(path string) (*DB, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	d := &DB{gate: make(chan struct{}, 1)}
	if path != ":memory:" {
		d.dir = filepath.Dir(path)
	}
	rc := C.sqlite3_open_v2(p, &d.c.p, C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_FULLMUTEX, nil)
	if rc != C.SQLITE_OK {
		msg := C.GoString(C.sqlite3_errmsg(d.c.p))
		C.sqlite3_close(d.c.p)
		return nil, fmt.Errorf("sqlite open: %s", msg)
	}
	C.sqlite3_busy_timeout(d.c.p, 5000)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA synchronous=FULL"} {
		if _, e := d.c.Query(q); e != nil {
			d.Close()
			return nil, e
		}
	}
	return d, nil
}
func (d *DB) Close() error {
	d.gate <- struct{}{}
	defer func() { <-d.gate }()
	if d.c.p != nil {
		if C.sqlite3_close(d.c.p) != C.SQLITE_OK {
			return fmt.Errorf("sqlite close busy")
		}
		d.c.p = nil
	}
	return nil
}
func (d *DB) Do(ctx context.Context, f func(*Conn) error) error {
	if d.dir != "" {
		leaseCtx, release, err := oplock.Acquire(ctx, d.dir, false)
		if err != nil {
			return err
		}
		defer release()
		ctx = leaseCtx
	}
	select {
	case d.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-d.gate }()
	if e := ctx.Err(); e != nil {
		return e
	}
	if d.c.p == nil {
		return fmt.Errorf("database closed")
	}
	done := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			C.sqlite3_interrupt(d.c.p)
		case <-done:
		}
	}()
	e := f(&d.c)
	close(done)
	<-joined
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return e
}
func (d *DB) Tx(ctx context.Context, write bool, f func(*Conn) error) error {
	return d.Do(ctx, func(c *Conn) error {
		start := "BEGIN"
		if write {
			start = "BEGIN IMMEDIATE"
		}
		if e := c.Exec(start); e != nil {
			return e
		}
		ok := false
		defer func() {
			if !ok {
				_ = c.Exec("ROLLBACK")
			}
		}()
		if e := f(c); e != nil {
			return e
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := c.Exec("COMMIT"); e != nil {
			return e
		}
		ok = true
		return nil
	})
}
func (c *Conn) Query(q string, args ...any) ([]Row, error) {
	rows := []Row{}
	err := c.Each(q, args, func(r Row) error { rows = append(rows, r); return nil })
	return rows, err
}
func (c *Conn) Each(q string, args []any, fn func(Row) error) error {
	sql := C.CString(q)
	defer C.free(unsafe.Pointer(sql))
	var s *C.sqlite3_stmt
	if rc := C.sqlite3_prepare_v2(c.p, sql, -1, &s, nil); rc != C.SQLITE_OK {
		return fmt.Errorf("sqlite prepare: %s", C.GoString(C.sqlite3_errmsg(c.p)))
	}
	defer C.sqlite3_finalize(s)
	if len(args) != int(C.sqlite3_bind_parameter_count(s)) {
		return fmt.Errorf("sqlite parameter count mismatch")
	}
	for i, a := range args {
		idx := C.int(i + 1)
		var rc C.int
		switch v := a.(type) {
		case nil:
			rc = C.sqlite3_bind_null(s, idx)
		case int:
			rc = C.sqlite3_bind_int64(s, idx, C.sqlite3_int64(v))
		case int64:
			rc = C.sqlite3_bind_int64(s, idx, C.sqlite3_int64(v))
		case float64:
			rc = C.sqlite3_bind_double(s, idx, C.double(v))
		case bool:
			var b int
			if v {
				b = 1
			}
			rc = C.sqlite3_bind_int64(s, idx, C.sqlite3_int64(b))
		case string:
			p := C.CString(v)
			rc = C.bind_string(s, idx, p, C.int(len(v)))
			C.free(unsafe.Pointer(p))
		default:
			return fmt.Errorf("sqlite unsupported argument %T", a)
		}
		if rc != C.SQLITE_OK {
			return fmt.Errorf("sqlite bind failed")
		}
	}

	for {
		rc := C.sqlite3_step(s)
		if rc == C.SQLITE_DONE {
			break
		}
		if rc != C.SQLITE_ROW {
			return fmt.Errorf("sqlite step: %s", C.GoString(C.sqlite3_errmsg(c.p)))
		}
		r := Row{}
		n := int(C.sqlite3_column_count(s))
		for i := 0; i < n; i++ {
			j := C.int(i)
			name := C.GoString(C.sqlite3_column_name(s, j))
			switch C.sqlite3_column_type(s, j) {
			case C.SQLITE_INTEGER:
				r[name] = int64(C.sqlite3_column_int64(s, j))
			case C.SQLITE_FLOAT:
				r[name] = float64(C.sqlite3_column_double(s, j))
			case C.SQLITE_NULL:
				r[name] = nil
			default:
				p := C.sqlite3_column_text(s, j)
				r[name] = C.GoStringN((*C.char)(unsafe.Pointer(p)), C.sqlite3_column_bytes(s, j))
			}
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}
func (c *Conn) Exec(q string, args ...any) error { _, e := c.Query(q, args...); return e }
func (c *Conn) Changes() int                     { return int(C.sqlite3_changes(c.p)) }
func String(r Row, k string) string              { v, _ := r[k].(string); return v }
func Int(r Row, k string) int64                  { v, _ := r[k].(int64); return v }
func Float(r Row, k string) float64              { v, _ := r[k].(float64); return v }
func Version() string                            { return C.GoString(C.sqlite3_libversion()) }
