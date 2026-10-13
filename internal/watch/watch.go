// Package watch provides optional OS change hints. Periodic complete scans remain authoritative.
package watch

type Monitor interface {
	Changed() (bool, error)
	Close() error
}
