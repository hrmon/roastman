package convert

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"roastman/internal/jsonorder"
)

func documentNode(root *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
}

func mapNode(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: pairs}
}

func seqNode(items ...*yaml.Node) *yaml.Node {
	if items == nil {
		items = []*yaml.Node{}
	}
	return &yaml.Node{Kind: yaml.SequenceNode, Content: items}
}

func keyNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func litNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.LiteralStyle}
}

func boolNode(v bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
}

func intNode(v int64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(v, 10)}
}

func floatNode(v float64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(v, 'f', -1, 64)}
}

func nullNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
}

func addPair(m *yaml.Node, key string, value *yaml.Node) {
	if value == nil {
		return
	}
	m.Content = append(m.Content, keyNode(key), value)
}

func encodeYAML(root *yaml.Node) ([]byte, error) {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(documentNode(root)); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

func parseYAML(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty YAML document")
	}
	return doc.Content[0], nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalarString(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

func scalarBool(n *yaml.Node) bool {
	if n == nil || n.Kind != yaml.ScalarNode {
		return false
	}
	v, _ := strconv.ParseBool(strings.ToLower(n.Value))
	return v
}

func scalarFloat(n *yaml.Node) float64 {
	if n == nil || n.Kind != yaml.ScalarNode {
		return 0
	}
	v, _ := strconv.ParseFloat(n.Value, 64)
	return v
}

func sequenceItems(n *yaml.Node) []*yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

// localKV is one header/query/path/form entry in the native Git format.
type localKV struct {
	Key         *string
	Value       *string
	Description *string
	Disabled    *bool
	Type        *string
	ContentType *string
	Kind        string // "text" or "file" for form-data
	Src         *string
}

// parseKeyValues accepts both the map form and the list form used by native
// Git. The bool result reports whether the map form was used.
func parseKeyValues(n *yaml.Node) ([]localKV, bool) {
	if n == nil {
		return nil, false
	}
	if n.Kind == yaml.MappingNode {
		out := make([]localKV, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			v := scalarString(n.Content[i+1])
			out = append(out, localKV{Key: &k, Value: &v})
		}
		return out, true
	}
	if n.Kind != yaml.SequenceNode {
		return nil, false
	}
	out := make([]localKV, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		kv := localKV{}
		if v := mapValue(item, "key"); v != nil {
			s := scalarString(v)
			kv.Key = &s
		}
		if v := mapValue(item, "value"); v != nil {
			s := scalarString(v)
			kv.Value = &s
		}
		if v := mapValue(item, "description"); v != nil {
			s := scalarString(v)
			kv.Description = &s
		}
		if v := mapValue(item, "disabled"); v != nil {
			b := scalarBool(v)
			kv.Disabled = &b
		}
		if v := mapValue(item, "$type"); v != nil {
			s := scalarString(v)
			kv.Type = &s
		}
		if v := mapValue(item, "contentType"); v != nil {
			s := scalarString(v)
			kv.ContentType = &s
		}
		if v := mapValue(item, "type"); v != nil {
			kv.Kind = scalarString(v)
		}
		if v := mapValue(item, "src"); v != nil {
			s := scalarString(v)
			kv.Src = &s
		}
		out = append(out, kv)
	}
	return out, false
}

func kvNodes(items []localKV) *yaml.Node {
	arr := &yaml.Node{Kind: yaml.SequenceNode}
	for _, kv := range items {
		item := mapNode()
		if kv.Key != nil {
			addPair(item, "key", strNode(*kv.Key))
		}
		if kv.Kind != "" {
			addPair(item, "type", strNode(kv.Kind))
		}
		if kv.Value != nil {
			addPair(item, "value", strNode(*kv.Value))
		}
		if kv.Src != nil {
			addPair(item, "src", strNode(*kv.Src))
		}
		if kv.ContentType != nil {
			addPair(item, "contentType", strNode(*kv.ContentType))
		}
		if kv.Description != nil {
			addPair(item, "description", strNode(*kv.Description))
		}
		if kv.Disabled != nil {
			addPair(item, "disabled", boolNode(*kv.Disabled))
		}
		if kv.Type != nil {
			addPair(item, "$type", strNode(*kv.Type))
		}
		arr.Content = append(arr.Content, item)
	}
	return arr
}

// simpleKVMap reports whether a set of entries can be rendered compactly as a
// key/value mapping, and returns that mapping if so.
func simpleKVMap(items []localKV, allowNull bool) (*yaml.Node, bool) {
	if len(items) == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	m := mapNode()
	for _, kv := range items {
		if kv.Key == nil {
			return nil, false
		}
		if kv.Disabled != nil || kv.Description != nil {
			return nil, false
		}
		if kv.Type != nil || kv.Kind != "" || kv.Src != nil || kv.ContentType != nil {
			return nil, false
		}
		if seen[*kv.Key] {
			return nil, false
		}
		seen[*kv.Key] = true
		val := ""
		if kv.Value != nil {
			val = *kv.Value
		}
		addPair(m, *kv.Key, strNode(val))
	}
	return m, true
}

func genericYAML(v *jsonorder.Value) *yaml.Node {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case jsonorder.Null:
		return nullNode()
	case jsonorder.Bool:
		return boolNode(v.B)
	case jsonorder.Number:
		tag := "!!int"
		if strings.ContainsAny(v.Num, ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v.Num}
	case jsonorder.String:
		if strings.Contains(v.Str, "\n") {
			return litNode(v.Str)
		}
		return strNode(v.Str)
	case jsonorder.Array:
		seq := seqNode()
		for _, item := range v.Items {
			seq.Content = append(seq.Content, genericYAML(item))
		}
		return seq
	case jsonorder.Object:
		m := mapNode()
		for _, f := range v.Fields {
			addPair(m, f.Key, genericYAML(f.Value))
		}
		return m
	default:
		return nullNode()
	}
}

func yamlToJSON(n *yaml.Node) *jsonorder.Value {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		obj := jsonorder.NewObject()
		for i := 0; i+1 < len(n.Content); i += 2 {
			obj.Fields = append(obj.Fields, jsonorder.Field{Key: n.Content[i].Value, Value: yamlToJSON(n.Content[i+1])})
		}
		return obj
	case yaml.SequenceNode:
		arr := jsonorder.NewArray()
		for _, item := range n.Content {
			arr.Items = append(arr.Items, yamlToJSON(item))
		}
		return arr
	case yaml.AliasNode:
		return yamlToJSON(n.Alias)
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			return jsonorder.NewNull()
		case "!!bool":
			return jsonorder.NewBool(scalarBool(n))
		case "!!int", "!!float":
			return jsonorder.NewNumber(n.Value)
		default:
			return jsonorder.NewString(n.Value)
		}
	default:
		return jsonorder.NewNull()
	}
}
