//go:build linux

package netplay

import "syscall"

func credentialNoFollow() int { return syscall.O_NOFOLLOW }
