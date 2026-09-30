// Package jobinput defines the execution envelope without interpreting domain inputs.
package jobinput

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrInvalid = errors.New("invalid job input envelope")

type Scope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Envelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Kind          string          `json:"kind"`
	Scope         Scope           `json:"scope"`
	ExecutionID   string          `json:"executionId"`
	Inputs        json.RawMessage `json:"inputs"`
}

func Encode(kind string, scope Scope, inputs any) ([]byte, error) {
	executionID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("create job execution ID: %w", err)
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("encode job inputs: %w", err)
	}
	return marshal(Envelope{
		SchemaVersion: 1, Kind: kind, Scope: scope,
		ExecutionID: executionID.String(), Inputs: encoded,
	})
}

func Decode(encoded []byte, kind string, scope Scope) (Envelope, error) {
	var input Envelope
	if err := json.Unmarshal(encoded, &input); err != nil {
		return Envelope{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if input.SchemaVersion != 1 || input.Kind != kind || input.Scope != scope ||
		len(input.Inputs) == 0 || !json.Valid(input.Inputs) || string(input.Inputs) == "null" {
		return Envelope{}, ErrInvalid
	}
	if _, err := uuid.Parse(input.ExecutionID); err != nil {
		return Envelope{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return input, nil
}

func Retry(previous []byte, kind string, scope Scope) ([]byte, error) {
	input, err := Decode(previous, kind, scope)
	if err != nil {
		return nil, err
	}
	executionID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("create retry execution ID: %w", err)
	}
	input.ExecutionID = executionID.String()
	return marshal(input)
}

func marshal(input Envelope) ([]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode job input envelope: %w", err)
	}
	return encoded, nil
}
