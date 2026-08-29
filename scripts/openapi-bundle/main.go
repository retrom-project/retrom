package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"go.yaml.in/yaml/v3"
)

type documentCache struct {
	root      string
	documents map[string]*yaml.Node
}

func main() {
	input := flag.String("input", "api/openapi.yaml", "root OpenAPI document")
	output := flag.String("output", "", "bundled OpenAPI YAML output")
	flag.Parse()

	if *output == "" {
		fatal(errors.New("-output is required"))
	}
	if err := bundle(*input, *output); err != nil {
		fatal(err)
	}
}

func bundle(input, output string) error {
	absoluteInput, err := filepath.Abs(input)
	if err != nil {
		return fmt.Errorf("resolve OpenAPI input: %w", err)
	}
	cache := &documentCache{
		root:      filepath.Dir(absoluteInput),
		documents: make(map[string]*yaml.Node),
	}
	document, err := cache.load(absoluteInput)
	if err != nil {
		return err
	}
	resolved, err := cache.resolveRoot(document, absoluteInput)
	if err != nil {
		return err
	}
	if err := cache.validateSourceClosure(); err != nil {
		return err
	}
	contents, err := yaml.Marshal(resolved)
	if err != nil {
		return fmt.Errorf("marshal bundled OpenAPI: %w", err)
	}
	loader := openapi3.NewLoader()
	parsed, err := loader.LoadFromData(contents)
	if err != nil {
		return fmt.Errorf("load bundled OpenAPI: %w", err)
	}
	if err := parsed.Validate(context.Background()); err != nil {
		return fmt.Errorf("validate bundled OpenAPI: %w", err)
	}
	if err := writeAtomically(output, contents); err != nil {
		return fmt.Errorf("write bundled OpenAPI: %w", err)
	}
	return nil
}

func (cache *documentCache) validateSourceClosure() error {
	for _, directory := range []string{"domains", "components"} {
		base := filepath.Join(cache.root, directory)
		entries, err := os.ReadDir(base)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read OpenAPI source directory %s: %w", base, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
				continue
			}
			filename := filepath.Join(base, entry.Name())
			if _, loaded := cache.documents[filename]; !loaded {
				return fmt.Errorf("OpenAPI source is not reachable from root: %s", filename)
			}
		}
	}
	return nil
}

func (cache *documentCache) load(filename string) (*yaml.Node, error) {
	if document, ok := cache.documents[filename]; ok {
		return document, nil
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read OpenAPI source %s: %w", filename, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("parse OpenAPI source %s: %w", filename, err)
	}
	cache.documents[filename] = &document
	return &document, nil
}

func (cache *documentCache) resolveRoot(document *yaml.Node, filename string) (*yaml.Node, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("OpenAPI root must be one YAML document")
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("OpenAPI root must be an object")
	}
	result := cloneShallow(root)
	result.Content = make([]*yaml.Node, 0, len(root.Content))
	for index := 0; index < len(root.Content); index += 2 {
		key := cloneNode(root.Content[index])
		value := root.Content[index+1]
		var resolved *yaml.Node
		var err error
		switch key.Value {
		case "paths":
			resolved, err = cache.resolveContainer(value, filename, 1)
		case "components":
			resolved, err = cache.resolveContainer(value, filename, 2)
		default:
			resolved, err = cache.resolveRegular(value, filename)
		}
		if err != nil {
			return nil, err
		}
		result.Content = append(result.Content, key, resolved)
	}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{result}}, nil
}

func (cache *documentCache) resolveContainer(node *yaml.Node, filename string, levels int) (*yaml.Node, error) {
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("OpenAPI paths/components container must be an object")
	}
	result := cloneShallow(node)
	result.Content = make([]*yaml.Node, 0, len(node.Content))
	for index := 0; index < len(node.Content); index += 2 {
		key := cloneNode(node.Content[index])
		value := node.Content[index+1]
		var resolved *yaml.Node
		var err error
		if levels > 1 {
			resolved, err = cache.resolveContainer(value, filename, levels-1)
		} else {
			resolved, err = cache.resolveInline(value, filename)
		}
		if err != nil {
			return nil, err
		}
		result.Content = append(result.Content, key, resolved)
	}
	return result, nil
}

func (cache *documentCache) resolveInline(node *yaml.Node, filename string) (*yaml.Node, error) {
	reference, external, err := externalReference(node)
	if err != nil {
		return nil, err
	}
	if !external {
		return cache.resolveRegular(node, filename)
	}
	target, fragment, err := cache.reference(filename, reference)
	if err != nil {
		return nil, err
	}
	document, err := cache.load(target)
	if err != nil {
		return nil, err
	}
	selected, err := selectJSONPointer(document, fragment)
	if err != nil {
		return nil, fmt.Errorf("resolve OpenAPI reference %s: %w", reference, err)
	}
	return cache.resolveRegular(selected, target)
}

func (cache *documentCache) resolveRegular(node *yaml.Node, filename string) (*yaml.Node, error) {
	reference, external, err := externalReference(node)
	if err != nil {
		return nil, err
	}
	if external {
		_, fragment, err := cache.reference(filename, reference)
		if err != nil {
			return nil, err
		}
		result := cloneNode(node)
		result.Content[1].Value = "#" + fragment
		return result, nil
	}
	result := cloneShallow(node)
	result.Content = make([]*yaml.Node, 0, len(node.Content))
	for _, child := range node.Content {
		resolved, err := cache.resolveRegular(child, filename)
		if err != nil {
			return nil, err
		}
		result.Content = append(result.Content, resolved)
	}
	return result, nil
}

func externalReference(node *yaml.Node) (string, bool, error) {
	if node.Kind != yaml.MappingNode {
		return "", false, nil
	}
	for index := 0; index < len(node.Content); index += 2 {
		if node.Content[index].Value != "$ref" {
			continue
		}
		reference := node.Content[index+1].Value
		if strings.HasPrefix(reference, "#") {
			return "", false, nil
		}
		if len(node.Content) != 2 {
			return "", false, fmt.Errorf("external OpenAPI reference has sibling fields: %s", reference)
		}
		return reference, true, nil
	}
	return "", false, nil
}

func (cache *documentCache) reference(currentFile, reference string) (string, string, error) {
	parsed, err := url.Parse(reference)
	if err != nil {
		return "", "", fmt.Errorf("parse OpenAPI reference %s: %w", reference, err)
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.RawQuery != "" || filepath.IsAbs(parsed.Path) {
		return "", "", fmt.Errorf("remote or absolute OpenAPI reference is forbidden: %s", parsed.Redacted())
	}
	target, err := filepath.Abs(filepath.Join(filepath.Dir(currentFile), filepath.FromSlash(parsed.Path)))
	if err != nil {
		return "", "", fmt.Errorf("resolve OpenAPI reference %s: %w", reference, err)
	}
	relative, err := filepath.Rel(cache.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("OpenAPI reference leaves API root: %s", parsed.Redacted())
	}
	if parsed.Fragment == "" {
		return "", "", fmt.Errorf("OpenAPI reference has no JSON pointer: %s", parsed.Redacted())
	}
	return target, "/" + strings.TrimPrefix(parsed.Fragment, "/"), nil
}

func selectJSONPointer(document *yaml.Node, pointer string) (*yaml.Node, error) {
	current := document
	if current.Kind == yaml.DocumentNode && len(current.Content) == 1 {
		current = current.Content[0]
	}
	for _, encoded := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		if current.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("JSON pointer segment %q does not select an object", segment)
		}
		found := false
		for index := 0; index < len(current.Content); index += 2 {
			if current.Content[index].Value == segment {
				current = current.Content[index+1]
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("JSON pointer segment %q does not exist", segment)
		}
	}
	return current, nil
}

func cloneNode(node *yaml.Node) *yaml.Node {
	result := cloneShallow(node)
	result.Content = make([]*yaml.Node, len(node.Content))
	for index, child := range node.Content {
		result.Content[index] = cloneNode(child)
	}
	return result
}

func cloneShallow(node *yaml.Node) *yaml.Node {
	result := *node
	result.Content = nil
	result.Alias = nil
	return &result
}

func writeAtomically(output string, contents []byte) error {
	directory := filepath.Dir(output)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".openapi-bundle-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, output)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
