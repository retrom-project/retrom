package detector

import (
	"bytes"
	"errors"
	"io"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
)

var (
	errCapturedOpen  = errors.New("rf04 injected open failure")
	errCapturedRead  = errors.New("rf04 injected read failure")
	errCapturedClose = errors.New("rf04 injected close failure")
)

type fileStats struct {
	Path                 string
	Opens, Reads, Closes int
	Bytes                int64
}

type traceIndex struct {
	source                        fileIndex
	sizes                         map[string]int64
	target, mode                  string
	stats                         map[string]*fileStats
	events                        []string
	active, maxActive, filesCalls int
}

func newTraceIndex(source fileIndex) *traceIndex {
	return &traceIndex{source: source, stats: make(map[string]*fileStats)}
}

func (index *traceIndex) Files() []policy.File {
	index.filesCalls++
	files := index.source.Files()
	for position := range files {
		if size, exists := index.sizes[files[position].Path]; exists {
			files[position].Size = size
		}
	}
	return files
}

func (index *traceIndex) Open(name string) (io.ReadCloser, error) {
	stats := index.stats[name]
	if stats == nil {
		stats = &fileStats{Path: name}
		index.stats[name] = stats
	}
	stats.Opens++
	index.events = append(index.events, "open:"+name)
	mode := ""
	if name == index.target {
		mode = index.mode
	}
	if mode == "open" {
		return nil, errCapturedOpen
	}
	if mode == "nil" {
		// Replay the old no-reader input; an error or typed-nil reader tests another boundary.
		return nil, nil
	}
	source, err := index.openSource(name, mode)
	if err != nil {
		return nil, err
	}
	reader := &traceReader{ReadCloser: source, index: index, stats: stats, mode: mode}
	if mode == "reader-and-open-error" {
		return reader, errCapturedOpen
	}
	index.active++
	index.maxActive = max(index.maxActive, index.active)
	return reader, nil
}

func (index *traceIndex) openSource(name, mode string) (io.ReadCloser, error) {
	if mode == "reader-and-open-error" {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	return index.source.Open(name)
}

type traceReader struct {
	io.ReadCloser
	index *traceIndex
	stats *fileStats
	mode  string
}

func (reader *traceReader) Read(buffer []byte) (int, error) {
	reader.stats.Reads++
	if reader.stats.Reads == 1 {
		reader.index.events = append(reader.index.events, "read:"+reader.stats.Path)
	}
	if reader.mode == "read" || reader.mode == "read-close" {
		return 0, errCapturedRead
	}
	count, err := reader.ReadCloser.Read(buffer)
	reader.stats.Bytes += int64(count)
	if reader.mode == "partial-read-close" && count > 0 {
		return count, errCapturedRead
	}
	return count, err
}

func (reader *traceReader) Close() error {
	reader.stats.Closes++
	reader.index.events = append(reader.index.events, "close:"+reader.stats.Path)
	reader.index.active--
	err := reader.ReadCloser.Close()
	if reader.mode == "close" || reader.mode == "read-close" || reader.mode == "partial-read-close" {
		return errCapturedClose
	}
	return err
}
