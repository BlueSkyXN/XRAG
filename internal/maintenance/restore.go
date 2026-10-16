// Package maintenance contains explicit offline operations, never remote
// arbitrary-path operations. Restore always creates a NEW directory.
package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func Restore(ctx context.Context, from, destination string) (map[string]any, error) {
	dest, e := filepath.Abs(destination)
	if e != nil {
		return nil, e
	}
	in, e := os.Open(from)
	if e != nil {
		return nil, e
	}
	defer in.Close()
	st, e := in.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("backup must be a regular SQLite file")
	}
	if st.Size() > 2<<30 {
		return nil, core.Err("BACKUP_LIMIT", "restore accepts a snapshot up to 2 GiB", 422)
	}
	header := make([]byte, 16)
	if _, e = io.ReadFull(in, header); e != nil || string(header) != "SQLite format 3\x00" {
		return nil, core.Err("INVALID_BACKUP", "not a SQLite snapshot", 400)
	}
	if _, e = in.Seek(0, io.SeekStart); e != nil {
		return nil, e
	}
	if e = os.Mkdir(dest, 0700); e != nil {
		return nil, core.Err("VERSION_CONFLICT", "restore destination must not exist (its parent directory must exist)", 409)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dest)
		}
	}()
	file, e := os.OpenFile(filepath.Join(dest, "xrag.sqlite"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, e
	}
	_, e = io.Copy(file, in)
	if e == nil {
		e = file.Sync()
	}
	ce := file.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return nil, e
	}
	d, e := db.Open(filepath.Join(dest, "xrag.sqlite"))
	if e != nil {
		return nil, e
	}
	e = d.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("PRAGMA integrity_check")
		if e != nil {
			return e
		}
		if len(rs) != 1 || db.String(rs[0], "integrity_check") != "ok" {
			return core.Err("INVALID_BACKUP", "database integrity_check did not return ok", 400)
		}
		fk, e := c.Query("PRAGMA foreign_key_check")
		if e != nil {
			return e
		}
		if len(fk) > 0 {
			return core.Err("INVALID_BACKUP", "database contains broken foreign keys", 400)
		}
		rs, e = c.Query("SELECT v FROM meta WHERE k='origin'")
		if e != nil || len(rs) != 1 || db.String(rs[0], "v") == "" {
			return core.Err("INVALID_BACKUP", "not an XRAG database snapshot", 400)
		}
		for _, table := range []string{"file", "edition", "kb", "api_key", "job"} {
			rs, e = c.Query("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table)
			if e != nil || len(rs) != 1 {
				return core.Err("INVALID_BACKUP", "missing XRAG tables", 400)
			}
		}
		return nil
	})
	d.Close()
	if e != nil {
		return nil, e
	}
	s, e := store.Open(dest)
	if e != nil {
		return nil, e
	}
	token, e := s.PrepareRestoredCopy(ctx)
	origin := s.Origin
	s.Close()
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(dest, "admin.key"), []byte(token+"\n"), 0600); e != nil {
		return nil, e
	}
	result := map[string]any{"restored": dest, "origin_id": origin, "admin_key_file": filepath.Join(dest, "admin.key"), "old_keys_disabled": true, "source_automation_disabled": true, "provider_config_disabled": true, "queued_and_running_jobs_stopped": true, "source_directories_included": false, "external_backends_included": false, "created_at": time.Now().UTC().Format(time.RFC3339)}
	b, _ := json.MarshalIndent(result, "", "  ")
	if e = os.WriteFile(filepath.Join(dest, "RESTORE_INFO.json"), b, 0600); e != nil {
		return nil, e
	}
	ok = true
	return result, nil
}
