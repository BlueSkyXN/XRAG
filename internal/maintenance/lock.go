package maintenance

import (
	"os"
	"path/filepath"
	"syscall"
	"github.com/BlueSkyXN/XRAG/core"
)

// RuntimeLock is held by every demo.4 Server/Worker process. Offline pruning
// takes it exclusively. Legacy workers must be stopped explicitly before upgrade.
func RuntimeLock(dir string, exclusive bool) (func(), error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	mode := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		mode = syscall.LOCK_EX | syscall.LOCK_NB
	}
	if e = syscall.Flock(int(f.Fd()), mode); e != nil {
		f.Close()
		return nil, core.Err("WORKSPACE_BUSY", "stop all Server and Worker processes before offline maintenance", 409)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
