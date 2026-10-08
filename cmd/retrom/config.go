package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"retrom/internal/model"
)

func trustedProxies() ([]*net.IPNet, error) {
	result := make([]*net.IPNet, 0)
	raw := os.Getenv("RETROM_TRUSTED_PROXY_CIDRS")
	if raw == "" {
		return result, nil
	}
	for _, value := range strings.Split(raw, ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("trusted proxy CIDR: %w", err)
		}
		result = append(result, network)
	}
	return result, nil
}

func linkKey(directory string) ([]byte, error) {
	name := filepath.Join(directory, "account-link.key")
	value, err := os.ReadFile(name)
	if err == nil {
		if len(value) != 32 {
			return nil, model.ErrInvalid
		}
		return value, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read account link key: %w", err)
	}
	value = make([]byte, 32)
	if _, err = rand.Read(value); err != nil {
		return nil, fmt.Errorf("create account link key: %w", err)
	}
	if err = os.WriteFile(name, value, 0o600); err != nil {
		return nil, fmt.Errorf("persist account link key: %w", err)
	}
	return value, nil
}
