package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// WriteBack only writes the explicitly registered destination. expectedHash is the
// last receipt's digest, not an instruction to overwrite an arbitrary existing file.
func WriteBack(ctx context.Context, t store.SyncTarget, rel, body, expectedHash string) (string, error) {
	if _, e := core.CleanPath(rel); e != nil {
		return "", e
	}
	if len(body) > 2<<20 {
		return "", core.Err("SOURCE_LIMIT", "published writeback exceeds 2 MiB", 422)
	}
	if t.Kind == "s3" {
		return writeS3(ctx, t, rel, body, expectedHash)
	}
	if t.Kind == "git" {
		return writeGit(ctx, t, rel, body, expectedHash)
	}
	if t.Kind != "path" {
		return "", core.Err("MODE_UNSUPPORTED", "unknown writeback destination", 422)
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	root, e := filepath.EvalSymlinks(t.Root)
	if e != nil {
		return "", e
	}
	if root != t.Root {
		return "", core.Err("SOURCE_CHANGED", "registered destination root changed", 409)
	}
	lock, e := os.OpenFile(filepath.Join(root, ".xrag-sync.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return "", e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return "", core.Err("WORKSPACE_BUSY", "destination is being updated", 409)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	parent := root
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		parent = filepath.Join(parent, part)
		if e = os.Mkdir(parent, 0700); e != nil && !os.IsExist(e) {
			return "", e
		}
		st, e := os.Lstat(parent)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return "", core.Err("SOURCE_CHANGED", "destination contains a symlink or non-directory", 409)
		}
	}
	dest := filepath.Join(parent, parts[len(parts)-1])
	old, e := readDestination(dest)
	originalExists := e == nil
	hash := core.TextHash(body)
	if e == nil {
		if core.TextHash(old) == hash {
			return hash, nil
		}
		if expectedHash == "" || core.TextHash(old) != expectedHash {
			return "", core.Err("SYNC_CONFLICT", "destination was edited outside XRAG; no file overwritten", 409)
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	tmp, e := os.CreateTemp(parent, ".xrag-write-")
	if e != nil {
		return "", e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, e = tmp.WriteString(body); e == nil {
		e = tmp.Sync()
	}
	ce := tmp.Close()
	if e != nil {
		return "", e
	}
	if ce != nil {
		return "", ce
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	// Recheck immediately before replacement. The directory lock coordinates XRAG writers.
	latest, le := readDestination(dest)
	if le == nil {
		if !originalExists {
			return "", core.Err("SYNC_CONFLICT", "destination was concurrently created", 409)
		}
		if core.TextHash(latest) != core.TextHash(old) {
			return "", core.Err("SYNC_CONFLICT", "destination changed while preparing writeback", 409)
		}
		if e = os.Rename(name, dest); e != nil {
			return "", e
		}
	} else if os.IsNotExist(le) {
		if originalExists {
			return "", core.Err("SYNC_CONFLICT", "destination was concurrently removed", 409)
		}
		if e = os.Link(name, dest); e != nil {
			return "", core.Err("SYNC_CONFLICT", "destination was concurrently created", 409)
		}
	} else {
		return "", le
	}
	if d, e := os.Open(parent); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return hash, nil
}
func readDestination(path string) (string, error) {
	st, e := os.Lstat(path)
	if e != nil {
		return "", e
	}
	if !st.Mode().IsRegular() || st.Size() > 2<<20 {
		return "", core.Err("SYNC_CONFLICT", "destination is not a small regular file", 409)
	}
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return "", e
	}
	defer f.Close()
	now, e := f.Stat()
	if e != nil || !os.SameFile(st, now) {
		return "", core.Err("SOURCE_CHANGED", "destination changed type", 409)
	}
	b, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if e != nil {
		return "", e
	}
	if int64(len(b)) != st.Size() {
		return "", core.Err("SOURCE_CHANGED", "destination changed size", 409)
	}
	return core.Normalize(b)
}
func writeGit(ctx context.Context, t store.SyncTarget, rel, body, expectedHash string) (string, error) {
	// Use a temporary index and plumbing commands; never checkout or alter the user's index.
	tmp, e := os.MkdirTemp("", "xrag-git-index-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(tmp)
	index := filepath.Join(tmp, "index")
	call := func(stdin string, args ...string) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		base := []string{"--no-optional-locks", "-c", "core.hooksPath=/dev/null", "-c", "gc.auto=0", "-C", t.Root}
		base = append(base, args...)
		cmd := exec.CommandContext(cctx, "git", base...)
		cmd.WaitDelay = time.Second
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "GIT_") {
				cmd.Env = append(cmd.Env, v)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=XRAG", "GIT_AUTHOR_EMAIL=xrag@localhost", "GIT_COMMITTER_NAME=XRAG", "GIT_COMMITTER_EMAIL=xrag@localhost", "GIT_NO_REPLACE_OBJECTS=1")
		cmd.Stdin = strings.NewReader(stdin)
		out := &boundedBuffer{max: 4 << 20}
		errout := &boundedBuffer{max: 8192}
		cmd.Stdout = out
		cmd.Stderr = errout
		if e := cmd.Run(); e != nil {
			return "", core.Err("SYNC_FAILED", fmt.Sprintf("git writeback %s failed: %s", args[0], strings.TrimSpace(errout.String())), 503)
		}
		return strings.TrimRight(out.String(), "\n"), nil
	}
	if _, e = call("", "check-ref-format", "refs/heads/"+t.Branch); e != nil {
		return "", e
	}
	branch := "refs/heads/" + t.Branch
	old, e := call("", "rev-parse", "--verify", "--end-of-options", branch+"^{commit}")
	exists := e == nil
	base := old
	if !exists {
		base, e = call("", "rev-parse", "--verify", "HEAD^{commit}")
		if e != nil {
			return "", core.Err("INVALID_ARGUMENT", "git destination must contain an initial commit", 400)
		}
	}
	if _, e = call("", "read-tree", base); e != nil {
		return "", e
	}
	// ls-tree distinguishes missing paths from inaccessible repository/errors.
	tree, e := call("", "ls-tree", "-z", base, "--", rel)
	if e != nil {
		return "", e
	}
	if tree != "" {
		entry := strings.SplitN(tree, "\t", 2)
		meta := strings.Fields(entry[0])
		if len(meta) != 3 || meta[0] != "100644" {
			return "", core.Err("SYNC_CONFLICT", "destination is not an ordinary Markdown file", 409)
		}
		raw, e := git(ctx, t.Root, 2<<20, "cat-file", "blob", meta[2])
		if e != nil {
			return "", e
		}
		text, e := core.Normalize(raw)
		if e != nil {
			return "", e
		}
		if core.TextHash(text) == core.TextHash(body) {
			return base, nil
		}
		if expectedHash == "" || core.TextHash(text) != expectedHash {
			return "", core.Err("SYNC_CONFLICT", "Git destination changed outside XRAG", 409)
		}
	}
	blob, e := call(body, "hash-object", "-w", "--stdin")
	if e != nil {
		return "", e
	}
	// stdin index-info avoids shell interpretation and path-option ambiguities.
	info := "100644 " + blob + "\t" + rel + "\x00"
	if _, e = call(info, "update-index", "-z", "--index-info"); e != nil {
		return "", e
	}
	newTree, e := call("", "write-tree")
	if e != nil {
		return "", e
	}
	commit, e := call("XRAG: publish "+rel+"\n", "commit-tree", newTree, "-p", base)
	if e != nil {
		return "", e
	}
	expected := old
	if !exists {
		expected = strings.Repeat("0", len(base))
	}
	if _, e = call("", "update-ref", branch, commit, expected); e != nil {
		return "", core.Err("SYNC_CONFLICT", "writeback branch moved; retry after reviewing destination", 409)
	}
	return commit, nil
}

// RenderPublished emits ordinary Markdown with an explicit source-reference appendix.
// It can be read outside XRAG; the marker is informational, never an import bypass.
func RenderPublished(origin string, k store.Knowledge) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "<!-- XRAG published copy: %s %s %s -->\n# %s\n\n%s\n\n## 原始资料引用\n\n", origin, k.ID, k.Draft, k.Title, k.Body)
	for i, r := range k.Refs {
		fmt.Fprintf(&b, "%d. `%s`\n", i+1, r)
	}
	return b.String()
}

// PushGit publishes only the registered xrag/* branch. Remote tip must equal the prior receipt.
func PushGit(ctx context.Context, t store.SyncTarget, commit, expected string) error {
	if !t.Push {
		return nil
	}
	branch := "refs/heads/" + t.Branch
	call := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "protocol.ext.allow=never", "-C", t.Root}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
		cmd.WaitDelay = time.Second
		out, errout := &boundedBuffer{max: 16384}, &boundedBuffer{max: 16384}
		cmd.Stdout = out
		cmd.Stderr = errout
		if e := cmd.Run(); e != nil {
			return "", core.Err("SYNC_CONFLICT", "git push refused or unavailable: "+strings.TrimSpace(errout.String()), 409)
		}
		return strings.TrimSpace(out.String()), nil
	}
	current, e := call("ls-remote", "--heads", t.RemoteURL, branch)
	if e != nil {
		return e
	}
	tip := ""
	if current != "" {
		fields := strings.Fields(current)
		if len(fields) != 2 || fields[1] != branch {
			return core.Err("SYNC_CONFLICT", "ambiguous remote branch", 409)
		}
		tip = fields[0]
	}
	if tip == commit {
		return nil
	}
	if tip != expected {
		return core.Err("SYNC_CONFLICT", "remote branch changed since the last receipt", 409)
	}
	if expected != "" {
		if _, e = call("merge-base", "--is-ancestor", expected, commit); e != nil {
			return core.Err("SYNC_CONFLICT", "refusing a non-fast-forward knowledge publication", 409)
		}
	}
	_, e = call("push", "--porcelain", "--force-with-lease="+branch+":"+expected, "--", t.RemoteURL, commit+":"+branch)
	return e
}
