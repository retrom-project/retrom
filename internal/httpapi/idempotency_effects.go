package httpapi

import "net/http"

// afterIdempotencyCommit registers a transient action, never a SQL operation or response payload.
func afterIdempotencyCommit(writer http.ResponseWriter, effect func()) {
	if response, ok := writer.(*bufferedResponse); ok {
		response.afterCommit = append(response.afterCommit, effect)
		return
	}
	effect()
}

func (response *bufferedResponse) runAfterCommit(committed bool) {
	if !committed {
		return
	}
	for _, effect := range response.afterCommit {
		effect()
	}
}
