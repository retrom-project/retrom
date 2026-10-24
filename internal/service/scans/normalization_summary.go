package scans

import (
	"encoding/json"
	"path"
	"strings"
	"unicode/utf8"

	"retrom/internal/model"
)

// normalizationSummary is a bounded diagnostic projection, never persisted as game content or identity.
type normalizationSummary struct {
	Kind                   string                 `json:"kind"`
	ConverterVersion       int                    `json:"converterVersion"`
	SourceContentHash      string                 `json:"sourceContentHash"`
	ArchiveCount           int64                  `json:"archiveCount"`
	VerifiedEntries        int64                  `json:"verifiedEntries"`
	MetadataCorrections    int64                  `json:"metadataCorrections"`
	OmittedPhysicalEntries int64                  `json:"omittedPhysicalEntries"`
	FormatCounts           map[string]int64       `json:"formatCounts"`
	Warnings               []normalizationWarning `json:"warnings"`
	Details                []normalizationDetail  `json:"details,omitempty"`
}

type normalizationWarning struct {
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

type normalizationDetail struct {
	Code           string  `json:"code"`
	File           string  `json:"file"`
	Member         string  `json:"member"`
	SourceSHA256   *string `json:"sourceSha256,omitempty"`
	OutputSHA256   *string `json:"outputSha256,omitempty"`
	SourceSize     *int64  `json:"sourceSize,omitempty"`
	OutputSize     *int64  `json:"outputSize,omitempty"`
	Seed           *uint32 `json:"seed,omitempty"`
	SourceChecksum *uint32 `json:"sourceChecksum,omitempty"`
	OutputChecksum *uint32 `json:"outputChecksum,omitempty"`
}

// String keeps optional values readable with the service's default text log handler.
func (summary normalizationSummary) String() string {
	body, err := json.Marshal(summary)
	if err != nil {
		return "invalid normalization summary"
	}
	return string(body)
}

func validateNormalizationSummary(summary *normalizationSummary) error {
	if summary == nil {
		return nil
	}
	if !diagnosticCode(summary.Kind) || summary.ConverterVersion < 1 || !model.Hash(summary.SourceContentHash) ||
		len(summary.FormatCounts) > 8 || len(summary.Warnings) > 8 {
		return model.ErrInvalid
	}
	counts := []int64{
		summary.ArchiveCount, summary.VerifiedEntries,
		summary.MetadataCorrections, summary.OmittedPhysicalEntries,
	}
	for _, count := range counts {
		if count < 0 {
			return model.ErrInvalid
		}
	}
	for name, count := range summary.FormatCounts {
		if !diagnosticCode(name) || count < 0 {
			return model.ErrInvalid
		}
	}
	for _, warning := range summary.Warnings {
		if !diagnosticCode(warning.Code) || warning.Count < 0 {
			return model.ErrInvalid
		}
	}
	return validateNormalizationDetails(summary)
}

func validateNormalizationDetails(summary *normalizationSummary) error {
	if len(summary.Details) > 2048 {
		return model.ErrInvalid
	}
	for _, detail := range summary.Details {
		if !diagnosticCode(detail.Code) || !diagnosticPath(detail.File) || !diagnosticPath(detail.Member) {
			return model.ErrInvalid
		}
		for _, hash := range []*string{detail.SourceSHA256, detail.OutputSHA256} {
			if hash != nil && !model.Hash(*hash) {
				return model.ErrInvalid
			}
		}
		for _, size := range []*int64{detail.SourceSize, detail.OutputSize} {
			if size != nil && (*size < 0 || *size > model.MaximumMemberSize) {
				return model.ErrInvalid
			}
		}
	}
	body, err := json.Marshal(summary)
	if err != nil || len(body) > 4*1024*1024 {
		return model.ErrInvalid
	}
	return nil
}

func diagnosticPath(value string) bool {
	if value == "" || value == "." || len(value) > 4096 || !utf8.ValidString(value) ||
		strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") ||
		path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	if len(value) >= 2 && value[1] == ':' {
		return false
	}
	return !strings.ContainsFunc(value, func(character rune) bool { return character < 32 || character == 127 })
}

func diagnosticCode(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", character) {
			return false
		}
	}
	return true
}
