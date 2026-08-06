//go:build linux

package runtime

import "syscall"

func syscallNoFollow() int {
	return syscall.O_NOFOLLOW
}
