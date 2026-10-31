package convert

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"roastman/internal/jsonorder"
)

// SplitOptions controls JSON -> native Git conversion.
type SplitOptions struct {
	OutputDir string
	Force     bool
}

// SplitResult describes a conversion.
type SplitResult struct {
	CollectionDir string
	Collection    string
	Requests      int
	Folders       int
}

// Split converts a Postman Collection v2.1 JSON document into the native Git
// v3 directory layout.
func Split(input []byte, opts SplitOptions) (*SplitResult, error) {
	root, err := jsonorder.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("parse collection JSON: %w", err)
	}
	if root.Kind != jsonorder.Object {
		return nil, errors.New("collection must be a JSON object")
	}
	info := root.Get("info")
	items := root.Get("item")
	if info == nil || info.Kind != jsonorder.Object {
		return nil, errors.New(`collection is missing the "info" object`)
	}
	if items == nil || items.Kind != jsonorder.Array {
		return nil, errors.New(`collection is missing the "item" array`)
	}

	name := valueString(info.Get("name"))
	if name == "" {
		name = "New Collection"
	}
	dirName := sanitizeFileName(name)

	out := opts.OutputDir
	if out == "" {
		out = dirName
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	out = resolveSplitOutput(out, dirName)
	cwd, _ := os.Getwd()
	if out == string(filepath.Separator) || out == cwd {
		return nil, fmt.Errorf("refusing to use the working directory or filesystem root as the output directory")
	}
	if err := prepareOutputDir(out, opts.Force); err != nil {
		return nil, err
	}

	meta := newMetadata(jsonorder.DetectFormat(input))
	meta.recordOrders(root)

	s := &splitter{root: root, meta: meta}
	if err := os.MkdirAll(filepath.Join(out, resourcesDir), 0o755); err != nil {
		return nil, err
	}

	def := s.collectionDefinition(info, name, filepath.Base(out))
	if err := writeYAML(filepath.Join(out, resourcesDir, definitionName), def); err != nil {
		return nil, err
	}

	used := map[string]bool{}
	if err := s.writeItems(out, items, "", used); err != nil {
		return nil, err
	}

	if err := writeScopedMetadata(out, meta, s.scopes); err != nil {
		return nil, err
	}

	return &SplitResult{
		CollectionDir: out,
		Collection:    name,
		Requests:      s.requests,
		Folders:       s.folders,
	}, nil
}

func prepareOutputDir(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0o755)
		}
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if !force {
		return fmt.Errorf("output directory %s already exists and is not empty (use --force to replace it)", dir)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func writeYAML(path string, root *yaml.Node) error {
	data, err := encodeYAML(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

type splitter struct {
	root     *jsonorder.Value
	meta     *Metadata
	scopes   []scopeInfo
	requests int
	folders  int
}

type scopeInfo struct {
	kind   string
	v2Path string
	dir    string
	stem   string
}

func (s *splitter) collectionDefinition(info *jsonorder.Value, name, dirName string) *yaml.Node {
	root := mapNode()
	addPair(root, "$kind", strNode("collection"))
	handledInfo := map[string]bool{}
	handledRoot := setOf("info", "item")

	if id := valueString(info.Get("_postman_id")); id != "" {
		addPair(root, "id", strNode(id))
		handledInfo["_postman_id"] = true
	}
	if strings.TrimSpace(name) != strings.TrimSpace(dirName) {
		addPair(root, "name", strNode(name))
	}
	handledInfo["name"] = true
	if desc := descriptionText(info.Get("description")); desc != "" {
		addPair(root, "description", strNode(desc))
		handledInfo["description"] = true
	}
	if vars := s.variablesDefinition(s.root.Get("variable"), "/variable"); vars != nil {
		addPair(root, "variables", vars)
		handledRoot["variable"] = true
	}
	if scripts := s.scriptsDefinition(s.root.Get("event"), true, "/event"); scripts != nil {
		addPair(root, "scripts", scripts)
		handledRoot["event"] = true
	}
	if auth := s.authDefinition(s.root.Get("auth"), true, "/auth"); auth != nil {
		addPair(root, "auth", auth)
		handledRoot["auth"] = true
	}

	s.collectExtras(info, "/info", handledInfo)
	s.collectExtras(s.root, "", handledRoot)
	return root
}

func (s *splitter) writeItems(dir string, items *jsonorder.Value, itemPath string, used map[string]bool) error {
	for i, item := range items.Items {
		if item.Kind != jsonorder.Object {
			continue
		}
		path := fmt.Sprintf("%s/item/%d", itemPath, i)
		name := valueString(item.Get("name"))
		children := item.Get("item")
		if children != nil && children.Kind == jsonorder.Array {
			if name == "" {
				name = "New Folder"
			}
			stem := sanitizeFileName(name)
			dirName := uniqueName(used, stem, "")
			folderDir := filepath.Join(dir, dirName)
			s.folders++
			if err := os.MkdirAll(filepath.Join(folderDir, resourcesDir), 0o755); err != nil {
				return err
			}
			s.scopes = append(s.scopes, scopeInfo{kind: "folder", v2Path: path, dir: folderDir})
			def := s.folderDefinition(item, name, dirName, i, path)
			if err := writeYAML(filepath.Join(folderDir, resourcesDir, definitionName), def); err != nil {
				return err
			}
			if err := s.writeItems(folderDir, children, path, map[string]bool{}); err != nil {
				return err
			}
			continue
		}

		if name == "" {
			name = "New Request"
		}
		stem := sanitizeFileName(name)
		fileName := uniqueName(used, stem, requestSuffix)
		fileStem := strings.TrimSuffix(fileName, requestSuffix)
		s.requests++

		req, err := s.requestFile(item, name, fileStem, path, i, filepath.Join(dir, fileStem))
		if err != nil {
			return err
		}
		if err := writeYAML(filepath.Join(dir, fileName), req); err != nil {
			return err
		}
		s.scopes = append(s.scopes, scopeInfo{kind: "request", v2Path: path, dir: dir, stem: fileStem})
	}
	return nil
}

func (s *splitter) folderDefinition(item *jsonorder.Value, name, dirName string, index int, itemPath string) *yaml.Node {
	root := mapNode()
	addPair(root, "$kind", strNode("collection"))
	handled := setOf("name", "item")

	if id := valueString(item.Get("id")); id != "" {
		addPair(root, "id", strNode(id))
		handled["id"] = true
	}
	if strings.TrimSpace(name) != strings.TrimSpace(dirName) {
		addPair(root, "name", strNode(name))
	}
	if desc := descriptionText(item.Get("description")); desc != "" {
		addPair(root, "description", strNode(desc))
		handled["description"] = true
	}
	if vars := s.variablesDefinition(item.Get("variable"), itemPath+"/variable"); vars != nil {
		addPair(root, "variables", vars)
		handled["variable"] = true
	}
	if scripts := s.scriptsDefinition(item.Get("event"), true, itemPath+"/event"); scripts != nil {
		addPair(root, "scripts", scripts)
		handled["event"] = true
	}
	if auth := s.authDefinition(item.Get("auth"), true, itemPath+"/auth"); auth != nil {
		addPair(root, "auth", auth)
		handled["auth"] = true
	}
	addPair(root, "order", intNode(int64((index+1)*orderStep)))

	s.collectExtras(item, itemPath, handled)
	return root
}

func (s *splitter) requestFile(item *jsonorder.Value, name, fileStem, itemPath string, index int, filePathBase string) (*yaml.Node, error) {
	req := item.Get("request")
	if req == nil {
		req = jsonorder.NewObject()
	}
	// A request can be represented as a bare URL string in some exports.
	var reqObj = req
	if req.Kind == jsonorder.String {
		reqObj = jsonorder.NewObject(jsonorder.Field{Key: "url", Value: req})
	}
	requestPath := itemPath + "/request"

	root := mapNode()
	addPair(root, "$kind", strNode("http-request"))
	if id := valueString(item.Get("id")); id != "" {
		addPair(root, "id", strNode(id))
	}
	if strings.TrimSpace(name) != strings.TrimSpace(fileStem) {
		addPair(root, "name", strNode(name))
	}
	if desc := descriptionText(reqObj.Get("description")); desc != "" {
		addPair(root, "description", strNode(desc))
	}

	urlValue := reqObj.Get("url")
	var urlStr string
	var query, pathVars *jsonorder.Value
	if urlValue != nil {
		switch urlValue.Kind {
		case jsonorder.String:
			urlStr = urlValue.Str
		case jsonorder.Object:
			s.meta.URLShapes[requestPath] = urlShapeMask(urlValue)
			urlStr = valueString(urlValue.Get("raw"))
			if urlStr == "" {
				urlStr = buildURLString(urlValue)
			}
			query = urlValue.Get("query")
			pathVars = urlValue.Get("variable")
			s.collectExtras(urlValue, requestPath+"/url", setOf("raw", "protocol", "host", "path", "port", "hash", "query", "variable", "auth"))
		}
	}
	if urlStr != "" || urlValue != nil {
		addPair(root, "url", strNode(urlStr))
	}
	if method := valueString(reqObj.Get("method")); method != "" {
		addPair(root, "method", strNode(method))
	}

	handledReq := setOf()
	if urlStr != "" || urlValue != nil {
		handledReq["url"] = true
	}
	if desc := descriptionText(reqObj.Get("description")); desc != "" {
		handledReq["description"] = true
	}
	if method := valueString(reqObj.Get("method")); method != "" {
		handledReq["method"] = true
	}
	if header := reqObj.Get("header"); header != nil && header.Kind == jsonorder.Array {
		kvs := parseV2KeyValues(header, requestPath+"/header", s.meta, "key", "value", "description", "disabled")
		addPair(root, "headers", keyValuesNode(kvs))
		handledReq["header"] = true
	}
	if query != nil && query.Kind == jsonorder.Array {
		kvs := parseV2KeyValues(query, requestPath+"/url/query", s.meta, "key", "value", "description", "disabled")
		addPair(root, "queryParams", keyValuesNode(kvs))
	}
	if pathVars != nil && pathVars.Kind == jsonorder.Array {
		kvs := parseV2KeyValues(pathVars, requestPath+"/url/variable", s.meta, "key", "value", "description", "disabled")
		addPair(root, "pathVariables", keyValuesNode(kvs))
	}

	if body := reqObj.Get("body"); body != nil && body.Kind == jsonorder.Object {
		if bodyNode := s.bodyNode(body, requestPath+"/body"); bodyNode != nil {
			addPair(root, "body", bodyNode)
			handledReq["body"] = true
		}
	}
	if auth := s.authDefinition(reqObj.Get("auth"), false, requestPath+"/auth"); auth != nil {
		addPair(root, "auth", auth)
		handledReq["auth"] = true
	}

	handledItem := setOf("name", "request")
	hasEvent := false
	if events := item.Get("event"); events != nil && events.Kind == jsonorder.Array {
		if scripts, mapped := s.scriptsDefinitionChecked(events, false, itemPath+"/event"); mapped && scripts != nil {
			addPair(root, "scripts", scripts)
			hasEvent = true
			handledItem["event"] = true
		} else if len(events.Items) > 0 {
			s.meta.addExtra(itemPath, "event", events)
		}
	}
	_ = hasEvent

	responses := item.Get("response")
	if responses == nil {
		s.meta.MissingResponse[requestPath] = true
	}
	examplesPath := ""
	if responses != nil && responses.Kind == jsonorder.Array && len(responses.Items) > 0 {
		if _, err := s.writeExamples(filePathBase, responses, itemPath+"/response"); err != nil {
			return nil, err
		}
		examplesPath = "./" + filepath.ToSlash(filepath.Join(resourcesDir, fileStem+".resources", "examples"))
	}
	if examplesPath != "" {
		addPair(root, "examples", strNode(examplesPath))
	}
	if responses != nil {
		handledItem["response"] = true
	}
	if id := valueString(item.Get("id")); id != "" {
		handledItem["id"] = true
	}
	addPair(root, "order", intNode(int64((index+1)*orderStep)))

	s.collectExtras(item, itemPath, handledItem)
	s.collectExtras(reqObj, requestPath, handledReq)
	return root, nil
}

func (s *splitter) bodyNode(body *jsonorder.Value, path string) *yaml.Node {
	mode := valueString(body.Get("mode"))
	node := mapNode()
	switch mode {
	case "raw":
		language := ""
		opts := body.Get("options")
		if opts != nil && opts.Kind == jsonorder.Object {
			if raw := opts.Get("raw"); raw != nil && raw.Kind == jsonorder.Object {
				language = strings.ToLower(valueString(raw.Get("language")))
			}
		}
		typ := "text"
		switch language {
		case "json", "xml", "html", "javascript":
			typ = language
		case "text":
			typ = "text"
		}
		addPair(node, "type", strNode(typ))
		addPair(node, "content", stringNode(valueString(body.Get("raw"))))
		// Non-text raw bodies have a canonical options block. Anything else
		// (including text options) is preserved through metadata.
		if opts != nil && typ != "text" && !isDefaultRawOptions(opts, typ) {
			s.meta.addExtra(path, "options", opts)
		} else if opts != nil && typ == "text" {
			s.meta.addExtra(path, "options", opts)
		}
	case "formdata":
		addPair(node, "type", strNode("formdata"))
		content := seqNode()
		if arr := body.Get("formdata"); arr != nil && arr.Kind == jsonorder.Array {
			for i, entry := range arr.Items {
				if entry.Kind != jsonorder.Object {
					continue
				}
				entryPath := fmt.Sprintf("%s/formdata/%d", path, i)
				item := mapNode()
				kind := valueString(entry.Get("type"))
				if kind == "" {
					kind = "text"
				}
				addPair(item, "type", strNode(kind))
				addPair(item, "key", strNode(valueString(entry.Get("key"))))
				if kind == "file" {
					src := ""
					fileField := ""
					if v := entry.Get("src"); v != nil {
						src = valueString(v)
						fileField = "src"
					} else if v := entry.Get("value"); v != nil {
						if v.Kind == jsonorder.String {
							src = v.Str
						}
						fileField = "value"
						if v.Kind == jsonorder.Null {
							s.meta.addExtra(entryPath, "value", v)
						}
					}
					if fileField != "" {
						s.meta.FileFields[entryPath] = fileField
					}
					addPair(item, "src", strNode(src))
				} else {
					addPair(item, "value", strNode(valueString(entry.Get("value"))))
				}
				if v := entry.Get("contentType"); v != nil {
					addPair(item, "contentType", strNode(valueString(v)))
				}
				if v := entry.Get("description"); v != nil {
					addPair(item, "description", strNode(valueString(v)))
				}
				if v := entry.Get("disabled"); v != nil {
					addPair(item, "disabled", boolNode(v.B))
				}
				content.Content = append(content.Content, item)
				s.collectExtras(entry, entryPath, setOf("key", "value", "type", "src", "contentType", "description", "disabled"))
			}
		}
		addPair(node, "content", content)
	case "urlencoded":
		addPair(node, "type", strNode("urlencoded"))
		content := seqNode()
		if arr := body.Get("urlencoded"); arr != nil && arr.Kind == jsonorder.Array {
			for i, entry := range arr.Items {
				if entry.Kind != jsonorder.Object {
					continue
				}
				entryPath := fmt.Sprintf("%s/urlencoded/%d", path, i)
				item := mapNode()
				addPair(item, "key", strNode(valueString(entry.Get("key"))))
				addPair(item, "value", strNode(valueString(entry.Get("value"))))
				if v := entry.Get("description"); v != nil {
					addPair(item, "description", strNode(valueString(v)))
				}
				if v := entry.Get("disabled"); v != nil {
					addPair(item, "disabled", boolNode(v.B))
				}
				content.Content = append(content.Content, item)
				s.collectExtras(entry, entryPath, setOf("key", "value", "description", "disabled"))
			}
		}
		addPair(node, "content", content)
	case "graphql":
		addPair(node, "type", strNode("graphql"))
		inner := mapNode()
		if g := body.Get("graphql"); g != nil && g.Kind == jsonorder.Object {
			addPair(inner, "query", stringNode(valueString(g.Get("query"))))
			if v := g.Get("variables"); v != nil && valueString(v) != "" {
				addPair(inner, "variables", stringNode(valueString(v)))
			}
			if v := g.Get("operationName"); v != nil && valueString(v) != "" {
				addPair(inner, "operationName", strNode(valueString(v)))
			}
			s.collectExtras(g, path+"/graphql", setOf("query", "variables", "operationName"))
		}
		addPair(node, "content", inner)
	case "file":
		addPair(node, "type", strNode("file"))
		inner := mapNode()
		if file := body.Get("file"); file != nil {
			src := ""
			if file.Kind == jsonorder.String {
				src = file.Str
			} else if file.Kind == jsonorder.Object {
				src = valueString(file.Get("src"))
				s.collectExtras(file, path+"/file", setOf("src"))
			}
			addPair(inner, "src", strNode(src))
		}
		addPair(node, "content", inner)
	default:
		return nil
	}
	s.collectExtras(body, path, setOf("mode", "raw", "formdata", "urlencoded", "graphql", "file", "options"))
	if opts := body.Get("options"); opts != nil {
		if mode == "raw" {
			language := ""
			if raw := opts.Get("raw"); raw != nil && raw.Kind == jsonorder.Object {
				language = strings.ToLower(valueString(raw.Get("language")))
			}
			if language != "text" && isDefaultRawOptions(opts, language) {
				s.meta.deleteExtra(path, "options")
			}
		}
	}
	return node
}

func (s *splitter) writeExamples(basePath string, responses *jsonorder.Value, responsePath string) (string, error) {
	examplesDir := filepath.Join(filepath.Dir(basePath), ".resources", filepath.Base(basePath)+".resources", "examples")
	if err := os.MkdirAll(examplesDir, 0o755); err != nil {
		return "", err
	}
	used := map[string]bool{}
	for i, resp := range responses.Items {
		if resp.Kind != jsonorder.Object {
			continue
		}
		name := valueString(resp.Get("name"))
		if name == "" {
			name = "example"
		}
		stem := sanitizeFileName(name)
		fileName := uniqueName(used, stem, exampleSuffix)
		ex := s.exampleNode(resp, strings.TrimSuffix(fileName, exampleSuffix), i, fmt.Sprintf("%s/%d", responsePath, i))
		if err := writeYAML(filepath.Join(examplesDir, fileName), ex); err != nil {
			return "", err
		}
	}
	return examplesDir, nil
}

func (s *splitter) exampleNode(resp *jsonorder.Value, stem string, index int, path string) *yaml.Node {
	root := mapNode()
	addPair(root, "$kind", strNode("http-example"))
	if id := valueString(resp.Get("id")); id != "" {
		addPair(root, "id", strNode(id))
	}
	name := valueString(resp.Get("name"))
	if name != "" && strings.TrimSpace(name) != strings.TrimSpace(stem) {
		addPair(root, "name", strNode(name))
	}

	original := resp.Get("originalRequest")
	reqNode := mapNode()
	if original != nil && original.Kind == jsonorder.Object {
		urlStr := ""
		switch u := original.Get("url"); {
		case u != nil && u.Kind == jsonorder.String:
			urlStr = u.Str
		case u != nil && u.Kind == jsonorder.Object:
			s.meta.URLShapes[path+"/originalRequest"] = urlShapeMask(u)
			urlStr = valueString(u.Get("raw"))
			if urlStr == "" {
				urlStr = buildURLString(u)
			}
		}
		addPair(reqNode, "url", strNode(urlStr))
		if method := valueString(original.Get("method")); method != "" {
			addPair(reqNode, "method", strNode(method))
		}
		if header := original.Get("header"); header != nil && header.Kind == jsonorder.Array {
			kvs := parseV2KeyValues(header, path+"/originalRequest/header", s.meta, "key", "value", "description", "disabled")
			addPair(reqNode, "headers", keyValuesNode(kvs))
		}
		if u := original.Get("url"); u != nil && u.Kind == jsonorder.Object {
			if q := u.Get("query"); q != nil && q.Kind == jsonorder.Array {
				kvs := parseV2KeyValues(q, path+"/originalRequest/url/query", s.meta, "key", "value", "description", "disabled")
				addPair(reqNode, "queryParams", keyValuesNode(kvs))
			}
			if pv := u.Get("variable"); pv != nil && pv.Kind == jsonorder.Array {
				kvs := parseV2KeyValues(pv, path+"/originalRequest/url/variable", s.meta, "key", "value", "description", "disabled")
				addPair(reqNode, "pathVariables", keyValuesNode(kvs))
			}
		}
		if body := original.Get("body"); body != nil && body.Kind == jsonorder.Object {
			addPair(reqNode, "body", s.bodyNode(body, path+"/originalRequest/body"))
		}
		s.collectExtras(original, path+"/originalRequest", setOf("url", "method", "header", "body"))
	}
	addPair(root, "request", reqNode)

	respNode := mapNode()
	if code := resp.Get("code"); code != nil {
		switch code.Kind {
		case jsonorder.Number:
			addPair(respNode, "statusCode", jsonToYAMLScalar(code))
		case jsonorder.String:
			addPair(respNode, "statusCode", strNode(code.Str))
		}
	}
	if status := valueString(resp.Get("status")); status != "" {
		addPair(respNode, "statusText", strNode(status))
	}
	if header := resp.Get("header"); header != nil && header.Kind == jsonorder.Array {
		kvs := parseV2KeyValues(header, path+"/header", s.meta, "key", "value", "description", "disabled")
		addPair(respNode, "headers", keyValuesNode(kvs))
	}
	bodyType := strings.ToLower(valueString(resp.Get("_postman_previewlanguage")))
	if bodyType == "" {
		bodyType = "text"
	}
	switch bodyType {
	case "json", "text", "xml", "html", "javascript":
	default:
		bodyType = "text"
	}
	bodyNode := mapNode()
	addPair(bodyNode, "type", strNode(bodyType))
	if body := resp.Get("body"); body != nil && body.Kind == jsonorder.String {
		addPair(bodyNode, "content", stringNode(body.Str))
	}
	addPair(respNode, "body", bodyNode)
	addPair(root, "response", respNode)
	addPair(root, "order", intNode(int64((index+1)*orderStep)))

	s.collectExtras(resp, path, setOf("id", "name", "originalRequest", "status", "code", "header", "body", "_postman_previewlanguage"))
	return root
}

func (s *splitter) variablesDefinition(vars *jsonorder.Value, path string) *yaml.Node {
	if vars == nil || vars.Kind != jsonorder.Array || len(vars.Items) == 0 {
		return nil
	}
	simple := true
	seen := map[string]bool{}
	for _, v := range vars.Items {
		if v.Kind != jsonorder.Object {
			simple = false
			break
		}
		key := valueString(v.Get("key"))
		if seen[key] {
			simple = false
			break
		}
		seen[key] = true
		for _, f := range v.Fields {
			switch f.Key {
			case "key", "value":
			default:
				simple = false
			}
		}
		if !simple {
			break
		}
	}
	if simple {
		m := mapNode()
		for i, v := range vars.Items {
			addPair(m, valueString(v.Get("key")), strNode(valueString(v.Get("value"))))
			s.collectExtras(v, fmt.Sprintf("%s/%d", path, i), setOf("key", "value"))
		}
		return m
	}
	seq := seqNode()
	for i, v := range vars.Items {
		if v.Kind != jsonorder.Object {
			continue
		}
		item := mapNode()
		addPair(item, "key", strNode(valueString(v.Get("key"))))
		if val := v.Get("value"); val != nil {
			addPair(item, "value", stringNode(valueString(val)))
		}
		if d := v.Get("description"); d != nil {
			addPair(item, "description", strNode(valueString(d)))
		}
		if d := v.Get("disabled"); d != nil {
			addPair(item, "disabled", boolNode(d.B))
		}
		seq.Content = append(seq.Content, item)
		s.collectExtras(v, fmt.Sprintf("%s/%d", path, i), setOf("key", "value", "description", "disabled"))
	}
	return seq
}

func (s *splitter) scriptsDefinition(events *jsonorder.Value, collectionScope bool, path string) *yaml.Node {
	scripts, _ := s.scriptsDefinitionChecked(events, collectionScope, path)
	return scripts
}

func (s *splitter) scriptsDefinitionChecked(events *jsonorder.Value, collectionScope bool, path string) (*yaml.Node, bool) {
	if events == nil || events.Kind != jsonorder.Array || len(events.Items) == 0 {
		return nil, true
	}
	seq := seqNode()
	for i, event := range events.Items {
		if event.Kind != jsonorder.Object {
			return nil, false
		}
		listen := valueString(event.Get("listen"))
		var typ string
		switch listen {
		case "prerequest":
			typ = "beforeRequest"
		case "test":
			typ = "afterResponse"
		default:
			return nil, false
		}
		if collectionScope {
			typ = "http:" + typ
		}
		script := event.Get("script")
		if script == nil || script.Kind != jsonorder.Object {
			return nil, false
		}
		item := mapNode()
		addPair(item, "type", strNode(typ))
		execValue := script.Get("exec")
		scriptPath := fmt.Sprintf("%s/%d/script", path, i)
		if execValue != nil {
			code := scriptCode(execValue)
			if execValue.Kind == jsonorder.Array {
				orig := make([]string, 0, len(execValue.Items))
				for _, line := range execValue.Items {
					orig = append(orig, valueString(line))
				}
				if !equalStrings(splitCode(code), orig) {
					s.meta.ScriptExec[scriptPath] = orig
				}
			}
			addPair(item, "code", stringNode(code))
		} else {
			addPair(item, "code", strNode(""))
			s.meta.MissingExec[scriptPath] = true
		}
		if language := valueString(script.Get("type")); language != "" {
			addPair(item, "language", strNode(language))
		}
		handledScript := setOf("exec", "type")
		for _, key := range []string{"packages", "requests"} {
			v := script.Get(key)
			if v != nil && v.Kind == jsonorder.Object && len(v.Fields) == 0 {
				addPair(item, key, genericYAML(v))
				handledScript[key] = true
			}
		}
		seq.Content = append(seq.Content, item)
		s.collectExtras(event, fmt.Sprintf("%s/%d", path, i), setOf("listen", "script"))
		s.collectExtras(script, fmt.Sprintf("%s/%d/script", path, i), handledScript)
	}
	return seq, true
}

func (s *splitter) authDefinition(auth *jsonorder.Value, collectionScope bool, path string) *yaml.Node {
	if auth == nil || auth.Kind != jsonorder.Object {
		return nil
	}
	typ := valueString(auth.Get("type"))
	authItem := mapNode()
	if collectionScope {
		addPair(authItem, "id", strNode(""))
	}
	addPair(authItem, "type", strNode(typ))
	if collectionScope && typ == "noauth" {
		addPair(authItem, "name", strNode("No Auth"))
	}
	if typ != "" && typ != "noauth" {
		if creds := auth.Get(typ); creds != nil && creds.Kind == jsonorder.Array {
			credNode := seqNode()
			simple := true
			seen := map[string]bool{}
			for i, c := range creds.Items {
				if c.Kind != jsonorder.Object {
					simple = false
					break
				}
				key := valueString(c.Get("key"))
				if seen[key] {
					simple = false
				}
				seen[key] = true
				for _, f := range c.Fields {
					switch f.Key {
					case "key", "value":
					default:
						simple = false
					}
				}
				if !simple {
					break
				}
				_ = i
			}
			if simple {
				m := mapNode()
				for i, c := range creds.Items {
					if c.Kind != jsonorder.Object {
						continue
					}
					val := ""
					if v := c.Get("value"); v != nil && v.Kind == jsonorder.String {
						val = v.Str
					}
					addPair(m, valueString(c.Get("key")), strNode(val))
					s.collectExtras(c, fmt.Sprintf("%s/%s/%d", path, typ, i), setOf("key", "value"))
				}
				addPair(authItem, "credentials", m)
			} else {
				for i, c := range creds.Items {
					if c.Kind != jsonorder.Object {
						continue
					}
					entry := mapNode()
					addPair(entry, "key", strNode(valueString(c.Get("key"))))
					addPair(entry, "value", strNode(valueString(c.Get("value"))))
					credNode.Content = append(credNode.Content, entry)
					s.collectExtras(c, fmt.Sprintf("%s/%s/%d", path, typ, i), setOf("key", "value"))
				}
				addPair(authItem, "credentials", credNode)
			}
			s.collectExtras(auth, path, setOf("type", typ))
		} else {
			s.collectExtras(auth, path, setOf("type"))
		}
	} else {
		s.collectExtras(auth, path, setOf("type"))
	}

	if collectionScope {
		return seqNode(authItem)
	}
	return authItem
}

func (s *splitter) collectExtras(v *jsonorder.Value, path string, handled map[string]bool) {
	if v == nil || v.Kind != jsonorder.Object {
		return
	}
	for _, f := range v.Fields {
		if !handled[f.Key] {
			s.meta.addExtra(path, f.Key, f.Value)
		}
	}
}

func parseV2KeyValues(v *jsonorder.Value, path string, meta *Metadata, fields ...string) []localKV {
	allowed := setOf(fields...)
	var out []localKV
	for i, item := range v.Items {
		if item.Kind != jsonorder.Object {
			continue
		}
		kv := localKV{}
		if key := item.Get("key"); key != nil {
			s := valueString(key)
			kv.Key = &s
		}
		if val := item.Get("value"); val != nil {
			s := valueString(val)
			kv.Value = &s
		}
		if d := item.Get("description"); d != nil {
			s := valueString(d)
			kv.Description = &s
		}
		if d := item.Get("disabled"); d != nil {
			b := d.B
			kv.Disabled = &b
		}
		if t := item.Get("type"); t != nil {
			// v2 parameter type metadata has no first-class native Git field.
			meta.addExtra(fmt.Sprintf("%s/%d", path, i), "type", t)
		}
		for _, f := range item.Fields {
			if !allowed[f.Key] && f.Key != "type" {
				meta.addExtra(fmt.Sprintf("%s/%d", path, i), f.Key, f.Value)
			}
		}
		out = append(out, kv)
	}
	return out
}

func keyValuesNode(items []localKV) *yaml.Node {
	if m, ok := simpleKVMap(items, true); ok {
		return m
	}
	return kvNodes(items)
}

func scriptCode(exec *jsonorder.Value) string {
	if exec == nil {
		return ""
	}
	if exec.Kind == jsonorder.String {
		return exec.Str
	}
	if exec.Kind != jsonorder.Array {
		return ""
	}
	lines := make([]string, 0, len(exec.Items))
	for _, line := range exec.Items {
		if line.Kind == jsonorder.String {
			lines = append(lines, line.Str)
		} else {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func descriptionText(v *jsonorder.Value) string {
	if v == nil {
		return ""
	}
	if v.Kind == jsonorder.String {
		return v.Str
	}
	if v.Kind == jsonorder.Object {
		if c := v.Get("content"); c != nil && c.Kind == jsonorder.String {
			return c.Str
		}
	}
	return ""
}

func valueString(v *jsonorder.Value) string {
	if v == nil {
		return ""
	}
	switch v.Kind {
	case jsonorder.String:
		return v.Str
	case jsonorder.Number:
		return v.Num
	case jsonorder.Bool:
		if v.B {
			return "true"
		}
		return "false"
	}
	return ""
}

func stringNode(s string) *yaml.Node {
	if strings.Contains(s, "\n") {
		return litNode(s)
	}
	return strNode(s)
}

func jsonToYAMLScalar(v *jsonorder.Value) *yaml.Node {
	switch v.Kind {
	case jsonorder.Number:
		if strings.ContainsAny(v.Num, ".eE") {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: v.Num}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: v.Num}
	case jsonorder.Bool:
		return boolNode(v.B)
	case jsonorder.Null:
		return nullNode()
	default:
		return strNode(valueString(v))
	}
}

func setOf(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func buildURLString(u *jsonorder.Value) string {
	raw := ""
	if proto := valueString(u.Get("protocol")); proto != "" {
		raw += proto + "://"
	}
	if host := u.Get("host"); host != nil && host.Kind == jsonorder.Array {
		parts := make([]string, 0, len(host.Items))
		for _, h := range host.Items {
			parts = append(parts, valueString(h))
		}
		raw += strings.Join(parts, ".")
	}
	if port := valueString(u.Get("port")); port != "" {
		raw += ":" + port
	}
	if path := u.Get("path"); path != nil && path.Kind == jsonorder.Array {
		for _, p := range path.Items {
			raw += "/" + valueString(p)
		}
	}
	if q := u.Get("query"); q != nil && q.Kind == jsonorder.Array && len(q.Items) > 0 {
		var pairs []string
		for _, item := range q.Items {
			if item.Kind != jsonorder.Object {
				continue
			}
			if d := item.Get("disabled"); d != nil && d.B {
				continue
			}
			pairs = append(pairs, valueString(item.Get("key"))+"="+valueString(item.Get("value")))
		}
		if len(pairs) > 0 {
			raw += "?" + strings.Join(pairs, "&")
		}
	}
	if hash := valueString(u.Get("hash")); hash != "" {
		raw += "#" + hash
	}
	return raw
}

func isDefaultRawOptions(opts *jsonorder.Value, language string) bool {
	if opts == nil || opts.Kind != jsonorder.Object || language == "" || language == "text" {
		return false
	}
	if len(opts.Fields) != 1 || opts.Fields[0].Key != "raw" {
		return false
	}
	raw := opts.Fields[0].Value
	if raw == nil || raw.Kind != jsonorder.Object || len(raw.Fields) != 1 || raw.Fields[0].Key != "language" {
		return false
	}
	return strings.EqualFold(raw.Fields[0].Value.String(), language)
}

func splitCode(code string) []string {
	if code == "" {
		return nil
	}
	return strings.Split(code, "\n")
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// resolveSplitOutput lets -o point at either a collection directory or a
// native Git workspace (a workspace root containing postman/collections, or
// the postman directory itself).
func resolveSplitOutput(out, collectionDirName string) string {
	info, err := os.Stat(out)
	if err != nil {
		switch filepath.Base(out) {
		case "postman":
			return filepath.Join(out, "collections", collectionDirName)
		case "collections":
			return filepath.Join(out, collectionDirName)
		default:
			return out
		}
	}
	if !info.IsDir() {
		return out
	}
	if _, err := os.Stat(filepath.Join(out, resourcesDir, definitionName)); err == nil {
		return out
	}
	workspaceCollections := filepath.Join(out, "postman", "collections")
	topCollections := filepath.Join(out, "collections")
	_, workspaceErr := os.Stat(workspaceCollections)
	hasWorkspaceDir := workspaceErr == nil
	hasWorkspace := dirHasCollections(workspaceCollections)
	hasTop := dirHasCollections(topCollections)
	if hasWorkspace {
		return filepath.Join(workspaceCollections, collectionDirName)
	}
	if hasTop {
		return filepath.Join(topCollections, collectionDirName)
	}
	if hasWorkspaceDir {
		return filepath.Join(workspaceCollections, collectionDirName)
	}
	if filepath.Base(out) == "collections" {
		return filepath.Join(out, collectionDirName)
	}
	return out
}

const (
	urlFieldRaw = 1 << iota
	urlFieldProtocol
	urlFieldHost
	urlFieldPath
	urlFieldPort
	urlFieldHash
	urlFieldQuery
	urlFieldVariable
	urlFieldAuth
)

func urlShapeMask(u *jsonorder.Value) int {
	mask := 0
	fields := map[string]int{
		"raw": urlFieldRaw, "protocol": urlFieldProtocol, "host": urlFieldHost,
		"path": urlFieldPath, "port": urlFieldPort, "hash": urlFieldHash,
		"query": urlFieldQuery, "variable": urlFieldVariable, "auth": urlFieldAuth,
	}
	for _, f := range u.Fields {
		mask |= fields[f.Key]
	}
	return mask
}

func writeScopedMetadata(collectionDir string, all *Metadata, scopes []scopeInfo) error {
	root := newMetadata(all.formatValue())
	root.Scope = "collection"
	if root.Format != nil {
		root.Format.InlinePaths = nil
	}
	byScope := make(map[*scopeInfo]*Metadata, len(scopes))
	for i := range scopes {
		meta := newMetadata(jsonorder.Format{})
		meta.Format = nil
		byScope[&scopes[i]] = meta
	}

	owner := func(path string) (*scopeInfo, string) {
		var best *scopeInfo
		for i := range scopes {
			candidate := &scopes[i]
			if len(candidate.v2Path) > 0 && (best == nil || len(candidate.v2Path) > len(best.v2Path)) {
				if _, ok := metadataRelativePath(candidate.v2Path, path); ok {
					best = candidate
				}
			}
		}
		if best == nil {
			return nil, path
		}
		rel, _ := metadataRelativePath(best.v2Path, path)
		return best, rel
	}
	sets := map[string]map[string]int{}
	// Reuse the global order-set table per destination, preserving only the
	// signatures needed by that scope.
	addOrder := func(dst *Metadata, rel string, idx int) {
		if idx < 0 || idx >= len(all.OrderSets) {
			return
		}
		keys := all.OrderSets[idx]
		sig := strings.Join(keys, "\x00")
		if setsByDest, ok := sets[fmt.Sprintf("%p", dst)]; ok {
			if existing, ok := setsByDest[sig]; ok {
				dst.KeyOrders[rel] = existing
				return
			}
		} else {
			sets[fmt.Sprintf("%p", dst)] = map[string]int{}
		}
		dst.KeyOrders[rel] = len(dst.OrderSets)
		dst.OrderSets = append(dst.OrderSets, append([]string{}, keys...))
		sets[fmt.Sprintf("%p", dst)][sig] = dst.KeyOrders[rel]
	}
	destination := func(path string) (*Metadata, string) {
		scope, rel := owner(path)
		if scope == nil {
			return root, rel
		}
		return byScope[scope], rel
	}
	for path, idx := range all.KeyOrders {
		dst, rel := destination(path)
		addOrder(dst, rel, idx)
	}
	for path, fields := range all.Extras {
		dst, rel := destination(path)
		if dst.Extras[rel] == nil {
			dst.Extras[rel] = map[string]json.RawMessage{}
		}
		for k, v := range fields {
			dst.Extras[rel][k] = append(json.RawMessage(nil), v...)
		}
	}
	for path, mask := range all.URLShapes {
		dst, rel := destination(path)
		dst.URLShapes[rel] = mask
	}
	for path, field := range all.FileFields {
		dst, rel := destination(path)
		dst.FileFields[rel] = field
	}
	for path, lines := range all.ScriptExec {
		dst, rel := destination(path)
		dst.ScriptExec[rel] = append([]string{}, lines...)
	}
	for path, value := range all.MissingResponse {
		dst, rel := destination(path)
		dst.MissingResponse[rel] = value
	}
	for path, value := range all.MissingExec {
		dst, rel := destination(path)
		dst.MissingExec[rel] = value
	}
	if all.Format != nil {
		for _, path := range all.Format.InlinePaths {
			dst, rel := destination(path)
			dst.InlinePaths = append(dst.InlinePaths, rel)
		}
	}
	for _, path := range all.InlinePaths {
		dst, rel := destination(path)
		dst.InlinePaths = append(dst.InlinePaths, rel)
	}

	if err := saveMetadataFile(filepath.Join(collectionDir, resourcesDir, metadataName), root); err != nil {
		return err
	}
	for i := range scopes {
		scope := &scopes[i]
		meta := byScope[scope]
		if !scopeHasMetadata(meta) {
			continue
		}
		switch scope.kind {
		case "folder":
			meta.Scope = "folder"
			if err := saveMetadataFile(filepath.Join(scope.dir, resourcesDir, metadataName), meta); err != nil {
				return err
			}
		case "request":
			meta.Scope = "request"
			if err := saveMetadataFile(requestMetadataPath(scope.dir, scope.stem), meta); err != nil {
				return err
			}
		}
	}
	return nil
}

func scopeHasMetadata(m *Metadata) bool {
	return len(m.KeyOrders) > 0 || len(m.Extras) > 0 || len(m.URLShapes) > 0 ||
		len(m.FileFields) > 0 || len(m.ScriptExec) > 0 || len(m.MissingResponse) > 0 ||
		len(m.MissingExec) > 0 || len(m.InlinePaths) > 0
}

func requestMetadataPath(dir, stem string) string {
	return filepath.Join(dir, resourcesDir, ".roastman", stem+".json")
}
