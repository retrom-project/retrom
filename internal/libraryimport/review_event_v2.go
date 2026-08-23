package libraryimport

import "encoding/json"

const emptyReviewEventV2 = `{"schemaVersion":2}`

func marshalReviewEventV2(fields map[string]any) string {
	fields["schemaVersion"] = 2
	encoded, _ := json.Marshal(fields)
	return string(encoded)
}
