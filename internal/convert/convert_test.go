package convert

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripRichCollection(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rich.postman_collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertRoundTrip(t, input)
}

func TestRoundTripMinifiedCollection(t *testing.T) {
	indented, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rich.postman_collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, indented); err != nil {
		t.Fatal(err)
	}
	assertRoundTrip(t, compact.Bytes())
}

// TestRoundTripProvidedSample is an integration test for the large sample that
// ships in some checkouts. It is skipped when data/ isn't present.
func TestRoundTripProvidedSample(t *testing.T) {
	path := filepath.Join("..", "..", "data", "test.postman_collection.json")
	input, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("sample collection not available: %v", err)
	}
	assertRoundTrip(t, input)
}

func assertRoundTrip(t *testing.T, input []byte) {
	t.Helper()
	tmp := t.TempDir()
	collectionDir := filepath.Join(tmp, "collection")
	res, err := Split(input, SplitOptions{OutputDir: collectionDir})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if res.Requests == 0 && !bytes.Contains(input, []byte(`"item"`)) {
		t.Fatal("unexpected empty collection")
	}
	joined, err := Join(collectionDir, JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if !bytes.Equal(input, joined.JSON) {
		// Show a short, readable diff hint without dumping megabytes.
		a := strings.Split(string(input), "\n")
		b := strings.Split(string(joined.JSON), "\n")
		line := 0
		for line < len(a) && line < len(b) && a[line] == b[line] {
			line++
		}
		lo := line - 3
		if lo < 0 {
			lo = 0
		}
		hiA := line + 6
		if hiA > len(a) {
			hiA = len(a)
		}
		hiB := line + 6
		if hiB > len(b) {
			hiB = len(b)
		}
		t.Fatalf("round trip is not byte-identical at line %d:\n--- input ---\n%s\n--- output ---\n%s",
			line+1, strings.Join(a[lo:hiA], "\n"), strings.Join(b[lo:hiB], "\n"))
	}
}

func TestRoundTripEmptyFields(t *testing.T) {
	input := []byte(`{
  "info": {
    "name": "Empty",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "item": [
    {
      "name": "Folder",
      "item": [],
      "event": [],
      "variable": [],
      "auth": null
    },
    {
      "name": "Request",
      "event": [],
      "request": {
        "method": "GET",
        "header": [],
        "description": "",
        "url": {
          "raw": "https://example.com",
          "host": ["example", "com"],
          "path": [],
          "query": [],
          "variable": []
        },
        "auth": null,
        "body": null
      },
      "response": []
    },
    {
      "name": "No Response",
      "request": {
        "method": "GET",
        "header": [],
        "url": "https://example.com"
      }
    }
  ],
  "variable": [],
  "event": [],
  "auth": null
}`)
	assertRoundTrip(t, input)
}

func TestSplitIntoWorkspace(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rich.postman_collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "postman", "collections"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Split(input, SplitOptions{OutputDir: tmp})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, "postman", "collections", sanitizeFileName("Rich/API: v1"))
	if res.CollectionDir != want {
		t.Fatalf("collection dir = %q, want %q", res.CollectionDir, want)
	}
	joined, err := Join(tmp, JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, joined.JSON) {
		t.Fatal("workspace round trip is not byte-identical")
	}
}

func TestJoinNativeLayoutWithoutMetadata(t *testing.T) {
	tmp := t.TempDir()
	collectionDir := filepath.Join(tmp, "postman", "collections", "Example")
	writeTestFile(t, filepath.Join(collectionDir, ".resources", "definition.yaml"), `$kind: collection
name: Example API
variables:
  - key: base_url
    value: https://example.com
`)
	writeTestFile(t, filepath.Join(collectionDir, "folder", "Get Thing.request.yaml"), `$kind: http-request
name: Get Thing
url: "{{base_url}}/things"
method: GET
headers:
  Accept: application/json
queryParams:
  limit: "10"
scripts:
  - type: afterResponse
    code: pm.response.to.have.status(200);
    language: text/javascript
order: 1000
`)
	writeTestFile(t, filepath.Join(collectionDir, "Top.request.yaml"), `$kind: http-request
url: "{{base_url}}/top"
method: DELETE
order: 2000
`)

	res, err := Join(filepath.Join(tmp, "postman"), JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var collection struct {
		Info struct {
			Name string `json:"name"`
		} `json:"info"`
		Item []map[string]any `json:"item"`
	}
	if err := json.Unmarshal(res.JSON, &collection); err != nil {
		t.Fatal(err)
	}
	if collection.Info.Name != "Example API" {
		t.Fatalf("name = %q", collection.Info.Name)
	}
	if len(collection.Item) != 2 {
		t.Fatalf("items = %d", len(collection.Item))
	}
	if got := collection.Item[0]["name"]; got != "folder" {
		t.Fatalf("first item = %v", got)
	}
}

func TestJoinSelectsCollection(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"One", "Two"} {
		dir := filepath.Join(tmp, "postman", "collections", name)
		writeTestFile(t, filepath.Join(dir, ".resources", "definition.yaml"), "$kind: collection\n")
		writeTestFile(t, filepath.Join(dir, "Request.request.yaml"), "$kind: http-request\nurl: https://example.com\n")
	}
	if _, err := Join(filepath.Join(tmp, "postman"), JoinOptions{}); err == nil {
		t.Fatal("expected an error for multiple collections")
	}
	res, err := Join(filepath.Join(tmp, "postman"), JoinOptions{Collection: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Info struct {
			Name string `json:"name"`
		} `json:"info"`
	}
	if err := json.Unmarshal(res.JSON, &c); err != nil {
		t.Fatal(err)
	}
	if c.Info.Name != "Two" {
		t.Fatalf("name = %q", c.Info.Name)
	}
}

func TestJoinWorksWithoutMetadata(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rich.postman_collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "collection")
	if _, err := Split(input, SplitOptions{OutputDir: dir}); err != nil {
		t.Fatal(err)
	}
	if err := removeAllMetadata(dir); err != nil {
		t.Fatal(err)
	}
	res, err := Join(dir, JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(res.JSON) {
		t.Fatal("join produced invalid JSON")
	}
}

func TestSplitForceProtection(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rich.postman_collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "out")
	writeTestFile(t, filepath.Join(dir, "existing.txt"), "keep me")
	if _, err := Split(input, SplitOptions{OutputDir: dir}); err == nil {
		t.Fatal("expected non-empty directory error")
	}
	if _, err := Split(input, SplitOptions{OutputDir: dir, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "existing.txt")); !os.IsNotExist(err) {
		t.Fatal("force should have removed the old contents")
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"Health Check":          "Health Check",
		"a/b:c*d?e":             "a-b-c-d-e",
		"trailing.  ":           "trailing-",
		"..":                    "-",
		"CON.txt":               "-.txt",
		strings.Repeat("x", 80): strings.Repeat("x", 64),
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScopedMetadataSurvivesInsertion(t *testing.T) {
	input := []byte(`{
  "info": {
    "name": "Index Test",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "item": [
    {
      "name": "A",
      "protocolProfileBehavior": {"disableBodyPruning": true},
      "request": {
        "method": "GET",
        "header": [{"key": "X-A", "value": "1", "type": "text"}],
        "url": {"raw": "https://example.com/a", "host": ["example", "com"], "path": ["a"]}
      },
      "response": []
    }
  ]
}`)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "collection")
	if _, err := Split(input, SplitOptions{OutputDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".resources", ".roastman", "A.json")); err != nil {
		t.Fatalf("request sidecar missing: %v", err)
	}

	// Insert a folder before the only existing request. This shifted all
	// item indexes under the old metadata scheme.
	newFolder := filepath.Join(dir, "0 Inserted")
	writeTestFile(t, filepath.Join(newFolder, ".resources", "definition.yaml"), `$kind: collection
order: 1
`)
	writeTestFile(t, filepath.Join(newFolder, "New.request.yaml"), `$kind: http-request
name: New
url: https://example.com/new
method: GET
order: 1
`)

	joined, err := Join(dir, JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Item []map[string]any `json:"item"`
	}
	if err := json.Unmarshal(joined.JSON, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Item) != 2 {
		t.Fatalf("items = %d", len(got.Item))
	}
	if got.Item[0]["name"] != "0 Inserted" {
		t.Fatalf("first item = %v", got.Item[0]["name"])
	}
	if _, ok := got.Item[0]["protocolProfileBehavior"]; ok {
		t.Fatal("old request metadata leaked onto inserted folder")
	}
	a := got.Item[1]
	if _, ok := a["protocolProfileBehavior"]; !ok {
		t.Fatal("request metadata was lost")
	}
	request := a["request"].(map[string]any)
	if _, ok := request["url"].(map[string]any); !ok {
		t.Fatalf("URL shape metadata was lost: %#v", request["url"])
	}
}

func removeAllMetadata(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".roastman" {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == metadataName {
			return os.Remove(path)
		}
		return nil
	})
}
