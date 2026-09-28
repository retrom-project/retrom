package httpapi

import (
	"sync"
	"testing"
)

func TestWaitJoinsDeferredValidationAfterResponse(t *testing.T) {
	server := &testServer{
		Server:
		// Model validation scheduled by a handler after persisting its response.
		&Server{},
	}

	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	defer finish()
	server.deferredWork.Go(func() { close(entered); <-release })
	<-entered
	go func() { server.Wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("returned with validation still running")
	default:
	}
	finish()
	<-joined
}
