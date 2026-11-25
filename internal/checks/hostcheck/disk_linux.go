//go:build linux

package hostcheck

import "syscall"

func statfs(path string) (fsStat, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return fsStat{}, err
	}
	return fsStat{
		Blocks: uint64(s.Blocks), Bfree: uint64(s.Bfree), Bavail: uint64(s.Bavail),
		Files: uint64(s.Files), Ffree: uint64(s.Ffree), Bsize: uint64(s.Bsize),
	}, nil
}
