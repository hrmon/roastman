package runner

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"roastman/internal/convert"
)

// Options configures a runner.
type Options struct {
	CollectionPath string
	Collection     string
	Environment    string
	Globals        string
	Data           string
	Iterations     int
	Folders        []string
	Requests       []string
	Items          []string
	DelayMS        int
	TimeoutMS      int
	MaxResponseMB  int
	Bail           bool
	Insecure       bool
	NoScripts      bool
	ReportBodies   bool
	EnvOverrides   map[string]string
	Verbose        bool
	OnItem         func(ItemReport)
}

type loaded struct {
	collection      *Collection
	collectionDir   string
	workspaceRoot   string // best effort
	environmentName string
	globals         *VarStore
	globalsName     string
	environment     *VarStore
	dataRows        []map[string]string
}

func load(opts Options) (*loaded, error) {
	collectionDir := ""
	var raw []byte
	st, err := os.Stat(opts.CollectionPath)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		collectionDir, err = convert.ResolveCollectionDir(opts.CollectionPath, opts.Collection)
		if err != nil {
			return nil, err
		}
		res, err := convert.Join(opts.CollectionPath, convert.JoinOptions{Collection: opts.Collection})
		if err != nil {
			return nil, err
		}
		raw = res.JSON
	} else {
		raw, err = os.ReadFile(opts.CollectionPath)
		if err != nil {
			return nil, err
		}
		collectionDir = filepath.Dir(opts.CollectionPath)
	}
	var collection Collection
	if err := json.Unmarshal(raw, &collection); err != nil {
		return nil, fmt.Errorf("decode collection: %w", err)
	}
	if collection.Info.Name == "" {
		collection.Info.Name = filepath.Base(collectionDir)
	}
	if collection.Item == nil {
		return nil, fmt.Errorf("collection has no items")
	}

	l := &loaded{
		collection:    &collection,
		collectionDir: collectionDir,
		workspaceRoot: findWorkspaceRoot(collectionDir),
		globals:       NewVarStore(),
		environment:   NewVarStore(),
	}
	if err := l.loadGlobals(opts.Globals); err != nil {
		return nil, err
	}
	if err := l.loadEnvironment(opts.Environment); err != nil {
		return nil, err
	}
	for k, v := range opts.EnvOverrides {
		l.environment.Set(k, v)
	}
	if opts.Data != "" {
		rows, err := loadData(opts.Data)
		if err != nil {
			return nil, err
		}
		l.dataRows = rows
	}
	return l, nil
}

func findWorkspaceRoot(collectionDir string) string {
	dir := collectionDir
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "postman", "collections")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".postman")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "collections")); err == nil && filepath.Base(dir) == "postman" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if filepath.Base(collectionDir) != "" {
		return filepath.Dir(collectionDir)
	}
	return ""
}

func (l *loaded) envDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		dir = filepath.Clean(dir)
		if dir == "." || dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	dir := l.collectionDir
	for i := 0; i < 7; i++ {
		add(filepath.Join(dir, "environments"))
		add(filepath.Join(dir, "postman", "environments"))
		if filepath.Base(dir) == "postman" {
			add(filepath.Join(dir, "environments"))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if l.workspaceRoot != "" {
		add(filepath.Join(l.workspaceRoot, "postman", "environments"))
		add(filepath.Join(l.workspaceRoot, "environments"))
	}
	return dirs
}

func (l *loaded) globalsDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		dir = filepath.Clean(dir)
		if dir == "." || dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	dir := l.collectionDir
	for i := 0; i < 7; i++ {
		add(filepath.Join(dir, "globals"))
		add(filepath.Join(dir, "postman", "globals"))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dirs
}

func (l *loaded) loadEnvironment(nameOrFile string) error {
	if nameOrFile == "" {
		return nil
	}
	var path string
	if st, err := os.Stat(nameOrFile); err == nil && !st.IsDir() {
		path = nameOrFile
	} else {
		base := strings.TrimSuffix(strings.TrimSuffix(nameOrFile, ".environment.yaml"), ".environment.yml")
		for _, dir := range l.envDirs() {
			for _, candidate := range []string{
				filepath.Join(dir, nameOrFile),
				filepath.Join(dir, base+".environment.yaml"),
				filepath.Join(dir, base+".environment.yml"),
				filepath.Join(dir, base+".postman_environment.json"),
				filepath.Join(dir, base+".environment.json"),
			} {
				if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
					path = candidate
					break
				}
			}
			if path != "" {
				break
			}
		}
		if path == "" {
			return fmt.Errorf("environment %q not found (searched %s)", nameOrFile, strings.Join(l.envDirs(), ", "))
		}
	}
	store, envName, err := loadVariableYAML(path)
	if err != nil {
		return err
	}
	l.environment = store
	l.environmentName = envName
	return nil
}

func (l *loaded) loadGlobals(file string) error {
	if file != "" {
		store, name, err := loadVariableYAML(file)
		if err != nil {
			return err
		}
		l.globals = store
		l.globalsName = name
		return nil
	}
	for _, dir := range l.globalsDirs() {
		for _, candidate := range []string{
			filepath.Join(dir, "workspace.globals.yaml"),
			filepath.Join(dir, "workspace.globals.yml"),
			filepath.Join(dir, "workspace.globals.json"),
		} {
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				store, name, err := loadVariableYAML(candidate)
				if err != nil {
					return err
				}
				l.globals = store
				l.globalsName = name
				return nil
			}
		}
	}
	return nil
}

type yamlVariableFile struct {
	Name   string `yaml:"name"`
	Values []struct {
		Key     string `yaml:"key"`
		Value   any    `yaml:"value"`
		Enabled *bool  `yaml:"enabled"`
		Type    string `yaml:"type"`
	} `yaml:"values"`
}

func loadVariableYAML(path string) (*VarStore, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var file yamlVariableFile
	trimmed := bytes.TrimSpace(data)
	if strings.EqualFold(filepath.Ext(path), ".json") || (len(trimmed) > 0 && trimmed[0] == '{') {
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, "", fmt.Errorf("parse %s: %w", path, err)
		}
	} else if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", path, err)
	}
	store := NewVarStore()
	for _, v := range file.Values {
		enabled := true
		if v.Enabled != nil {
			enabled = *v.Enabled
		}
		store.SetTyped(v.Key, valueAsString(v.Value), enabled, v.Type)
	}
	name := file.Name
	if name == "" {
		name = filepath.Base(path)
		name = strings.TrimSuffix(name, filepath.Ext(name))
		name = strings.TrimSuffix(name, ".environment")
		name = strings.TrimSuffix(name, ".globals")
	}
	return store, name, nil
}

func loadData(path string) ([]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		r := csv.NewReader(bytes.NewReader(data))
		r.FieldsPerRecord = -1
		records, err := r.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("parse CSV data: %w", err)
		}
		if len(records) < 2 {
			return nil, fmt.Errorf("CSV data must contain a header row and at least one data row")
		}
		headers := records[0]
		rows := make([]map[string]string, 0, len(records)-1)
		for _, record := range records[1:] {
			row := map[string]string{}
			for i, h := range headers {
				if i < len(record) {
					row[h] = record[i]
				}
			}
			rows = append(rows, row)
		}
		return rows, nil
	case ".json":
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("parse JSON data: %w", err)
		}
		var list []map[string]any
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				m, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("JSON data rows must be objects")
				}
				list = append(list, m)
			}
		case map[string]any:
			list = []map[string]any{v}
		default:
			return nil, fmt.Errorf("JSON data must be an object or array of objects")
		}
		rows := make([]map[string]string, 0, len(list))
		for _, m := range list {
			row := map[string]string{}
			for k, v := range m {
				row[k] = valueAsString(v)
			}
			rows = append(rows, row)
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unsupported data file %s (use .csv or .json)", path)
	}
}
