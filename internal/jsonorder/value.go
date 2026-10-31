// Package jsonorder provides a small JSON document model that keeps object
// member order and number literals intact. Keeping the order is what lets a
// Postman collection survive a JSON -> local files -> JSON round trip without
// producing cosmetic diffs.
package jsonorder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Kind is the type of a Value.
type Kind int

const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Field is an object member.
type Field struct {
	Key   string
	Value *Value
}

// Value is an ordered JSON value.
type Value struct {
	Kind   Kind
	Fields []Field
	Items  []*Value
	Str    string
	Num    string
	B      bool
}

// NewObject builds an object from fields. Nil values are ignored.
func NewObject(fields ...Field) *Value {
	v := &Value{Kind: Object}
	for _, f := range fields {
		if f.Value != nil {
			v.Fields = append(v.Fields, f)
		}
	}
	return v
}

// NewArray builds an array from items. Nil items are ignored.
func NewArray(items ...*Value) *Value {
	v := &Value{Kind: Array}
	for _, item := range items {
		if item != nil {
			v.Items = append(v.Items, item)
		}
	}
	return v
}

// NewString returns a string value.
func NewString(s string) *Value { return &Value{Kind: String, Str: s} }

// NewNumber returns a number value from its literal representation.
func NewNumber(n string) *Value { return &Value{Kind: Number, Num: n} }

// NewInt returns an integer value.
func NewInt(n int64) *Value { return NewNumber(strconv.FormatInt(n, 10)) }

// NewBool returns a boolean value.
func NewBool(b bool) *Value { return &Value{Kind: Bool, B: b} }

// NewNull returns a null value.
func NewNull() *Value { return &Value{Kind: Null} }

// Parse decodes a JSON document while retaining object order and number
// literals.
func Parse(data []byte) (*Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	// Make sure there is no trailing garbage.
	if dec.More() {
		return nil, fmt.Errorf("unexpected data after JSON value")
	}
	if tok, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("unexpected token after JSON value: %v", tok)
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (*Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			v := &Value{Kind: Object}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("expected object key, got %v", keyTok)
				}
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				v.Fields = append(v.Fields, Field{Key: key, Value: val})
			}
			if _, err := dec.Token(); err != nil { // closing }
				return nil, err
			}
			return v, nil
		case '[':
			v := &Value{Kind: Array}
			for dec.More() {
				item, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				v.Items = append(v.Items, item)
			}
			if _, err := dec.Token(); err != nil { // closing ]
				return nil, err
			}
			return v, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		return NewString(t), nil
	case json.Number:
		return NewNumber(t.String()), nil
	case bool:
		return NewBool(t), nil
	case nil:
		return NewNull(), nil
	default:
		return nil, fmt.Errorf("unexpected token %T", tok)
	}
}

// Get returns the member with the given key, or nil.
func (v *Value) Get(key string) *Value {
	if v == nil || v.Kind != Object {
		return nil
	}
	for i := range v.Fields {
		if v.Fields[i].Key == key {
			return v.Fields[i].Value
		}
	}
	return nil
}

// Has reports whether the object has the given key.
func (v *Value) Has(key string) bool { return v.Get(key) != nil }

// Set replaces an existing member or appends a new one.
func (v *Value) Set(key string, value *Value) {
	if v == nil || v.Kind != Object {
		return
	}
	if value == nil {
		v.Delete(key)
		return
	}
	for i := range v.Fields {
		if v.Fields[i].Key == key {
			v.Fields[i].Value = value
			return
		}
	}
	v.Fields = append(v.Fields, Field{Key: key, Value: value})
}

// Delete removes a member.
func (v *Value) Delete(key string) {
	if v == nil || v.Kind != Object {
		return
	}
	out := v.Fields[:0]
	for _, f := range v.Fields {
		if f.Key != key {
			out = append(out, f)
		}
	}
	v.Fields = out
}

// Keys returns the object keys in order.
func (v *Value) Keys() []string {
	if v == nil || v.Kind != Object {
		return nil
	}
	keys := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		keys[i] = f.Key
	}
	return keys
}

// String returns the string value, or "" if this is not a string.
func (v *Value) String() string {
	if v == nil || v.Kind != String {
		return ""
	}
	return v.Str
}

// StringOK returns the string value and whether the value is a string.
func (v *Value) StringOK() (string, bool) {
	if v == nil || v.Kind != String {
		return "", false
	}
	return v.Str, true
}

// BoolOK returns the boolean value and whether the value is a bool.
func (v *Value) BoolOK() (bool, bool) {
	if v == nil || v.Kind != Bool {
		return false, false
	}
	return v.B, true
}

// IsNull reports whether the value is null.
func (v *Value) IsNull() bool { return v == nil || v.Kind == Null }

// Clone returns a deep copy.
func (v *Value) Clone() *Value {
	if v == nil {
		return nil
	}
	c := &Value{Kind: v.Kind, Str: v.Str, Num: v.Num, B: v.B}
	if v.Kind == Object {
		c.Fields = make([]Field, len(v.Fields))
		for i, f := range v.Fields {
			c.Fields[i] = Field{Key: f.Key, Value: f.Value.Clone()}
		}
	}
	if v.Kind == Array {
		c.Items = make([]*Value, len(v.Items))
		for i, item := range v.Items {
			c.Items[i] = item.Clone()
		}
	}
	return c
}

// MarshalCompact renders the value as compact JSON.
func (v *Value) MarshalCompact() []byte {
	var b bytes.Buffer
	v.writeCompact(&b)
	return b.Bytes()
}

func (v *Value) writeCompact(b *bytes.Buffer) {
	if v == nil {
		b.WriteString("null")
		return
	}
	switch v.Kind {
	case Null:
		b.WriteString("null")
	case Bool:
		if v.B {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case Number:
		b.WriteString(v.Num)
	case String:
		writeJSONString(b, v.Str)
	case Array:
		b.WriteByte('[')
		for i, item := range v.Items {
			if i > 0 {
				b.WriteByte(',')
			}
			item.writeCompact(b)
		}
		b.WriteByte(']')
	case Object:
		b.WriteByte('{')
		for i, f := range v.Fields {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, f.Key)
			b.WriteByte(':')
			f.Value.writeCompact(b)
		}
		b.WriteByte('}')
	}
}

func writeJSONString(b *bytes.Buffer, s string) {
	// encoding/json's encoder is used so escaping stays valid JSON. HTML
	// escaping is disabled because Postman exports don't escape <, > and &.
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	out := tmp.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	b.Write(out)
}

// Format describes how a JSON document was formatted.
type Format struct {
	// Indent is the whitespace used per nesting level. Empty means compact.
	Indent string `json:"indent,omitempty"`
	// Pretty is true when the input document contained line breaks.
	Pretty bool `json:"pretty"`
	// Newline is the line ending used by the input.
	Newline string `json:"newline,omitempty"`
	// FinalNewline reports whether the input ended with a newline.
	FinalNewline bool `json:"finalNewline,omitempty"`
	// InlinePaths lists non-empty containers that were written on one line in
	// an otherwise pretty-printed document.
	InlinePaths []string `json:"inlinePaths,omitempty"`
	// InlineCommaSpace records whether inline containers put a space after a
	// comma. InlineColonSpace does the same for a colon.
	InlineCommaSpace bool `json:"inlineCommaSpace,omitempty"`
	InlineColonSpace bool `json:"inlineColonSpace,omitempty"`
}

// DetectFormat inspects a JSON document's whitespace style.
func DetectFormat(data []byte) Format {
	f := Format{Newline: "\n"}
	if len(data) == 0 {
		return f
	}
	if bytes.Contains(data, []byte("\r\n")) {
		f.Newline = "\r\n"
	}
	f.FinalNewline = data[len(data)-1] == '\n'
	f.Pretty = bytes.Contains(data, []byte("\n"))
	if !f.Pretty {
		return f
	}
	// Use the second line's leading whitespace as the indent string.
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines[1:] {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			continue
		}
		i := 0
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		f.Indent = string(line[:i])
		break
	}
	layout := detectLayout(data)
	f.InlinePaths = layout.inlinePaths
	f.InlineCommaSpace = layout.commaSpace
	f.InlineColonSpace = layout.colonSpace
	return f
}

// MarshalFormatted renders the value using a detected format.
func (v *Value) MarshalFormatted(f Format) []byte {
	if !f.Pretty || f.Indent == "" {
		out := v.MarshalCompact()
		if f.FinalNewline {
			out = append(out, '\n')
		}
		return out
	}
	w := &formatter{
		format: f,
		inline: make(map[string]bool, len(f.InlinePaths)),
	}
	for _, path := range f.InlinePaths {
		w.inline[path] = true
	}
	w.writeValue(v, "", 0)
	out := w.b.Bytes()
	if f.Newline == "\r\n" {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	}
	if f.FinalNewline {
		out = append(out, []byte(f.Newline)...)
	}
	return out
}

type formatter struct {
	b      bytes.Buffer
	format Format
	inline map[string]bool
}

func (w *formatter) writeValue(v *Value, path string, depth int) {
	if v == nil {
		w.b.WriteString("null")
		return
	}
	switch v.Kind {
	case Null:
		w.b.WriteString("null")
	case Bool:
		if v.B {
			w.b.WriteString("true")
		} else {
			w.b.WriteString("false")
		}
	case Number:
		w.b.WriteString(v.Num)
	case String:
		writeJSONString(&w.b, v.Str)
	case Object:
		if len(v.Fields) == 0 {
			w.b.WriteString("{}")
			return
		}
		inline := w.inline[path]
		w.b.WriteByte('{')
		if !inline {
			w.newline(depth + 1)
		}
		for i, f := range v.Fields {
			if i > 0 {
				w.b.WriteByte(',')
				if inline {
					if w.format.InlineCommaSpace {
						w.b.WriteByte(' ')
					}
				} else {
					w.newline(depth + 1)
				}
			}
			writeJSONString(&w.b, f.Key)
			w.b.WriteByte(':')
			if inline {
				if w.format.InlineColonSpace {
					w.b.WriteByte(' ')
				}
			} else {
				w.b.WriteByte(' ')
			}
			w.writeValue(f.Value, path+"/"+PointerEscape(f.Key), depth+1)
		}
		if !inline {
			w.newline(depth)
		}
		w.b.WriteByte('}')
	case Array:
		if len(v.Items) == 0 {
			w.b.WriteString("[]")
			return
		}
		inline := w.inline[path]
		w.b.WriteByte('[')
		if !inline {
			w.newline(depth + 1)
		}
		for i, item := range v.Items {
			if i > 0 {
				w.b.WriteByte(',')
				if inline {
					if w.format.InlineCommaSpace {
						w.b.WriteByte(' ')
					}
				} else {
					w.newline(depth + 1)
				}
			}
			w.writeValue(item, path+"/"+strconv.Itoa(i), depth+1)
		}
		if !inline {
			w.newline(depth)
		}
		w.b.WriteByte(']')
	}
}

func (w *formatter) newline(depth int) {
	w.b.WriteByte('\n')
	for i := 0; i < depth; i++ {
		w.b.WriteString(w.format.Indent)
	}
}

// PointerEscape escapes one JSON Pointer path segment.
func PointerEscape(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

// Walk visits every object and array in the value, providing its JSON Pointer
// path. The callback is called before descending.
func (v *Value) Walk(fn func(path string, value *Value)) {
	walkPath("", v, fn)
}

func walkPath(path string, v *Value, fn func(string, *Value)) {
	if v == nil {
		return
	}
	fn(path, v)
	switch v.Kind {
	case Object:
		for _, f := range v.Fields {
			walkPath(path+"/"+PointerEscape(f.Key), f.Value, fn)
		}
	case Array:
		for i, item := range v.Items {
			walkPath(path+"/"+strconv.Itoa(i), item, fn)
		}
	}
}

// KeyOrders captures the member order of every object in a document.
func KeyOrders(v *Value) map[string][]string {
	out := map[string][]string{}
	v.Walk(func(path string, value *Value) {
		if value.Kind == Object {
			out[path] = value.Keys()
		}
	})
	return out
}

// ApplyKeyOrders reorders object members to match the provided order map.
// Keys unknown to the map keep their relative order after the known keys.
func ApplyKeyOrders(v *Value, orders map[string][]string) {
	v.Walk(func(path string, value *Value) {
		if value.Kind != Object {
			return
		}
		order, ok := orders[path]
		if !ok {
			return
		}
		value.reorder(order)
	})
}

// Reorder arranges object members according to order, leaving unknown keys in
// their relative order at the end.
func (v *Value) Reorder(order []string) { v.reorder(order) }

func (v *Value) reorder(order []string) {
	if len(v.Fields) < 2 {
		return
	}
	byKey := make(map[string]Field, len(v.Fields))
	for _, f := range v.Fields {
		byKey[f.Key] = f
	}
	out := make([]Field, 0, len(v.Fields))
	used := make(map[string]bool, len(order))
	for _, key := range order {
		if f, ok := byKey[key]; ok && !used[key] {
			out = append(out, f)
			used[key] = true
		}
	}
	for _, f := range v.Fields {
		if !used[f.Key] {
			out = append(out, f)
		}
	}
	v.Fields = out
}

// MergeExtras adds raw members to the object at path, without replacing
// members that already exist.
func MergeExtras(root *Value, extras map[string]map[string]json.RawMessage) error {
	if len(extras) == 0 {
		return nil
	}
	index := map[string]*Value{}
	root.Walk(func(path string, value *Value) {
		index[path] = value
	})
	for path, fields := range extras {
		obj := index[path]
		if obj == nil || obj.Kind != Object {
			continue
		}
		for key, raw := range fields {
			if obj.Has(key) {
				continue
			}
			v, err := Parse(raw)
			if err != nil {
				return fmt.Errorf("invalid extra %q at %s: %w", key, path, err)
			}
			obj.Set(key, v)
		}
	}
	return nil
}
