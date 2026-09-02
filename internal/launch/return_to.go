package launch

import (
	"strings"

	"github.com/google/uuid"
)

func validReturnTo(value, gameID string, saveStateID *string) bool {
	if strings.ContainsAny(value, "#%\\") {
		return false
	}
	if value == "/" || value == "/library" || value == "/saves" || value == "/games/"+gameID {
		return true
	}
	return validImmersiveReturnTo(value, gameID, saveStateID)
}

func validImmersiveReturnTo(value, gameID string, saveStateID *string) bool {
	if strings.HasPrefix(value, "/immersive/platforms/") {
		return saveStateID == nil && validImmersivePlatformReturn(value, gameID)
	}
	return validImmersiveLibraryReturn(value, gameID, saveStateID)
}

func validImmersivePlatformReturn(value, gameID string) bool {
	const prefix = "/immersive/platforms/"
	const separator = "?gameId="
	if !strings.HasPrefix(value, prefix) || strings.Count(value, "?") != 1 {
		return false
	}
	platformID, returnedGameID, found := strings.Cut(strings.TrimPrefix(value, prefix), separator)
	if !found || returnedGameID != gameID || len(platformID) == 0 || len(platformID) > 64 {
		return false
	}
	for _, character := range platformID {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func validImmersiveLibraryReturn(value, gameID string, saveStateID *string) bool {
	pathAndQuery := strings.Split(value, "?")
	if len(pathAndQuery) != 2 {
		return false
	}
	query, valid := parseImmersiveReturnQuery(pathAndQuery[1])
	if !valid || query["gameId"] != gameID {
		return false
	}
	switch pathAndQuery[0] {
	case "/immersive/library/all", "/immersive/library/recent":
		return saveStateID == nil && len(query) == 1
	case "/immersive/library/favorites":
		return saveStateID == nil && (len(query) == 1 ||
			len(query) == 2 && validCanonicalUUID(query["folderId"]))
	case "/immersive/library/saves":
		return saveStateID != nil && len(query) == 2 && query["saveStateId"] == *saveStateID
	default:
		return false
	}
}

func parseImmersiveReturnQuery(value string) (map[string]string, bool) {
	result := make(map[string]string)
	for _, entry := range strings.Split(value, "&") {
		name, entryValue, found := strings.Cut(entry, "=")
		if !found || name == "" || entryValue == "" {
			return nil, false
		}
		if _, duplicate := result[name]; duplicate {
			return nil, false
		}
		result[name] = entryValue
	}
	return result, true
}

func validCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}
