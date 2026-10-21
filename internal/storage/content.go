package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"retrom/internal/model"
)

// Content is a logical view with an independent descriptor for each request.
// SyscallConn preserves net/http's sendfile path. ServeContent must bound copies
// with its LimitedReader: sendfile bypasses Read and the descriptor sees the ZIP.
type Content struct {
	file *os.File
	base int64
	size int64
}

func (s *Store) OpenContent(key string, offset, size int64) (*Content, error) {
	file, err := s.Read(key)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || offset < 0 || size < 0 || offset > info.Size() ||
		size > info.Size()-offset {
		closeFile(file)
		if err != nil {
			return nil, fmt.Errorf("stat content: %w", err)
		}
		return nil, model.ErrInvalid
	}
	view := &Content{file: file, base: offset, size: size}
	if _, err = view.Seek(0, io.SeekStart); err != nil {
		closeFile(file)
		return nil, err
	}
	return view, nil
}

func (f *Content) Read(buffer []byte) (int, error) {
	physical, err := f.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("content position: %w", err)
	}
	remaining := f.size - (physical - f.base)
	if remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	n, err := f.file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("read content: %w", err)
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, nil
}

func (f *Content) Seek(offset int64, whence int) (int64, error) {
	var origin int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		physical, err := f.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, fmt.Errorf("content position: %w", err)
		}
		origin = physical - f.base
	case io.SeekEnd:
		origin = f.size
	default:
		return 0, model.ErrInvalid
	}
	if origin < 0 || origin > f.size || offset < -origin || offset > f.size-origin {
		return 0, model.ErrInvalid
	}
	logical := origin + offset
	if _, err := f.file.Seek(f.base+logical, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek content: %w", err)
	}
	return logical, nil
}

func (f *Content) Close() error {
	if err := f.file.Close(); err != nil {
		return fmt.Errorf("close content: %w", err)
	}
	return nil
}

func (f *Content) SyscallConn() (syscall.RawConn, error) {
	conn, err := f.file.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("content descriptor: %w", err)
	}
	return conn, nil
}
