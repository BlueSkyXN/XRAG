// Package source implements complete, bounded inventories. Git reads immutable
// objects at one commit; it never checks out or writes the user's working tree.
package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

const MaxFiles = 5000
const MaxFileBytes = 5 << 20
const MaxScanBytes = 64 << 20

type Entry struct {
	Path string
	Body string
	Blob string
}
type Scan struct {
	Revision    string
	Consistency string
	Fingerprint string
	Entries     []Entry
}

type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("git output exceeds source limit")
	}
	return b.Buffer.Write(p)
}
func git(ctx context.Context, dir string, max int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	a := []string{"--no-optional-locks", "-c", "core.hooksPath=/dev/null", "-c", "gc.auto=0", "-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=never", "-c", "credential.helper="}
	if dir != "" {
		a = append(a, "-C", dir)
	}
	a = append(a, args...)
	cmd := exec.CommandContext(ctx, "git", a...)
	cmd.WaitDelay = time.Second
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1")
	out, errout := &boundedBuffer{max: max}, &boundedBuffer{max: 16384}
	cmd.Stdout = out
	cmd.Stderr = errout
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return nil, core.Err("SOURCE_TIMEOUT", "git source operation timed out or was cancelled", 504)
		}
		return nil, core.Err("SOURCE_UNAVAILABLE", fmt.Sprintf("git %s failed: %s", args[0], strings.TrimSpace(errout.String())), 503)
	}
	return out.Bytes(), nil
}
func repository(ctx context.Context, data string, s store.Source) (string, error) {
	if !strings.HasPrefix(s.Root, "https://") {
		return s.Root, nil
	}
	dir := filepath.Join(data, "git", s.KB, s.ID)
	// An immutable origin config prevents reusing a cache for a different repository.
	expected := core.Hash(s.Root)
	if raw, e := os.ReadFile(filepath.Join(dir, "xrag-origin")); e == nil {
		if string(raw) != expected {
			return "", core.Err("VERSION_CONFLICT", "git cache belongs to another source URL", 409)
		}
	} else {
		if e = os.MkdirAll(filepath.Dir(dir), 0700); e != nil {
			return "", e
		}
		tmp, e := os.MkdirTemp(filepath.Dir(dir), "clone-")
		if e != nil {
			return "", e
		}
		defer os.RemoveAll(tmp)
		clone := filepath.Join(tmp, "repo.git")
		if _, e = git(ctx, "", 16384, "clone", "--mirror", "--", s.Root, clone); e != nil {
			return "", e
		}
		if e = os.WriteFile(filepath.Join(clone, "xrag-origin"), []byte(expected), 0600); e != nil {
			return "", e
		}
		if e = os.Rename(clone, dir); e != nil {
			if b, x := os.ReadFile(filepath.Join(dir, "xrag-origin")); x != nil || string(b) != expected {
				return "", e
			}
		}
	}
	if _, e := git(ctx, dir, 16384, "fetch", "--prune", "origin"); e != nil {
		return "", e
	}
	return dir, nil
}
func Open(ctx context.Context, data string, s store.Source) (Scan, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out := Scan{Entries: []Entry{}, Consistency: "filesystem_live"}
	var e error
	switch s.Kind {
	case "git":
		out, e = scanGit(ctx, data, s)
	case "s3":
		out, e = scanS3(ctx, s)
	case "weknora":
		out, e = scanWeKnora(ctx, s)
	default:
		out, e = scanPath(ctx, s.Root)
	}
	if e != nil {
		return out, e
	}
	filtered := []Entry{}
	for _, entry := range out.Entries {
		if core.Included(entry.Path, s.Options.Include, s.Options.Exclude) {
			filtered = append(filtered, entry)
		}
	}
	out.Entries = filtered
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Path < out.Entries[j].Path })
	manifest := []any{}
	for _, v := range out.Entries {
		manifest = append(manifest, []string{v.Path, core.TextHash(v.Body)})
	}
	out.Fingerprint = core.Hash([]any{s.Kind, s.Ref, out.Revision, manifest})
	return out, nil
}
func scanGit(ctx context.Context, data string, s store.Source) (Scan, error) {
	out := Scan{Consistency: "revision_pinned", Entries: []Entry{}}
	repo, e := repository(ctx, data, s)
	if e != nil {
		return out, e
	}
	ref := s.Ref
	if ref == "" {
		ref = "HEAD"
	}
	raw, e := git(ctx, repo, 256, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if e != nil {
		return out, e
	}
	out.Revision = strings.TrimSpace(string(raw))
	raw, e = git(ctx, repo, 8<<20, "ls-tree", "-r", "-z", "-l", "--full-tree", out.Revision)
	if e != nil {
		return out, e
	}
	total := 0
	for _, line := range bytes.Split(raw, []byte{0}) {
		if len(line) == 0 {
			continue
		}
		parts := bytes.SplitN(line, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return out, core.Err("SOURCE_INVALID", "malformed git tree entry", 422)
		}
		fields := strings.Fields(string(parts[0]))
		if len(fields) != 4 {
			return out, core.Err("SOURCE_INVALID", "malformed git object metadata", 422)
		}
		if fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			continue
		} // no symlinks or submodules
		path := string(parts[1])
		if !supported(path) {
			continue
		}
		if _, e = core.CleanPath(path); e != nil {
			return out, e
		}
		n, e := strconv.Atoi(fields[3])
		if e != nil || n < 0 || n > MaxFileBytes {
			return out, core.Err("SOURCE_LIMIT", "git file exceeds 5 MiB", 422)
		}
		total += n
		if total > MaxScanBytes || len(out.Entries) >= MaxFiles {
			return out, core.Err("SOURCE_LIMIT", "source exceeds 5000 files or 64 MiB", 422)
		}
		blob, e := git(ctx, repo, MaxFileBytes+1, "cat-file", "blob", fields[2])
		if e != nil {
			return out, e
		}
		if len(blob) != n {
			return out, core.Err("SOURCE_CHANGED", "git blob size mismatch", 409)
		}
		text, e := core.Normalize(blob)
		if e != nil {
			return out, e
		}
		out.Entries = append(out.Entries, Entry{Path: path, Body: text, Blob: fields[2]})
	}
	return out, nil
}
func supported(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".markdown", ".txt":
		return true
	}
	return false
}
func scanPath(ctx context.Context, root string) (Scan, error) {
	out := Scan{Entries: []Entry{}, Consistency: "filesystem_live"}
	total := 0
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return out, e
	}
	e = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !supported(p) {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if _, e = core.CleanPath(rel); e != nil {
			return e
		}
		real, e := filepath.EvalSymlinks(p)
		if e != nil {
			return e
		}
		inside, e := filepath.Rel(root, real)
		if e != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return core.Err("SOURCE_CHANGED", "source path moved outside its root", 409)
		}
		before, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !before.Mode().IsRegular() || before.Size() > MaxFileBytes {
			return core.Err("SOURCE_LIMIT", "source file must be regular and at most 5 MiB", 422)
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		actual, e := f.Stat()
		if e != nil || !os.SameFile(before, actual) {
			f.Close()
			return core.Err("SOURCE_CHANGED", "source changed before opening", 409)
		}
		raw, e := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
		after, ae := f.Stat()
		f.Close()
		if e != nil {
			return e
		}
		end, ee := os.Lstat(p)
		if ae != nil || ee != nil || !os.SameFile(before, end) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || len(raw) > MaxFileBytes {
			return core.Err("SOURCE_CHANGED", "file changed during scan", 409)
		}
		total += len(raw)
		if total > MaxScanBytes || len(out.Entries) >= MaxFiles {
			return core.Err("SOURCE_LIMIT", "source exceeds 5000 files or 64 MiB", 422)
		}
		text, e := core.Normalize(raw)
		if e != nil {
			return e
		}
		out.Entries = append(out.Entries, Entry{Path: rel, Body: text})
		return nil
	})
	return out, e
}
