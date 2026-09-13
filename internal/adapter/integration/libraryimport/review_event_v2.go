package libraryimport

import "encoding/json"

func marshalReviewEventV2(fields map[string]any) string {
	fields["schemaVersion"] = 2
	encoded, _ := json.Marshal(fields)
	return string(encoded)
}
