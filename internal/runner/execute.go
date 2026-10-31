package runner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

type planItem struct {
	Name       string
	ID         string
	Path       []string
	Request    *Request
	Auth       json.RawMessage
	FolderVars map[string]string
	FolderIDs  []string
	PreEvents  []Event
	TestEvents []Event
	Settings   map[string]any
}

// Runner is an offline collection runner.
type Runner struct {
	opts          Options
	loaded        *loaded
	collectionDir string
	plan          []*planItem
	lookup        map[string]int
	collectionVar *VarStore
	globals       *VarStore
	environment   *VarStore
	transport     *http.Transport
	jar           http.CookieJar
	started       time.Time
}

// New loads a collection, environment, globals, data, and compiles scripts.
func New(opts Options) (*Runner, error) {
	l, err := load(opts)
	if err != nil {
		return nil, err
	}
	if !opts.NoScripts {
		if err := compileScripts(l.collection); err != nil {
			return nil, fmt.Errorf("collection %s: %w", l.collectionDir, err)
		}
	}
	collectionVars := NewVarStore()
	for _, v := range l.collection.Variable {
		collectionVars.SetEnabled(v.Key, v.Value, !v.Disabled)
	}
	globals := l.globals
	if globals == nil {
		globals = NewVarStore()
	}
	environment := l.environment
	if environment == nil {
		environment = NewVarStore()
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if opts.Insecure {
		tlsConfig.InsecureSkipVerify = true
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       tlsConfig,
	}
	r := &Runner{
		opts:          opts,
		loaded:        l,
		collectionDir: l.collectionDir,
		collectionVar: collectionVars,
		globals:       globals,
		environment:   environment,
		transport:     transport,
		jar:           jar,
		lookup:        map[string]int{},
	}
	fullPlan := buildPlan(l.collection)
	selectedPlan, err := selectPlan(fullPlan, opts)
	if err != nil {
		return nil, err
	}
	if len(selectedPlan) == 0 {
		return nil, fmt.Errorf("no requests selected")
	}
	r.plan = selectedPlan
	r.lookup = buildLookup(selectedPlan)
	return r, nil
}

func buildPlan(c *Collection) []*planItem {
	collectionPre, collectionTest := splitEvents(c.Event)
	var out []*planItem
	var walk func(items []Item, path []string, folderIDs []string, folderVars map[string]string, pre, test []Event, auth json.RawMessage, settings map[string]any)
	walk = func(items []Item, path []string, folderIDs []string, folderVars map[string]string, pre, test []Event, auth json.RawMessage, settings map[string]any) {
		for i := range items {
			item := &items[i]
			itemPath := append(append([]string{}, path...), item.Name)
			if item.Request != nil {
				effective := effectiveAuth(item.Auth, auth)
				effective = effectiveAuth(item.Request.Auth, effective)
				localPre, localTest := splitEvents(item.Event)
				pi := &planItem{
					Name:       item.Name,
					ID:         item.ID,
					Path:       itemPath,
					Request:    item.Request,
					Auth:       effective,
					FolderVars: copyMap(folderVars),
					FolderIDs:  append([]string{}, folderIDs...),
					PreEvents:  append(append(append([]Event{}, collectionPre...), pre...), localPre...),
					TestEvents: append(append(append([]Event{}, localTest...), test...), collectionTest...),
					Settings:   mergeSettings(settings, item.ProtocolProfileBehavior),
				}
				out = append(out, pi)
				continue
			}
			if len(item.Item) == 0 {
				continue
			}
			fvars := copyMap(folderVars)
			for _, v := range item.Variable {
				if !v.Disabled {
					fvars[v.Key] = v.Value
				}
			}
			fpre, ftest := splitEvents(item.Event)
			nextPre := append(append([]Event{}, pre...), fpre...)
			nextTest := append(append([]Event{}, ftest...), test...)
			nextIDs := append(append([]string{}, folderIDs...), item.ID)
			walk(item.Item, itemPath, nextIDs, fvars, nextPre, nextTest, effectiveAuth(item.Auth, auth), mergeSettings(settings, item.ProtocolProfileBehavior))
		}
	}
	walk(c.Item, nil, nil, map[string]string{}, nil, nil, c.Auth, c.ProtocolProfileBehavior)
	return out
}

func splitEvents(events []Event) (pre, test []Event) {
	for _, e := range events {
		switch scriptEventType(e.Listen) {
		case "prerequest":
			pre = append(pre, e)
		case "test":
			test = append(test, e)
		}
	}
	return pre, test
}

func effectiveAuth(child, parent json.RawMessage) json.RawMessage {
	if len(child) == 0 || string(child) == "null" {
		return parent
	}
	typ, _, err := parseAuth(child)
	if err == nil && typ == "inherit" {
		return parent
	}
	return child
}

func mergeSettings(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func buildLookup(plan []*planItem) map[string]int {
	lookup := map[string]int{}
	for idx, item := range plan {
		if item.ID != "" {
			if _, ok := lookup[item.ID]; !ok {
				lookup[item.ID] = idx
			}
		}
		if _, ok := lookup[item.Name]; !ok {
			lookup[item.Name] = idx
		}
		joined := strings.Join(item.Path, "/")
		if _, ok := lookup[joined]; !ok {
			lookup[joined] = idx
		}
	}
	return lookup
}

type folderRef struct {
	path string
	ids  []string
}

func selectPlan(plan []*planItem, opts Options) ([]*planItem, error) {
	if len(opts.Items) == 0 && len(opts.Folders) == 0 && len(opts.Requests) == 0 {
		return plan, nil
	}
	if len(opts.Items) > 0 {
		folders := collectFolders(plan)
		var selected []*planItem
		seen := map[int]bool{}
		for _, selector := range opts.Items {
			indexes, err := resolveSelector(plan, folders, selector)
			if err != nil {
				return nil, err
			}
			for _, idx := range indexes {
				if seen[idx] {
					continue
				}
				seen[idx] = true
				selected = append(selected, plan[idx])
			}
		}
		return selected, nil
	}
	var selected []*planItem
	for _, item := range plan {
		if len(opts.Requests) > 0 {
			ok := false
			full := strings.Join(item.Path, "/")
			for _, want := range opts.Requests {
				if want == item.Name || want == full {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if len(opts.Folders) > 0 {
			ok := false
			for _, folder := range opts.Folders {
				for _, part := range item.Path[:len(item.Path)-1] {
					if part == folder {
						ok = true
						break
					}
				}
				if ok {
					break
				}
			}
			if !ok {
				continue
			}
		}
		selected = append(selected, item)
	}
	return selected, nil
}

func collectFolders(plan []*planItem) []folderRef {
	seen := map[string]bool{}
	var refs []folderRef
	for _, item := range plan {
		for depth := 0; depth < len(item.Path)-1; depth++ {
			p := strings.Join(item.Path[:depth+1], "/")
			if seen[p] {
				continue
			}
			seen[p] = true
			ref := folderRef{path: p}
			if depth < len(item.FolderIDs) && item.FolderIDs[depth] != "" {
				ref.ids = append(ref.ids, item.FolderIDs[depth])
			}
			refs = append(refs, ref)
		}
	}
	return refs
}

func resolveSelector(plan []*planItem, folders []folderRef, selector string) ([]int, error) {
	request := strings.LastIndexByte(selector, '/')
	if request >= 0 {
		// Prefer an exact request path.
		for i, item := range plan {
			if strings.Join(item.Path, "/") == selector || item.ID == selector {
				return []int{i}, nil
			}
		}
	}
	for i, item := range plan {
		if item.Name == selector || item.ID == selector {
			return []int{i}, nil
		}
	}
	for _, folder := range folders {
		matched := folder.path == selector
		if !matched {
			parts := strings.Split(folder.path, "/")
			matched = parts[len(parts)-1] == selector
		}
		if !matched {
			for _, id := range folder.ids {
				if id == selector {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}
		var indexes []int
		prefix := folder.path + "/"
		for i, item := range plan {
			itemFolder := strings.Join(item.Path[:len(item.Path)-1], "/")
			if itemFolder == folder.path || strings.HasPrefix(itemFolder, prefix) {
				indexes = append(indexes, i)
			}
		}
		return indexes, nil
	}
	return nil, fmt.Errorf("item %q not found (use a request name, folder name, UID, or path)", selector)
}

func (r *Runner) iterations() int {
	if r.opts.Iterations > 0 {
		return r.opts.Iterations
	}
	if len(r.loaded.dataRows) > 0 {
		return len(r.loaded.dataRows)
	}
	return 1
}

// Run executes the collection and returns a report. A non-nil error is
// returned only for runner-level failures; test/request failures are in the
// report.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	r.started = time.Now()
	report := &Report{
		Collection:  r.loaded.collection.Info.Name,
		Environment: r.loaded.environmentName,
		StartedAt:   r.started,
		Iterations:  r.iterations(),
	}
	iterations := report.Iterations
	for iteration := 0; iteration < iterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		var dataRow map[string]string
		if len(r.loaded.dataRows) > 0 {
			dataRow = r.loaded.dataRows[iteration%len(r.loaded.dataRows)]
		}
		local := NewVarStore()
		state := &scriptState{iteration: iteration + 1, data: dataRow}
		var vm *scriptVM
		if !r.opts.NoScripts {
			var err error
			vm, err = newScriptVM(r, state)
			if err != nil {
				return report, err
			}
		}
		index := 0
		requestCount := 0
		for index >= 0 && index < len(r.plan) {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			if requestCount > 100000 {
				return report, fmt.Errorf("request loop exceeded 100000 items")
			}
			requestCount++
			item := r.plan[index]
			itemStart := time.Now()
			ir := ItemReport{
				Iteration: iteration + 1,
				Name:      item.Name,
				Folder:    strings.Join(item.Path[:len(item.Path)-1], "/"),
				Method:    strings.ToUpper(item.Request.Method),
			}
			view := &View{
				Globals:     r.globals,
				Collection:  r.collectionVar,
				Folder:      item.FolderVars,
				Environment: r.environment,
				Data:        dataRow,
				Local:       local,
			}
			draft, err := r.buildDraft(item, view)
			if err != nil {
				ir.Error = err.Error()
				ir.Failed = true
				ir.DurationMS = time.Since(itemStart).Milliseconds()
				r.emitItem(report, ir)
				report.Errors++
				if r.opts.Bail {
					index = -1
					break
				}
				index++
				continue
			}

			state.runner = r
			state.ctx = ctx
			state.view = view
			state.draft = draft
			state.response = nil
			state.tests = nil
			state.console = nil
			state.nextSet = false
			state.next = ""
			state.skip = false

			if !r.opts.NoScripts {
				state.tests = &ir.Tests
				state.console = &ir.Console
				if len(item.PreEvents) > 0 {
					initial := cloneDraft(draft)
					preErr := runEvents(vm, item.PreEvents, draft, nil, &ir, state)
					if preErr != nil {
						ir.Tests = append(ir.Tests, TestResult{Name: "pre-request script", Passed: false, Error: preErr.Error()})
					}
					if rebuilt, rebuildErr := r.buildDraft(item, view); rebuildErr == nil {
						draft = applyScriptChanges(rebuilt, initial, draft)
					}
				}
				if state.skip {
					ir.Method = draft.Method
					ir.URL = draft.URL
					ir.Skipped = true
					ir.DurationMS = time.Since(itemStart).Milliseconds()
					tallyItem(report, &ir)
					r.emitItem(report, ir)
					report.Skipped++
					index, err = r.nextIndex(item, state, index, report)
					if err != nil {
						return report, err
					}
					continue
				}
			}

			ir.Method = draft.Method
			ir.URL = draft.URL

			// Missing URL variables cannot produce a meaningful request. Treat
			// them as warnings and skip just this item instead of failing the
			// whole run.
			if missing := unresolvedVariables(draft.URL); len(missing) > 0 {
				warn := fmt.Sprintf("unresolved URL variable(s): %s", strings.Join(missing, ", "))
				ir.Warnings = append(ir.Warnings, warn)
				ir.Skipped = true
				ir.DurationMS = time.Since(itemStart).Milliseconds()
				if r.opts.ReportBodies {
					ir.RequestHeaders = draftHeaderMap(draft.Headers)
					ir.RequestBody = string(draft.Body)
				}
				report.Warnings++
				report.Skipped++
				tallyItem(report, &ir)
				r.emitItem(report, ir)
				if r.opts.DelayMS > 0 {
					select {
					case <-ctx.Done():
						return report, ctx.Err()
					case <-time.After(time.Duration(r.opts.DelayMS) * time.Millisecond):
					}
				}
				index, err = r.nextIndex(item, state, index, report)
				if err != nil {
					return report, err
				}
				continue
			}
			for _, key := range unresolvedHeaderVariables(draft.Headers) {
				ir.Warnings = append(ir.Warnings, "unresolved header variable: "+key)
			}
			for _, key := range unresolvedVariables(string(draft.Body)) {
				ir.Warnings = append(ir.Warnings, "unresolved body variable: "+key)
			}
			report.Warnings += len(ir.Warnings)

			if r.opts.ReportBodies {
				ir.RequestHeaders = draftHeaderMap(draft.Headers)
				ir.RequestBody = string(draft.Body)
			}
			reqCtx := ctx
			cancel := func() {}
			if r.opts.TimeoutMS > 0 {
				reqCtx, cancel = context.WithTimeout(ctx, time.Duration(r.opts.TimeoutMS)*time.Millisecond)
			}
			resp, sendErr := r.send(reqCtx, draft, item.Settings)
			cancel()
			ir.URL = draft.URL
			ir.Method = draft.Method
			ir.DurationMS = time.Since(itemStart).Milliseconds()
			report.Requests++
			if sendErr != nil {
				ir.Error = sendErr.Error()
				ir.Failed = true
				report.Errors++
			} else {
				ir.Status = resp.Status
				ir.Code = resp.Code
				ir.ResponseTimeMS = resp.ResponseTime.Milliseconds()
				ir.ResponseSize = resp.Size
				if r.opts.ReportBodies {
					ir.RequestHeaders = headerMap(resp.RequestHeader)
					ir.RequestBody = string(draft.Body)
					ir.ResponseHeaders = headerMap(resp.Headers)
					ir.ResponseBody = resp.Body
				}
				state.response = resp
				if !r.opts.NoScripts {
					testErr := runEvents(vm, item.TestEvents, draft, resp, &ir, state)
					if testErr != nil {
						ir.Tests = append(ir.Tests, TestResult{Name: "test script", Passed: false, Error: testErr.Error()})
					}
				}
				ir.DurationMS = time.Since(itemStart).Milliseconds()
			}
			tallyItem(report, &ir)
			r.emitItem(report, ir)
			if r.opts.Bail && ir.Failed {
				index = -1
				break
			}
			if r.opts.DelayMS > 0 {
				select {
				case <-ctx.Done():
					return report, ctx.Err()
				case <-time.After(time.Duration(r.opts.DelayMS) * time.Millisecond):
				}
			}
			index, err = r.nextIndex(item, state, index, report)
			if err != nil {
				return report, err
			}
		}
	}
	report.DurationMS = time.Since(r.started).Milliseconds()
	report.FinishedAt = time.Now()
	return report, nil
}

func unresolvedVariables(input string) []string {
	matches := templateVar.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		key := strings.TrimSpace(m[1])
		if key == "" || strings.HasPrefix(key, "$") || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

func unresolvedHeaderVariables(headers []Header) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range headers {
		for _, key := range unresolvedVariables(h.Key + "\n" + h.Value) {
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func draftHeaderMap(headers []Header) map[string][]string {
	out := map[string][]string{}
	for _, h := range headers {
		out[h.Key] = append(out[h.Key], h.Value)
	}
	return out
}

func headerMap(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for k, v := range headers {
		out[k] = append([]string{}, v...)
	}
	return out
}

func (r *Runner) emitItem(report *Report, ir ItemReport) {
	report.Items = append(report.Items, ir)
	if r.opts.OnItem != nil {
		r.opts.OnItem(ir)
	}
}

func tallyItem(report *Report, ir *ItemReport) {
	for _, t := range ir.Tests {
		report.Assertions++
		if !t.Passed {
			report.Failures++
			ir.Failed = true
		}
	}
	if ir.Error != "" {
		ir.Failed = true
	}
}

func cloneDraft(d *draft) *draft {
	if d == nil {
		return nil
	}
	out := *d
	out.Headers = append([]Header{}, d.Headers...)
	out.Body = append([]byte{}, d.Body...)
	return &out
}

// applyScriptChanges keeps explicit pm.request changes while allowing
// pre-request variable assignments to affect the rebuilt URL/body.
func applyScriptChanges(rebuilt, initial, scripted *draft) *draft {
	if rebuilt == nil || initial == nil || scripted == nil {
		return rebuilt
	}
	if scripted.URL != initial.URL {
		rebuilt.URL = scripted.URL
	}
	if scripted.Method != initial.Method {
		rebuilt.Method = scripted.Method
	}
	initialHeaders := map[string]string{}
	for _, h := range initial.Headers {
		initialHeaders[strings.ToLower(h.Key)] = h.Value
	}
	scriptedHeaders := map[string]string{}
	for _, h := range scripted.Headers {
		scriptedHeaders[strings.ToLower(h.Key)] = h.Value
	}
	for key, value := range scriptedHeaders {
		if old, ok := initialHeaders[key]; !ok || old != value {
			upsertDraftHeader(rebuilt, key, value)
		}
	}
	for key := range initialHeaders {
		if _, ok := scriptedHeaders[key]; !ok {
			removeDraftHeader(rebuilt, key)
		}
	}
	return rebuilt
}

func upsertDraftHeader(d *draft, key, value string) {
	for i := range d.Headers {
		if strings.EqualFold(d.Headers[i].Key, key) {
			d.Headers[i].Value = value
			return
		}
	}
	d.Headers = append(d.Headers, Header{Key: key, Value: value})
}

func removeDraftHeader(d *draft, key string) {
	out := d.Headers[:0]
	for _, h := range d.Headers {
		if !strings.EqualFold(h.Key, key) {
			out = append(out, h)
		}
	}
	d.Headers = out
}

func (r *Runner) nextIndex(item *planItem, state *scriptState, index int, report *Report) (int, error) {
	if !state.nextSet {
		return index + 1, nil
	}
	if state.next == "" {
		return -1, nil
	}
	next, ok := r.lookup[state.next]
	if !ok {
		return -1, fmt.Errorf("setNextRequest: request %q not found", state.next)
	}
	return next, nil
}

func runEvents(vm *scriptVM, events []Event, draft *draft, resp *Response, ir *ItemReport, state *scriptState) error {
	for _, event := range events {
		typ := scriptEventType(event.Listen)
		if typ != "prerequest" && typ != "test" {
			continue
		}
		if event.Script.Program == nil {
			continue
		}
		state.draft = draft
		state.response = resp
		state.tests = &ir.Tests
		state.console = &ir.Console
		vm.installLegacy(resp, state)
		err := vm.run(event.Script.Program, event.Listen, 5*time.Second)
		vm.collectLegacyTests(ir)
		if err != nil {
			return err
		}
	}
	return nil
}

// CollectionDir returns the resolved collection directory.
func (r *Runner) CollectionDir() string { return r.collectionDir }

// CollectionName returns the collection name.
func (r *Runner) CollectionName() string { return r.loaded.collection.Info.Name }

// EnvironmentName returns the loaded environment name.
func (r *Runner) EnvironmentName() string { return r.loaded.environmentName }
