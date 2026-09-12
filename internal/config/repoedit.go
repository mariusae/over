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
	return editRepo(path, func(top *yaml.Node) error {
		layers := mapValue(top, "layers")
		if layers != nil && layers.Kind != yaml.MappingNode {
			return fmt.Errorf("layers is not a mapping")
		}
		if layers != nil && mapValue(layers, name) != nil {
			return fmt.Errorf("layer %s is already declared", name)
		}
		layer, err := layerEntry(top, name)
		if err != nil {
			return err
		}
		layer.Content = append(layer.Content, scalar("root"), scalar(root))
		return nil
	})
}

// AddRule adds patterns to a layer's track or ignore list in the
// repository configuration at path, creating the layer's entry if it
// has none. Patterns already there are skipped; the ones added are
// returned.
func AddRule(path, name, kind string, patterns []string) ([]string, error) {
	var added []string
	err := editRepo(path, func(top *yaml.Node) error {
		layer, err := layerEntry(top, name)
		if err != nil {
			return err
		}
		list := mapValue(layer, kind)
		if list == nil {
			list = &yaml.Node{Kind: yaml.SequenceNode}
			layer.Content = append(layer.Content, scalar(kind), list)
		}
		if list.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s of layer %s is not a list", kind, name)
		}
		for _, pattern := range patterns {
			if seqIndex(list, pattern) >= 0 {
				continue
			}
			list.Content = append(list.Content, scalar(pattern))
			added = append(added, pattern)
		}
		return nil
	})
	return added, err
}

// RemoveRule drops patterns from a layer's track and ignore lists. It
// returns the ones it removed.
func RemoveRule(path, name string, patterns []string) ([]string, error) {
	var removed []string
	err := editRepo(path, func(top *yaml.Node) error {
		layer, err := layerEntry(top, name)
		if err != nil {
			return err
		}
		for _, pattern := range patterns {
			for _, kind := range []string{"track", "ignore"} {
				list := mapValue(layer, kind)
				if list == nil || list.Kind != yaml.SequenceNode {
					continue
				}
				if i := seqIndex(list, pattern); i >= 0 {
					list.Content = append(list.Content[:i], list.Content[i+1:]...)
					removed = append(removed, pattern)
					break
				}
			}
		}
		return nil
	})
	return removed, err
}

// editRepo applies edit to the parsed document at path and writes it
// back. See [AddLayer] for why the document is edited rather than
// re-marshalled.
func editRepo(path string, edit func(top *yaml.Node) error) error {
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
	if err := edit(top); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
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

// layerEntry returns the mapping holding a layer's configuration,
// creating it, and the layers mapping above it, where they are missing.
func layerEntry(top *yaml.Node, name string) (*yaml.Node, error) {
	layers := mapValue(top, "layers")
	if layers == nil {
		layers = &yaml.Node{Kind: yaml.MappingNode}
		top.Content = append(top.Content, scalar("layers"), layers)
	}
	if layers.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("layers is not a mapping")
	}
	layer := mapValue(layers, name)
	if layer == nil {
		layer = &yaml.Node{Kind: yaml.MappingNode}
		layers.Content = append(layers.Content, scalar(name), layer)
	}
	if layer.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("layer %s is not a mapping", name)
	}
	return layer, nil
}

// seqIndex returns the position of a scalar in a sequence, or -1.
func seqIndex(list *yaml.Node, value string) int {
	for i, item := range list.Content {
		if item.Value == value {
			return i
		}
	}
	return -1
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
