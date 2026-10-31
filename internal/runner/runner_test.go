package runner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roastman/internal/convert"
)

func TestRunnerScriptsAndVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true,"token":"abc"}`)
		case "/secure":
			if r.Header.Get("Authorization") != "Bearer abc" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"secure":true}`)
		case "/echo":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Runner Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [
    {
      "name": "Hello",
      "event": [
        {"listen": "test", "script": {"exec": [
          "pm.test('status', function () { pm.response.to.have.status(200); });",
          "pm.test('json', function () { var j = pm.response.json(); pm.expect(j.ok).to.be.true; pm.expect(j.token).to.eql('abc'); });",
          "pm.test('property', function () { pm.expect({x: 1}).to.have.property('x'); pm.expect({}).to.not.have.property('x'); });",
          "pm.environment.set('token', pm.response.json().token);",
          "tests['legacy'] = true;"
        ], "type": "text/javascript"}}
      ],
      "request": {"method": "GET", "header": [], "url": "{{base_url}}/hello"},
      "response": []
    },
    {
      "name": "Secure",
      "event": [
        {"listen": "test", "script": {"exec": [
          "pm.test('auth works', function () { pm.response.to.have.status(200); pm.expect(pm.response.json().secure).to.eql(true); });"
        ], "type": "text/javascript"}}
      ],
      "request": {"method": "GET", "header": [], "auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{token}}"}]}, "url": "{{base_url}}/secure"},
      "response": []
    },
    {
      "name": "Echo",
      "request": {
        "method": "POST",
        "header": [{"key": "Content-Type", "value": "application/json"}],
        "body": {"mode": "raw", "raw": "{\"value\":\"{{token}}\"}", "options": {"raw": {"language": "json"}}},
        "url": "{{base_url}}/echo"
      },
      "response": []
    }
  ]
}`

	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	collectionDir := filepath.Join(tmp, "native")
	if _, err := convert.Split([]byte(collection), convert.SplitOptions{OutputDir: collectionDir}); err != nil {
		t.Fatal(err)
	}

	r, err := New(Options{
		CollectionPath: collectionDir,
		TimeoutMS:      5000,
		EnvOverrides:   map[string]string{"base_url": server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Requests != 3 {
		t.Fatalf("requests = %d", report.Requests)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatalf("failures=%d errors=%d", report.Failures, report.Errors)
	}
	if report.Assertions != 5 {
		t.Fatalf("assertions = %d", report.Assertions)
	}
}

func TestRunnerAuthAndPreRequestMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Added") != "yes" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Mutation", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Mutate",
    "event": [
      {"listen": "prerequest", "script": {"exec": [
        "pm.request.headers.add({key: 'X-Added', value: 'yes'});",
        "pm.environment.set('extra', 'value');"
      ], "type": "text/javascript"}},
      {"listen": "test", "script": {"exec": ["pm.test('ok', function () { pm.response.to.have.status(200); });"], "type": "text/javascript"}}
    ],
    "request": {"method": "GET", "header": [], "url": "{{base_url}}/x"},
    "response": []
  }]
}`

	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatal("unexpected failures")
	}
}

func TestRunnerDataIterations(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("name"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Data", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{"name": "Request", "request": {"method": "GET", "header": [], "url": "{{base_url}}/x?name={{name}}"}, "response": []}]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	csvPath := filepath.Join(tmp, "data.csv")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csvPath, []byte("name\nalice\nbob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{
		CollectionPath: jsonPath,
		Data:           csvPath,
		TimeoutMS:      5000,
		EnvOverrides:   map[string]string{"base_url": server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Requests != 2 {
		t.Fatalf("requests = %d", report.Requests)
	}
	if len(seen) != 2 || seen[0] != "alice" || seen[1] != "bob" {
		t.Fatalf("seen = %v", seen)
	}
}

func TestRunnerSetNextRequest(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Next", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [
    {"name": "First", "event": [{"listen": "test", "script": {"exec": ["pm.execution.setNextRequest('Third');"], "type": "text/javascript"}}], "request": {"method": "GET", "header": [], "url": "{{base_url}}/first"}, "response": []},
    {"name": "Second", "request": {"method": "GET", "header": [], "url": "{{base_url}}/second"}, "response": []},
    {"name": "Third", "event": [{"listen": "test", "script": {"exec": ["pm.execution.setNextRequest(null);"], "type": "text/javascript"}}], "request": {"method": "GET", "header": [], "url": "{{base_url}}/third"}, "response": []}
  ]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Requests != 2 {
		t.Fatalf("requests = %d", report.Requests)
	}
	if len(seen) != 2 || seen[0] != "/first" || seen[1] != "/third" {
		t.Fatalf("seen = %v", seen)
	}
}

func TestRunnerPreRequestVariableAffectsCurrentRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dynamic" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Pre Request", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Dynamic",
    "event": [
      {"listen": "prerequest", "script": {"exec": ["pm.environment.set('path', 'dynamic');"], "type": "text/javascript"}},
      {"listen": "test", "script": {"exec": ["pm.test('ok', function () { pm.response.to.have.status(200); });"], "type": "text/javascript"}}
    ],
    "request": {"method": "GET", "header": [], "url": "{{base_url}}/{{path}}"},
    "response": []
  }]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatal("unexpected failure")
	}
}

func TestRunnerWorkspaceEnvironment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmp := t.TempDir()
	collectionDir := filepath.Join(tmp, "postman", "collections", "EnvTest")
	envDir := filepath.Join(tmp, "postman", "environments")
	if err := os.MkdirAll(filepath.Join(collectionDir, ".resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	def := "$kind: collection\nname: EnvTest\n"
	if err := os.WriteFile(filepath.Join(collectionDir, ".resources", "definition.yaml"), []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	request := `$kind: http-request
name: Request
url: "{{base_url}}/ok"
method: GET
`
	if err := os.WriteFile(filepath.Join(collectionDir, "Request.request.yaml"), []byte(request), 0o644); err != nil {
		t.Fatal(err)
	}
	env := fmt.Sprintf("name: Local\nvalues:\n  - key: base_url\n    value: %s\n    enabled: true\n", server.URL)
	if err := os.WriteFile(filepath.Join(envDir, "Local.environment.yaml"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: filepath.Join(tmp, "postman"), Environment: "Local", TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Requests != 1 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatalf("requests=%d errors=%d", report.Requests, report.Errors)
	}
	if report.Environment != "Local" {
		t.Fatalf("environment = %q", report.Environment)
	}
}

func TestRunnerFailureAndBail(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Failures", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [
    {"name": "One", "event": [{"listen": "test", "script": {"exec": ["pm.test('expects 200', function () { pm.response.to.have.status(200); });"], "type": "text/javascript"}}], "request": {"method": "GET", "header": [], "url": "{{base_url}}/one"}, "response": []},
    {"name": "Two", "request": {"method": "GET", "header": [], "url": "{{base_url}}/two"}, "response": []}
  ]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, Bail: true, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Failed() || report.Failures != 1 || report.Requests != 1 {
		t.Fatalf("failed=%v failures=%d requests=%d", report.Failed(), report.Failures, report.Requests)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d", hits)
	}
	var xmlOut strings.Builder
	if err := report.WriteJUnit(&xmlOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xmlOut.String(), "<failure") {
		t.Fatalf("junit output has no failure: %s", xmlOut.String())
	}
}

func TestRunnerURLAndBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/item/abc":
			if r.URL.Query().Get("keep") != "1" || r.URL.Query().Has("off") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/form":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if r.FormValue("field") != "value" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/encoded":
			if err := r.ParseForm(); err != nil || r.FormValue("a") != "1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/graphql":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["query"] != "query { x }" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Bodies", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [
    {"name": "URL", "request": {"method": "GET", "header": [], "url": {"raw": "{{base_url}}/item/:id?keep=1&off=2", "path": ["item", ":id"], "query": [{"key":"keep","value":"1"},{"key":"off","value":"2","disabled":true}], "variable": [{"key":"id","value":"abc"}]}}, "response": []},
    {"name": "Form", "request": {"method": "POST", "header": [], "body": {"mode": "formdata", "formdata": [{"key":"field","value":"value","type":"text"}]}, "url": "{{base_url}}/form"}, "response": []},
    {"name": "Encoded", "request": {"method": "POST", "header": [], "body": {"mode": "urlencoded", "urlencoded": [{"key":"a","value":"1"},{"key":"b","value":"2","disabled":true}]}, "url": "{{base_url}}/encoded"}, "response": []},
    {"name": "GraphQL", "request": {"method": "POST", "header": [], "body": {"mode": "graphql", "graphql": {"query":"query { x }"}}, "url": "{{base_url}}/graphql"}, "response": []}
  ]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatalf("errors = %d", report.Errors)
	}
}

func TestRunnerPmSendRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"nested":true}`)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Send", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Main",
    "event": [{"listen": "test", "script": {"exec": [
      "pm.sendRequest({\"url\": \"{{base_url}}/nested\", \"method\": \"GET\"}, function (err, res) { pm.test('nested', function () { if (err) throw err; pm.expect(res.json().nested).to.eql(true); }); });"
    ], "type": "text/javascript"}}],
    "request": {"method": "GET", "header": [], "url": "{{base_url}}/main"},
    "response": []
  }]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatal("unexpected failure")
	}
}

func TestRunnerFolderAndCollectionEvents(t *testing.T) {
	var header string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Order")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Events", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "event": [
    {"listen":"prerequest","script":{"exec":["pm.environment.set('order', 'c');"],"type":"text/javascript"}},
    {"listen":"test","script":{"exec":["pm.test('collection test', function(){ pm.response.to.have.status(200); });"],"type":"text/javascript"}}
  ],
  "item": [{
    "name": "Folder",
    "event": [
      {"listen":"prerequest","script":{"exec":["pm.environment.set('order', pm.environment.get('order') + 'f');"],"type":"text/javascript"}},
      {"listen":"test","script":{"exec":["pm.test('folder test', function(){ pm.response.to.have.status(200); });"],"type":"text/javascript"}}
    ],
    "item": [{
      "name": "Request",
      "event": [
        {"listen":"prerequest","script":{"exec":["pm.environment.set('order', pm.environment.get('order') + 'r');"],"type":"text/javascript"}},
        {"listen":"test","script":{"exec":["pm.test('request test', function(){ pm.response.to.have.status(200); });"],"type":"text/javascript"}}
      ],
      "request": {"method":"GET","header":[{"key":"X-Order","value":"{{order}}"}],"url":"{{base_url}}/x"},
      "response": []
    }]
  }]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if header != "cfr" {
		t.Fatalf("X-Order = %q", header)
	}
	if report.Assertions != 3 || report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatalf("assertions=%d failures=%d errors=%d", report.Assertions, report.Failures, report.Errors)
	}
}

func TestRunnerAuthInheritance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer collection-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Auth", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "auth": {"type":"bearer","bearer":[{"key":"token","value":"collection-token"}]},
  "item": [{"name":"Request","request":{"method":"GET","header":[],"url":"{{base_url}}/x"},"response":[]}]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatalf("errors=%d", report.Errors)
	}
}

func TestRunnerItemSelectionOrder(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Select", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [
    {"name": "A", "request": {"method":"GET","header":[],"url":"{{base_url}}/a"}, "response":[]},
    {"name": "Folder", "item": [
      {"name": "B", "request": {"method":"GET","header":[],"url":"{{base_url}}/b"}, "response":[]},
      {"name": "C", "request": {"method":"GET","header":[],"url":"{{base_url}}/c"}, "response":[]}
    ]},
    {"name": "D", "request": {"method":"GET","header":[],"url":"{{base_url}}/d"}, "response":[]}
  ]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{
		CollectionPath: jsonPath,
		TimeoutMS:      5000,
		Items:          []string{"Folder", "D"},
		EnvOverrides:   map[string]string{"base_url": server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Requests != 3 {
		t.Fatalf("requests = %d", report.Requests)
	}
	if strings.Join(seen, ",") != "/b,/c,/d" {
		t.Fatalf("seen = %v", seen)
	}
}

func TestRunnerExportFinalEnvironment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Export", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Set",
    "event": [{"listen": "test", "script": {"exec": ["pm.environment.set('from_script', 'final-value');", "pm.globals.set('global_from_script', 'global-value');"], "type": "text/javascript"}}],
    "request": {"method": "GET", "header": [], "url": "{{base_url}}/x"},
    "response": []
  }]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	envPath := filepath.Join(tmp, "Local.environment.yaml")
	globalsPath := filepath.Join(tmp, "globals.yaml")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("name: Local\nvalues:\n  - key: base_url\n    value: "+server.URL+"\n    enabled: true\n  - key: old\n    value: old-value\n    enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalsPath, []byte("name: Globals\nvalues:\n  - key: g\n    value: v\n    enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, Environment: envPath, Globals: globalsPath, TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Errors != 0 {
		t.Fatal("unexpected run error")
	}
	finalEnv := filepath.Join(tmp, "out", "final.environment.yaml")
	if err := r.ExportEnvironment(finalEnv); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(finalEnv)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"name: Local", "from_script", "final-value", "old-value", "enabled: false"} {
		if !strings.Contains(text, want) {
			t.Fatalf("environment export missing %q:\n%s", want, text)
		}
	}
	finalGlobals := filepath.Join(tmp, "final.globals.yaml")
	if err := r.ExportGlobals(finalGlobals); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(finalGlobals)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "global_from_script") || !strings.Contains(string(data), "global-value") {
		t.Fatalf("globals export missing mutated value:\n%s", data)
	}
}

func TestRunnerTopLevelAwait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Await", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Request",
    "event": [{"listen": "prerequest", "script": {"exec": [
      "const sleep = (ms) => new Promise((r) => setTimeout(r, ms));",
      "",
      "await sleep(10);",
      "pm.test('after await', function () { pm.expect(1).to.eql(1); });"
    ], "type": "text/javascript"}}],
    "request": {"method":"GET","header":[],"url":"{{base_url}}/x"},
    "response": []
  }]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatal("top-level await failed")
	}
}

func TestRunnerPostmanJSONEnvironment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	collection := `{"info":{"name":"JSON Env","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"R","request":{"method":"GET","header":[],"url":"{{base_url}}/x"},"response":[]}]}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	envPath := filepath.Join(tmp, "Local.postman_environment.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	env := fmt.Sprintf(`{"id":"x","name":"Local","values":[{"key":"base_url","value":%q,"enabled":true}]}`, server.URL)
	if err := os.WriteFile(envPath, []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, Environment: envPath, TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Errors != 0 || report.Environment != "Local" {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatalf("errors=%d environment=%q", report.Errors, report.Environment)
	}
}

func TestRunnerScriptScopesAndCrypto(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "Scopes", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Two Scripts",
    "event": [
      {"listen": "prerequest", "script": {"exec": [
        "const randomString = 'one';",
        "pm.environment.set('one', randomString);",
        "pm.environment.set('uuid', crypto.randomUUID());"
      ], "type": "text/javascript"}},
      {"listen": "prerequest", "script": {"exec": [
        "const randomString = 'two';",
        "pm.environment.set('two', randomString);"
      ], "type": "text/javascript"}},
      {"listen": "test", "script": {"exec": [
        "pm.test('ok', function () { pm.response.to.have.status(200); });"
      ], "type": "text/javascript"}}
    ],
    "request": {"method":"GET","header":[],"url":"{{base_url}}/x"},
    "response": []
  }]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatal("script scope/crypto failed")
	}
	if v, ok := r.environment.Get("one"); !ok || v != "one" {
		t.Fatalf("one = %q", v)
	}
	if v, ok := r.environment.Get("two"); !ok || v != "two" {
		t.Fatalf("two = %q", v)
	}
	if v, ok := r.environment.Get("uuid"); !ok || len(v) != 36 {
		t.Fatalf("uuid = %q", v)
	}
}

func TestRunnerCryptoJS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	collection := `{
  "info": {"name": "Crypto", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "R",
    "event": [{"listen":"test","script":{"exec":[
      "pm.test('sha1', function () { pm.expect(CryptoJS.SHA1('abc').toString()).to.eql('a9993e364706816aba3e25717850c26c9cd0d89d'); });",
      "pm.test('uuid', function () { pm.expect(crypto.randomUUID().length).to.eql(36); });"
    ],"type":"text/javascript"}}],
    "request": {"method":"GET","header":[],"url":"{{base_url}}/x"},
    "response": []
  }]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatal("crypto failed")
	}
}

func TestRunnerUnresolvedVariableIsWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	collection := `{
  "info": {"name": "Warnings", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [
    {"name":"Missing", "request":{"method":"GET","header":[],"url":"{{base_url}}/x/{{missing_id}}"}, "response":[]},
    {"name":"Body", "request":{"method":"POST","header":[],"body":{"mode":"raw","raw":"{\"x\":\"{{missing_body}}\"}","options":{"raw":{"language":"json"}}},"url":"{{base_url}}/body"}, "response":[]}
  ]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failures != 0 || report.Errors != 0 {
		report.WriteCLI(os.Stderr, true, true)
		t.Fatalf("failures=%d errors=%d", report.Failures, report.Errors)
	}
	if report.Warnings != 2 || report.Skipped != 1 || report.Requests != 1 {
		t.Fatalf("warnings=%d skipped=%d requests=%d", report.Warnings, report.Skipped, report.Requests)
	}
}

func TestRunnerReportBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reply", "yes")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	collection := `{
  "info": {"name": "Bodies Report", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{"name":"R","request":{"method":"POST","header":[{"key":"X-Test","value":"one"}],"body":{"mode":"raw","raw":"hello","options":{"raw":{"language":"text"}}},"url":"{{base_url}}/x"},"response":[]}]
}`
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(jsonPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CollectionPath: jsonPath, TimeoutMS: 5000, ReportBodies: true, EnvOverrides: map[string]string{"base_url": server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 1 {
		t.Fatalf("items = %d", len(report.Items))
	}
	item := report.Items[0]
	if item.RequestBody != "hello" || item.ResponseBody != `{"ok":true}` {
		t.Fatalf("bodies = %q / %q", item.RequestBody, item.ResponseBody)
	}
	if item.RequestHeaders["X-Test"][0] != "one" || item.ResponseHeaders["X-Reply"][0] != "yes" {
		t.Fatalf("headers = %#v / %#v", item.RequestHeaders, item.ResponseHeaders)
	}
}
