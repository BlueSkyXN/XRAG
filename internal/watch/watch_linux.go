//go:build linux

package watch

import (
	"errors"
	"io/fs"
	"path/filepath"
	"syscall"
)

type monitor struct{ fd int }

func Available() bool { return true }
func New(root string) (Monitor, error) {
	fd, e := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if e != nil {
		return nil, e
	}
	count := 0
	e = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if count >= 20000 {
			return errors.New("watch directory limit exceeded")
		}
		_, err = syscall.InotifyAddWatch(fd, p, syscall.IN_CLOSE_WRITE|syscall.IN_MOVED_TO|syscall.IN_MOVED_FROM|syscall.IN_CREATE|syscall.IN_DELETE|syscall.IN_ATTRIB|syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF)
		count++
		return err
	})
	if e != nil {
		syscall.Close(fd)
		return nil, e
	}
	return &monitor{fd}, nil
}
func (m *monitor) Changed() (bool, error) {
	b := make([]byte, 65536)
	changed := false
	for {
		n, e := syscall.Read(m.fd, b)
		if e == syscall.EAGAIN {
			return changed, nil
		}
		if e != nil {
			return changed, e
		}
		if n <= 0 {
			return changed, nil
		}
		changed = true
	}
}
func (m *monitor) Close() error { return syscall.Close(m.fd) }
