package runtimeprovider

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

type countedProviderReader struct {
	*bytes.Reader
	reads int
}

func (r *countedProviderReader) Read(buffer []byte) (int, error) {
	r.reads++
	return r.Reader.Read(buffer)
}

func (r *countedProviderReader) WriteTo(io.Writer) (int64, error) {
	return 0, errors.New("small-block WriterTo must not bypass the verification buffer")
}

func TestProviderDigestUsesBoundedLargeReadsAndChecksEveryByte(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3}, 1024*1024)
	digest := sha256.Sum256(data)
	reader := &countedProviderReader{Reader: bytes.NewReader(data)}
	actual, err := installedFileDigest(reader)
	if err != nil || actual != hex.EncodeToString(digest[:]) || reader.reads > 4 {
		t.Fatalf("digest=%s err=%v reads=%d", actual, err, reader.reads)
	}
	data[len(data)-1] ^= 1
	changed, err := installedFileDigest(bytes.NewReader(data))
	if err != nil || changed == actual {
		t.Fatalf("changed final byte was not detected: %v", err)
	}
}
