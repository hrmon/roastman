package runner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// VarValue is a variable and its enabled state.
type VarValue struct {
	Value   string
	Enabled bool
	Type    string
}

// VariableEntry is an ordered variable value used for export.
type VariableEntry struct {
	Key     string
	Value   string
	Enabled bool
	Type    string
}

// VarStore is a mutable variable scope.
type VarStore struct {
	values map[string]VarValue
	order  []string
}

func NewVarStore() *VarStore {
	return &VarStore{values: map[string]VarValue{}}
}

func (s *VarStore) Set(key, value string) { s.SetEnabled(key, value, true) }

func (s *VarStore) SetEnabled(key, value string, enabled bool) {
	s.SetTyped(key, value, enabled, "")
}

func (s *VarStore) SetTyped(key, value string, enabled bool, typ string) {
	if s == nil {
		return
	}
	if s.values == nil {
		s.values = map[string]VarValue{}
	}
	if _, exists := s.values[key]; !exists {
		s.order = append(s.order, key)
	}
	s.values[key] = VarValue{Value: value, Enabled: enabled, Type: typ}
}

func (s *VarStore) Get(key string) (string, bool) {
	if s == nil {
		return "", false
	}
	v, ok := s.values[key]
	if !ok || !v.Enabled {
		return "", false
	}
	return v.Value, true
}

func (s *VarStore) Has(key string) bool {
	_, ok := s.Get(key)
	return ok
}

func (s *VarStore) Unset(key string) {
	if s != nil {
		delete(s.values, key)
	}
}

func (s *VarStore) Clear() {
	if s != nil {
		s.values = map[string]VarValue{}
		s.order = nil
	}
}

// Entries returns all variables in insertion order, including disabled ones.
func (s *VarStore) Entries() []VariableEntry {
	if s == nil {
		return nil
	}
	out := make([]VariableEntry, 0, len(s.values))
	for _, key := range s.order {
		v, ok := s.values[key]
		if !ok {
			continue
		}
		out = append(out, VariableEntry{Key: key, Value: v.Value, Enabled: v.Enabled, Type: v.Type})
	}
	return out
}

func (s *VarStore) ToMap() map[string]string {
	out := map[string]string{}
	if s == nil {
		return out
	}
	for _, e := range s.Entries() {
		if e.Enabled {
			out[e.Key] = e.Value
		}
	}
	return out
}

func (s *VarStore) ForEach(fn func(string, string)) {
	if s == nil {
		return
	}
	seen := map[string]bool{}
	for _, e := range s.Entries() {
		seen[e.Key] = true
		if e.Enabled {
			fn(e.Key, e.Value)
		}
	}
	// Keep any values added directly before order tracking was introduced.
	for k, v := range s.values {
		if !seen[k] && v.Enabled {
			fn(k, v.Value)
		}
	}
}

// View is the variable resolution context for one request.
type View struct {
	Globals     *VarStore
	Collection  *VarStore
	Folder      map[string]string
	Environment *VarStore
	Data        map[string]string
	Local       *VarStore
}

func (v *View) Lookup(key string) (string, bool) {
	if v == nil {
		return "", false
	}
	if val, ok := v.Local.Get(key); ok {
		return val, true
	}
	if val, ok := v.Data[key]; ok {
		return val, true
	}
	if val, ok := v.Environment.Get(key); ok {
		return val, true
	}
	if val, ok := v.Folder[key]; ok {
		return val, true
	}
	if val, ok := v.Collection.Get(key); ok {
		return val, true
	}
	if val, ok := v.Globals.Get(key); ok {
		return val, true
	}
	return "", false
}

func (v *View) All() map[string]string {
	out := map[string]string{}
	if v == nil {
		return out
	}
	v.Globals.ForEach(func(k, val string) {
		if _, exists := out[k]; !exists {
			out[k] = val
		}
	})
	v.Collection.ForEach(func(k, val string) {
		if _, ok := v.Folder[k]; !ok {
			out[k] = val
		}
	})
	for k, val := range v.Folder {
		out[k] = val
	}
	v.Environment.ForEach(func(k, val string) { out[k] = val })
	for k, val := range v.Data {
		out[k] = val
	}
	v.Local.ForEach(func(k, val string) { out[k] = val })
	return out
}

var templateVar = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// Replace expands {{variables}}, recursively and with common dynamic
// variables supported by Postman.
func (v *View) Replace(input string) string {
	if input == "" || !strings.Contains(input, "{{") {
		return input
	}
	out := input
	for i := 0; i < 10; i++ {
		changed := false
		out = templateVar.ReplaceAllStringFunc(out, func(match string) string {
			name := strings.TrimSpace(match[2 : len(match)-2])
			if strings.HasPrefix(name, "$") {
				changed = true
				return dynamicValue(name)
			}
			if val, ok := v.Lookup(name); ok {
				changed = true
				return val
			}
			return match
		})
		if !changed {
			break
		}
	}
	return out
}

func dynamicValue(name string) string {
	switch name {
	case "$guid", "$randomUUID":
		return uuid()
	case "$timestamp":
		return fmt.Sprintf("%d", time.Now().Unix())
	case "$isoTimestamp":
		return time.Now().UTC().Format(time.RFC3339)
	case "$randomInt":
		n, _ := rand.Int(rand.Reader, big.NewInt(1000))
		return n.String()
	case "$randomAlphaNumeric":
		return randomAlpha(1)
	case "$randomPassword":
		return randomAlpha(12)
	default:
		return "{{" + name + "}}"
	}
}

func uuid() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}

func randomAlpha(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[v.Int64()]
	}
	return string(b)
}
