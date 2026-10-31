package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type exportFile struct {
	Name   string        `yaml:"name" json:"name"`
	Values []exportValue `yaml:"values" json:"values"`
}

type exportValue struct {
	Key     string `yaml:"key" json:"key"`
	Value   string `yaml:"value" json:"value"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Type    string `yaml:"type,omitempty" json:"type,omitempty"`
}

// ExportEnvironment writes the final, in-memory environment after a run.
func (r *Runner) ExportEnvironment(path string) error {
	name := r.loaded.environmentName
	if name == "" {
		name = "Environment"
	}
	return writeExportFile(path, name, r.environment)
}

// ExportGlobals writes the final, in-memory globals after a run.
func (r *Runner) ExportGlobals(path string) error {
	name := r.loaded.globalsName
	if name == "" {
		name = "Globals"
	}
	return writeExportFile(path, name, r.globals)
}

func writeExportFile(path, name string, store *VarStore) error {
	file := exportFile{Name: name}
	for _, e := range store.Entries() {
		file.Values = append(file.Values, exportValue{
			Key: e.Key, Value: e.Value, Enabled: e.Enabled, Type: e.Type,
		})
	}
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create export directory: %w", err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if strings.EqualFold(filepath.Ext(path), ".json") {
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(file)
	}
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(file)
}
