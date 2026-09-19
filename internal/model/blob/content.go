// Package blob defines verified content facts independently of storage resources.
package blob

// PreparedBlob describes bytes whose size and hashes have been verified.
// It does not prove that a CAS object is still available or protected from GC.
type PreparedBlob struct {
	SHA256 string
	MD5    string
	SHA1   string
	CRC32  string
	Size   int64
}

// BlobRef identifies a cataloged blob and its verified content.
type BlobRef struct {
	BlobID string
	PreparedBlob
}
