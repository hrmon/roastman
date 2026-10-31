package convert

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"roastman/internal/jsonorder"
)

const (
	metadataVersion  = 2
	metadataName     = ".roastman.json"
	resourcesDir     = ".resources"
	definitionName   = "definition.yaml"
	requestSuffix    = ".request.yaml"
	exampleSuffix    = ".example.yaml"
	requestSuffixYML = ".request.yml"
	exampleSuffixYML = ".example.yml"
	orderStep        = 1000
)

// Metadata stores the small amount of information that the native Git layout
// can't represent itself. It is what makes an unchanged round trip byte-for-
// byte identical. If it is deleted, conversion still works, but object member
// order and a few compatibility-only fields may differ.
type Metadata struct {
	Version int    `json:"version"`
	Scope   string `json:"scope,omitempty"`

	// Format holds global formatting. It is stored only in the collection
	// metadata file.
	Format *jsonorder.Format `json:"format,omitempty"`

	// InlinePaths holds single-line container paths relative to this scope.
	InlinePaths []string `json:"inlinePaths,omitempty"`

	// KeyOrders maps a JSON Pointer path in the v2 document to an index in
	// OrderSets. Objects that share a member layout share an entry.
	KeyOrders map[string]int `json:"keyOrders,omitempty"`
	OrderSets [][]string     `json:"orderSets,omitempty"`

	// Extras stores v2 fields that have no native representation. They are
	// merged back into the generated document when the field is still absent.
	Extras map[string]map[string]json.RawMessage `json:"extras,omitempty"`

	// URLShapes is a bit mask of the members that existed on a v2 URL object.
	// URL parsing can infer protocol/host/path fields that the original object
	// didn't store, so the shape is needed for an exact round trip.
	URLShapes map[string]int `json:"urlShapes,omitempty"`

	// FileField records whether a form-data file entry used "value" or "src"
	// in v2.
	FileFields map[string]string `json:"fileFields,omitempty"`

	// ScriptExec preserves exec arrays whose string elements contain line
	// breaks. Joining and re-splitting such arrays would otherwise change the
	// JSON representation.
	ScriptExec map[string][]string `json:"scriptExec,omitempty"`

	// MissingResponse records requests that had no "response" member at all.
	// An empty response array is different from an absent one.
	MissingResponse map[string]bool `json:"missingResponse,omitempty"`

	// MissingExec records script objects that had no "exec" member. The local
	// file still gets `code: ""` so that it is valid v3 YAML.
	MissingExec map[string]bool `json:"missingExec,omitempty"`
}

func newMetadata(format jsonorder.Format) *Metadata {
	return &Metadata{
		Version:         metadataVersion,
		Format:          &format,
		KeyOrders:       map[string]int{},
		Extras:          map[string]map[string]json.RawMessage{},
		URLShapes:       map[string]int{},
		FileFields:      map[string]string{},
		ScriptExec:      map[string][]string{},
		MissingResponse: map[string]bool{},
		MissingExec:     map[string]bool{},
	}
}

func loadMetadata(collectionDir string) (*Metadata, error) {
	return loadMetadataFile(filepath.Join(collectionDir, resourcesDir, metadataName))
}

func loadMetadataFile(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if m.KeyOrders == nil {
		m.KeyOrders = map[string]int{}
	}
	if m.Extras == nil {
		m.Extras = map[string]map[string]json.RawMessage{}
	}
	if m.URLShapes == nil {
		m.URLShapes = map[string]int{}
	}
	if m.FileFields == nil {
		m.FileFields = map[string]string{}
	}
	if m.ScriptExec == nil {
		m.ScriptExec = map[string][]string{}
	}
	if m.MissingResponse == nil {
		m.MissingResponse = map[string]bool{}
	}
	if m.MissingExec == nil {
		m.MissingExec = map[string]bool{}
	}
	return &m, nil
}

func saveMetadataFile(path string, m *Metadata) error {
	m.Version = metadataVersion
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// recordOrders stores the member order of every object in the document.
func (m *Metadata) recordOrders(root *jsonorder.Value) {
	sets := map[string]int{}
	m.KeyOrders = map[string]int{}
	m.OrderSets = nil
	root.Walk(func(path string, value *jsonorder.Value) {
		if value.Kind != jsonorder.Object || len(value.Fields) < 2 {
			return
		}
		keys := value.Keys()
		if order, known := canonicalKeys(path, value); known && equalStrings(orderProjection(order, keys), keys) {
			return
		}
		sig := strings.Join(keys, "\x00")
		idx, ok := sets[sig]
		if !ok {
			idx = len(m.OrderSets)
			sets[sig] = idx
			m.OrderSets = append(m.OrderSets, keys)
		}
		m.KeyOrders[path] = idx
	})
}

func (m *Metadata) ordersByPath() map[string][]string {
	out := make(map[string][]string, len(m.KeyOrders))
	for path, idx := range m.KeyOrders {
		if idx >= 0 && idx < len(m.OrderSets) {
			out[path] = m.OrderSets[idx]
		}
	}
	return out
}

func (m *Metadata) addExtra(path, key string, value *jsonorder.Value) {
	if value == nil {
		return
	}
	if m.Extras == nil {
		m.Extras = map[string]map[string]json.RawMessage{}
	}
	if m.Extras[path] == nil {
		m.Extras[path] = map[string]json.RawMessage{}
	}
	m.Extras[path][key] = append(json.RawMessage(nil), value.MarshalCompact()...)
}

func (m *Metadata) hasExtra(path, key string) bool {
	fields := m.Extras[path]
	if fields == nil {
		return false
	}
	_, ok := fields[key]
	return ok
}

// sanitizeFileName mirrors Postman's native Git naming rules.
func sanitizeFileName(s string) string {
	// Replace characters that are unsafe on common filesystems.
	s = reUnsafeChars.ReplaceAllString(s, "-")
	s = reControlChars.ReplaceAllString(s, "-")
	if reOnlyDots.MatchString(s) {
		s = "-"
	}
	lower := strings.ToLower(s)
	base := lower
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "con", "prn", "aux", "nul", "com0", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt0", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		s = "-" + s[len(base):]
	}
	s = strings.TrimRight(s, " ")
	if strings.HasSuffix(s, ".") {
		s = strings.TrimRight(s, ".") + "-"
	}
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = truncateUTF8(s, 64)
	// Postman runs the sanitizer a second time with an empty replacement,
	// which trims trailing dots and spaces.
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return "request"
	}
	return s
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xc0) == 0x80 {
		cut--
	}
	return s[:cut]
}

// uniqueName finds a case-insensitive unique name in used. The suffix is
// inserted before extension, matching Postman's behavior.
func uniqueName(used map[string]bool, stem, extension string) string {
	candidate := stem + extension
	if !used[strings.ToLower(candidate)] {
		used[strings.ToLower(candidate)] = true
		return candidate
	}
	for i := 1; i <= 1000; i++ {
		candidate = fmt.Sprintf("%s-%d%s", stem, i, extension)
		if !used[strings.ToLower(candidate)] {
			used[strings.ToLower(candidate)] = true
			return candidate
		}
	}
	// This cannot realistically be reached, but avoid returning a duplicate.
	candidate = fmt.Sprintf("%s-%d%s", stem, len(used)+1, extension)
	used[strings.ToLower(candidate)] = true
	return candidate
}

func (m *Metadata) deleteExtra(path, key string) {
	if m == nil || m.Extras[path] == nil {
		return
	}
	delete(m.Extras[path], key)
	if len(m.Extras[path]) == 0 {
		delete(m.Extras, path)
	}
}

var (
	reUnsafeChars  = regexp.MustCompile(`[/?<>\\:*|"]`)
	reControlChars = regexp.MustCompile(`[\x00-\x1f\x80-\x9f]`)
	reOnlyDots     = regexp.MustCompile(`^\.+$`)
)

var requestSuffixes = []string{requestSuffix, requestSuffixYML}
var exampleSuffixes = []string{exampleSuffix, exampleSuffixYML}
var definitionNames = []string{definitionName, "definition.yml"}

func definitionPath(dir string) (string, bool) {
	for _, name := range definitionNames {
		p := filepath.Join(dir, resourcesDir, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

func trimRequestSuffix(name string) (string, bool) {
	for _, suffix := range requestSuffixes {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return name[:len(name)-len(suffix)], true
		}
	}
	return "", false
}

func trimExampleSuffix(name string) (string, bool) {
	for _, suffix := range exampleSuffixes {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return name[:len(name)-len(suffix)], true
		}
	}
	return "", false
}

func metadataScopePath(scopePath, rel string) string {
	if scopePath == "" {
		return rel
	}
	if rel == "" {
		return scopePath
	}
	return scopePath + rel
}

func metadataRelativePath(scopePath, path string) (string, bool) {
	if scopePath == "" {
		return path, true
	}
	if path == scopePath {
		return "", true
	}
	prefix := scopePath + "/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, scopePath), true
	}
	return "", false
}

// mergeMetadata copies src into dst, prefixing every local path with scopePath.
func mergeMetadata(dst, src *Metadata, scopePath string) {
	if dst == nil || src == nil {
		return
	}
	setIndex := map[string]int{}
	for i, keys := range dst.OrderSets {
		setIndex[strings.Join(keys, "\x00")] = i
	}
	for path, idx := range src.KeyOrders {
		if idx < 0 || idx >= len(src.OrderSets) {
			continue
		}
		keys := src.OrderSets[idx]
		sig := strings.Join(keys, "\x00")
		newIdx, ok := setIndex[sig]
		if !ok {
			newIdx = len(dst.OrderSets)
			setIndex[sig] = newIdx
			dst.OrderSets = append(dst.OrderSets, append([]string{}, keys...))
		}
		dst.KeyOrders[metadataScopePath(scopePath, path)] = newIdx
	}
	for path, fields := range src.Extras {
		target := metadataScopePath(scopePath, path)
		if dst.Extras[target] == nil {
			dst.Extras[target] = map[string]json.RawMessage{}
		}
		for k, v := range fields {
			dst.Extras[target][k] = append(json.RawMessage(nil), v...)
		}
	}
	for path, mask := range src.URLShapes {
		dst.URLShapes[metadataScopePath(scopePath, path)] = mask
	}
	for path, field := range src.FileFields {
		dst.FileFields[metadataScopePath(scopePath, path)] = field
	}
	for path, lines := range src.ScriptExec {
		dst.ScriptExec[metadataScopePath(scopePath, path)] = append([]string{}, lines...)
	}
	for path, value := range src.MissingResponse {
		dst.MissingResponse[metadataScopePath(scopePath, path)] = value
	}
	for path, value := range src.MissingExec {
		dst.MissingExec[metadataScopePath(scopePath, path)] = value
	}
	for _, path := range src.InlinePaths {
		dst.InlinePaths = append(dst.InlinePaths, metadataScopePath(scopePath, path))
	}
	// v1 records inline paths in Format; v2 stores them in InlinePaths.
	if src.Format != nil {
		for _, path := range src.Format.InlinePaths {
			dst.InlinePaths = append(dst.InlinePaths, metadataScopePath(scopePath, path))
		}
	}
}

func (m *Metadata) formatValue() jsonorder.Format {
	if m != nil && m.Format != nil {
		return *m.Format
	}
	return jsonorder.Format{}
}
