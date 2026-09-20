package archive

import (
	"context"
	"errors"
	"io"

	"retrom/internal/capability/format/importing"
	"retrom/internal/model/diagnostics"
)

type asarCursor struct {
	ctx            context.Context
	reporter       diagnostics.ErrorReporter
	archive        *electronZIPArchive
	appReader      io.ReadCloser
	appMonitor     *archiveReadMonitor
	appSize        int64
	dataOffset     int64
	position       int64
	members        []importing.ASARMember
	index          int
	method         uint16
	active         bool
	unpackedReader io.ReadCloser
	monitor        *archiveReadMonitor
	packedVerified bool
	failure        error
	done           bool
	closed         bool
}

func (cursor *asarCursor) Next() (importing.ArchiveMemberHeader, error) {
	if cursor.closed || cursor.active {
		return importing.ArchiveMemberHeader{}, errCursorState
	}
	if cursor.failure != nil {
		return importing.ArchiveMemberHeader{}, cursor.failure
	}
	if cursor.done {
		return importing.ArchiveMemberHeader{}, io.EOF
	}
	if cursor.index == len(cursor.members) || cursor.members[cursor.index].Unpacked {
		if err := cursor.verifyPacked(); err != nil {
			cursor.failure = err
			return importing.ArchiveMemberHeader{}, err
		}
	}
	if cursor.index == len(cursor.members) {
		cursor.done = true
		return importing.ArchiveMemberHeader{}, io.EOF
	}
	member := cursor.members[cursor.index]
	reader, method, err := cursor.openMember(member)
	if err != nil {
		cursor.failure = err
		return importing.ArchiveMemberHeader{}, err
	}
	cursor.method = method
	cursor.monitor = &archiveReadMonitor{ctx: cursor.ctx, reader: io.LimitReader(reader, member.Size), limit: member.Size}
	cursor.active = true
	return member.Header(method), nil
}

func (cursor *asarCursor) openMember(member importing.ASARMember) (io.Reader, uint16, error) {
	if member.Unpacked {
		item := cursor.archive.layout.Unpacked[importing.ASCIICaseFold(member.Path)]
		reader, err := cursor.archive.archive.reader.File[item.Ordinal].Open()
		if err != nil {
			return nil, 0, importing.ErrElectronASARInvalid
		}
		cursor.unpackedReader = reader
		return reader, item.Header.Method, nil
	}
	if err := copyExact(cursor.appMonitor, member.Offset-cursor.position); err != nil {
		return nil, 0, importing.ErrElectronASARInvalid
	}
	return cursor.appMonitor, cursor.archive.layout.AppASAR.Header.Method, nil
}

func (cursor *asarCursor) verifyPacked() error {
	if cursor.packedVerified {
		return nil
	}
	remaining := cursor.appSize - cursor.dataOffset - cursor.position
	if remaining < 0 || copyExact(cursor.appMonitor, remaining) != nil {
		return importing.ErrElectronASARInvalid
	}
	if _, err := io.Copy(io.Discard, cursor.appMonitor); err != nil || cursor.appMonitor.written != cursor.appSize {
		return importing.ErrElectronASARInvalid
	}
	cursor.packedVerified = true
	return nil
}

func (cursor *asarCursor) Read(buffer []byte) (int, error) {
	if cursor.closed || !cursor.active {
		return 0, errCursorState
	}
	if cursor.failure != nil {
		return 0, cursor.failure
	}
	count, err := cursor.monitor.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		cursor.failure = err
	}
	return count, err
}

func (cursor *asarCursor) Complete(content importing.ArchiveContent) (importing.ArchiveEntry, error) {
	if cursor.closed || !cursor.active {
		return importing.ArchiveEntry{}, errCursorState
	}
	member := cursor.members[cursor.index]
	var entry importing.ArchiveEntry
	err := cursor.failure
	if err == nil {
		entry, err = member.Complete(cursor.method, content, cursor.monitor.written, cursor.monitor.observedPrefix())
	}
	if member.Unpacked {
		err = cursor.completeUnpacked(member, entry, err)
	}
	cursor.active = false
	cursor.monitor = nil
	if err != nil {
		cursor.failure = err
		return importing.ArchiveEntry{}, err
	}
	if !member.Unpacked {
		cursor.position = member.Offset + member.Size
	}
	cursor.index++
	return entry, nil
}

func (cursor *asarCursor) Close() error {
	if cursor.closed {
		return nil
	}
	cursor.closed = true
	var closeErr error
	if cursor.unpackedReader != nil {
		closeErr = cursor.unpackedReader.Close()
		cursor.unpackedReader = nil
	}
	cursor.active = false
	cursor.monitor = nil
	if cursor.appReader != nil {
		closeResource(cursor.ctx, cursor.reporter, cursor.appReader)
		cursor.appReader = nil
	}
	closeZIP(cursor.ctx, cursor.reporter, &cursor.archive.archive)
	return closeErr
}
