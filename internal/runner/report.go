package runner

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
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
	ID              string              `json:"id,omitempty"`
	Name            string              `json:"name"`
	DisplayName     string              `json:"displayName,omitempty"`
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

// styles renders ANSI escape sequences when enabled. Styling is applied to
// complete text fragments, so callers can keep using plain strings for width
// calculations.
type styles struct{ enabled bool }

func newStyles(enabled bool) styles { return styles{enabled: enabled} }

func (s styles) paint(code, text string) string {
	if !s.enabled || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s styles) bold(text string) string    { return s.paint("1", text) }
func (s styles) dim(text string) string     { return s.paint("2", text) }
func (s styles) red(text string) string     { return s.paint("31", text) }
func (s styles) green(text string) string   { return s.paint("32", text) }
func (s styles) yellow(text string) string  { return s.paint("33", text) }
func (s styles) cyan(text string) string    { return s.paint("36", text) }
func (s styles) boldRed(text string) string { return s.paint("1;31", text) }

func (s styles) statusCode(code int) string {
	if code == 0 {
		return ""
	}
	text := fmt.Sprintf(" %d", code)
	switch {
	case code >= 200 && code < 300:
		return s.green(text)
	case code >= 300 && code < 400:
		return s.cyan(text)
	case code >= 400:
		return s.red(text)
	default:
		return text
	}
}

// ColorSupported reports whether ANSI colors should be used for w. It is
// disabled when NO_COLOR is set, TERM is "dumb", or w is not a terminal.
func ColorSupported(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// WriteCLI writes a human-readable Newman-inspired summary.
func (r *Report) WriteCLI(w io.Writer, verbose, noColor bool, showBodies ...bool) {
	st := newStyles(!noColor)
	bodies := len(showBodies) > 0 && showBodies[0]
	showAll := verbose || bodies
	fmt.Fprintf(w, "\n%s\n", st.bold(r.Collection))
	if r.Environment != "" {
		fmt.Fprintf(w, "%s %s\n", st.dim("Environment:"), st.cyan(r.Environment))
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
			fmt.Fprintln(w, st.bold(strings.ReplaceAll(g.folder, "/", " › ")))
		}
		for _, item := range g.items {
			writeItem(w, item, showAll, bodies, st)
		}
	}

	writeReportSummary(w, r, false, st)

}

// CLIPrinter streams Newman-style results while a run is in progress.
type CLIPrinter struct {
	W          io.Writer
	Verbose    bool
	Bodies     bool
	Color      bool
	lastFolder string
	started    bool
}

// NewCLIPrinter returns a live CLI printer.
func NewCLIPrinter(w io.Writer, verbose, bodies bool) *CLIPrinter {
	return &CLIPrinter{W: w, Verbose: verbose, Bodies: bodies, Color: ColorSupported(w)}
}

func (p *CLIPrinter) styles() styles { return newStyles(p.Color) }

// Header prints the collection and environment heading.
func (p *CLIPrinter) Header(collection, environment string) {
	st := p.styles()
	fmt.Fprintf(p.W, "\n%s\n", st.bold(collection))
	if environment != "" {
		fmt.Fprintf(p.W, "%s %s\n", st.dim("Environment:"), st.cyan(environment))
	}
}

// Item prints one completed request immediately.
func (p *CLIPrinter) Item(item ItemReport) {
	st := p.styles()
	if item.Folder != p.lastFolder {
		if p.started && item.Folder != "" {
			fmt.Fprintln(p.W)
		}
		if item.Folder != "" {
			fmt.Fprintln(p.W, st.bold(strings.ReplaceAll(item.Folder, "/", " › ")))
		}
		p.lastFolder = item.Folder
		p.started = true
	}
	writeItem(p.W, item, p.Verbose || p.Bodies, p.Bodies, st)
}

// Summary prints the run summary table and totals.
func (p *CLIPrinter) Summary(r *Report) { writeReportSummary(p.W, r, true, p.styles()) }

// WriteCLISummary writes only the final summary.
func (r *Report) WriteCLISummary(w io.Writer) {
	writeReportSummary(w, r, true, newStyles(ColorSupported(w)))
}

func writeReportSummary(w io.Writer, r *Report, includeFailures bool, st styles) {
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
	writeReportTable(w, rows, st)
	fmt.Fprintf(w, "%s %s\n", st.dim("total run duration:"), humanDuration(r.DurationMS))
	if dataReceived > 0 {
		fmt.Fprintf(w, "%s %s (approx)\n", st.dim("total data received:"), humanBytes(dataReceived))
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
		fmt.Fprintf(w, "%s %s (min: %s, max: %s)\n", st.dim("average response time:"),
			humanMilliseconds(sum/int64(len(responseTimes))), humanMilliseconds(min), humanMilliseconds(max))
	}
	if includeFailures {
		writeFailureRecap(w, r, st)
	}
	if r.Failed() {
		fmt.Fprintf(w, "\n%s\n", st.boldRed(fmt.Sprintf("%d problem(s) found.", r.Failures+r.Errors)))
	} else if r.Warnings > 0 || r.Skipped > 0 {
		fmt.Fprintf(w, "\n%s\n", st.yellow(fmt.Sprintf("Run completed with %d warning(s) and %d skipped request(s).", r.Warnings, r.Skipped)))
	} else {
		fmt.Fprintf(w, "\n%s\n", st.green("No failures."))
	}
}

func writeFailureRecap(w io.Writer, r *Report, st styles) {
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
			fmt.Fprintf(w, "\n%s\n", st.boldRed("Failures:"))
			printedHeader = true
		}
		path := itemDisplayName(item)
		if item.Folder != "" {
			path = strings.ReplaceAll(item.Folder, "/", " › ") + " › " + path
		}
		fmt.Fprintf(w, "  %s %s%s [%s]\n", st.red("✗"), st.red(path), st.statusCode(item.Code), st.dim(item.Method))
		if item.Error != "" {
			fmt.Fprintf(w, "      %s %s\n", st.red("error:"), st.red(item.Error))
		}
		for _, test := range failedTests {
			fmt.Fprintf(w, "      %s %s\n", st.red("✗"), st.red(test.Name))
			for _, line := range strings.Split(test.Error, "\n") {
				if line != "" {
					fmt.Fprintf(w, "        %s\n", st.red(line))
				}
			}
		}
	}
}

func itemDisplayName(item ItemReport) string {
	if item.DisplayName != "" {
		return item.DisplayName
	}
	return item.Name
}

func writeItem(w io.Writer, item ItemReport, verbose, bodies bool, st styles) {
	symbol := st.green("✓")
	if item.Skipped {
		symbol = st.yellow("↷")
	} else if item.Failed {
		symbol = st.red("✗")
	} else if len(item.Warnings) > 0 {
		symbol = st.yellow("⚠")
	}
	fmt.Fprintf(w, "  %s %s%s [%s · %s]\n", symbol, st.bold(itemDisplayName(item)), st.statusCode(item.Code), st.cyan(item.Method), st.dim(humanMilliseconds(item.DurationMS)))
	if item.Error != "" {
		fmt.Fprintf(w, "      %s %s\n", st.red("✗"), st.red(item.Error))
	}
	for _, warning := range item.Warnings {
		fmt.Fprintf(w, "      %s %s\n", st.yellow("⚠"), st.yellow(warning))
	}
	for _, t := range item.Tests {
		if !verbose && t.Passed {
			continue
		}
		symbol := st.green("✓")
		name := t.Name
		if !t.Passed {
			symbol = st.red("✗")
			name = st.red(name)
		}
		fmt.Fprintf(w, "      %s %s\n", symbol, name)
		if !t.Passed && t.Error != "" {
			for _, line := range strings.Split(t.Error, "\n") {
				fmt.Fprintf(w, "        %s\n", st.red(line))
			}
		}
	}
	if verbose || item.Failed || len(item.Warnings) > 0 {
		for _, line := range item.Console {
			fmt.Fprintf(w, "      %s %s\n", st.dim("console:"), st.dim(line))
		}
	}
	if bodies {
		writeBodies(w, item, st)
	}
}

func writeBodies(w io.Writer, item ItemReport, st styles) {
	fmt.Fprintf(w, "      %s %s %s\n", st.bold("Request:"), st.cyan(item.Method), item.URL)
	writeHeaderBlock(w, item.RequestHeaders, st)
	if item.RequestBody != "" {
		fmt.Fprintf(w, "      %s\n", st.bold("Request body:"))
		writeIndentedBody(w, item.RequestBody)
	}
	if item.Code != 0 {
		fmt.Fprintf(w, "      %s %s\n", st.bold("Response:"), st.bold(item.Status))
		writeHeaderBlock(w, item.ResponseHeaders, st)
		if item.ResponseBody != "" {
			fmt.Fprintf(w, "      %s\n", st.bold("Response body:"))
			writeIndentedBody(w, item.ResponseBody)
		}
	}
}

func writeHeaderBlock(w io.Writer, headers map[string][]string, st styles) {
	if len(headers) == 0 {
		return
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "      %s\n", st.bold("Headers:"))
	for _, k := range keys {
		for _, v := range headers[k] {
			fmt.Fprintf(w, "        %s: %s\n", st.cyan(k), v)
		}
	}
}

func writeIndentedBody(w io.Writer, body string) {
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		fmt.Fprintf(w, "        %s\n", line)
	}
}

func writeReportTable(w io.Writer, rows [][3]string, st styles) {
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
	cell := func(text string, width int) string {
		return text + strings.Repeat(" ", width-len(text))
	}
	paint := func(fn func(string) string, text string) string {
		if fn == nil {
			return text
		}
		return fn(text)
	}
	line := func(a, b, c string, sa, sb, sc func(string) string) {
		fmt.Fprintf(w, "%s %s %s %s %s %s %s\n",
			st.dim("│"), paint(sa, cell(a, w1)),
			st.dim("│"), paint(sb, cell(b, w2)),
			st.dim("│"), paint(sc, cell(c, w3)),
			st.dim("│"))
	}
	border := "┌" + strings.Repeat("─", w1+2) + "┬" + strings.Repeat("─", w2+2) + "┬" + strings.Repeat("─", w3+2) + "┐"
	middle := "├" + strings.Repeat("─", w1+2) + "┼" + strings.Repeat("─", w2+2) + "┼" + strings.Repeat("─", w3+2) + "┤"
	bottom := "└" + strings.Repeat("─", w1+2) + "┴" + strings.Repeat("─", w2+2) + "┴" + strings.Repeat("─", w3+2) + "┘"
	fmt.Fprintln(w, st.dim(border))
	line(headers[0], headers[1], headers[2], st.bold, st.bold, st.bold)
	fmt.Fprintln(w, st.dim(middle))
	for _, row := range rows {
		var failedStyle func(string) string
		if row[2] != "" && row[2] != "0" {
			failedStyle = st.red
		}
		line(row[0], row[1], row[2], st.bold, nil, failedStyle)
	}
	fmt.Fprintln(w, st.dim(bottom))
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
			displayName := itemDisplayName(item)
			switch {
			case item.Skipped:
				suite.Cases = append(suite.Cases, junitTestCase{
					Name: displayName, Classname: classname, Time: duration,
					Skipped: &junitSkipped{Message: "skipped"},
				})
				suite.Tests++
			case item.Error != "":
				suite.Cases = append(suite.Cases, junitTestCase{
					Name: displayName, Classname: classname, Time: duration,
					Error: &junitFailure{Message: item.Error, Body: item.Error},
				})
				suite.Tests++
				suite.Errors++
			case len(item.Tests) > 0:
				for _, t := range item.Tests {
					tc := junitTestCase{Name: displayName + " :: " + t.Name, Classname: classname, Time: duration}
					suite.Tests++
					if !t.Passed {
						tc.Failure = &junitFailure{Message: t.Error, Body: t.Error}
						suite.Failures++
					}
					suite.Cases = append(suite.Cases, tc)
				}
			default:
				suite.Cases = append(suite.Cases, junitTestCase{Name: displayName, Classname: classname, Time: duration})
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
