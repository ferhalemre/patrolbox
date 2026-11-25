//go:build !linux

package hostcheck

import "errors"

func statfs(string) (fsStat, error) {
	return fsStat{}, errors.New("disk statistics are only supported on Linux")
}
