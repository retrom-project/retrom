package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

var errNotReady = errors.New("backend is not ready")

// healthCLI checks this container's own readiness endpoint; the static image has no shell or HTTP client.
func healthCLI(ctx context.Context) error {
	_, port, err := net.SplitHostPort(environment("RETROM_HTTP_ADDR", "127.0.0.1:8080"))
	if err != nil {
		return fmt.Errorf("parse RETROM_HTTP_ADDR: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+net.JoinHostPort("127.0.0.1", port)+"/health/ready", http.NoBody)
	if err != nil {
		return fmt.Errorf("readiness request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("readiness check: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", errNotReady, response.StatusCode)
	}
	return nil
}
