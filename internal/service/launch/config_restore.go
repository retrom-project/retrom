package launch

import (
	"encoding/json"
	"net/url"
	"slices"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/runtime/runtimebundle"
)

func providerRestore(id string, restore model.ConfigRestore, target runtimebundle.Target) (any, bool, error) {
	if !restore.Required {
		return nil, false, nil
	}
	if !restore.Found || target.Checkpoint == nil || !slices.Contains(target.Checkpoint.ReadFormats, restore.Format) {
		return nil, false, model.ErrCredential
	}
	return map[string]any{
		"url": "/runtime/launches/" + id + "/state", "format": restore.Format,
		"sha256": restore.Digest, "sizeBytes": restore.Size,
	}, true, nil
}

func providerNetplay(publicOrigin string, source model.ConfigSource) (any, string, error) {
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
	return map[string]any{
		"roomId": *source.NetplayRoom, "sessionId": *source.NetplayID, "playerNo": *source.NetplayPlayer,
		"socketUrl": socket, "profile": profile,
	}, "NETPLAY", nil
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
