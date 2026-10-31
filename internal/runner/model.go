// Package runner implements an offline Postman-compatible collection runner
// for the native Git local file layout.
package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/grafana/sobek"
)

// Collection is the subset of a Postman v2.1 collection needed to execute it.
type Collection struct {
	Info                    Info            `json:"info"`
	Item                    []Item          `json:"item"`
	Event                   []Event         `json:"event"`
	Variable                []Variable      `json:"variable"`
	Auth                    json.RawMessage `json:"auth"`
	ProtocolProfileBehavior map[string]any  `json:"protocolProfileBehavior"`
}

// Info is collection metadata.
type Info struct {
	ID   string `json:"_postman_id"`
	Name string `json:"name"`
}

// Item is a folder or request.
type Item struct {
	ID                      string            `json:"id"`
	Name                    string            `json:"name"`
	Item                    []Item            `json:"item"`
	Request                 *Request          `json:"request"`
	Event                   []Event           `json:"event"`
	Response                []json.RawMessage `json:"response"`
	Variable                []Variable        `json:"variable"`
	Auth                    json.RawMessage   `json:"auth"`
	ProtocolProfileBehavior map[string]any    `json:"protocolProfileBehavior"`
}

// Request is an HTTP request definition.
type Request struct {
	Method      string          `json:"method"`
	Header      []Header        `json:"header"`
	Body        *Body           `json:"body"`
	URL         URL             `json:"url"`
	Auth        json.RawMessage `json:"auth"`
	Description string          `json:"description"`
}

// Header is a request header.
type Header struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
}

// Param is a query or path parameter.
type Param struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
}

// URL supports Postman's string-or-object URL representation.
type URL struct {
	Raw    string
	Object *URLObject
}

// URLObject is the structured form of a Postman URL.
type URLObject struct {
	Raw      string   `json:"raw"`
	Protocol string   `json:"protocol"`
	Host     []string `json:"host"`
	Path     []string `json:"path"`
	Port     string   `json:"port"`
	Hash     string   `json:"hash"`
	Query    []Param  `json:"query"`
	Variable []Param  `json:"variable"`
	Auth     any      `json:"auth"`
}

// UnmarshalJSON accepts a URL string or object.
func (u *URL) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		return json.Unmarshal(trimmed, &u.Raw)
	}
	var obj URLObject
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return err
	}
	u.Object = &obj
	return nil
}

// Body is a request body.
type Body struct {
	Mode       string       `json:"mode"`
	Raw        string       `json:"raw"`
	URLEncoded []Param      `json:"urlencoded"`
	FormData   []FormParam  `json:"formdata"`
	GraphQL    *GraphQLBody `json:"graphql"`
	File       any          `json:"file"`
	Options    *BodyOptions `json:"options"`
	Disabled   bool         `json:"disabled"`
}

// BodyOptions carries raw body options.
type BodyOptions struct {
	Raw *RawOptions `json:"raw"`
}

// RawOptions carries the raw body language.
type RawOptions struct {
	Language string `json:"language"`
}

// FormParam is one multipart form-data entry.
type FormParam struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Type        string `json:"type"`
	Src         string `json:"src"`
	ContentType string `json:"contentType"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
}

// GraphQLBody is a GraphQL request body.
type GraphQLBody struct {
	Query         string `json:"query"`
	Variables     string `json:"variables"`
	OperationName string `json:"operationName"`
}

// Event is a pre-request or test script event.
type Event struct {
	Listen string `json:"listen"`
	Script Script `json:"script"`
}

// Script is a collection script.
type Script struct {
	Exec     ScriptCode      `json:"exec"`
	Type     string          `json:"type"`
	Packages json.RawMessage `json:"packages,omitempty"`
	Requests json.RawMessage `json:"requests,omitempty"`

	// Program is populated when scripts are enabled.
	Program *sobek.Program `json:"-"`
}

// ScriptCode accepts the array-or-string form of script exec.
type ScriptCode []string

// UnmarshalJSON accepts []string or a single string.
func (s *ScriptCode) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		var one string
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return err
		}
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(trimmed, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

// Lines returns the script body as lines.
func (s Script) Lines() []string { return []string(s.Exec) }

// Code returns the script body joined with newlines.
func (s Script) Code() string { return strings.Join(s.Exec, "\n") }

// Variable is a collection or environment variable.
type Variable struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
	Type        string `json:"type"`
}

func (u URL) String() string {
	if u.Object != nil {
		if u.Object.Raw != "" {
			return u.Object.Raw
		}
		var b strings.Builder
		if u.Object.Protocol != "" {
			b.WriteString(u.Object.Protocol)
			b.WriteString("://")
		}
		b.WriteString(strings.Join(u.Object.Host, "."))
		if u.Object.Port != "" {
			b.WriteString(":")
			b.WriteString(u.Object.Port)
		}
		for _, p := range u.Object.Path {
			b.WriteString("/")
			b.WriteString(p)
		}
		if len(u.Object.Query) > 0 {
			b.WriteString("?")
			for i, q := range u.Object.Query {
				if i > 0 {
					b.WriteString("&")
				}
				b.WriteString(q.Key)
				b.WriteString("=")
				b.WriteString(q.Value)
			}
		}
		if u.Object.Hash != "" {
			b.WriteString("#")
			b.WriteString(u.Object.Hash)
		}
		return b.String()
	}
	return u.Raw
}

func (b *Body) rawLanguage() string {
	if b == nil || b.Options == nil || b.Options.Raw == nil {
		return ""
	}
	return strings.ToLower(b.Options.Raw.Language)
}

func (b *Body) contentType() string {
	if b == nil {
		return ""
	}
	switch b.Mode {
	case "raw":
		switch b.rawLanguage() {
		case "json":
			return "application/json"
		case "xml":
			return "application/xml"
		case "html":
			return "text/html"
		case "javascript":
			return "application/javascript"
		default:
			return "text/plain"
		}
	case "urlencoded":
		return "application/x-www-form-urlencoded"
	case "graphql":
		return "application/json"
	}
	return ""
}

func (s ScriptCode) String() string { return strings.Join(s, "\n") }

func scriptEventType(listen string) string {
	switch listen {
	case "prerequest", "beforeRequest", "http:beforeRequest":
		return "prerequest"
	case "test", "afterResponse", "http:afterResponse":
		return "test"
	default:
		return listen
	}
}

func valueAsString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func parseAuth(raw json.RawMessage) (typ string, values map[string]string, err error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", nil, fmt.Errorf("parse auth: %w", err)
	}
	typ, _ = obj["type"].(string)
	values = map[string]string{}
	if typ == "" {
		return "", values, nil
	}
	switch entries := obj[typ].(type) {
	case []any:
		for _, entry := range entries {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			key, _ := m["key"].(string)
			values[key] = valueAsString(m["value"])
		}
	case map[string]any:
		for k, v := range entries {
			values[k] = valueAsString(v)
		}
	}
	return typ, values, nil
}
