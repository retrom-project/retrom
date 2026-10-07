package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"
)

type (
	Entry struct {
		Key        string
		ModifiedAt time.Time
	}
	directoryCursor struct {
		key  string
		file *os.File
	}
)

// Scanner retains directory offsets between bounded cleanup batches.
type Scanner struct {
	store   *Store
	pending []string
	stack   []directoryCursor
}

func (s *Store) Scanner() *Scanner {
	return &Scanner{store: s, pending: []string{"games", "saves", "bios", "temporary"}}
}

func (s *Scanner) Close() {
	for _, cursor := range s.stack {
		closeFile(cursor.file)
	}
	s.stack = nil
}

func (s *Scanner) Next(maximum int) ([]Entry, bool, error) {
	result := make([]Entry, 0, maximum)
	for processed := 0; processed < maximum; processed++ {
		if len(s.stack) == 0 {
			if len(s.pending) == 0 {
				return result, true, nil
			}
			key := s.pending[0]
			s.pending = s.pending[1:]
			if err := s.open(key); err != nil {
				return nil, false, err
			}
			continue
		}
		current := s.stack[len(s.stack)-1]
		entries, err := current.file.ReadDir(1)
		if errors.Is(err, io.EOF) || errors.Is(err, os.ErrNotExist) {
			closeFile(current.file)
			s.stack = s.stack[:len(s.stack)-1]
			continue
		}
		if err != nil {
			return nil, false, fmt.Errorf("read managed cleanup directory: %w", err)
		}
		if len(entries) == 0 {
			continue
		}
		value, err := s.entry(current.key, entries[0])
		if err != nil {
			return nil, false, err
		}
		if value.Key != "" {
			result = append(result, value)
		}
	}
	return result, false, nil
}

func (s *Scanner) open(key string) error {
	file, err := s.store.root.Open(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open managed cleanup directory: %w", err)
	}
	s.stack = append(s.stack, directoryCursor{key: key, file: file})
	return nil
}

func (s *Scanner) entry(directory string, entry os.DirEntry) (Entry, error) {
	if entry.Type()&os.ModeSymlink != 0 {
		return Entry{}, nil
	}
	key := path.Join(directory, entry.Name())
	if entry.IsDir() {
		if len(s.stack) < 32 {
			if err := s.open(key); err != nil {
				return Entry{}, err
			}
		}
		return Entry{}, nil
	}
	info, err := entry.Info()
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, nil
	}
	if err != nil {
		return Entry{}, fmt.Errorf("stat managed cleanup entry: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Entry{}, nil
	}
	return Entry{Key: key, ModifiedAt: info.ModTime()}, nil
}
