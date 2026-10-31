package convert

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"roastman/internal/jsonorder"
)

const collectionSchemaV21 = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// JoinOptions controls native Git -> JSON conversion.
type JoinOptions struct {
	// OutputPath, when set, is where the JSON file is written. Otherwise the
	// JSON is returned in Result.JSON.
	OutputPath string
	// Collection selects a collection when the input is a workspace or a
	// collections directory containing more than one collection.
	Collection string
}

// JoinResult describes a conversion.
type JoinResult struct {
	Collection string
	JSON       []byte
	Warnings   []string
}

// Join converts a native Git collection directory back to Postman
// Collection v2.1 JSON.
func Join(inputPath string, opts JoinOptions) (*JoinResult, error) {
	collectionDir, err := resolveCollectionDir(inputPath, opts.Collection)
	if err != nil {
		return nil, err
	}
	meta, err := loadMetadata(collectionDir)
	if err != nil {
		return nil, err
	}

	j := &joiner{dir: collectionDir, meta: meta}
	if meta != nil && meta.Version >= 2 && (meta.Scope == "" || meta.Scope == "collection") {
		j.useScoped = true
		rootMeta := meta
		j.meta = newMetadata(rootMeta.formatValue())
		if rootMeta.Format != nil {
			copyFormat := *rootMeta.Format
			copyFormat.InlinePaths = nil
			j.meta.Format = &copyFormat
		}
		mergeMetadata(j.meta, rootMeta, "")
	}
	root, err := j.buildCollection()
	if err != nil {
		return nil, err
	}
	format := jsonorder.Format{Indent: "  ", Pretty: true, Newline: "\n"}
	if j.meta != nil && j.meta.Format != nil {
		format = *j.meta.Format
	}
	if j.useScoped && len(j.meta.InlinePaths) > 0 {
		format.InlinePaths = append([]string{}, j.meta.InlinePaths...)
	}
	if format.Newline == "" {
		format.Newline = "\n"
	}
	if format.Pretty && format.Indent == "" {
		format.Indent = "  "
	}
	out := root.MarshalFormatted(format)

	if opts.OutputPath != "" {
		dir := filepath.Dir(opts.OutputPath)
		if dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
		if err := os.WriteFile(opts.OutputPath, out, 0o644); err != nil {
			return nil, err
		}
	}

	name := valueString(root.Get("info").Get("name"))
	var warnings []string
	if meta != nil && meta.Version < metadataVersion {
		warnings = append(warnings, fmt.Sprintf("metadata version %d is index-based; re-run to-files to upgrade before adding or reordering items", meta.Version))
	}
	return &JoinResult{Collection: name, JSON: out, Warnings: warnings}, nil
}

// ResolveCollectionDir locates a native Git collection directory from a
// collection path, a workspace root, a postman directory, or a collections
// directory. It is exported for the runner.
func ResolveCollectionDir(input, requested string) (string, error) {
	return resolveCollectionDir(input, requested)
}

func resolveCollectionDir(input, requested string) (string, error) {
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		if filepath.Base(abs) == definitionName {
			abs = filepath.Dir(filepath.Dir(abs))
		} else {
			return "", fmt.Errorf("%s is not a directory", input)
		}
	}
	// A workspace root: postman/collections/<name> or collections/<name>.
	// Prefer a candidate that actually contains a collection; some workspaces
	// contain an empty placeholder directory at the other location.
	var candidates []string
	for _, candidate := range []string{
		filepath.Join(abs, "postman", "collections"),
		filepath.Join(abs, "collections"),
		abs,
	} {
		if filepath.Base(candidate) == "collections" || candidate == abs && filepath.Base(abs) == "collections" {
			if dirHasCollections(candidate) {
				candidates = append(candidates, candidate)
			}
		}
	}
	if len(candidates) > 0 {
		return selectCollection(candidates[0], requested)
	}
	if _, ok := definitionPath(abs); ok {
		return abs, nil
	}
	if looksLikeCollection(abs) {
		return abs, nil
	}
	return "", fmt.Errorf("no native Git collection found at %s (expected %s/%s)", input, resourcesDir, definitionName)
}

func dirHasCollections(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, ok := definitionPath(filepath.Join(dir, e.Name())); ok {
			return true
		}
	}
	return false
}

func looksLikeCollection(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			name := e.Name()
			if name == resourcesDir || name == ".git" || name == ".postman" || name == "node_modules" {
				continue
			}
			if _, ok := definitionPath(filepath.Join(dir, name)); ok {
				return true
			}
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), requestSuffix) {
			return true
		}
	}
	return false
}

func selectCollection(collectionsDir, requested string) (string, error) {
	entries, err := os.ReadDir(collectionsDir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, ok := definitionPath(filepath.Join(collectionsDir, e.Name())); ok {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if requested != "" {
		for _, name := range names {
			if name == requested {
				return filepath.Join(collectionsDir, name), nil
			}
		}
		return "", fmt.Errorf("collection %q not found in %s", requested, collectionsDir)
	}
	switch len(names) {
	case 0:
		return "", fmt.Errorf("no collections found in %s", collectionsDir)
	case 1:
		return filepath.Join(collectionsDir, names[0]), nil
	default:
		return "", fmt.Errorf("multiple collections found in %s; use --collection: %s", collectionsDir, strings.Join(names, ", "))
	}
}

type joiner struct {
	dir       string
	meta      *Metadata
	useScoped bool
}

type localDefinition struct {
	Kind        string
	ID          string
	Name        string
	Description string
	Variables   *yaml.Node
	Scripts     *yaml.Node
	Auth        *yaml.Node
	Order       float64
}

type localRequest struct {
	Kind           string
	ID             string
	Name           string
	Description    string
	URL            string
	Method         string
	Headers        *yaml.Node
	Query          *yaml.Node
	PathVariables  *yaml.Node
	Body           *yaml.Node
	Auth           *yaml.Node
	Scripts        *yaml.Node
	Settings       *yaml.Node
	Examples       string
	Order          float64
	FileStem       string
	hasHeaderKey   bool
	hasQueryKey    bool
	hasPathVarsKey bool
	hasBodyKey     bool
	hasAuthKey     bool
	hasSettingsKey bool
	hasURLKey      bool
}

type localItem struct {
	Name     string
	Order    float64
	IsFolder bool
	Folder   *localDefinition
	Request  *localRequest
	DirPath  string
	FilePath string
}

func (j *joiner) mergeScoped(meta *Metadata, scopePath string) {
	if j == nil || j.meta == nil || meta == nil {
		return
	}
	mergeMetadata(j.meta, meta, scopePath)
}

func (j *joiner) buildCollection() (*jsonorder.Value, error) {
	path, ok := definitionPath(j.dir)
	var def *localDefinition
	var err error
	if ok {
		def, err = readDefinition(path)
		if err != nil {
			return nil, err
		}
	} else {
		def = &localDefinition{}
	}
	if def.Kind != "" && def.Kind != "collection" {
		return nil, fmt.Errorf("%s: $kind must be collection", path)
	}
	name := def.Name
	if name == "" {
		name = filepath.Base(j.dir)
	}

	items, err := j.readChildren(j.dir, "", 0)
	if err != nil {
		return nil, err
	}

	root := jsonorder.NewObject()
	info := jsonorder.NewObject()
	if def.ID != "" {
		info.Fields = append(info.Fields, jsonorder.Field{Key: "_postman_id", Value: jsonorder.NewString(def.ID)})
	}
	info.Fields = append(info.Fields, jsonorder.Field{Key: "name", Value: jsonorder.NewString(name)})
	if def.Description != "" {
		info.Fields = append(info.Fields, jsonorder.Field{Key: "description", Value: jsonorder.NewString(def.Description)})
	}
	root.Fields = append(root.Fields, jsonorder.Field{Key: "info", Value: info})
	root.Fields = append(root.Fields, jsonorder.Field{Key: "item", Value: items})

	if def.Variables != nil {
		if v := j.variablesToV2(def.Variables, "/variable"); v != nil {
			root.Set("variable", v)
		}
	}
	if def.Scripts != nil {
		if v := j.scriptsToV2(def.Scripts, "/event", true); v != nil {
			root.Set("event", v)
		}
	}
	if def.Auth != nil {
		if v := j.authToV2(def.Auth, "/auth"); v != nil {
			root.Set("auth", v)
		}
	}

	if j.meta != nil {
		if err := jsonorder.MergeExtras(root, j.meta.Extras); err != nil {
			return nil, err
		}
	}
	ApplyCanonicalOrders(root)
	if info.Get("schema") == nil {
		info.Set("schema", jsonorder.NewString(collectionSchemaV21))
	}
	ApplyCanonicalOrders(root)
	if j.meta != nil {
		jsonorder.ApplyKeyOrders(root, j.meta.ordersByPath())
	}
	return root, nil
}

func readDefinition(path string) (*localDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n, err := parseYAML(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	def := &localDefinition{
		Kind:        scalarString(mapValue(n, "$kind")),
		ID:          scalarString(mapValue(n, "id")),
		Name:        scalarString(mapValue(n, "name")),
		Description: scalarString(mapValue(n, "description")),
		Variables:   mapValue(n, "variables"),
		Scripts:     mapValue(n, "scripts"),
		Auth:        mapValue(n, "auth"),
		Order:       scalarFloat(mapValue(n, "order")),
	}
	return def, nil
}

func readRequest(path string) (*localRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n, err := parseYAML(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	name, _ := trimRequestSuffix(filepath.Base(path))
	req := &localRequest{
		Kind:          scalarString(mapValue(n, "$kind")),
		ID:            scalarString(mapValue(n, "id")),
		Name:          scalarString(mapValue(n, "name")),
		Description:   scalarString(mapValue(n, "description")),
		URL:           scalarString(mapValue(n, "url")),
		Method:        scalarString(mapValue(n, "method")),
		Headers:       mapValue(n, "headers"),
		Query:         mapValue(n, "queryParams"),
		PathVariables: mapValue(n, "pathVariables"),
		Body:          mapValue(n, "body"),
		Auth:          mapValue(n, "auth"),
		Scripts:       mapValue(n, "scripts"),
		Settings:      mapValue(n, "settings"),
		Examples:      scalarString(mapValue(n, "examples")),
		Order:         scalarFloat(mapValue(n, "order")),
		FileStem:      name,
	}
	if req.Kind != "" && req.Kind != "http-request" {
		return nil, fmt.Errorf("%s: unsupported request kind %q", path, req.Kind)
	}
	req.hasHeaderKey = req.Headers != nil
	req.hasQueryKey = req.Query != nil
	req.hasPathVarsKey = req.PathVariables != nil
	req.hasBodyKey = req.Body != nil
	req.hasAuthKey = req.Auth != nil
	req.hasSettingsKey = req.Settings != nil
	_, req.hasURLKey = lookupMap(n, "url")
	if req.Name == "" {
		req.Name = req.FileStem
	}
	return req, nil
}

func lookupMap(m *yaml.Node, key string) (*yaml.Node, bool) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], true
		}
	}
	return nil, false
}

func (j *joiner) readChildren(dir, parentPath string, depth int) (*jsonorder.Value, error) {
	if depth > 64 {
		return nil, errors.New("folder nesting is too deep")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var items []localItem
	for _, entry := range entries {
		name := entry.Name()
		if name == resourcesDir || name == ".git" || name == ".postman" || name == "node_modules" {
			continue
		}
		full := filepath.Join(dir, name)
		if entry.IsDir() {
			def := &localDefinition{}
			if defFile, ok := definitionPath(full); ok {
				var err error
				def, err = readDefinition(defFile)
				if err != nil {
					return nil, err
				}
			}
			folderName := def.Name
			if folderName == "" {
				folderName = name
			}
			items = append(items, localItem{
				Name:     folderName,
				Order:    def.Order,
				IsFolder: true,
				Folder:   def,
				DirPath:  full,
			})
			continue
		}
		if _, ok := trimRequestSuffix(name); ok {
			req, err := readRequest(full)
			if err != nil {
				return nil, err
			}
			items = append(items, localItem{
				Name:     req.Name,
				Order:    req.Order,
				Request:  req,
				FilePath: full,
			})
		}
	}
	sort.SliceStable(items, func(i, k int) bool {
		if items[i].Order != items[k].Order {
			return items[i].Order < items[k].Order
		}
		return items[i].Name < items[k].Name
	})

	arr := jsonorder.NewArray()
	for i, item := range items {
		path := fmt.Sprintf("%s/item/%d", parentPath, i)
		if j.useScoped {
			var metaPath string
			if item.IsFolder {
				metaPath = filepath.Join(item.DirPath, resourcesDir, metadataName)
			} else {
				metaPath = requestMetadataPath(filepath.Dir(item.FilePath), item.Request.FileStem)
			}
			if meta, err := loadMetadataFile(metaPath); err != nil {
				return nil, err
			} else if meta != nil && meta.Version >= 2 {
				j.mergeScoped(meta, path)
			}
		}
		if item.IsFolder {
			v, err := j.folderToV2(item, path)
			if err != nil {
				return nil, err
			}
			arr.Items = append(arr.Items, v)
		} else {
			v, err := j.requestToV2(item, path)
			if err != nil {
				return nil, err
			}
			arr.Items = append(arr.Items, v)
		}
	}
	return arr, nil
}

func (j *joiner) folderToV2(item localItem, path string) (*jsonorder.Value, error) {
	obj := jsonorder.NewObject()
	obj.Fields = append(obj.Fields, jsonorder.Field{Key: "name", Value: jsonorder.NewString(item.Name)})
	children, err := j.readChildren(item.DirPath, path, strings.Count(path, "/"))
	if err != nil {
		return nil, err
	}
	obj.Fields = append(obj.Fields, jsonorder.Field{Key: "item", Value: children})
	if item.Folder.ID != "" {
		obj.Set("id", jsonorder.NewString(item.Folder.ID))
	}
	if item.Folder.Description != "" {
		obj.Set("description", jsonorder.NewString(item.Folder.Description))
	}
	if item.Folder.Variables != nil {
		if v := j.variablesToV2(item.Folder.Variables, path+"/variable"); v != nil {
			obj.Set("variable", v)
		}
	}
	if item.Folder.Scripts != nil {
		if v := j.scriptsToV2(item.Folder.Scripts, path+"/event", true); v != nil {
			obj.Set("event", v)
		}
	}
	if item.Folder.Auth != nil {
		if v := j.authToV2(item.Folder.Auth, path+"/auth"); v != nil {
			obj.Set("auth", v)
		}
	}
	return obj, nil
}

func (j *joiner) requestToV2(item localItem, path string) (*jsonorder.Value, error) {
	req := item.Request
	obj := jsonorder.NewObject()
	obj.Fields = append(obj.Fields, jsonorder.Field{Key: "name", Value: jsonorder.NewString(item.Name)})
	if req.ID != "" {
		obj.Set("id", jsonorder.NewString(req.ID))
	}
	if req.Scripts != nil {
		if v := j.scriptsToV2(req.Scripts, path+"/event", false); v != nil {
			obj.Set("event", v)
		}
	}

	requestPath := path + "/request"
	reqObj := jsonorder.NewObject()
	if req.Description != "" {
		reqObj.Set("description", jsonorder.NewString(req.Description))
	}
	if req.Method != "" {
		reqObj.Set("method", jsonorder.NewString(req.Method))
	}
	if req.hasHeaderKey {
		reqObj.Set("header", j.keyValuesToV2(req.Headers, requestPath+"/header"))
	}
	if req.hasBodyKey {
		if v := j.bodyToV2(req.Body, requestPath+"/body"); v != nil {
			reqObj.Set("body", v)
		}
	}
	hasURLShape := false
	if j.meta != nil {
		_, hasURLShape = j.meta.URLShapes[requestPath]
	}
	if req.hasURLKey || req.hasQueryKey || req.hasPathVarsKey || hasURLShape {
		reqObj.Set("url", j.urlToV2(req, requestPath))
	}
	if req.hasAuthKey {
		if v := j.authToV2(req.Auth, requestPath+"/auth"); v != nil {
			reqObj.Set("auth", v)
		}
	}
	if req.hasSettingsKey {
		if v := j.settingsToProtocolProfile(req.Settings); v != nil {
			obj.Set("protocolProfileBehavior", v)
		}
	}
	obj.Fields = append(obj.Fields, jsonorder.Field{Key: "request", Value: reqObj})

	responses, err := j.responsesToV2(req, item.FilePath, path)
	if err != nil {
		return nil, err
	}
	if j.meta == nil || !j.meta.MissingResponse[requestPath] {
		obj.Fields = append(obj.Fields, jsonorder.Field{Key: "response", Value: responses})
	}
	return obj, nil
}

func (j *joiner) urlToV2(req *localRequest, requestPath string) *jsonorder.Value {
	objectForm := req.hasQueryKey || req.hasPathVarsKey
	if j.meta != nil {
		if _, ok := j.meta.URLShapes[requestPath]; ok {
			objectForm = true
		}
	}
	if !objectForm {
		return jsonorder.NewString(req.URL)
	}
	obj := parseURLObject(req.URL)
	if req.hasQueryKey {
		if q := j.keyValuesToV2(req.Query, requestPath+"/url/query"); q != nil {
			obj.Set("query", q)
		}
	}
	if req.hasPathVarsKey {
		if v := j.keyValuesToV2(req.PathVariables, requestPath+"/url/variable"); v != nil {
			obj.Set("variable", v)
		}
	}
	if j.meta != nil {
		if mask, ok := j.meta.URLShapes[requestPath]; ok {
			applyURLShape(obj, mask)
		}
	}
	return obj
}

func parseURLObject(raw string) *jsonorder.Value {
	obj := jsonorder.NewObject()
	obj.Set("raw", jsonorder.NewString(raw))
	rest := raw
	if i := strings.Index(rest, "://"); i > 0 {
		proto := rest[:i]
		if isScheme(proto) {
			obj.Set("protocol", jsonorder.NewString(proto))
			rest = rest[i+3:]
		}
	}
	hash := ""
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		hash = rest[i+1:]
		rest = rest[:i]
	}
	query := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}
	hostPart := rest
	pathPart := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		hostPart = rest[:i]
		pathPart = rest[i+1:]
	}
	// Strip userinfo from the host portion; Postman keeps it in url.auth.
	if i := strings.LastIndexByte(hostPart, '@'); i >= 0 {
		userinfo := hostPart[:i]
		hostPart = hostPart[i+1:]
		if userinfo != "" {
			user, pass := userinfo, ""
			if k := strings.IndexByte(userinfo, ':'); k >= 0 {
				user, pass = userinfo[:k], userinfo[k+1:]
			}
			auth := jsonorder.NewObject()
			auth.Set("user", jsonorder.NewString(user))
			auth.Set("password", jsonorder.NewString(pass))
			obj.Set("auth", auth)
		}
	}
	port := ""
	if i := strings.LastIndexByte(hostPart, ':'); i >= 0 && !strings.Contains(hostPart[i+1:], "]") {
		port = hostPart[i+1:]
		hostPart = hostPart[:i]
	}
	if hostPart != "" {
		host := jsonorder.NewArray()
		for _, part := range strings.Split(hostPart, ".") {
			host.Items = append(host.Items, jsonorder.NewString(part))
		}
		obj.Set("host", host)
	}
	if pathPart != "" {
		path := jsonorder.NewArray()
		for _, part := range strings.Split(pathPart, "/") {
			path.Items = append(path.Items, jsonorder.NewString(part))
		}
		obj.Set("path", path)
	}
	if port != "" {
		obj.Set("port", jsonorder.NewString(port))
	}
	if hash != "" {
		obj.Set("hash", jsonorder.NewString(hash))
	}
	if query != "" {
		q := jsonorder.NewArray()
		for _, pair := range strings.Split(query, "&") {
			key, val := pair, ""
			if i := strings.IndexByte(pair, '='); i >= 0 {
				key, val = pair[:i], pair[i+1:]
			}
			item := jsonorder.NewObject()
			item.Set("key", jsonorder.NewString(key))
			item.Set("value", jsonorder.NewString(val))
			q.Items = append(q.Items, item)
		}
		obj.Set("query", q)
	}
	return obj
}

func applyURLShape(u *jsonorder.Value, mask int) {
	fields := []struct {
		name string
		bit  int
		def  func() *jsonorder.Value
	}{
		{"raw", urlFieldRaw, func() *jsonorder.Value { return jsonorder.NewString("") }},
		{"protocol", urlFieldProtocol, func() *jsonorder.Value { return jsonorder.NewString("") }},
		{"host", urlFieldHost, func() *jsonorder.Value { return jsonorder.NewArray() }},
		{"path", urlFieldPath, func() *jsonorder.Value { return jsonorder.NewArray() }},
		{"port", urlFieldPort, func() *jsonorder.Value { return jsonorder.NewString("") }},
		{"hash", urlFieldHash, func() *jsonorder.Value { return jsonorder.NewString("") }},
		{"query", urlFieldQuery, func() *jsonorder.Value { return jsonorder.NewArray() }},
		{"variable", urlFieldVariable, func() *jsonorder.Value { return jsonorder.NewArray() }},
		{"auth", urlFieldAuth, func() *jsonorder.Value { return jsonorder.NewObject() }},
	}
	for _, f := range fields {
		if mask&f.bit == 0 {
			u.Delete(f.name)
		} else if u.Get(f.name) == nil {
			u.Set(f.name, f.def())
		}
	}
}

func isScheme(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return false
			}
			continue
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func (j *joiner) keyValuesToV2(n *yaml.Node, basePath string) *jsonorder.Value {
	items, _ := parseKeyValues(n)
	arr := jsonorder.NewArray()
	for i, kv := range items {
		obj := jsonorder.NewObject()
		if kv.Key != nil {
			obj.Set("key", jsonorder.NewString(*kv.Key))
		}
		if kv.Value != nil {
			obj.Set("value", jsonorder.NewString(*kv.Value))
		}
		if kv.Description != nil {
			obj.Set("description", jsonorder.NewString(*kv.Description))
		}
		if kv.Disabled != nil {
			obj.Set("disabled", jsonorder.NewBool(*kv.Disabled))
		}
		if kv.Type != nil {
			obj.Set("type", jsonorder.NewString(*kv.Type))
		}
		arr.Items = append(arr.Items, obj)
		_ = basePath
		_ = i
	}
	return arr
}

func (j *joiner) bodyToV2(n *yaml.Node, path string) *jsonorder.Value {
	bodyType := scalarString(mapValue(n, "type"))
	if bodyType == "" {
		return nil
	}
	obj := jsonorder.NewObject()
	switch bodyType {
	case "none":
		return nil
	case "text", "json", "xml", "html", "javascript":
		obj.Set("mode", jsonorder.NewString("raw"))
		obj.Set("raw", jsonorder.NewString(scalarString(mapValue(n, "content"))))
		hasOptionsExtra := j.meta != nil && j.meta.hasExtra(path, "options")
		if !hasOptionsExtra && bodyType != "text" {
			opts := jsonorder.NewObject()
			rawOpt := jsonorder.NewObject()
			rawOpt.Set("language", jsonorder.NewString(bodyType))
			opts.Set("raw", rawOpt)
			obj.Set("options", opts)
		}
	case "formdata":
		obj.Set("mode", jsonorder.NewString("formdata"))
		arr := jsonorder.NewArray()
		content := sequenceItems(mapValue(n, "content"))
		for i, entry := range content {
			if entry.Kind != yaml.MappingNode {
				continue
			}
			entryPath := fmt.Sprintf("%s/formdata/%d", path, i)
			kind := scalarString(mapValue(entry, "type"))
			item := jsonorder.NewObject()
			item.Set("key", jsonorder.NewString(scalarString(mapValue(entry, "key"))))
			if kind == "file" {
				src := scalarString(mapValue(entry, "src"))
				fileField := ""
				if j.meta != nil {
					fileField = j.meta.FileFields[entryPath]
				}
				nullExtra := j.meta != nil && j.meta.extraIsNull(entryPath, "value")
				switch fileField {
				case "value":
					if !(src == "" && nullExtra) {
						item.Set("value", jsonorder.NewString(src))
					}
				case "src":
					item.Set("src", jsonorder.NewString(src))
				default:
					if src != "" {
						item.Set("src", jsonorder.NewString(src))
					}
				}
				item.Set("type", jsonorder.NewString("file"))
			} else {
				item.Set("value", jsonorder.NewString(scalarString(mapValue(entry, "value"))))
				item.Set("type", jsonorder.NewString("text"))
			}
			if v := mapValue(entry, "contentType"); v != nil {
				item.Set("contentType", jsonorder.NewString(scalarString(v)))
			}
			if v := mapValue(entry, "description"); v != nil {
				item.Set("description", jsonorder.NewString(scalarString(v)))
			}
			if v := mapValue(entry, "disabled"); v != nil {
				item.Set("disabled", jsonorder.NewBool(scalarBool(v)))
			}
			arr.Items = append(arr.Items, item)
		}
		obj.Set("formdata", arr)
	case "urlencoded":
		obj.Set("mode", jsonorder.NewString("urlencoded"))
		arr := jsonorder.NewArray()
		for _, entry := range sequenceItems(mapValue(n, "content")) {
			if entry.Kind != yaml.MappingNode {
				continue
			}
			item := jsonorder.NewObject()
			item.Set("key", jsonorder.NewString(scalarString(mapValue(entry, "key"))))
			item.Set("value", jsonorder.NewString(scalarString(mapValue(entry, "value"))))
			if v := mapValue(entry, "description"); v != nil {
				item.Set("description", jsonorder.NewString(scalarString(v)))
			}
			if v := mapValue(entry, "disabled"); v != nil {
				item.Set("disabled", jsonorder.NewBool(scalarBool(v)))
			}
			arr.Items = append(arr.Items, item)
		}
		obj.Set("urlencoded", arr)
	case "graphql":
		obj.Set("mode", jsonorder.NewString("graphql"))
		inner := jsonorder.NewObject()
		if c := mapValue(n, "content"); c != nil {
			if v := mapValue(c, "query"); v != nil {
				inner.Set("query", jsonorder.NewString(scalarString(v)))
			}
			if v := mapValue(c, "variables"); v != nil {
				inner.Set("variables", jsonorder.NewString(scalarString(v)))
			}
			if v := mapValue(c, "operationName"); v != nil {
				inner.Set("operationName", jsonorder.NewString(scalarString(v)))
			}
		}
		obj.Set("graphql", inner)
	case "file":
		obj.Set("mode", jsonorder.NewString("file"))
		inner := jsonorder.NewObject()
		if c := mapValue(n, "content"); c != nil {
			inner.Set("src", jsonorder.NewString(scalarString(mapValue(c, "src"))))
		}
		obj.Set("file", inner)
	default:
		return nil
	}
	return obj
}

func (j *joiner) authToV2(n *yaml.Node, path string) *jsonorder.Value {
	authNode := n
	if n != nil && n.Kind == yaml.SequenceNode {
		if len(n.Content) == 0 {
			return nil
		}
		authNode = n.Content[0]
	}
	if authNode == nil || authNode.Kind != yaml.MappingNode {
		return nil
	}
	typ := scalarString(mapValue(authNode, "type"))
	if typ == "" {
		return nil
	}
	obj := jsonorder.NewObject()
	obj.Set("type", jsonorder.NewString(typ))
	if typ == "noauth" {
		return obj
	}
	creds := mapValue(authNode, "credentials")
	if creds == nil {
		return obj
	}
	arr := jsonorder.NewArray()
	if creds.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(creds.Content); i += 2 {
			item := jsonorder.NewObject()
			item.Set("key", jsonorder.NewString(creds.Content[i].Value))
			item.Set("value", jsonorder.NewString(scalarString(creds.Content[i+1])))
			arr.Items = append(arr.Items, item)
		}
	} else if creds.Kind == yaml.SequenceNode {
		for _, e := range creds.Content {
			if e.Kind != yaml.MappingNode {
				continue
			}
			item := jsonorder.NewObject()
			if v := mapValue(e, "key"); v != nil {
				item.Set("key", jsonorder.NewString(scalarString(v)))
			}
			if v := mapValue(e, "value"); v != nil {
				item.Set("value", jsonorder.NewString(scalarString(v)))
			}
			arr.Items = append(arr.Items, item)
		}
	}
	obj.Set(typ, arr)
	return obj
}

func (j *joiner) variablesToV2(n *yaml.Node, path string) *jsonorder.Value {
	if n == nil {
		return nil
	}
	arr := jsonorder.NewArray()
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			item := jsonorder.NewObject()
			item.Set("key", jsonorder.NewString(n.Content[i].Value))
			item.Set("value", jsonorder.NewString(scalarString(n.Content[i+1])))
			arr.Items = append(arr.Items, item)
		}
		return arr
	}
	for _, e := range sequenceItems(n) {
		if e.Kind != yaml.MappingNode {
			continue
		}
		item := jsonorder.NewObject()
		if v := mapValue(e, "key"); v != nil {
			item.Set("key", jsonorder.NewString(scalarString(v)))
		}
		if v := mapValue(e, "value"); v != nil {
			item.Set("value", jsonorder.NewString(scalarString(v)))
		}
		if v := mapValue(e, "description"); v != nil {
			item.Set("description", jsonorder.NewString(scalarString(v)))
		}
		if v := mapValue(e, "disabled"); v != nil {
			item.Set("disabled", jsonorder.NewBool(scalarBool(v)))
		}
		if v := mapValue(e, "type"); v != nil {
			item.Set("type", jsonorder.NewString(scalarString(v)))
		}
		if v := mapValue(e, "secret"); v != nil {
			item.Set("secret", jsonorder.NewBool(scalarBool(v)))
		}
		if v := mapValue(e, "source"); v != nil {
			item.Set("source", jsonorder.NewString(scalarString(v)))
		}
		arr.Items = append(arr.Items, item)
	}
	return arr
}

func (j *joiner) scriptsToV2(n *yaml.Node, path string, scoped bool) *jsonorder.Value {
	if n == nil {
		return nil
	}
	scripts := sequenceItems(n)
	if len(scripts) == 0 {
		return nil
	}
	arr := jsonorder.NewArray()
	for i, script := range scripts {
		if script.Kind != yaml.MappingNode {
			continue
		}
		typ := scalarString(mapValue(script, "type"))
		listen := ""
		switch typ {
		case "beforeRequest", "http:beforeRequest":
			listen = "prerequest"
		case "afterResponse", "http:afterResponse":
			listen = "test"
		default:
			continue
		}
		obj := jsonorder.NewObject()
		obj.Set("listen", jsonorder.NewString(listen))
		scriptObj := jsonorder.NewObject()
		if codeNode, hasCode := lookupMap(script, "code"); hasCode {
			scriptPath := fmt.Sprintf("%s/%d/script", path, i)
			if j.meta == nil || !j.meta.MissingExec[scriptPath] {
				code := scalarString(codeNode)
				lines := splitCode(code)
				if j.meta != nil {
					if original, ok := j.meta.ScriptExec[scriptPath]; ok && strings.Join(original, "\n") == code {
						lines = original
					}
				}
				exec := jsonorder.NewArray()
				for _, line := range lines {
					exec.Items = append(exec.Items, jsonorder.NewString(line))
				}
				scriptObj.Set("exec", exec)
			}
		}
		if v := mapValue(script, "language"); v != nil {
			scriptObj.Set("type", jsonorder.NewString(scalarString(v)))
		}
		if v := mapValue(script, "packages"); v != nil {
			scriptObj.Set("packages", yamlToJSON(v))
		}
		if v := mapValue(script, "requests"); v != nil {
			scriptObj.Set("requests", yamlToJSON(v))
		}
		obj.Set("script", scriptObj)
		arr.Items = append(arr.Items, obj)
		_ = i
		_ = scoped
	}
	if len(arr.Items) == 0 {
		return nil
	}
	return arr
}

func (j *joiner) settingsToProtocolProfile(n *yaml.Node) *jsonorder.Value {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	obj := yamlToJSON(n)
	return obj
}

func (j *joiner) responsesToV2(req *localRequest, requestFile, path string) (*jsonorder.Value, error) {
	arr := jsonorder.NewArray()
	examplesDir := ""
	if req.Examples != "" {
		examplesDir = filepath.Join(filepath.Dir(requestFile), filepath.FromSlash(strings.TrimPrefix(req.Examples, "./")))
	} else {
		examplesDir = filepath.Join(filepath.Dir(requestFile), resourcesDir, req.FileStem+".resources", "examples")
	}
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return arr, nil
		}
		return nil, err
	}
	type ex struct {
		node  *yaml.Node
		name  string
		stem  string
		order float64
	}
	var list []ex
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := trimExampleSuffix(entry.Name()); !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(examplesDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		n, err := parseYAML(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		fileStem, _ := trimExampleSuffix(entry.Name())
		list = append(list, ex{
			node:  n,
			name:  scalarString(mapValue(n, "name")),
			stem:  fileStem,
			order: scalarFloat(mapValue(n, "order")),
		})
	}
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].order != list[b].order {
			return list[a].order < list[b].order
		}
		return list[a].name < list[b].name
	})
	for i, e := range list {
		arr.Items = append(arr.Items, j.exampleToV2(e.node, e.stem, fmt.Sprintf("%s/response/%d", path, i)))
	}
	return arr, nil
}

func (j *joiner) exampleToV2(n *yaml.Node, stem, path string) *jsonorder.Value {
	obj := jsonorder.NewObject()
	if v := mapValue(n, "id"); v != nil {
		obj.Set("id", jsonorder.NewString(scalarString(v)))
	}
	if v := mapValue(n, "name"); v != nil {
		obj.Set("name", jsonorder.NewString(scalarString(v)))
	} else if stem != "" {
		obj.Set("name", jsonorder.NewString(stem))
	}
	req := jsonorder.NewObject()
	requestPath := path + "/originalRequest"
	if r := mapValue(n, "request"); r != nil {
		rawURL := scalarString(mapValue(r, "url"))
		query := mapValue(r, "queryParams")
		pathVars := mapValue(r, "pathVariables")
		hasShape := false
		if j.meta != nil {
			_, hasShape = j.meta.URLShapes[requestPath]
		}
		objectURL := hasShape || query != nil || pathVars != nil
		if objectURL {
			u := parseURLObject(rawURL)
			if query != nil {
				if q := parseKeyValuesToV2(query); q != nil {
					u.Set("query", q)
				}
			}
			if pathVars != nil {
				if pv := parseKeyValuesToV2(pathVars); pv != nil {
					u.Set("variable", pv)
				}
			}
			if j.meta != nil {
				if mask, ok := j.meta.URLShapes[requestPath]; ok {
					applyURLShape(u, mask)
				}
			}
			req.Set("url", u)
		} else if rawURL != "" {
			req.Set("url", jsonorder.NewString(rawURL))
		}
		if v := mapValue(r, "method"); v != nil {
			req.Set("method", jsonorder.NewString(scalarString(v)))
		}
		if v := mapValue(r, "headers"); v != nil {
			req.Set("header", parseKeyValuesToV2(v))
		}
		if v := mapValue(r, "body"); v != nil {
			if body := exampleBodyToV2(v); body != nil {
				req.Set("body", body)
			}
		}
	}
	obj.Set("originalRequest", req)
	if r := mapValue(n, "response"); r != nil {
		if v := mapValue(r, "statusText"); v != nil {
			obj.Set("status", jsonorder.NewString(scalarString(v)))
		}
		if v := mapValue(r, "statusCode"); v != nil {
			obj.Set("code", yamlToJSON(v))
		}
		if v := mapValue(r, "headers"); v != nil {
			obj.Set("header", parseKeyValuesToV2(v))
		}
		if v := mapValue(r, "body"); v != nil {
			typ := scalarString(mapValue(v, "type"))
			content := scalarString(mapValue(v, "content"))
			if typ != "none" {
				obj.Set("body", jsonorder.NewString(content))
				obj.Set("_postman_previewlanguage", jsonorder.NewString(typ))
			}
		}
	}
	return obj
}

func exampleBodyToV2(n *yaml.Node) *jsonorder.Value {
	typ := scalarString(mapValue(n, "type"))
	if typ == "" || typ == "none" {
		return nil
	}
	obj := jsonorder.NewObject()
	obj.Set("mode", jsonorder.NewString("raw"))
	obj.Set("raw", jsonorder.NewString(scalarString(mapValue(n, "content"))))
	opts := jsonorder.NewObject()
	raw := jsonorder.NewObject()
	raw.Set("language", jsonorder.NewString(typ))
	opts.Set("raw", raw)
	obj.Set("options", opts)
	return obj
}

func parseKeyValuesToV2(n *yaml.Node) *jsonorder.Value {
	items, _ := parseKeyValues(n)
	arr := jsonorder.NewArray()
	for _, kv := range items {
		obj := jsonorder.NewObject()
		if kv.Key != nil {
			obj.Set("key", jsonorder.NewString(*kv.Key))
		}
		if kv.Value != nil {
			obj.Set("value", jsonorder.NewString(*kv.Value))
		}
		if kv.Description != nil {
			obj.Set("description", jsonorder.NewString(*kv.Description))
		}
		if kv.Disabled != nil {
			obj.Set("disabled", jsonorder.NewBool(*kv.Disabled))
		}
		arr.Items = append(arr.Items, obj)
	}
	return arr
}

func (m *Metadata) extraIsNull(path, key string) bool {
	if m == nil {
		return false
	}
	raw, ok := m.Extras[path][key]
	if !ok {
		return false
	}
	return string(raw) == "null"
}
