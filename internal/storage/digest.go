package storage

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type Digest struct {
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	MD5       string `json:"md5"`
	Name      string `json:"name"`
}

func Measure(ctx context.Context, root *os.Root, name string, maximum int64) (Digest, error) {
	file, err := root.Open(name)
	if err != nil {
		return Digest{}, fmt.Errorf("open source digest: %w", err)
	}
	defer closeFile(file)
	sha, md := sha256.New(), md5.New()
	size, err := io.Copy(io.MultiWriter(sha, md), io.LimitReader(&contextReader{ctx: ctx, reader: file}, maximum+1))
	if err != nil {
		return Digest{}, fmt.Errorf("measure source file: %w", err)
	}
	if size > maximum {
		return Digest{}, errTooLarge
	}
	return Digest{
		SizeBytes: size, SHA256: hex.EncodeToString(sha.Sum(nil)),
		MD5: hex.EncodeToString(md.Sum(nil)), Name: name,
	}, nil
}
