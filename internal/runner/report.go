package runner

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Report is the complete result of a run.
type Report struct {
	Collection  string       `json:"collection"`
	Environment string       `json:"environment,omitempty"`
	StartedAt   time.Time    `json:"startedAt"`
	FinishedAt  time.Time    `json:"finishedAt"`
	DurationMS  int64        `json:"durationMs"`
	Iterations  int          `json:"iterations"`
	Requests    int          `json:"requests"`
	Assertions  int          `json:"assertions"`
	Failures    int          `json:"failures"`
	Errors      int          `json:"errors"`
	Warnings    int          `json:"warnings"`
	Skipped     int          `json:"skipped"`
	Items       []ItemReport `json:"items"`
}

// ItemReport is the result of one request.
type ItemReport struct {
	Iteration       int                 `json:"iteration"`
	Name            string              `json:"name"`
	Folder          string              `json:"folder,omitempty"`
	Method          string              `json:"method,omitempty"`
	URL             string              `json:"url,omitempty"`
	Status          string              `json:"status,omitempty"`
	Code            int                 `json:"code,omitempty"`
	ResponseTimeMS  int64               `json:"responseTimeMs,omitempty"`
	ResponseSize    int64               `json:"responseSize,omitempty"`
	DurationMS      int64               `json:"durationMs"`
	Tests           []TestResult        `json:"tests,omitempty"`
	Console         []string            `json:"console,omitempty"`
	Error           string              `json:"error,omitempty"`
	Skipped         bool                `json:"skipped,omitempty"`
	Failed          bool                `json:"failed,omitempty"`
	Warnings        []string            `json:"warnings,omitempty"`
	RequestHeaders  map[string][]string `json:"requestHeaders,omitempty"`
	RequestBody     string              `json:"requestBody,omitempty"`
	ResponseHeaders map[string][]string `json:"responseHeaders,omitempty"`
	ResponseBody    string              `json:"responseBody,omitempty"`
}

// Failed reports whether the run has test failures or request errors.
func (r *Report) Failed() bool { return r.Failures > 0 || r.Errors > 0 }

// WriteJSON writes a machine-readable report.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteCLI writes a human-readable Newman-inspired summary.
func (r *Report) WriteCLI(w io.Writer, verbose, noColor bool, showBodies ...bool) {
	_ = noColor
	bodies := len(showBodies) > 0 && showBodies[0]
	showAll := verbose || bodies
	fmt.Fprintf(w, "\n%s\n", r.Collection)
	if r.Environment != "" {
		fmt.Fprintf(w, "Environment: %s\n", r.Environment)
	}

	type group struct {
		folder string
		items  []ItemReport
	}
	var groups []group
	index := map[string]int{}
	for _, item := range r.Items {
		show := showAll || item.Failed || item.Skipped || len(item.Warnings) > 0
		if !show {
			continue
		}
		gi, ok := index[item.Folder]
		if !ok {
			gi = len(groups)
			index[item.Folder] = gi
			groups = append(groups, group{folder: item.Folder})
		}
		groups[gi].items = append(groups[gi].items, item)
	}
	if len(groups) > 0 {
		fmt.Fprintln(w)
	}
	for _, g := range groups {
		if g.folder != "" {
			fmt.Fprintln(w, strings.ReplaceAll(g.folder, "/", " › "))
		}
		for _, item := range g.items {
			writeItem(w, item, showAll, bodies)
		}
	}

	writeReportSummary(w, r, false)

}

// CLIPrinter streams Newman-style results while a run is in progress.
type CLIPrinter struct {
	W          io.Writer
	Verbose    bool
	Bodies     bool
	lastFolder string
	started    bool
}

// NewCLIPrinter returns a live CLI printer.
func NewCLIPrinter(w io.Writer, verbose, bodies bool) *CLIPrinter {
	return &CLIPrinter{W: w, Verbose: verbose, Bodies: bodies}
}

// Header prints the collection and environment heading.
func (p *CLIPrinter) Header(collection, environment string) {
	fmt.Fprintf(p.W, "\n%s\n", collection)
	if environment != "" {
		fmt.Fprintf(p.W, "Environment: %s\n", environment)
	}
}

// Item prints one completed request immediately.
func (p *CLIPrinter) Item(item ItemReport) {
	if item.Folder != p.lastFolder {
		if p.started && item.Folder != "" {
			fmt.Fprintln(p.W)
		}
		if item.Folder != "" {
			fmt.Fprintln(p.W, strings.ReplaceAll(item.Folder, "/", " › "))
		}
		p.lastFolder = item.Folder
		p.started = true
	}
	writeItem(p.W, item, p.Verbose || p.Bodies, p.Bodies)
}

// Summary prints the run summary table and totals.
func (p *CLIPrinter) Summary(r *Report) { writeReportSummary(p.W, r, true) }

// WriteCLISummary writes only the final summary.
func (r *Report) WriteCLISummary(w io.Writer) { writeReportSummary(w, r, true) }

func writeReportSummary(w io.Writer, r *Report, includeFailures bool) {
	failedRequests := 0
	var responseTimes []int64
	var dataReceived int64
	for _, item := range r.Items {
		if item.Failed {
			failedRequests++
		}
		if item.Code != 0 {
			responseTimes = append(responseTimes, item.ResponseTimeMS)
			dataReceived += item.ResponseSize
		}
	}
	rows := [][3]string{
		{"iterations", fmt.Sprintf("%d", r.Iterations), ""},
		{"requests", fmt.Sprintf("%d", r.Requests), fmt.Sprintf("%d", failedRequests)},
		{"assertions", fmt.Sprintf("%d", r.Assertions), fmt.Sprintf("%d", r.Failures)},
		{"warnings", fmt.Sprintf("%d", r.Warnings), ""},
		{"skipped", fmt.Sprintf("%d", r.Skipped), ""},
		{"errors", fmt.Sprintf("%d", r.Errors), ""},
	}
	fmt.Fprintln(w)
	writeReportTable(w, rows)
	fmt.Fprintf(w, "total run duration: %s\n", humanDuration(r.DurationMS))
	if dataReceived > 0 {
		fmt.Fprintf(w, "total data received: %s (approx)\n", humanBytes(dataReceived))
	}
	if len(responseTimes) > 0 {
		min, max, sum := responseTimes[0], responseTimes[0], int64(0)
		for _, t := range responseTimes {
			if t < min {
				min = t
			}
			if t > max {
				max = t
			}
			sum += t
		}
		fmt.Fprintf(w, "average response time: %s (min: %s, max: %s)\n",
			humanMilliseconds(sum/int64(len(responseTimes))), humanMilliseconds(min), humanMilliseconds(max))
	}
	if includeFailures {
		writeFailureRecap(w, r)
	}
	if r.Failed() {
		fmt.Fprintf(w, "\n%d problem(s) found.\n", r.Failures+r.Errors)
	} else if r.Warnings > 0 || r.Skipped > 0 {
		fmt.Fprintf(w, "\nRun completed with %d warning(s) and %d skipped request(s).\n", r.Warnings, r.Skipped)
	} else {
		fmt.Fprintln(w, "\nNo failures.")
	}
}

func writeFailureRecap(w io.Writer, r *Report) {
	printedHeader := false
	for _, item := range r.Items {
		var failedTests []TestResult
		for _, test := range item.Tests {
			if !test.Passed {
				failedTests = append(failedTests, test)
			}
		}
		if len(failedTests) == 0 && item.Error == "" {
			continue
		}
		if !printedHeader {
			fmt.Fprintln(w, "\nFailures:")
			printedHeader = true
		}
		path := item.Name
		if item.Folder != "" {
			path = strings.ReplaceAll(item.Folder, "/", " › ") + " › " + item.Name
		}
		status := ""
		if item.Code != 0 {
			status = fmt.Sprintf(" %d", item.Code)
		}
		fmt.Fprintf(w, "  ✗ %s%s [%s]\n", path, status, item.Method)
		if item.Error != "" {
			fmt.Fprintf(w, "      error: %s\n", item.Error)
		}
		for _, test := range failedTests {
			fmt.Fprintf(w, "      ✗ %s\n", test.Name)
			for _, line := range strings.Split(test.Error, "\n") {
				if line != "" {
					fmt.Fprintf(w, "        %s\n", line)
				}
			}
		}
	}
}

func writeItem(w io.Writer, item ItemReport, verbose, bodies bool) {
	symbol := "✓"
	if item.Skipped {
		symbol = "↷"
	} else if item.Failed {
		symbol = "✗"
	} else if len(item.Warnings) > 0 {
		symbol = "⚠"
	}
	status := ""
	if item.Code != 0 {
		status = fmt.Sprintf(" %d", item.Code)
	}
	fmt.Fprintf(w, "  %s %s%s [%s · %s]\n", symbol, item.Name, status, item.Method, humanMilliseconds(item.DurationMS))
	if item.Error != "" {
		fmt.Fprintf(w, "      ✗ %s\n", item.Error)
	}
	for _, warning := range item.Warnings {
		fmt.Fprintf(w, "      ⚠ %s\n", warning)
	}
	for _, t := range item.Tests {
		if !verbose && t.Passed {
			continue
		}
		symbol := "✓"
		if !t.Passed {
			symbol = "✗"
		}
		fmt.Fprintf(w, "      %s %s\n", symbol, t.Name)
		if !t.Passed && t.Error != "" {
			for _, line := range strings.Split(t.Error, "\n") {
				fmt.Fprintf(w, "        %s\n", line)
			}
		}
	}
	if verbose || item.Failed || len(item.Warnings) > 0 {
		for _, line := range item.Console {
			fmt.Fprintf(w, "      console: %s\n", line)
		}
	}
	if bodies {
		writeBodies(w, item)
	}
}

func writeBodies(w io.Writer, item ItemReport) {
	fmt.Fprintf(w, "      Request: %s %s\n", item.Method, item.URL)
	writeHeaderBlock(w, item.RequestHeaders)
	if item.RequestBody != "" {
		fmt.Fprintln(w, "      Request body:")
		writeIndentedBody(w, item.RequestBody)
	}
	if item.Code != 0 {
		fmt.Fprintf(w, "      Response: %s\n", item.Status)
		writeHeaderBlock(w, item.ResponseHeaders)
		if item.ResponseBody != "" {
			fmt.Fprintln(w, "      Response body:")
			writeIndentedBody(w, item.ResponseBody)
		}
	}
}

func writeHeaderBlock(w io.Writer, headers map[string][]string) {
	if len(headers) == 0 {
		return
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintln(w, "      Headers:")
	for _, k := range keys {
		for _, v := range headers[k] {
			fmt.Fprintf(w, "        %s: %s\n", k, v)
		}
	}
}

func writeIndentedBody(w io.Writer, body string) {
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		fmt.Fprintf(w, "        %s\n", line)
	}
}

func writeReportTable(w io.Writer, rows [][3]string) {
	headers := [3]string{"", "executed", "failed"}
	w1, w2, w3 := len(headers[0]), len(headers[1]), len(headers[2])
	for _, row := range rows {
		if len(row[0]) > w1 {
			w1 = len(row[0])
		}
		if len(row[1]) > w2 {
			w2 = len(row[1])
		}
		if len(row[2]) > w3 {
			w3 = len(row[2])
		}
	}
	line := func(a, b, c string) {
		fmt.Fprintf(w, "│ %-*s │ %-*s │ %-*s │\n", w1, a, w2, b, w3, c)
	}
	border := "┌" + strings.Repeat("─", w1+2) + "┬" + strings.Repeat("─", w2+2) + "┬" + strings.Repeat("─", w3+2) + "┐"
	middle := "├" + strings.Repeat("─", w1+2) + "┼" + strings.Repeat("─", w2+2) + "┼" + strings.Repeat("─", w3+2) + "┤"
	bottom := "└" + strings.Repeat("─", w1+2) + "┴" + strings.Repeat("─", w2+2) + "┴" + strings.Repeat("─", w3+2) + "┘"
	fmt.Fprintln(w, border)
	line(headers[0], headers[1], headers[2])
	fmt.Fprintln(w, middle)
	for _, row := range rows {
		line(row[0], row[1], row[2])
	}
	fmt.Fprintln(w, bottom)
}

func humanDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.2fs", float64(ms)/1000)
	}
	seconds := ms / 1000
	return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
}

func humanMilliseconds(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d ms", ms)
	}
	return fmt.Sprintf("%.2f s", float64(ms)/1000)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Time     string          `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitFailure `xml:"error,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit writes a JUnit XML report.
func (r *Report) WriteJUnit(w io.Writer) error {
	suites := junitTestSuites{
		Name:  r.Collection,
		Time:  seconds(r.DurationMS),
		Tests: 0,
	}
	grouped := map[string][]ItemReport{}
	var order []string
	for _, item := range r.Items {
		key := item.Folder
		if key == "" {
			key = r.Collection
		}
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], item)
	}
	sort.Strings(order)
	for _, name := range order {
		suite := junitTestSuite{Name: name}
		var suiteMS int64
		for _, item := range grouped[name] {
			suiteMS += item.DurationMS
			duration := seconds(item.DurationMS)
			classname := name
			switch {
			case item.Skipped:
				suite.Cases = append(suite.Cases, junitTestCase{
					Name: item.Name, Classname: classname, Time: duration,
					Skipped: &junitSkipped{Message: "skipped"},
				})
				suite.Tests++
			case item.Error != "":
				suite.Cases = append(suite.Cases, junitTestCase{
					Name: item.Name, Classname: classname, Time: duration,
					Error: &junitFailure{Message: item.Error, Body: item.Error},
				})
				suite.Tests++
				suite.Errors++
			case len(item.Tests) > 0:
				for _, t := range item.Tests {
					tc := junitTestCase{Name: item.Name + " :: " + t.Name, Classname: classname, Time: duration}
					suite.Tests++
					if !t.Passed {
						tc.Failure = &junitFailure{Message: t.Error, Body: t.Error}
						suite.Failures++
					}
					suite.Cases = append(suite.Cases, tc)
				}
			default:
				suite.Cases = append(suite.Cases, junitTestCase{Name: item.Name, Classname: classname, Time: duration})
				suite.Tests++
			}
		}
		suite.Time = seconds(suiteMS)
		suites.Tests += suite.Tests
		suites.Failures += suite.Failures
		suites.Errors += suite.Errors
		suites.Suites = append(suites.Suites, suite)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(suites); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func seconds(ms int64) string {
	return fmt.Sprintf("%.3f", float64(ms)/1000.0)
}

// FilterReport returns only failed items. Useful for compact output.
func FilterReport(r *Report) *Report {
	out := *r
	out.Items = nil
	for _, item := range r.Items {
		if item.Failed || item.Skipped {
			out.Items = append(out.Items, item)
		}
	}
	return &out
}
