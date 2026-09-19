package httpapi

import (
	"encoding/json"

	"retrom/internal/foundation/cursor"
)

func cursorFilterDigest(filter any) string {
	encoded, _ := json.Marshal(filter)
	return cursor.FilterDigest(encoded)
}

func (server *Server) encodeCursor(payload cursor.Payload) (string, error) {
	token, err := server.cursors.Encode(payload, server.now().UnixMilli())
	if err != nil {
		return "", cursor.ErrInvalid
	}
	return token, nil
}

func (server *Server) decodeCursor(
	token string,
	operationID string,
	filterDigest string,
	sortCode string,
) (cursor.Payload, error) {
	payload, err := server.cursors.Decode(token, operationID, filterDigest, sortCode, server.now().UnixMilli())
	if err != nil {
		return cursor.Payload{}, cursor.ErrInvalid
	}
	return payload, nil
}
