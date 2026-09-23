package profilemodel

import "encoding/json"

type RPGReview struct {
	Generation               string          `json:"generation"`
	EvidenceFamily           string          `json:"evidenceFamily"`
	EvidenceGeneration       *string         `json:"evidenceGeneration"`
	EvidenceConfidence       string          `json:"evidenceConfidence"`
	EngineVersion            *string         `json:"engineVersion"`
	EntryHTMLPath            *string         `json:"entryHtmlPath"`
	FileCount                int             `json:"fileCount"`
	TotalBytes               int64           `json:"totalBytes"`
	ProjectFingerprint       string          `json:"projectFingerprint"`
	RequirementsSHA256       string          `json:"requirementsSha256"`
	Analysis                 json.RawMessage `json:"analysis"`
	SelfContainedOverride    int             `json:"selfContainedOverride"`
	ProviderID               string          `json:"providerId"`
	TargetID                 string          `json:"targetId"`
	DependencySnapshotSHA256 string          `json:"dependencySnapshotSha256"`
}

type RPGGame struct {
	EvidenceFamily     string          `json:"evidenceFamily"`
	EvidenceGeneration *string         `json:"evidenceGeneration"`
	EvidenceConfidence string          `json:"evidenceConfidence"`
	EngineVersion      *string         `json:"engineVersion"`
	EntryHTMLPath      *string         `json:"entryHtmlPath"`
	FileCount          int             `json:"fileCount"`
	TotalBytes         int64           `json:"totalBytes"`
	ProjectFingerprint string          `json:"projectFingerprint"`
	RequirementsSHA256 string          `json:"requirementsSha256"`
	Analysis           json.RawMessage `json:"analysis"`
}

type RPGVariant struct {
	Generation               string `json:"generation"`
	DependencySnapshotSHA256 string `json:"dependencySnapshotSha256"`
}

func (value *RPGReview) Game() *RPGGame {
	return &RPGGame{
		EvidenceFamily: value.EvidenceFamily, EvidenceGeneration: value.EvidenceGeneration,
		EvidenceConfidence: value.EvidenceConfidence, EngineVersion: value.EngineVersion,
		EntryHTMLPath: value.EntryHTMLPath, FileCount: value.FileCount, TotalBytes: value.TotalBytes,
		ProjectFingerprint: value.ProjectFingerprint, RequirementsSHA256: value.RequirementsSHA256,
		Analysis: value.Analysis,
	}
}
