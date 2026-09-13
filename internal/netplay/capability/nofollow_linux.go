//go:build linux

package capability

import "syscall"

func credentialNoFollow() int { return syscall.O_NOFOLLOW }
