// Package oplock coordinates bounded content operations across same-machine
// Server/Worker processes. A batch collector excludes readers while deleting;
// ordinary requests hold a shared lease through result creation/adoption.
package oplock

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
)

type key struct{}
type lease struct {
	dir       string
	exclusive bool
}

// Acquire is reentrant only through the returned context. Never upgrade a read
// lease: a collector must start outside a request/read operation.
func Acquire(ctx context.Context, dir string, exclusive bool) (context.Context, func(), error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ctx, nil, err
	}
	if held, ok := ctx.Value(key{}).([]lease); ok {
		for _, l := range held {
			if l.dir == dir {
				if exclusive && !l.exclusive {
					return ctx, nil, core.Err("LEASE_UPGRADE_DENIED", "start content maintenance outside a read operation", 409)
				}
				return ctx, func() {}, nil
			}
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, "content.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return ctx, nil, err
	}
	mode := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		mode = syscall.LOCK_EX | syscall.LOCK_NB
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return ctx, nil, err
		}
		err = syscall.Flock(int(f.Fd()), mode)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN && err != syscall.EINTR {
			f.Close()
			return ctx, nil, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return ctx, nil, ctx.Err()
		case <-timer.C:
		}
	}
	held, _ := ctx.Value(key{}).([]lease)
	copyHeld := append(append([]lease{}, held...), lease{dir, exclusive})
	var once sync.Once
	release := func() { once.Do(func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }) }
	return context.WithValue(ctx, key{}, copyHeld), release, nil
}

func Exclusive(ctx context.Context, dir string) bool {
	dir, _ = filepath.Abs(dir)
	held, _ := ctx.Value(key{}).([]lease)
	for _, l := range held {
		if l.dir == dir && l.exclusive {
			return true
		}
	}
	return false
}
