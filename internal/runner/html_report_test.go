package runner

import (
	"strings"
	"testing"
	"time"
)

func TestWriteHTMLReport(t *testing.T) {
	report := &Report{
		Collection:  `Pizza & <Shop> "Report"`,
		Environment: "Local <env>",
		StartedAt:   time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC),
		FinishedAt:  time.Date(2026, 11, 15, 10, 30, 2, 0, time.UTC),
		DurationMS:  1800,
		Iterations:  2,
		Requests:    3,
		Assertions:  3,
		Failures:    1,
		Errors:      1,
		Warnings:    3,
		Skipped:     1,
		Items: []ItemReport{
			{
				Iteration:      1,
				Name:           "List users",
				DisplayName:    "List users (1/2)",
				Folder:         "API/Users",
				Method:         "GET",
				URL:            "https://example.test/users?q=<all>",
				Status:         "200 OK",
				Code:           200,
				ResponseTimeMS: 12,
				ResponseSize:   512,
				DurationMS:     16,
				Failed:         true,
				Warnings:       []string{"unresolved <env>"},
				Tests: []TestResult{
					{Name: "status < 300", Passed: true},
					{Name: "body matches", Passed: false, Error: "expected <200> but got 500"},
				},
				Console: []string{`console.log("hello <world>")`},
				RequestHeaders: map[string][]string{
					"Accept": {"application/json"},
					"X-Test": {"<value>"},
				},
				RequestBody:     `{"name":"<script>alert('x')</script>"}`,
				ResponseHeaders: map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
				ResponseBody:    `<html>response & "quotes"</html>`,
			},
			{
				Iteration:  2,
				Name:       "Create user",
				Folder:     "API",
				Method:     "POST",
				URL:        "https://example.test/users",
				DurationMS: 4,
				Failed:     true,
				Error:      "dial <fail>",
			},
			{
				Iteration:  2,
				Name:       "Skipped request",
				Folder:     "API",
				Method:     "GET",
				URL:        "https://example.test/skipped",
				Skipped:    true,
				DurationMS: 1,
				Warnings:   []string{"missing base_url"},
			},
			{
				Iteration:      2,
				Name:           "Health",
				Folder:         "",
				Method:         "HEAD",
				URL:            "https://example.test/health",
				Status:         "204 No Content",
				Code:           204,
				ResponseTimeMS: 2,
				DurationMS:     2,
				Tests:          []TestResult{{Name: "is up", Passed: true}},
			},
			{
				Iteration:  2,
				Name:       "Deprecated",
				Folder:     "API",
				Method:     "PATCH",
				URL:        "https://example.test/deprecated",
				Status:     "200 OK",
				Code:       200,
				DurationMS: 3,
				Warnings:   []string{"deprecated endpoint"},
			},
		},
	}

	var out strings.Builder
	if err := report.WriteHTML(&out); err != nil {
		t.Fatal(err)
	}
	got := out.String()

	for _, want := range []string{
		"<!DOCTYPE html>",
		`<html lang="en">`,
		"Pizza &amp; &lt;Shop&gt;",
		"roastman run report",
		"API › Users",
		"Collection root",
		"List users (1/2)",
		"Create user",
		"Skipped request",
		"Failures &amp; errors",
		`data-filter="warnings"`,
		"body matches",
		"expected &lt;200&gt; but got 500",
		`data-state="failed"`,
		`data-state="passed"`,
		`data-state="warning"`,
		`data-state="skipped"`,
		`id="item-1"`,
		`href="#item-1"`,
		`<pre class="code-block">`,
		"application/json",
		"Content-Type",
		"unresolved &lt;env&gt;",
		"console.log(&#34;hello &lt;world&gt;&#34;)",
		"PATCH",
		"204",
		"Content-Security-Policy",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML report is missing %q", want)
		}
	}

	if strings.Contains(got, "<script>alert") {
		t.Fatal("HTML report contains an unescaped script body")
	}
	if !strings.Contains(got, "&lt;script&gt;alert") {
		t.Fatal("HTML report does not escape the script body")
	}
	if strings.Contains(got, `<Shop> "Report"`) {
		t.Fatal("HTML report contains an unescaped collection name")
	}
	for _, external := range []string{`<script src=`, `<link rel="stylesheet"`, "@import"} {
		if strings.Contains(got, external) {
			t.Fatalf("HTML report loads an external asset: %s", external)
		}
	}
	if !strings.HasSuffix(got, "</html>\n") {
		t.Fatalf("HTML report does not end with </html>: %q", got)
	}
	for _, tag := range []string{"details", "div", "section", "table"} {
		open := strings.Count(got, "<"+tag)
		closed := strings.Count(got, "</"+tag+">")
		if open != closed {
			t.Errorf("unbalanced <%s> tags: %d open, %d close", tag, open, closed)
		}
	}
}

func TestWriteHTMLEmptyReport(t *testing.T) {
	report := &Report{Collection: "Empty"}
	var out strings.Builder
	if err := report.WriteHTML(&out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "No requests were executed in this run.") {
		t.Fatalf("empty report has no empty state:\n%s", got)
	}
	if strings.Contains(got, `class="toolbar"`) {
		t.Fatal("empty report renders the filter toolbar")
	}
}

func TestWriteHTMLReportUsesSortedHeaders(t *testing.T) {
	report := &Report{
		Collection: "Headers",
		Requests:   1,
		Items: []ItemReport{{
			Name:           "Headers",
			Method:         "GET",
			URL:            "https://example.test/",
			Code:           200,
			RequestHeaders: map[string][]string{"Zeta": {"z"}, "Alpha": {"a"}},
		}},
	}
	var out strings.Builder
	if err := report.WriteHTML(&out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	alpha := strings.Index(got, ">Alpha<")
	zeta := strings.Index(got, ">Zeta<")
	if alpha == -1 || zeta == -1 {
		t.Fatalf("headers are missing:\n%s", got)
	}
	if alpha > zeta {
		t.Fatal("headers are not sorted")
	}
	if !strings.Contains(got, "Collection root") {
		t.Fatal("root-only report has no group heading")
	}
}
