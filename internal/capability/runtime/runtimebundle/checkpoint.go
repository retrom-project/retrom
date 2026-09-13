package runtimebundle

func validCheckpointShape(checkpoint map[string]any) bool {
	semantics, present := checkpoint["semantics"]
	if !present {
		return exactMap(checkpoint, "writeFormat", "readFormats", "maxBytes")
	}
	return (semantics == "INSTANT" || semantics == "GAME_SAVE") &&
		exactMap(checkpoint, "writeFormat", "readFormats", "maxBytes", "semantics")
}

func validCheckpointSemantics(value string) bool {
	return value == "" || value == "INSTANT" || value == "GAME_SAVE"
}
