//go:build linux

package filestore

import "syscall"

func syscallNoFollow() int {
	return syscall.O_NOFOLLOW
}
