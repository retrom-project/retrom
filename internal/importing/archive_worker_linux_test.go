//go:build linux

package importing

import "testing"

func TestArchiveWorkerAddressSpaceLeavesRoomForFullBinaryAndDecoder(t *testing.T) {
	if archiveWorkerHeapLimitBytes != 512<<20 {
		t.Fatalf("archive worker heap limit = %d", archiveWorkerHeapLimitBytes)
	}
	if archiveWorkerAddressSpaceBytes < 4<<30 {
		t.Fatalf("archive worker address space = %d", archiveWorkerAddressSpaceBytes)
	}
}
