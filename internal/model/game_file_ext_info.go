package model

import "strings"

// OriginGameInfo describes a private, offline attachment, never runtime content.
type OriginGameInfo struct {
	StorageKey string `json:"storage_key"`
	LogicalKey string `json:"logical_key"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
}

type GameFileConversionInfo struct {
	OriginInfo  *OriginGameInfo `json:"origin_info,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
}

type GameFileTabExtInfo struct {
	ConversionInfo *GameFileConversionInfo `json:"conversion_info,omitempty"`
}

func OriginStorageKey(gameID, fileID string) (string, error) {
	if !UUID(gameID) || !UUID(fileID) {
		return "", ErrInvalid
	}
	return "games/" + gameID + "/origins/" + fileID + "/original", nil
}

func ParseOriginStorageKey(key string) (string, string, bool) {
	parts := strings.Split(key, "/")
	if len(parts) != 5 || parts[0] != "games" || parts[2] != "origins" || parts[4] != "original" ||
		!UUID(parts[1]) || !UUID(parts[3]) {
		return "", "", false
	}
	return parts[1], parts[3], true
}

func ValidateGameFileExtInfo(gameID string, file GameFile) error {
	conversion := file.ExtInfo.ConversionInfo
	if conversion == nil {
		return nil
	}
	if conversion.Fingerprint != "" && !Hash(conversion.Fingerprint) {
		return ErrInvalid
	}
	origin := conversion.OriginInfo
	if origin == nil {
		return nil
	}
	key, err := OriginStorageKey(gameID, file.ID)
	if err != nil || origin.StorageKey != key || !SafeContentPath(origin.LogicalKey) ||
		origin.SizeBytes < 0 || origin.SizeBytes > MaximumPackageSize || !Hash(origin.SHA256) {
		return ErrInvalid
	}
	return nil
}
