//go:build !linux

package hostmetrics

import (
	"errors"
	"os"
)

// Local is this machine. Roost can only measure Linux, so a Collector of it
// fails each time.
func Local(root string) System {
	return System{FS: os.DirFS(root), Statfs: func(string) (uint64, uint64, error) {
		return 0, 0, errors.ErrUnsupported
	}}
}
