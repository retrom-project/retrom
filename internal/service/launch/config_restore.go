package launch

import (
	"encoding/json"
	"net/url"
	"slices"

	model "retrom/internal/model/launch"
	runtimecontract "retrom/internal/model/runtimecontract"
)

func providerRestore(
	id string,
	restore model.ConfigRestore,
	target runtimecontract.Target,
) (json.RawMessage, bool, error) {
	if !restore.Required {
		return nil, false, nil
	}
	if !restore.Found || target.Checkpoint == nil || !slices.Contains(target.Checkpoint.ReadFormats, restore.Format) {
		return nil, false, model.ErrCredential
	}
	encoded, err := encodeProviderValue(map[string]any{
		"url": "/runtime/launches/" + id + "/state", "format": restore.Format,
		"sha256": restore.Digest, "sizeBytes": restore.Size,
	})
	return encoded, true, err
}

func providerNetplay(publicOrigin string, source model.ConfigSource) (json.RawMessage, string, error) {
	if source.NetplayID == nil {
		return nil, "SINGLE", nil
	}
	if source.NetplayRoom == nil || source.NetplayProfile == nil || source.NetplayPlayer == nil {
		return nil, "", model.ErrCredential
	}
	var profile map[string]any
	if err := json.Unmarshal([]byte(*source.NetplayProfile), &profile); err != nil {
		return nil, "", model.ErrCredential
	}
	socket, err := netplaySocketURL(publicOrigin, *source.NetplayRoom)
	if err != nil {
		return nil, "", err
	}
	encoded, err := encodeProviderValue(map[string]any{
		"roomId": *source.NetplayRoom, "sessionId": *source.NetplayID, "playerNo": *source.NetplayPlayer,
		"socketUrl": socket, "profile": profile,
	})
	return encoded, "NETPLAY", err
}

func netplaySocketURL(publicOrigin, roomID string) (string, error) {
	origin, err := url.Parse(publicOrigin)
	if err != nil || origin.Host == "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" {
		return "", model.ErrCredential
	}
	switch origin.Scheme {
	case "http":
		origin.Scheme = "ws"
	case "https":
		origin.Scheme = "wss"
	default:
		return "", model.ErrCredential
	}
	origin.Path = "/runtime/netplay/rooms/" + roomID + "/socket"
	return origin.String(), nil
}
