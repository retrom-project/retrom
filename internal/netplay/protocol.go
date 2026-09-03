package netplay

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxTextMessageBytes  = 64 << 10
	MaxInputMessageBytes = 4 << 10
	MaxWSMessageBytes    = 2 << 20
	StateHeaderBytes     = 52
)

var ErrProtocol = errors.New("NETPLAY_PROTOCOL_VIOLATION")

type ClientMessage struct {
	V                     int     `json:"v"`
	Type                  string  `json:"type"`
	SessionID             string  `json:"sessionId"`
	Epoch                 uint32  `json:"epoch"`
	Seq                   uint64  `json:"seq"`
	ProtocolVersion       string  `json:"protocolVersion,omitempty"`
	ProfileDigest         string  `json:"profileDigest,omitempty"`
	PlayerNo              int     `json:"playerNo,omitempty"`
	CredentialGeneration  int64   `json:"credentialGeneration,omitempty"`
	LastCanonicalFrame    int64   `json:"lastCanonicalFrame,omitempty"`
	LastServerSeq         uint64  `json:"lastServerSeq,omitempty"`
	ProviderID            string  `json:"providerId,omitempty"`
	TargetID              string  `json:"targetId,omitempty"`
	TargetContractSHA256  string  `json:"targetContractSha256,omitempty"`
	Frame                 int64   `json:"frame,omitempty"`
	Controls              []int16 `json:"controls,omitempty"`
	CoreDigest            string  `json:"coreDigest,omitempty"`
	Reason                string  `json:"reason,omitempty"`
	TransferID            string  `json:"transferId,omitempty"`
	NextFrame             int64   `json:"nextFrame,omitempty"`
	ByteLength            int     `json:"byteLength,omitempty"`
	StateSHA256           string  `json:"stateSha256,omitempty"`
	CoreSHA256            string  `json:"coreSha256,omitempty"`
	RecaptureMatched      bool    `json:"recaptureMatched,omitempty"`
	NativeLoadCompleted   bool    `json:"nativeLoadCompleted,omitempty"`
	HistoryAppliedThrough int64   `json:"historyAppliedThrough,omitempty"`
}

func DecodeClientMessage(contents []byte) (ClientMessage, error) {
	if len(contents) == 0 || len(contents) > MaxTextMessageBytes || !utf8.Valid(contents) {
		return ClientMessage{}, ErrProtocol
	}
	if err := validateJSONShape(contents, 8); err != nil {
		return ClientMessage{}, ErrProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var message ClientMessage
	if err := decoder.Decode(&message); err != nil {
		return ClientMessage{}, ErrProtocol
	}
	if message.V != 1 || message.SessionID == "" || message.Type == "" ||
		!validClientMessageFields(contents, message.Type) {
		return ClientMessage{}, ErrProtocol
	}
	return message, nil
}

func validClientMessageFields(contents []byte, messageType string) bool {
	base := []string{"v", "type", "sessionId", "epoch", "seq"}
	typeFields := map[string][]string{
		"HELLO": {
			"protocolVersion", "profileDigest", "playerNo", "credentialGeneration",
			"lastCanonicalFrame", "lastServerSeq",
		},
		"RUNTIME_READY":   {"providerId", "targetId", "targetContractSha256"},
		"INPUT":           {"frame", "playerNo", "controls"},
		"HASH":            {"frame", "coreDigest"},
		"PAUSED":          {},
		"STATE_META":      {"transferId", "nextFrame", "byteLength", "stateSha256", "coreSha256"},
		"STATE_READY":     {"transferId", "stateSha256", "coreSha256", "recaptureMatched"},
		"STATE_APPLIED":   {"transferId", "stateSha256", "coreSha256", "nativeLoadCompleted", "recaptureMatched"},
		"HISTORY_APPLIED": {"historyAppliedThrough"},
		"END_REQUEST":     {"reason"},
	}
	extra, known := typeFields[messageType]
	if !known {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil || len(fields) != len(base)+len(extra) {
		return false
	}
	for _, name := range append(base, extra...) {
		if _, exists := fields[name]; !exists {
			return false
		}
	}
	return true
}

func validateJSONShape(contents []byte, maxDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, 0, maxDepth); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrProtocol
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder, depth, maxDepth int) error {
	if depth > maxDepth {
		return ErrProtocol
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrProtocol
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		return validateJSONObject(decoder, depth, maxDepth)
	case '[':
		return validateJSONArray(decoder, depth, maxDepth)
	default:
		return ErrProtocol
	}
}

func validateJSONObject(decoder *json.Decoder, depth, maxDepth int) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok {
			return ErrProtocol
		}
		if _, duplicate := seen[key]; duplicate {
			return ErrProtocol
		}
		seen[key] = struct{}{}
		if err := validateJSONValue(decoder, depth+1, maxDepth); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return ErrProtocol
	}
	return nil
}

func validateJSONArray(decoder *json.Decoder, depth, maxDepth int) error {
	for decoder.More() {
		if err := validateJSONValue(decoder, depth+1, maxDepth); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim(']') {
		return ErrProtocol
	}
	return nil
}

type StateFrame struct {
	SessionID uuid.UUID
	Transfer  uuid.UUID
	Epoch     uint32
	NextFrame uint64
	Payload   []byte
}

func ParseStateFrame(contents []byte) (StateFrame, error) {
	if len(contents) < StateHeaderBytes || len(contents) > StateHeaderBytes+MaxStateBytes ||
		string(contents[:4]) != "RNS1" {
		return StateFrame{}, ErrProtocol
	}
	sessionID, sessionErr := uuid.FromBytes(contents[4:20])
	transferID, transferErr := uuid.FromBytes(contents[20:36])
	length := int(binary.BigEndian.Uint32(contents[48:52]))
	if sessionErr != nil || transferErr != nil || length < 1 || length > MaxStateBytes ||
		length != len(contents)-StateHeaderBytes {
		return StateFrame{}, ErrProtocol
	}
	payload := make([]byte, length)
	copy(payload, contents[StateHeaderBytes:])
	return StateFrame{
		SessionID: sessionID, Transfer: transferID, Epoch: binary.BigEndian.Uint32(contents[36:40]),
		NextFrame: binary.BigEndian.Uint64(contents[40:48]), Payload: payload,
	}, nil
}

func StateDigests(state []byte) (string, string, error) {
	if len(state) == 0 || len(state) > MaxStateBytes {
		return "", "", ErrProtocol
	}
	digest := sha256.Sum256(state)
	encoded := hex.EncodeToString(digest[:])
	return encoded, encoded, nil
}
