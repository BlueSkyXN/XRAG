package path

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/BlueSkyXN/XRAG/core/types"
)

type Entry struct {
	Rel string
	Abs string
}

// Inventory lists regular .md and .txt files. Symlinks are skipped. Removal is
// decided by the caller only after this listing completes.
func Inventory(root string) ([]Entry, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, types.Fail("INVALID_ARGUMENT", "path source is not a directory", 400)
	}
	var out []Entry
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = norm.NFC.String(filepath.ToSlash(rel))
		if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			return types.Fail("INVALID_ARGUMENT", "source path escapes the root", 400)
		}
		out = append(out, Entry{Rel: rel, Abs: p})
		return nil
	})
	return out, err
}

// Read fetches one file and retries once when the file changes mid-read.
func Read(abs string) ([]byte, error) {
	first, err := readOnce(abs)
	if err != nil {
		return nil, err
	}
	second, err := readOnce(abs)
	if err != nil {
		return nil, err
	}
	if string(first) != string(second) {
		return nil, types.Fail("SOURCE_CHANGED", "file changed while reading", 409)
	}
	return second, nil
}

func readOnce(abs string) ([]byte, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func Mount(mount, rel string) (string, error) {
	mount = strings.Trim(filepath.ToSlash(mount), "/")
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if strings.Contains(mount, "..") || strings.Contains(rel, "..") {
		return "", types.Fail("INVALID_ARGUMENT", "mount or path contains ..", 400)
	}
	if mount == "" {
		return rel, nil
	}
	if rel == "" {
		return mount, nil
	}
	return mount + "/" + rel, nil
}
