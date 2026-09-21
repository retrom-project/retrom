package uploads

import (
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"

	"retrom/internal/importing"
)

func validateCreateRequest(request CreateRequest) (int64, error) {
	if !validUploadShape(request) {
		return 0, ErrInvalid
	}
	if request.Purpose != "GENERAL" && request.SourceType == "FILES" &&
		(len(request.Files) != 1 || !isProjectUpload(request.Purpose, request.Files[0].RelativePath)) {
		return 0, ErrInvalid
	}
	seen := make(map[string]struct{}, len(request.Files))
	var total int64
	for _, file := range request.Files {
		if !validUploadFile(file) {
			return 0, ErrInvalid
		}
		if _, duplicate := seen[file.RelativePath]; duplicate {
			return 0, ErrInvalid
		}
		seen[file.RelativePath] = struct{}{}
		total += file.SizeBytes
		if total > 32<<30 {
			return 0, ErrInvalid
		}
	}
	return total, nil
}

func validUploadShape(request CreateRequest) bool {
	validPurpose := request.Purpose == "GENERAL" || request.Purpose == "PROJECT"
	validSource := request.SourceType == "FILES" || request.SourceType == "DIRECTORY"
	return validPurpose && validSource && len(request.Files) >= 1 && len(request.Files) <= 10_000
}

func validUploadFile(file FileDeclaration) bool {
	if file.ClientFileID == "" || file.SizeBytes < 0 || file.SizeBytes > 8<<30 ||
		len([]byte(file.RelativePath)) > 1024 {
		return false
	}
	_, err := importing.ValidateLogicalPath(file.RelativePath)
	return err == nil
}

func isProjectUpload(purpose, relativePath string) bool {
	extension := strings.ToLower(filepath.Ext(relativePath))
	return extension == ".zip" || extension == ".7z" ||
		purpose == "PROJECT" && extension == ".exe"
}

func parseRange(value string) (byteRange, error) {
	if !strings.HasPrefix(value, "bytes ") {
		return byteRange{}, ErrInvalid
	}
	span, totalText, ok := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	startText, endText, okSpan := strings.Cut(span, "-")
	start, startErr := strconv.ParseInt(startText, 10, 64)
	end, endErr := strconv.ParseInt(endText, 10, 64)
	total, totalErr := strconv.ParseInt(totalText, 10, 64)
	if !ok || !okSpan || startErr != nil || endErr != nil || totalErr != nil || start < 0 || end < start ||
		total <= end ||
		end-start+1 > PartSize {
		return byteRange{}, ErrInvalid
	}
	return byteRange{start: start, end: end, total: total}, nil
}

func parseDigest(value string) (string, error) {
	if !strings.HasPrefix(value, "sha-256=:") || !strings.HasSuffix(value, ":") {
		return "", ErrInvalid
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(value, "sha-256=:"), ":"))
	if err != nil || len(decoded) != 32 {
		return "", ErrInvalid
	}
	return hex.EncodeToString(decoded), nil
}
