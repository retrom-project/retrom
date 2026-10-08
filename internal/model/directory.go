package model

import "encoding/json"

type Directory struct {
	ID            string   `json:"id"`
	PlatformID    string   `json:"platformId"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Description   string   `json:"description"`
	DefaultCoreID string   `json:"defaultCoreId"`
	CoreIDs       []string `json:"coreIds"`
	Enabled       bool     `json:"enabled"`
	Version       int64    `json:"version"`
	GameCount     int64    `json:"gameCount"`
}

type DirectoryInput struct {
	PlatformID    string   `json:"platformId"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Description   string   `json:"description"`
	DefaultCoreID string   `json:"defaultCoreId"`
	CoreIDs       []string `json:"coreIds"`
	Enabled       bool     `json:"enabled"`
	Version       int64    `json:"version"`
}

type (
	Platform struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	Core struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		PlatformIDs []string `json:"platformIds"`
		Fingerprint *string  `json:"fingerprint"`
	}
)

type Catalog struct {
	Providers []json.RawMessage `json:"providers"`
	Bindings  any               `json:"bindings"`
	Platforms []Platform        `json:"platforms"`
	Cores     []Core            `json:"cores"`
}
