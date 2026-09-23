package profilemodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrUnknownKind  = errors.New("unknown profile kind")
	ErrInvalidModel = errors.New("invalid profile model")
	ErrInvalidData  = errors.New("invalid profile data")
)

const RPGMakerProject = "RPG_MAKER_PROJECT"

type Scope string

const (
	Review  Scope = "review"
	Game    Scope = "game"
	Variant Scope = "variant"
)

type envelope struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// Each owner has its own model for a kind. New content kinds register their
// model here without changing the database schema or the envelope format.
var models = map[Scope]map[string]func() any{
	Review:  {RPGMakerProject: func() any { return new(RPGReview) }},
	Game:    {RPGMakerProject: func() any { return new(RPGGame) }},
	Variant: {RPGMakerProject: func() any { return new(RPGVariant) }},
}

func Decode(scope Scope, value string) (any, error) {
	var wrapped envelope
	if err := json.Unmarshal([]byte(value), &wrapped); err != nil {
		return nil, fmt.Errorf("decode %s profile: %w", scope, err)
	}
	construct, ok := models[scope][wrapped.Kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s/%q", ErrUnknownKind, scope, wrapped.Kind)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(wrapped.Data), []byte{'{'}) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidData, scope)
	}
	result := construct()
	if err := json.Unmarshal(wrapped.Data, result); err != nil {
		return nil, fmt.Errorf("decode %s profile %q: %w", scope, wrapped.Kind, err)
	}
	return result, nil
}

func Encode(scope Scope, kind string, value any) (string, error) {
	construct, ok := models[scope][kind]
	if !ok || reflect.TypeOf(value) != reflect.TypeOf(construct()) {
		return "", fmt.Errorf("%w: %s/%q", ErrInvalidModel, scope, kind)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode %s profile data: %w", scope, err)
	}
	if !bytes.HasPrefix(data, []byte{'{'}) {
		return "", fmt.Errorf("%w: %s", ErrInvalidData, scope)
	}
	encoded, err := json.Marshal(envelope{Kind: kind, Data: data})
	if err != nil {
		return "", fmt.Errorf("encode %s profile: %w", scope, err)
	}
	return string(encoded), nil
}
