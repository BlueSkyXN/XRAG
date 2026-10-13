//go:build !linux

package watch

import "errors"

func Available() bool { return false }
func New(root string) (Monitor, error) {
	return nil, errors.New("native watcher not built on this platform; periodic scans remain available")
}
