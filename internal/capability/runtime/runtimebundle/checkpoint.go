package runtimebundle

import runtimejson "retrom/internal/capability/runtime/runtimejson"

func validCheckpointShape(checkpoint map[string]any) bool {
	semantics, present := checkpoint["semantics"]
	if !present {
		return runtimejson.ExactMap(checkpoint, "writeFormat", "readFormats", "maxBytes")
	}
	return (semantics == "INSTANT" || semantics == "GAME_SAVE") &&
		runtimejson.ExactMap(checkpoint, "writeFormat", "readFormats", "maxBytes", "semantics")
}

func validCheckpointSemantics(value string) bool {
	return value == "" || value == "INSTANT" || value == "GAME_SAVE"
}
