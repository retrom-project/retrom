//go:build !linux

package capability

func credentialNoFollow() int { return 0 }
