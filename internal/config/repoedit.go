package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// AddLayer declares a layer in the repository configuration at path,
// creating the file if it is not there.
//
// The edit is made on the parsed document rather than by re-marshalling
// a struct, so that everything else in the file survives: comments, key
// order, and any keys a later version of over might add. A layer
// repository's configuration is written by hand as often as by over,
// and over does not get to quietly discard what it does not understand.
func AddLayer(path, name, root string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	top, err := mappingRoot(&doc)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	layers := mapValue(top, "layers")
	if layers == nil {
		layers = &yaml.Node{Kind: yaml.MappingNode}
		top.Content = append(top.Content, scalar("layers"), layers)
	}
	if layers.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: layers is not a mapping", path)
	}
	if mapValue(layers, name) != nil {
		return fmt.Errorf("%s: layer %s is already declared", path, name)
	}
	entry := &yaml.Node{Kind: yaml.MappingNode}
	entry.Content = append(entry.Content, scalar("root"), scalar(root))
	layers.Content = append(layers.Content, scalar(name), entry)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return WriteFile(path, buf.Bytes(), 0o644)
}

// mappingRoot returns the top-level mapping of a document, filling in an
// empty document with one.
func mappingRoot(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind == 0 {
		*doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode}},
		}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, fmt.Errorf("not a single YAML document")
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("not a mapping")
	}
	return m, nil
}

// mapValue returns the value node for a key of a mapping, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// scalar returns a plain scalar node.
func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
