package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type BackupInfo struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Created   int64  `json:"created"`
	Available bool   `json:"available"`
}

func (s *Store) Backups(ctx context.Context) ([]BackupInfo, error) {
	out := []BackupInfo{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT * FROM backup_record ORDER BY created DESC,id DESC")
		if e != nil {
			return e
		}
		for _, r := range rs {
			b := BackupInfo{ID: db.String(r, "id"), Filename: db.String(r, "filename"), Size: db.Int(r, "size"), SHA256: db.String(r, "sha256"), Created: db.Int(r, "created")}
			_, e = s.backupFile(b.Filename)
			b.Available = e == nil
			out = append(out, b)
		}
		return nil
	})
	return out, e
}
func (s *Store) backupFile(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(name, ".sqlite") {
		return "", core.Err("NOT_FOUND", "backup not found", 404)
	}
	dir := filepath.Join(s.Dir, "backups")
	resolved, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return "", core.Err("NOT_FOUND", "backup file unavailable", 404)
	}
	root, e := filepath.EvalSymlinks(s.Dir)
	if e != nil {
		return "", e
	}
	if resolved != filepath.Join(root, "backups") {
		return "", core.Err("INVALID_ARGUMENT", "backup directory cannot be a symlink", 400)
	}
	path := filepath.Join(dir, name)
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() {
		return "", core.Err("NOT_FOUND", "backup file unavailable", 404)
	}
	return path, nil
}
func (s *Store) BackupPath(ctx context.Context, id string) (BackupInfo, string, error) {
	xs, e := s.Backups(ctx)
	if e != nil {
		return BackupInfo{}, "", e
	}
	for _, b := range xs {
		if b.ID == id {
			p, e := s.backupFile(b.Filename)
			return b, p, e
		}
	}
	return BackupInfo{}, "", core.Err("NOT_FOUND", "backup not found", 404)
}
func (s *Store) CreateBackup(ctx context.Context) (BackupInfo, error) {
	b := BackupInfo{ID: core.ID("bk_"), Created: time.Now().Unix()}
	xs, e := s.Backups(ctx)
	if e != nil {
		return b, e
	}
	if len(xs) >= 20 {
		return b, core.Err("BACKUP_LIMIT", "at most 20 registered snapshots; download and remove an older snapshot first", 422)
	}
	dir := filepath.Join(s.Dir, "backups")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return b, e
	}
	if st, e := os.Lstat(dir); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return b, core.Err("INVALID_ARGUMENT", "backup directory must be a real directory", 400)
	}
	b.Filename = b.ID + ".sqlite"
	tmp := filepath.Join(dir, b.ID+".partial")
	dest := filepath.Join(dir, b.Filename)
	defer os.Remove(tmp)
	if e = s.Backup(ctx, tmp); e != nil {
		return b, e
	}
	if e = os.Chmod(tmp, 0600); e != nil {
		return b, e
	}
	f, e := os.Open(tmp)
	if e != nil {
		return b, e
	}
	h := sha256.New()
	b.Size, e = io.Copy(h, f)
	f.Close()
	if e != nil {
		return b, e
	}
	b.SHA256 = hex.EncodeToString(h.Sum(nil))
	if e = os.Rename(tmp, dest); e != nil {
		return b, e
	}
	b.Available = true
	e = s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO backup_record(id,filename,size,sha256,created) VALUES(?,?,?,?,?)", b.ID, b.Filename, b.Size, b.SHA256, b.Created)
	})
	if e != nil {
		_ = os.Remove(dest)
	}
	return b, e
}
func (s *Store) RemoveBackup(ctx context.Context, id string) error {
	xs, e := s.Backups(ctx)
	if e != nil {
		return e
	}
	for _, b := range xs {
		if b.ID == id {
			if b.Available {
				p, e := s.backupFile(b.Filename)
				if e != nil {
					return e
				}
				if e = os.Remove(p); e != nil {
					return e
				}
			}
			return s.DB.Tx(ctx, true, func(c *db.Conn) error {
				if e := c.Exec("DELETE FROM automatic_backup WHERE id=?", id); e != nil {
					return e
				}
				return c.Exec("DELETE FROM backup_record WHERE id=?", id)
			})
		}
	}
	return core.Err("NOT_FOUND", "backup not found", 404)
}
