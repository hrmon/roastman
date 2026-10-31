package convert

import (
	"regexp"
	"strings"

	"roastman/internal/jsonorder"
)

var (
	reItem     = regexp.MustCompile(`(?:^|/)item/\d+$`)
	reResponse = regexp.MustCompile(`/response/\d+$`)
	reEvent    = regexp.MustCompile(`/event/\d+$`)
	reScript   = regexp.MustCompile(`/event/\d+/script$`)
	reHeader   = regexp.MustCompile(`/header/\d+$`)
	reQuery    = regexp.MustCompile(`/query/\d+$`)
	reVariable = regexp.MustCompile(`/variable/\d+$`)
	reFormData = regexp.MustCompile(`/formdata/\d+$`)
	reURLEnc   = regexp.MustCompile(`/urlencoded/\d+$`)
)

// canonicalKeys returns the member order the converter itself emits for a
// given JSON Pointer. If known is false, callers should preserve the exact
// order in metadata because there is no safe default.
func canonicalKeys(path string, obj *jsonorder.Value) (order []string, known bool) {
	keys := obj.Keys()
	has := func(k string) bool { return obj.Has(k) }

	switch {
	case path == "":
		return []string{"info", "item", "event", "variable", "auth", "protocolProfileBehavior"}, true
	case path == "/info":
		return []string{"_postman_id", "name", "description", "schema", "_exporter_id"}, true
	case reItem.MatchString(path):
		if has("item") {
			return []string{"name", "item", "description", "event", "auth", "variable", "protocolProfileBehavior", "id"}, true
		}
		return []string{"name", "event", "protocolProfileBehavior", "request", "response", "description", "id"}, true
	case strings.HasSuffix(path, "/request"):
		return []string{"method", "header", "body", "url", "description", "auth", "proxy", "certificate"}, true
	case strings.HasSuffix(path, "/url"):
		return []string{"raw", "protocol", "host", "path", "port", "hash", "query", "variable", "auth"}, true
	case strings.HasSuffix(path, "/body"):
		return []string{"mode", "raw", "formdata", "urlencoded", "graphql", "file", "options", "disabled"}, true
	case strings.HasSuffix(path, "/auth"):
		typ := ""
		if v := obj.Get("type"); v != nil {
			typ = v.String()
		}
		return []string{"type", typ, "id", "name", "description", "credentials", "rules"}, true
	case reScript.MatchString(path):
		if has("packages") || has("requests") {
			return []string{"exec", "type", "packages", "requests", "language"}, true
		}
		// The v2 exporter commonly puts type before exec when there is no
		// package metadata, but some generators put exec first.
		return []string{"type", "exec", "language"}, true
	case reEvent.MatchString(path):
		return []string{"listen", "script"}, true
	case reHeader.MatchString(path) || reQuery.MatchString(path) || reVariable.MatchString(path):
		return []string{"key", "value", "description", "disabled", "type"}, true
	case reFormData.MatchString(path):
		if v := obj.Get("type"); v != nil && v.String() == "file" {
			return []string{"key", "type", "value", "src", "contentType", "description", "disabled", "uuid"}, true
		}
		return []string{"key", "value", "type", "contentType", "description", "disabled", "uuid"}, true
	case reURLEnc.MatchString(path):
		return []string{"key", "value", "description", "disabled"}, true
	case reResponse.MatchString(path):
		return []string{
			"id", "name", "originalRequest", "status", "code",
			"_postman_previewlanguage", "header", "cookie", "body",
			"responseTime", "timings", "_postman_previewtype",
		}, true
	}

	// Variables are stored as key/value objects in the same shape as headers.
	if has("key") && (has("value") || has("disabled") || has("type")) {
		return []string{"key", "value", "description", "disabled", "type", "secret", "source", "id"}, true
	}
	_ = keys
	return nil, false
}

// orderProjection filters a canonical order down to the keys actually present.
func orderProjection(order, keys []string) []string {
	if len(order) == 0 {
		return nil
	}
	present := make(map[string]bool, len(keys))
	for _, k := range keys {
		present[k] = true
	}
	out := make([]string, 0, len(keys))
	for _, k := range order {
		if present[k] {
			out = append(out, k)
		}
	}
	return out
}

// ApplyCanonicalOrders reorders every object the converter knows about. It
// runs before metadata overrides, so overrides stay exceptional and small.
func ApplyCanonicalOrders(root *jsonorder.Value) {
	root.Walk(func(path string, value *jsonorder.Value) {
		if value.Kind != jsonorder.Object {
			return
		}
		order, known := canonicalKeys(path, value)
		if !known {
			return
		}
		value.Reorder(order)
	})
}
