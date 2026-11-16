package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesHTMLReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	collection := fmt.Sprintf(`{
  "info": {"name": "HTML Smoke", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "item": [{
    "name": "Ping",
    "event": [{"listen": "test", "script": {"exec": [
      "pm.test('status is 200', function () { pm.response.to.have.status(200); });"
    ], "type": "text/javascript"}}],
    "request": {"method": "GET", "header": [], "url": %q}
  }]
}`, server.URL+"/ping")

	tmp := t.TempDir()
	collectionPath := filepath.Join(tmp, "collection.json")
	if err := os.WriteFile(collectionPath, []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(tmp, "report.html")
	if err := run([]string{"run", collectionPath, "--report", "html", "--report-bodies", "-o", reportPath}); err != nil {
		t.Fatalf("run: %v", err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{
		"<!DOCTYPE html>",
		"HTML Smoke",
		"Ping",
		"status is 200",
		`class="item"`,
		"Response",
		"Content-Type",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("HTML report is missing %q", want)
		}
	}
	if strings.Contains(report, "\x1b[") {
		t.Fatal("HTML report contains ANSI escapes")
	}
}

func TestRunRejectsUnknownReportFormat(t *testing.T) {
	err := run([]string{"run", "does-not-matter.json", "--report", "yaml"})
	if err == nil || !strings.Contains(err.Error(), "unknown report format") {
		t.Fatalf("err = %v, want unknown report format error", err)
	}
}
