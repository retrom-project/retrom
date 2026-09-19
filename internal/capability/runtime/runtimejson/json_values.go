package runtimejson

func ValidJSONValue(value any, depth int, requireObject bool) bool {
	if depth > 8 {
		return false
	}
	if requireObject {
		_, ok := value.(map[string]any)
		if !ok {
			return false
		}
	}
	switch typed := value.(type) {
	case nil, bool, string, int64:
		return !requireObject
	case []any:
		if requireObject || len(typed) > 256 {
			return false
		}
		for _, item := range typed {
			if !ValidJSONValue(item, depth+1, false) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(typed) > 64 {
			return false
		}
		for _, item := range typed {
			if !ValidJSONValue(item, depth+1, false) {
				return false
			}
		}
		return true
	}
	return false
}

func ExactMap(value map[string]any, keys ...string) bool {
	if len(value) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, exists := value[key]; !exists {
			return false
		}
	}
	return true
}
