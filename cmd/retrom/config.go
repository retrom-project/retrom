package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"retrom/internal/model"
)

func sourceRoots() ([]model.Root, error) {
	result := make([]model.Root, 0)
	raw := os.Getenv("RETROM_SOURCE_ROOTS")
	if raw == "" {
		return result, nil
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("source roots configuration: %w", err)
	}
	var definitions []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &definitions); err != nil {
		return nil, fmt.Errorf("source root paths: %w", err)
	}
	seen := make(map[string]bool, len(definitions))
	for i, root := range definitions {
		if root.ID == "" || seen[root.ID] || !filepath.IsAbs(root.Path) {
			return nil, model.ErrInvalid
		}
		seen[root.ID] = true
		result[i].Path = root.Path
	}
	return result, nil
}

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
