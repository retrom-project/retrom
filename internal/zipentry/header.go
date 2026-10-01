package zipentry

import (
	"archive/zip"
	"time"
)

func StoreHeader(name string) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	header.Modified = time.Time{}
	// archive/zip otherwise emits an extended timestamp. These DOS fields encode
	// 1980-01-01 00:00:00 while keeping Extra empty.
	header.ModifiedDate = 33 //nolint:staticcheck // Deterministic ZIP wire contract.
	header.ModifiedTime = 0  //nolint:staticcheck // Deterministic ZIP wire contract.
	header.Extra = nil
	header.Comment = ""
	return header
}
