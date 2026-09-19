package architecture

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var errExternalSchema = errors.New("architecture: external schema loading is forbidden")

type noExternalSchemas struct{}

func (noExternalSchemas) Load(string) (any, error) {
	return nil, errExternalSchema
}

func compileOperationSchema(root string) (*jsonschema.Schema, error) {
	data, err := readInventoryFile(root, "quality/architecture/operations.schema.json")
	if err != nil {
		return nil, err
	}
	var document any
	if err := decodeStrictJSON(data, &document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(noExternalSchemas{})
	const location = "https://retrom.invalid/architecture/operation"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, fmt.Errorf("register operation schema: %w", err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile operation schema: %w", err)
	}
	return schema, nil
}

func validateOperationSchema(schema *jsonschema.Schema, data []byte) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode operation instance: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("validate operation instance: %w", err)
	}
	return nil
}

func loadContractFile(root, name string, target any) error {
	data, err := readInventoryFile(root, "quality/architecture/"+name)
	if err != nil {
		return err
	}
	return decodeStrictJSON(data, target)
}
