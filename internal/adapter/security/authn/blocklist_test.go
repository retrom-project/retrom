package authn

import (
	"bytes"
	"errors"
	"io"
	"testing"

	authnpolicy "retrom/internal/capability/security/authn"
)

type blocklistInput struct {
	reader     io.Reader
	readErr    error
	closeErr   error
	bytesRead  int
	closeCalls int
}

func (input *blocklistInput) Read(buffer []byte) (int, error) {
	count, err := input.reader.Read(buffer)
	input.bytesRead += count
	if errors.Is(err, io.EOF) && input.readErr != nil {
		return count, input.readErr
	}
	return count, err
}

func (input *blocklistInput) Close() error {
	input.closeCalls++
	return input.closeErr
}

func TestBlocklistReadIsBoundedAndAlwaysClosed(t *testing.T) {
	t.Parallel()
	closeFailure := errors.New("public fixture close failure")
	input := &blocklistInput{
		reader:   bytes.NewReader(bytes.Repeat([]byte{'x'}, 2*authnpolicy.BlocklistSize)),
		closeErr: closeFailure,
	}
	blocklist, err := readBlocklist(input)
	if blocklist != nil || !errors.Is(err, authnpolicy.ErrBlocklistInvalid) || errors.Is(err, closeFailure) {
		t.Fatalf("bounded invalid input changed error: %v", err)
	}
	if input.bytesRead != authnpolicy.BlocklistSize+1 || input.closeCalls != 1 {
		t.Fatalf("read/close boundary changed: bytes=%d closes=%d", input.bytesRead, input.closeCalls)
	}
}

func TestBlocklistReadFailureIsFailClosed(t *testing.T) {
	t.Parallel()
	readFailure := errors.New("public fixture read failure")
	input := &blocklistInput{reader: bytes.NewReader(nil), readErr: readFailure, closeErr: errors.New("public fixture close failure")}
	blocklist, err := readBlocklist(input)
	if blocklist != nil || !errors.Is(err, authnpolicy.ErrBlocklistInvalid) || errors.Is(err, readFailure) {
		t.Fatalf("read failure changed error: %v", err)
	}
	if input.closeCalls != 1 {
		t.Fatalf("failed read closed %d times", input.closeCalls)
	}
}
