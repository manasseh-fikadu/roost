package hostmetrics

import (
	"os"

	"golang.org/x/sys/unix"
)

// Local is this machine.
func Local() System {
	return System{FS: os.DirFS("/"), Statfs: statfs}
}

func statfs(path string) (total, free uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize) //nolint:gosec // G115: a block size is positive.
	return st.Blocks * bsize, st.Bfree * bsize, nil
}
