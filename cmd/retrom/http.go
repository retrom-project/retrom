package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"retrom/internal/config"
)

const httpDrainTimeout = 15 * time.Second

func serveHTTP(lifetime context.Context, configuration config.Config, handler http.Handler,
	beginShutdown func(), progress *shutdownProgress,
) error {
	requests := &activeRequests{handler: handler}
	requestContext, cancelRequests := context.WithCancel(context.WithoutCancel(lifetime))
	defer cancelRequests()
	server := &http.Server{
		Addr: configuration.HTTPAddr, Handler: requests,
		BaseContext:       func(net.Listener) context.Context { return requestContext },
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 64 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() { slog.Info("retrom HTTP listening"); serveErrors <- server.ListenAndServe() }()
	var serveErr error
	select {
	case serveErr = <-serveErrors:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	case <-lifetime.Done():
		slog.Info("shutdown requested")
	}
	beginShutdown()
	progress.set("HTTP requests", requests.pending)
	return errors.Join(serveErr, drainHTTP(lifetime, server, requests, cancelRequests, httpDrainTimeout))
}

func drainHTTP(parent context.Context, server *http.Server, requests *activeRequests,
	cancel context.CancelFunc, timeout time.Duration,
) error {
	requests.stop()
	ctx, finish := context.WithTimeout(context.WithoutCancel(parent), timeout)
	defer finish()
	err := server.Shutdown(ctx)
	cancel()
	if err != nil {
		err = errors.Join(err, server.Close())
	}
	// Shutdown/Close and cancellation do not prove handlers have returned.
	// The independent process deadline supervises this join, including handlers ignoring cancellation.
	requests.wait.Wait()
	if err != nil {
		return fmt.Errorf("drain HTTP: %w", err)
	}
	return nil
}

type activeRequests struct {
	handler  http.Handler
	mutex    sync.Mutex
	stopping bool
	count    int
	wait     sync.WaitGroup
}

func (requests *activeRequests) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	requests.mutex.Lock()
	if requests.stopping {
		requests.mutex.Unlock()
		http.Error(writer, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	requests.count++
	requests.wait.Add(1)
	requests.mutex.Unlock()
	defer func() {
		requests.mutex.Lock()
		requests.count--
		requests.mutex.Unlock()
		requests.wait.Done()
	}()
	requests.handler.ServeHTTP(writer, request)
}

func (requests *activeRequests) stop() {
	requests.mutex.Lock()
	defer requests.mutex.Unlock()
	requests.stopping = true
}

func (requests *activeRequests) pending() []string {
	requests.mutex.Lock()
	defer requests.mutex.Unlock()
	return []string{fmt.Sprintf("%d active handlers", requests.count)}
}
