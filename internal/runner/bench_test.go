package runner

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkRunnerLocalHTTP(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	var items []string
	for i := 0; i < 200; i++ {
		items = append(items, fmt.Sprintf(`{"name":"Request %d","request":{"method":"GET","header":[],"url":"{{base_url}}/x/%d"},"response":[]}`, i, i))
	}
	collection := fmt.Sprintf(`{"info":{"name":"Bench","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[%s]}`, strings.Join(items, ","))
	tmp := b.TempDir()
	path := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(path, []byte(collection), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := New(Options{
			CollectionPath: path,
			TimeoutMS:      5000,
			NoScripts:      true,
			EnvOverrides:   map[string]string{"base_url": server.URL},
		})
		if err != nil {
			b.Fatal(err)
		}
		report, err := r.Run(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		if report.Requests != 200 || report.Errors != 0 {
			b.Fatalf("requests=%d errors=%d", report.Requests, report.Errors)
		}
	}
}

func BenchmarkRunnerScriptOverhead(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	collection := `{
  "info": {"name": "BenchScript", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Scripted",
    "event": [{"listen": "test", "script": {"exec": ["pm.test('ok', function(){ pm.response.to.have.status(200); pm.expect(pm.response.json().ok).to.be.true; });"], "type": "text/javascript"}}],
    "request": {"method": "GET", "header": [], "url": "{{base_url}}/x"},
    "response": []
  }]
}`
	tmp := b.TempDir()
	path := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(path, []byte(collection), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := New(Options{
			CollectionPath: path,
			TimeoutMS:      5000,
			EnvOverrides:   map[string]string{"base_url": server.URL},
		})
		if err != nil {
			b.Fatal(err)
		}
		report, err := r.Run(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		_ = report
	}
}
