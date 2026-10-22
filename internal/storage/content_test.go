package storage

import (
	"context"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/google/uuid"
)

func TestContentInterval(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f, err := s.Write(context.Background(), "games", uuid.NewString(), strings.NewReader("BEFOREpayloadAFTER"), 100)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.OpenContent(f.Key, 6, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	bytes, err := io.ReadAll(r)
	if err != nil || string(bytes) != "payload" {
		t.Fatalf("body=%q err=%v", bytes, err)
	}
	if _, err = r.Seek(-1, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	bytes, err = io.ReadAll(r)
	if err != nil || string(bytes) != "d" {
		t.Fatalf("tail=%q err=%v", bytes, err)
	}
	for _, offset := range []int64{-1, 8} {
		if _, err = r.Seek(offset, io.SeekStart); err == nil {
			t.Fatalf("allowed offset %d", offset)
		}
	}
	if _, err = s.OpenContent(f.Key, 10, 100); err == nil {
		t.Fatal("allowed interval beyond object")
	}
	if _, err = s.OpenContent(f.Key, 1<<62, 1<<62); err == nil {
		t.Fatal("allowed overflowing interval")
	}
}

func TestUnboundedSocketCopyStaysInsideMember(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f, err := s.Write(context.Background(), "games", uuid.NewString(), strings.NewReader("BEFOREpayloadAFTER"), 100)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.OpenContent(f.Key, 6, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if _, exposed := any(r).(syscall.Conn); exposed {
		t.Fatal("logical content exposes its whole-object descriptor")
	}
	if _, sendfile := r.ServeContentReader().(syscall.Conn); !sendfile {
		t.Fatal("ServeContent lost the sendfile path")
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	received := make(chan string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			received <- acceptErr.Error()
			return
		}
		defer func() { _ = conn.Close() }()
		bytes, _ := io.ReadAll(conn)
		received <- string(bytes)
	}()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// TCPConn.ReadFrom would use sendfile to EOF of the ZIP if the descriptor were exposed.
	if _, err = io.Copy(conn, r); err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	if body := <-received; body != "payload" {
		t.Fatalf("socket received %q", body)
	}
}
