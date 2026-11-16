package runner

import (
	"fmt"
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WriteHTML writes a self-contained HTML report. All styles and scripts are
// inline, so the file can be archived as a CI artifact and opened offline.
func (r *Report) WriteHTML(w io.Writer) error {
	tmpl, err := htmlReportTemplate()
	if err != nil {
		return fmt.Errorf("parse html report template: %w", err)
	}
	if err := tmpl.Execute(w, newHTMLReportView(r)); err != nil {
		return fmt.Errorf("write html report: %w", err)
	}
	return nil
}

// The template is parsed lazily so a malformed report template can never break
// the conversion commands at startup.
var htmlReportTemplate = sync.OnceValues(func() (*template.Template, error) {
	return template.New("report").Parse(htmlReportTemplateSource)
})

// htmlReportView is the precomputed view model for the HTML report.
type htmlReportView struct {
	DocumentTitle string
	Collection    string
	Environment   string
	Verdict       string
	VerdictClass  string
	Started       string
	StartedISO    string
	Duration      string
	Iterations    int
	AvgResponse   string
	DataReceived  string
	Stats         []htmlStat
	Groups        []htmlGroup
	Recap         []htmlFailure
	Total         int
	PassedCount   int
	FailedCount   int
	WarningCount  int
	SkippedCount  int
}

type htmlStat struct {
	Label  string
	Value  string
	Detail string
	Tone   string
}

type htmlGroup struct {
	Name    string
	Label   string
	Summary string
	Items   []htmlItem
}

type htmlItem struct {
	ID               string
	State            string
	StateIcon        string
	StateLabel       string
	Name             string
	Path             string
	Method           string
	MethodClass      string
	URL              string
	CodeText         string
	CodeClass        string
	Status           string
	IterationLabel   string
	ShowIteration    bool
	Duration         string
	ResponseMeta     string
	ShowResponse     bool
	Error            string
	Warnings         []string
	ConsoleText      string
	Tests            []htmlTest
	FailedTests      []htmlTest
	TestsSummary     string
	RequestHeaders   []htmlHeader
	ResponseHeaders  []htmlHeader
	RequestBody      string
	ResponseBody     string
	HasRequestDetail bool
	HasFailures      bool
	Search           string
}

type htmlTest struct {
	Name   string
	Passed bool
	Error  string
}

type htmlHeader struct {
	Name   string
	Values []string
}

type htmlFailure struct {
	Target      string
	Path        string
	Method      string
	MethodClass string
	CodeText    string
	CodeClass   string
	Error       string
	FailedTests []htmlTest
}

func newHTMLReportView(r *Report) htmlReportView {
	collection := strings.TrimSpace(r.Collection)
	if collection == "" {
		collection = "Untitled collection"
	}
	view := htmlReportView{
		DocumentTitle: collection + " · roastman report",
		Collection:    collection,
		Environment:   r.Environment,
		Duration:      humanDuration(r.DurationMS),
		Iterations:    r.Iterations,
		Total:         len(r.Items),
	}
	switch {
	case r.Failed():
		view.Verdict, view.VerdictClass = "Failed", "bad"
	case r.Warnings > 0:
		view.Verdict, view.VerdictClass = "Passed with warnings", "warn"
	case r.Skipped > 0:
		view.Verdict, view.VerdictClass = "Passed with skipped requests", "warn"
	default:
		view.Verdict, view.VerdictClass = "Passed", "good"
	}
	if !r.StartedAt.IsZero() {
		view.Started = r.StartedAt.Local().Format("2006-01-02 15:04:05 MST")
		view.StartedISO = r.StartedAt.Format(time.RFC3339)
	}

	var responseTimes []int64
	var received int64
	groupIndex := map[string]int{}
	for i, item := range r.Items {
		if item.Code != 0 {
			responseTimes = append(responseTimes, item.ResponseTimeMS)
			received += item.ResponseSize
		}
		iv := newHTMLItem(item, i, r.Iterations)
		if iv.State == "failed" {
			view.FailedCount++
		}
		if iv.State == "skipped" {
			view.SkippedCount++
		}
		if iv.State == "passed" || iv.State == "warning" {
			view.PassedCount++
		}
		if len(item.Warnings) > 0 {
			view.WarningCount++
		}
		gi, ok := groupIndex[item.Folder]
		if !ok {
			gi = len(view.Groups)
			groupIndex[item.Folder] = gi
			view.Groups = append(view.Groups, htmlGroup{
				Name:  item.Folder,
				Label: folderLabel(item.Folder),
			})
		}
		view.Groups[gi].Items = append(view.Groups[gi].Items, iv)
		if iv.HasFailures {
			recap := htmlFailure{
				Target:      iv.ID,
				Path:        iv.Path,
				Method:      iv.Method,
				MethodClass: iv.MethodClass,
				CodeText:    iv.CodeText,
				CodeClass:   iv.CodeClass,
				Error:       iv.Error,
				FailedTests: iv.FailedTests,
			}
			view.Recap = append(view.Recap, recap)
		}
	}
	for i := range view.Groups {
		failed, skipped := 0, 0
		for _, item := range view.Groups[i].Items {
			if item.State == "failed" {
				failed++
			}
			if item.State == "skipped" {
				skipped++
			}
		}
		view.Groups[i].Summary = requestSummary(len(view.Groups[i].Items), failed, skipped)
	}
	// Root-level requests would otherwise appear to belong to the folder above
	// them. Always label the root group so it also anchors the item headings.
	for i := range view.Groups {
		if view.Groups[i].Name == "" {
			view.Groups[i].Label = "Collection root"
		}
	}

	if len(responseTimes) > 0 {
		min, max, sum := responseTimes[0], responseTimes[0], int64(0)
		for _, ms := range responseTimes {
			if ms < min {
				min = ms
			}
			if ms > max {
				max = ms
			}
			sum += ms
		}
		view.AvgResponse = humanMilliseconds(sum / int64(len(responseTimes)))
	}
	if received > 0 {
		view.DataReceived = humanBytes(received)
	}

	requestsDetail := "no request failures"
	if view.FailedCount > 0 {
		requestsDetail = fmt.Sprintf("%d failed", view.FailedCount)
	}
	assertionsDetail := "no assertions"
	if r.Assertions > 0 && r.Failures > 0 {
		assertionsDetail = fmt.Sprintf("%d failed", r.Failures)
	} else if r.Assertions > 0 {
		assertionsDetail = "all passed"
	}
	view.Stats = []htmlStat{
		{Label: "Requests", Value: strconv.Itoa(r.Requests), Detail: requestsDetail},
		{Label: "Assertions", Value: strconv.Itoa(r.Assertions), Detail: assertionsDetail},
		{Label: "Failures", Value: strconv.Itoa(r.Failures), Detail: "assertion checks", Tone: toneIf(r.Failures > 0, "bad")},
		{Label: "Errors", Value: strconv.Itoa(r.Errors), Detail: "request errors", Tone: toneIf(r.Errors > 0, "bad")},
		{Label: "Warnings", Value: strconv.Itoa(r.Warnings), Detail: "non-fatal", Tone: toneIf(r.Warnings > 0, "warn")},
		{Label: "Skipped", Value: strconv.Itoa(r.Skipped), Detail: "not executed", Tone: toneIf(r.Skipped > 0, "muted")},
	}
	return view
}

func newHTMLItem(item ItemReport, index, iterations int) htmlItem {
	name := itemDisplayName(item)
	folder := folderLabel(item.Folder)
	path := name
	if folder != "" {
		path = folder + " › " + name
	}
	failedTests := failedTestsOf(item.Tests)
	failed := item.Failed || item.Error != "" || len(failedTests) > 0

	state, icon, label := "passed", "✓", "passed"
	switch {
	case item.Skipped:
		state, icon, label = "skipped", "↷", "skipped"
	case failed:
		state, icon, label = "failed", "✗", "failed"
	case len(item.Warnings) > 0:
		state, icon, label = "warning", "⚠", "passed with warnings"
	}

	iterationLabel := ""
	if item.Iteration > 0 {
		iterationLabel = fmt.Sprintf("iteration %d", item.Iteration)
	}

	responseMeta := ""
	if item.Code != 0 {
		responseMeta = "response " + humanMilliseconds(item.ResponseTimeMS)
		if item.DurationMS > 0 {
			responseMeta += " · total " + humanMilliseconds(item.DurationMS)
		}
		if item.ResponseSize > 0 {
			responseMeta += " · " + humanBytes(item.ResponseSize)
		}
	}

	testsSummary := ""
	if len(item.Tests) > 0 {
		passed := len(item.Tests) - len(failedTests)
		switch {
		case passed > 0 && len(failedTests) > 0:
			testsSummary = fmt.Sprintf("%d passed · %d failed", passed, len(failedTests))
		case len(failedTests) > 0:
			testsSummary = fmt.Sprintf("%d failed", len(failedTests))
		default:
			testsSummary = fmt.Sprintf("%d passed", passed)
		}
	}

	requestHeaders := sortedHTMLHeaders(item.RequestHeaders)
	responseHeaders := sortedHTMLHeaders(item.ResponseHeaders)
	hasRequestDetail := item.Method != "" || item.URL != "" || len(requestHeaders) > 0 || item.RequestBody != ""

	searchParts := []string{
		name, item.Name, item.Folder, path, item.Method, item.URL, item.Status, item.Error,
	}
	if item.Code != 0 {
		searchParts = append(searchParts, strconv.Itoa(item.Code))
	}
	searchParts = append(searchParts, item.Warnings...)
	searchParts = append(searchParts, item.Console...)
	for _, test := range item.Tests {
		searchParts = append(searchParts, test.Name, test.Error)
	}

	return htmlItem{
		ID:               fmt.Sprintf("item-%d", index+1),
		State:            state,
		StateIcon:        icon,
		StateLabel:       label,
		Name:             name,
		Path:             path,
		Method:           item.Method,
		MethodClass:      methodClass(item.Method),
		URL:              item.URL,
		CodeText:         codeText(item),
		CodeClass:        codeClass(item),
		Status:           item.Status,
		IterationLabel:   iterationLabel,
		ShowIteration:    iterations > 1 && iterationLabel != "",
		Duration:         humanMilliseconds(item.DurationMS),
		ResponseMeta:     responseMeta,
		ShowResponse:     item.Code != 0,
		Error:            item.Error,
		Warnings:         item.Warnings,
		ConsoleText:      strings.Join(item.Console, "\n"),
		Tests:            htmlTests(item.Tests),
		FailedTests:      failedTests,
		TestsSummary:     testsSummary,
		RequestHeaders:   requestHeaders,
		ResponseHeaders:  responseHeaders,
		RequestBody:      item.RequestBody,
		ResponseBody:     item.ResponseBody,
		HasRequestDetail: hasRequestDetail,
		HasFailures:      failed,
		Search:           strings.ToLower(strings.Join(searchParts, "\n")),
	}
}

func htmlTests(tests []TestResult) []htmlTest {
	if len(tests) == 0 {
		return nil
	}
	out := make([]htmlTest, 0, len(tests))
	for _, test := range tests {
		out = append(out, htmlTest{Name: test.Name, Passed: test.Passed, Error: test.Error})
	}
	return out
}

func failedTestsOf(tests []TestResult) []htmlTest {
	var out []htmlTest
	for _, test := range tests {
		if !test.Passed {
			out = append(out, htmlTest{Name: test.Name, Passed: false, Error: test.Error})
		}
	}
	return out
}

func sortedHTMLHeaders(headers map[string][]string) []htmlHeader {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]htmlHeader, 0, len(keys))
	for _, key := range keys {
		out = append(out, htmlHeader{Name: key, Values: headers[key]})
	}
	return out
}

func folderLabel(folder string) string {
	return strings.ReplaceAll(folder, "/", " › ")
}

func requestSummary(total, failed, skipped int) string {
	parts := []string{fmt.Sprintf("%d request%s", total, pluralSuffix(total))}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	return strings.Join(parts, " · ")
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func toneIf(condition bool, tone string) string {
	if condition {
		return tone
	}
	return ""
}

func codeText(item ItemReport) string {
	switch {
	case item.Code != 0:
		return strconv.Itoa(item.Code)
	case item.Skipped:
		return "skipped"
	case item.Error != "":
		return "error"
	default:
		return ""
	}
}

func codeClass(item ItemReport) string {
	switch {
	case item.Code >= 500:
		return "c5"
	case item.Code >= 400:
		return "c4"
	case item.Code >= 300:
		return "c3"
	case item.Code >= 200:
		return "c2"
	case item.Error != "":
		return "c5"
	default:
		return "none"
	}
}

func methodClass(method string) string {
	switch strings.ToUpper(method) {
	case "GET", "HEAD":
		return "m-get"
	case "POST":
		return "m-post"
	case "PUT":
		return "m-put"
	case "PATCH":
		return "m-patch"
	case "DELETE":
		return "m-delete"
	case "OPTIONS":
		return "m-other"
	default:
		return "m-other"
	}
}
