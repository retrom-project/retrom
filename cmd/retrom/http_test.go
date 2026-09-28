package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestHTTPDrainTimeoutCancelsButStillJoinsHandlers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	requests := &activeRequests{handler: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(entered)
		<-request.Context().Done()
		close(cancelled)
		<-release
	})}
	server := httptest.NewUnstartedServer(requests)
	server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	server.Start()
	defer server.Close()
	releaseHandler := sync.OnceFunc(func() { close(release) })
	defer func() { cancel(); releaseHandler() }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Error(err)
			return
		}
		response, err := server.Client().Do(request)
		if err == nil {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	<-entered
	drained := make(chan error, 1)
	go func() { drained <- drainHTTP(t.Context(), server.Config, requests, cancel, 0) }()
	<-cancelled
	select {
	case <-drained:
		t.Fatal("HTTP timeout treated as handler completion")
	default:
	}
	late := httptest.NewRecorder()
	requests.ServeHTTP(late, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if late.Code != http.StatusServiceUnavailable {
		t.Fatalf("late request=%d", late.Code)
	}
	releaseHandler()
	if err := <-drained; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain=%v", err)
	}
	<-clientDone
}
