// Package multidisc contains the bounded, side-effect-free rules shared by
// multi-disc import, review attachment validation, and content identity.
package multidisc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	ContentKind       = "MULTI_DISC_M3U_V1"
	MinDiscs          = 2
	MaxDiscs          = 8
	MaxPlaylistBytes  = 65_536
	MaxReferenceBytes = 255
	MaxTotalBytes     = 1_073_741_824
)

type ErrorCode string

const (
	CodePlaylistInvalid ErrorCode = "MULTI_DISC_PLAYLIST_INVALID"
	CodeReferenceUnsafe ErrorCode = "MULTI_DISC_REFERENCE_UNSAFE"
	CodeCHDInvalid      ErrorCode = "MULTI_DISC_CHD_INVALID"
	CodeLimitExceeded   ErrorCode = "MULTI_DISC_LIMIT_EXCEEDED"
)

type ValidationError struct {
	Code   ErrorCode
	Reason string
}

func (validationError *ValidationError) Error() string {
	return string(validationError.Code) + ": " + validationError.Reason
}

func ErrorHasCode(err error, code ErrorCode) bool {
	var validationError *ValidationError
	return errors.As(err, &validationError) && validationError.Code == code
}

type Limits struct {
	MaxDiscs      int
	MaxTotalBytes int64
}

func DefaultLimits() Limits {
	return Limits{MaxDiscs: MaxDiscs, MaxTotalBytes: MaxTotalBytes}
}

type File struct {
	Basename     string
	LogicalName  string
	UploadFileID string
	BlobID       string
	BlobSHA256   string
	SizeBytes    int64
	Header       []byte
}

type EntryState string

const (
	EntryPresent EntryState = "PRESENT"
	EntryMissing EntryState = "MISSING"
)

type Entry struct {
	Ordinal             int
	SourceReference     string
	NormalizedReference string
	CanonicalName       string
	State               EntryState
	File                *File
}

type Result struct {
	Entries           []Entry
	CanonicalPlaylist []byte
	PresentTotalBytes int64
}

// Parse validates a single playlist and matches it only against files already
// frozen in the playlist's normalized directory. It performs no host I/O.
func Parse(playlist []byte, files []File, limits Limits) (Result, error) {
	exact, folded, err := indexFiles(files)
	if err != nil {
		return Result{}, err
	}
	playlist, err = validatePlaylistInput(playlist, limits)
	if err != nil {
		return Result{}, err
	}
	references, err := parseReferences(playlist, limits.MaxDiscs)
	if err != nil {
		return Result{}, err
	}
	result := Result{Entries: make([]Entry, 0, len(references))}
	canonical := make([]byte, 0, len(references)*13)
	for ordinal, reference := range references {
		normalized := ASCIIFold(reference)
		entry := Entry{
			Ordinal: ordinal, SourceReference: reference, NormalizedReference: normalized,
			CanonicalName: fmt.Sprintf("disc-%03d.chd", ordinal+1), State: EntryMissing,
		}
		matched, present, matchErr := matchFile(reference, normalized, exact, folded)
		if matchErr != nil {
			return Result{}, matchErr
		}
		if present {
			if matchErr := validateMatchedFile(matched, result.PresentTotalBytes, limits.MaxTotalBytes); matchErr != nil {
				return Result{}, matchErr
			}
			entry.State = EntryPresent
			entry.File = matched
			result.PresentTotalBytes += matched.SizeBytes
		}
		result.Entries = append(result.Entries, entry)
		canonical = append(canonical, entry.CanonicalName...)
		canonical = append(canonical, '\n')
	}
	result.CanonicalPlaylist = canonical
	return result, nil
}

func validatePlaylistInput(playlist []byte, limits Limits) ([]byte, error) {
	if limits.MaxDiscs < MinDiscs || limits.MaxDiscs > MaxDiscs || limits.MaxTotalBytes <= 0 {
		return nil, invalid(CodeLimitExceeded, "invalid frozen limits")
	}
	if len(playlist) > MaxPlaylistBytes {
		return nil, invalid(CodeLimitExceeded, "playlist exceeds byte limit")
	}
	if bytes.HasPrefix(playlist, []byte{0xef, 0xbb, 0xbf}) {
		playlist = playlist[3:]
	}
	if !utf8.Valid(playlist) {
		return nil, invalid(CodePlaylistInvalid, "playlist is not valid UTF-8")
	}
	return playlist, nil
}

func matchFile(
	reference, normalized string,
	exact map[string]*File,
	folded map[string][]*File,
) (*File, bool, error) {
	if matched := exact[reference]; matched != nil {
		return matched, true, nil
	}
	matches := folded[normalized]
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return nil, false, invalid(CodePlaylistInvalid, "ASCII case-fold match is ambiguous")
	}
}

func validateMatchedFile(file *File, currentTotal, maximumTotal int64) error {
	if file.SizeBytes <= 0 || len(file.Header) < 8 || !bytes.Equal(file.Header[:8], []byte("MComprHD")) {
		return invalid(CodeCHDInvalid, "referenced CHD is empty or has invalid magic")
	}
	if file.SizeBytes > maximumTotal-currentTotal {
		return invalid(CodeLimitExceeded, "referenced CHD total exceeds byte limit")
	}
	return nil
}

func parseReferences(playlist []byte, maxDiscs int) ([]string, error) {
	lines := bytes.Split(playlist, []byte{'\n'})
	references := make([]string, 0, maxDiscs)
	seen := make(map[string]struct{}, maxDiscs)
	for _, rawLine := range lines {
		if len(rawLine) > 0 && rawLine[len(rawLine)-1] == '\r' {
			rawLine = rawLine[:len(rawLine)-1]
		}
		if len(rawLine) == 0 || rawLine[0] == '#' {
			continue
		}
		reference, validationErr := validateReference(rawLine)
		if validationErr != nil {
			return nil, validationErr
		}
		normalized := ASCIIFold(reference)
		if _, duplicate := seen[normalized]; duplicate {
			return nil, invalid(CodePlaylistInvalid, "playlist contains a duplicate reference")
		}
		seen[normalized] = struct{}{}
		references = append(references, reference)
		if len(references) > maxDiscs {
			return nil, invalid(CodeLimitExceeded, "playlist has too many discs")
		}
	}
	if len(references) < MinDiscs {
		return nil, invalid(CodePlaylistInvalid, "playlist must contain at least two discs")
	}
	return references, nil
}

func validateReference(rawLine []byte) (string, error) {
	if len(rawLine) > MaxReferenceBytes {
		return "", invalid(CodeLimitExceeded, "playlist reference exceeds byte limit")
	}
	if !safeBasename(string(rawLine)) {
		return "", invalid(CodeReferenceUnsafe, "playlist reference is not a safe basename")
	}
	reference := string(rawLine)
	if !hasCHDExtension(reference) {
		return "", invalid(CodePlaylistInvalid, "playlist reference is not a CHD")
	}
	return reference, nil
}

func indexFiles(files []File) (map[string]*File, map[string][]*File, error) {
	exact := make(map[string]*File, len(files))
	folded := make(map[string][]*File, len(files))
	for index := range files {
		file := &files[index]
		if !safeBasename(file.Basename) {
			return nil, nil, invalid(CodePlaylistInvalid, "upload directory contains an unsafe basename")
		}
		if _, exists := exact[file.Basename]; exists {
			return nil, nil, invalid(CodePlaylistInvalid, "upload directory contains a duplicate basename")
		}
		exact[file.Basename] = file
		normalized := ASCIIFold(file.Basename)
		folded[normalized] = append(folded[normalized], file)
	}
	return exact, folded, nil
}

func safeBasename(value string) bool {
	raw := []byte(value)
	return len(raw) >= 1 && len(raw) <= MaxReferenceBytes && utf8.ValidString(value) &&
		!hasUnsafeReferenceByte(raw) && !hasASCIIBoundaryWhitespace(raw) &&
		!bytes.ContainsAny(raw, "/\\?#") && value != "." && value != ".." && !hasURIScheme(raw)
}

func hasUnsafeReferenceByte(value []byte) bool {
	for _, current := range value {
		if current < 0x20 || current == 0x7f {
			return true
		}
	}
	return false
}

func hasASCIIBoundaryWhitespace(value []byte) bool {
	return len(value) > 0 && (isASCIIWhitespace(value[0]) || isASCIIWhitespace(value[len(value)-1]))
}

func isASCIIWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\v' || value == '\f'
}

func hasURIScheme(value []byte) bool {
	colon := bytes.IndexByte(value, ':')
	if colon <= 0 || !asciiAlpha(value[0]) {
		return false
	}
	for _, current := range value[1:colon] {
		if !asciiAlpha(current) && (current < '0' || current > '9') && current != '+' && current != '-' && current != '.' {
			return false
		}
	}
	return true
}

func asciiAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func hasCHDExtension(value string) bool {
	return len(value) > 4 && ASCIIFold(value[len(value)-4:]) == ".chd"
}

func invalid(code ErrorCode, reason string) error {
	return &ValidationError{Code: code, Reason: reason}
}

func ASCIIFold(value string) string {
	folded := []byte(value)
	for index, current := range folded {
		if current >= 'A' && current <= 'Z' {
			folded[index] = current + ('a' - 'A')
		}
	}
	return string(folded)
}

func ExpectedSetDigest(entries []Entry) (string, error) {
	hash := sha256.New()
	_, _ = hash.Write([]byte("RETROM_MISSING_DISC_SET_V1\x00"))
	lastOrdinal := -1
	for _, entry := range entries {
		if entry.State != EntryMissing {
			continue
		}
		if entry.Ordinal <= lastOrdinal || entry.Ordinal < 0 || entry.Ordinal >= MaxDiscs ||
			entry.NormalizedReference == "" || entry.NormalizedReference != ASCIIFold(entry.SourceReference) {
			return "", invalid(CodePlaylistInvalid, "missing entries are not canonical and ordinal-sorted")
		}
		_, _ = hash.Write([]byte(strconv.Itoa(entry.Ordinal)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(entry.NormalizedReference))
		_, _ = hash.Write([]byte{0})
		lastOrdinal = entry.Ordinal
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ContentIdentity(discSHA256 []string) (string, error) {
	if len(discSHA256) < MinDiscs || len(discSHA256) > MaxDiscs {
		return "", invalid(CodePlaylistInvalid, "identity disc count is out of range")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("RETROM_CONTENT_IDENTITY_V2\x00" + ContentKind + "\x00"))
	_, _ = hash.Write([]byte(strconv.Itoa(len(discSHA256))))
	_, _ = hash.Write([]byte{0})
	for ordinal, digest := range discSHA256 {
		if len(digest) != sha256.Size*2 || digest != strings.ToLower(digest) {
			return "", invalid(CodePlaylistInvalid, "disc digest is not lowercase SHA-256")
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return "", invalid(CodePlaylistInvalid, "disc digest is not lowercase SHA-256")
		}
		_, _ = hash.Write([]byte(strconv.Itoa(ordinal)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(digest))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func GroupKey(normalizedDirname, playlistSHA256 string) (string, error) {
	if normalizedDirname == "" || len(playlistSHA256) != sha256.Size*2 ||
		playlistSHA256 != strings.ToLower(playlistSHA256) {
		return "", invalid(CodePlaylistInvalid, "group input is not canonical")
	}
	if _, err := hex.DecodeString(playlistSHA256); err != nil {
		return "", invalid(CodePlaylistInvalid, "group playlist digest is not SHA-256")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("RETROM_MULTIDISC_GROUP_V1\x00"))
	_, _ = hash.Write([]byte(normalizedDirname))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(playlistSHA256))
	_, _ = hash.Write([]byte{0})
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type AttachmentState string

const (
	AttachmentQueued          AttachmentState = "QUEUED"
	AttachmentRunning         AttachmentState = "RUNNING"
	AttachmentAccepted        AttachmentState = "ACCEPTED"
	AttachmentRejected        AttachmentState = "REJECTED"
	AttachmentFailedRetryable AttachmentState = "FAILED_RETRYABLE"
	AttachmentCancelled       AttachmentState = "CANCELLED"
)

func CanTransitionAttachment(from, to AttachmentState) bool {
	switch from {
	case AttachmentQueued:
		return to == AttachmentRunning || to == AttachmentCancelled
	case AttachmentRunning:
		return to == AttachmentAccepted || to == AttachmentRejected ||
			to == AttachmentFailedRetryable || to == AttachmentCancelled
	case AttachmentFailedRetryable:
		return to == AttachmentRunning || to == AttachmentCancelled
	case AttachmentAccepted, AttachmentRejected, AttachmentCancelled:
		return false
	default:
		return false
	}
}
