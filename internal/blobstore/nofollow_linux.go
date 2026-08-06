//go:build linux

package blobstore

import "syscall"

func syscallNoFollow() int {
	return syscall.O_NOFOLLOW
}
