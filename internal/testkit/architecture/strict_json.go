package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func decodeStrictJSON(data []byte, target any) error {
	if err := inspectJSONValue(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRegistry, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode registry: %w", err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return ErrInvalidRegistry
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return ErrInvalidRegistry
	}
	value, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read registry token: %w", err)
	}
	delimiter, ok := value.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return inspectJSONObject(decoder, depth)
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		return closeJSONContainer(decoder, ']')
	default:
		return ErrInvalidRegistry
	}
}

func inspectJSONObject(decoder *json.Decoder, depth int) error {
	keys := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("read registry key: %w", err)
		}
		key, ok := token.(string)
		if !ok || keys[key] {
			return ErrInvalidRegistry
		}
		keys[key] = true
		if err := inspectJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	return closeJSONContainer(decoder, '}')
}

func closeJSONContainer(decoder *json.Decoder, closing json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read registry container: %w", err)
	}
	if token != closing {
		return ErrInvalidRegistry
	}
	return nil
}
