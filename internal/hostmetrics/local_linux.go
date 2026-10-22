package hostmetrics

import (
	"cmp"
	"os"

	"golang.org/x/sys/unix"
)

// Local is this machine.
func Local() System {
	return System{FS: os.DirFS("/"), Statfs: statfs}
}

func statfs(path string) (used, avail uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	// As in df, the counts are in fragments, and Bavail leaves out the blocks
	// reserved for root, which Bfree counts as free.
	size := cmp.Or(uint64(st.Frsize), uint64(st.Bsize)) //nolint:gosec // G115: block sizes are positive.
	return (st.Blocks - min(st.Bfree, st.Blocks)) * size, st.Bavail * size, nil
}
