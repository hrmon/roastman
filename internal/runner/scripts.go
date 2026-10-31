package runner

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/sobek"
)

// TestResult is one pm.test / assertion outcome.
type TestResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

type scriptState struct {
	runner      *Runner
	ctx         context.Context
	view        *View
	draft       *draft
	response    *Response
	tests       *[]TestResult
	console     *[]string
	nextSet     bool
	next        string
	skip        bool
	scriptName  string
	scriptError string
	iteration   int
	data        map[string]string
}

type scriptVM struct {
	vm    *sobek.Runtime
	state *scriptState
}

func compileScripts(c *Collection) error {
	cache := map[string]*sobek.Program{}
	var compileEvents func([]Event, string) error
	compileEvents = func(events []Event, location string) error {
		for i := range events {
			code := events[i].Script.Code()
			if strings.TrimSpace(code) == "" {
				continue
			}
			if p, ok := cache[code]; ok {
				events[i].Script.Program = p
				continue
			}
			// Execute every script in its own async function scope. Besides
			// supporting top-level await, this avoids `const`/`let`
			// redeclaration errors when several scripts in a run use the same
			// helper names, matching Postman's per-script sandbox scope.
			p, wrapErr := sobek.Compile("postman-script", wrapScript(code), false)
			if wrapErr != nil {
				// Compile the original source too, so syntax errors report the
				// author's line number rather than the wrapper's offset.
				if _, err := sobek.Compile("postman-script", code, false); err != nil {
					return fmt.Errorf("compile %s script for %s: %w\nscript source:\n%s",
						scriptEventType(events[i].Listen), location, err, indentSource(code))
				}
				return fmt.Errorf("compile %s script for %s: %w\nscript source:\n%s",
					scriptEventType(events[i].Listen), location, wrapErr, indentSource(code))
			}
			cache[code] = p
			events[i].Script.Program = p
		}
		return nil
	}
	if err := compileEvents(c.Event, "collection-level scripts"); err != nil {
		return err
	}
	var walk func(items []Item, path []string) error
	walk = func(items []Item, path []string) error {
		for i := range items {
			itemPath := append(append([]string{}, path...), items[i].Name)
			location := strings.Join(itemPath, "/")
			if err := compileEvents(items[i].Event, location); err != nil {
				return err
			}
			if err := walk(items[i].Item, itemPath); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(c.Item, nil)
}

func wrapScript(code string) string {
	return "(async function(){\n" + code + "\n})().catch(function(e){ __roast.scriptError(String(e && e.message ? e.message : e)); });"
}

func indentSource(code string) string {
	lines := strings.Split(code, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func newScriptVM(r *Runner, state *scriptState) (*scriptVM, error) {
	vm := sobek.New()
	roast := map[string]interface{}{
		"recordTest": func(name string, passed bool, message string) {
			if state.tests != nil {
				*state.tests = append(*state.tests, TestResult{Name: name, Passed: passed, Error: message})
			}
		},
		"scriptError": func(message string) {
			state.scriptError = message
		},
		"randomUUID": func() string { return uuid() },
		"sha1": func(value string) string {
			sum := sha1.Sum([]byte(value))
			return hex.EncodeToString(sum[:])
		},
		"sha256": func(value string) string {
			sum := sha256.Sum256([]byte(value))
			return hex.EncodeToString(sum[:])
		},
		"console": func(level, message string) {
			if state.console != nil {
				*state.console = append(*state.console, level+": "+message)
			}
		},
		"getVar": func(scope, key string) sobek.Value {
			if value, ok := getScope(state, scope).Get(key); ok {
				return vm.ToValue(value)
			}
			return sobek.Undefined()
		},
		"hasVar": func(scope, key string) bool {
			return getScope(state, scope).Has(key)
		},
		"setVar": func(scope, key, value string) {
			getScope(state, scope).Set(key, value)
		},
		"unsetVar":   func(scope, key string) { getScope(state, scope).Unset(key) },
		"clearScope": func(scope string) { getScope(state, scope).Clear() },
		"varsObject": func(scope string) map[string]string {
			return getScope(state, scope).ToMap()
		},
		"allVars": func() map[string]string {
			return state.view.All()
		},
		"replaceIn": func(text string) string { return state.view.Replace(text) },
		"sleep": func(ms int) {
			if ms > 0 {
				time.Sleep(time.Duration(ms) * time.Millisecond)
			}
		},
		"reqMethod": func() string {
			if state.draft == nil {
				return ""
			}
			return state.draft.Method
		},
		"reqURL": func() string {
			if state.draft == nil {
				return ""
			}
			return state.draft.URL
		},
		"reqSetURL": func(value string) {
			if state.draft != nil {
				state.draft.URL = value
			}
		},
		"reqSetMethod": func(value string) {
			if state.draft != nil {
				state.draft.Method = strings.ToUpper(value)
			}
		},
		"reqHeaderGet": func(key string) sobek.Value {
			if state.draft == nil {
				return sobek.Undefined()
			}
			for _, h := range state.draft.Headers {
				if strings.EqualFold(h.Key, key) {
					return vm.ToValue(h.Value)
				}
			}
			return sobek.Undefined()
		},
		"reqHeaderHas": func(key string) bool {
			if state.draft == nil {
				return false
			}
			for _, h := range state.draft.Headers {
				if strings.EqualFold(h.Key, key) {
					return true
				}
			}
			return false
		},
		"reqHeaderAdd": func(key, value string) {
			if state.draft != nil {
				state.draft.Headers = append(state.draft.Headers, Header{Key: key, Value: value})
			}
		},
		"reqHeaderUpsert": func(key, value string) {
			if state.draft == nil {
				return
			}
			for i := range state.draft.Headers {
				if strings.EqualFold(state.draft.Headers[i].Key, key) {
					state.draft.Headers[i].Value = value
					return
				}
			}
			state.draft.Headers = append(state.draft.Headers, Header{Key: key, Value: value})
		},
		"reqHeaderRemove": func(key string) {
			if state.draft == nil {
				return
			}
			out := state.draft.Headers[:0]
			for _, h := range state.draft.Headers {
				if !strings.EqualFold(h.Key, key) {
					out = append(out, h)
				}
			}
			state.draft.Headers = out
		},
		"reqHeadersObject": func() map[string]string {
			out := map[string]string{}
			if state.draft != nil {
				for _, h := range state.draft.Headers {
					out[h.Key] = h.Value
				}
			}
			return out
		},
		"reqBody": func() string {
			if state.draft == nil {
				return ""
			}
			return string(state.draft.Body)
		},
		"responseData": func() interface{} {
			if state.response == nil {
				return nil
			}
			headers := map[string]string{}
			for k, vals := range state.response.Headers {
				if len(vals) > 0 {
					headers[k] = vals[0]
				}
			}
			cookies := make([]map[string]string, 0, len(state.response.Cookies))
			for _, c := range state.response.Cookies {
				cookies = append(cookies, map[string]string{
					"name":   c.Name,
					"value":  c.Value,
					"domain": c.Domain,
					"path":   c.Path,
				})
			}
			return map[string]interface{}{
				"code":         state.response.Code,
				"status":       state.response.Status,
				"body":         state.response.Body,
				"headers":      headers,
				"responseTime": float64(state.response.ResponseTime.Milliseconds()),
				"size":         state.response.Size,
				"cookies":      cookies,
				"url":          state.response.URL,
				"method":       state.response.Method,
			}
		},
		"setNextRequest": func(value sobek.Value) {
			state.nextSet = true
			if value == nil || sobek.IsNull(value) || sobek.IsUndefined(value) {
				state.next = ""
				return
			}
			state.next = value.String()
		},
		"skipRequest": func() { state.skip = true },
	}
	roast["sendRequest"] = func(call sobek.FunctionCall) sobek.Value {
		res, err := state.runner.sendRequestFromScript(state, call.Argument(0))
		if err != nil {
			return vm.ToValue(map[string]interface{}{"error": err.Error()})
		}
		return vm.ToValue(res)
	}
	if err := vm.Set("__roast", roast); err != nil {
		return nil, err
	}
	if _, err := vm.RunString(pmBootstrap); err != nil {
		return nil, fmt.Errorf("initialize Postman sandbox: %w", err)
	}
	return &scriptVM{vm: vm, state: state}, nil
}

func getScope(state *scriptState, scope string) *VarStore {
	switch scope {
	case "environment":
		if state.view.Environment == nil {
			state.view.Environment = NewVarStore()
		}
		return state.view.Environment
	case "globals":
		if state.view.Globals == nil {
			state.view.Globals = NewVarStore()
		}
		return state.view.Globals
	case "collection", "collectionVariables":
		if state.view.Collection == nil {
			state.view.Collection = NewVarStore()
		}
		return state.view.Collection
	case "iterationData":
		store := NewVarStore()
		for k, v := range state.view.Data {
			store.Set(k, v)
		}
		return store
	default:
		if state.view.Local == nil {
			state.view.Local = NewVarStore()
		}
		return state.view.Local
	}
}

func (r *Runner) sendRequestFromScript(state *scriptState, arg sobek.Value) (map[string]interface{}, error) {
	exported := arg.Export()
	obj, ok := exported.(map[string]interface{})
	if !ok {
		if s, ok := exported.(string); ok {
			obj = map[string]interface{}{"url": s}
		} else {
			return nil, fmt.Errorf("pm.sendRequest expects a URL string or request object")
		}
	}
	urlVal, _ := obj["url"].(string)
	method, _ := obj["method"].(string)
	if method == "" {
		method = "GET"
	}
	d := &draft{Method: strings.ToUpper(method), URL: urlVal}
	var headers []Header
	switch h := obj["header"].(type) {
	case []interface{}:
		for _, item := range h {
			if m, ok := item.(map[string]interface{}); ok {
				key, _ := m["key"].(string)
				value, _ := m["value"].(string)
				headers = append(headers, Header{Key: key, Value: value})
			}
		}
	case map[string]interface{}:
		for k, v := range h {
			headers = append(headers, Header{Key: k, Value: valueAsString(v)})
		}
	}
	d.Headers = headers
	switch b := obj["body"].(type) {
	case string:
		d.Body = []byte(b)
	case map[string]interface{}:
		if raw, ok := b["raw"].(string); ok {
			d.Body = []byte(state.view.Replace(raw))
		}
	}
	d.URL = state.view.Replace(d.URL)
	for i := range d.Headers {
		d.Headers[i].Key = state.view.Replace(d.Headers[i].Key)
		d.Headers[i].Value = state.view.Replace(d.Headers[i].Value)
	}
	ctx := state.ctx
	cancel := func() {}
	if r.opts.TimeoutMS > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(r.opts.TimeoutMS)*time.Millisecond)
	}
	resp, err := r.send(ctx, d, nil)
	cancel()
	if err != nil {
		return nil, err
	}
	headersOut := map[string]string{}
	for k, vals := range resp.Headers {
		if len(vals) > 0 {
			headersOut[k] = vals[0]
		}
	}
	return map[string]interface{}{
		"code":         resp.Code,
		"status":       resp.Status,
		"body":         resp.Body,
		"headers":      headersOut,
		"responseTime": float64(resp.ResponseTime.Milliseconds()),
		"size":         resp.Size,
	}, nil
}

func (s *scriptVM) installLegacy(resp *Response, state *scriptState) {
	_ = s.vm.Set("responseBody", "")
	_ = s.vm.Set("responseCode", nil)
	_ = s.vm.Set("responseHeaders", map[string]string{})
	_ = s.vm.Set("responseTime", 0)
	_ = s.vm.Set("iteration", state.iteration)
	_ = s.vm.Set("data", state.data)
	if state.view != nil {
		_ = s.vm.Set("environment", state.view.Environment.ToMap())
		_ = s.vm.Set("globals", state.view.Globals.ToMap())
	}
	_ = s.vm.Set("postman", map[string]interface{}{
		"setEnvironmentVariable":   func(k, v string) { state.view.Environment.Set(k, v) },
		"getEnvironmentVariable":   func(k string) string { v, _ := state.view.Environment.Get(k); return v },
		"clearEnvironmentVariable": func(k string) { state.view.Environment.Unset(k) },
		"setGlobalVariable":        func(k, v string) { state.view.Globals.Set(k, v) },
		"getGlobalVariable":        func(k string) string { v, _ := state.view.Globals.Get(k); return v },
		"clearGlobalVariable":      func(k string) { state.view.Globals.Unset(k) },
		"setNextRequest": func(name sobek.Value) {
			state.nextSet = true
			if name == nil || sobek.IsNull(name) || sobek.IsUndefined(name) {
				state.next = ""
				return
			}
			state.next = name.String()
		},
	})
	if resp != nil {
		_ = s.vm.Set("responseBody", resp.Body)
		_ = s.vm.Set("responseCode", map[string]interface{}{
			"code": resp.Code, "name": resp.Status, "detail": resp.Status,
		})
		headers := map[string]string{}
		for k, vals := range resp.Headers {
			if len(vals) > 0 {
				headers[k] = vals[0]
			}
		}
		_ = s.vm.Set("responseHeaders", headers)
		_ = s.vm.Set("responseTime", resp.ResponseTime.Milliseconds())
	}
	// Legacy test arrays are collected after the script runs.
	_, _ = s.vm.RunString("var tests = {};")
}

func (s *scriptVM) collectLegacyTests(ir *ItemReport) {
	v := s.vm.Get("tests")
	if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
		return
	}
	obj, ok := v.(*sobek.Object)
	if !ok {
		return
	}
	for _, key := range obj.Keys() {
		val := obj.Get(key)
		if val == nil {
			continue
		}
		passed := val.ToBoolean()
		result := TestResult{Name: key, Passed: passed}
		if !passed {
			result.Error = "legacy test failed"
		}
		ir.Tests = append(ir.Tests, result)
	}
}

func (s *scriptVM) run(program *sobek.Program, name string, timeout time.Duration) error {
	s.state.scriptName = name
	s.state.scriptError = ""
	if program == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.AfterFunc(timeout, func() { s.vm.Interrupt("script timed out") })
	_, err := s.vm.RunProgram(program)
	timer.Stop()
	s.vm.ClearInterrupt()
	if err != nil {
		if _, ok := err.(*sobek.InterruptedError); ok {
			return fmt.Errorf("script timed out after %s", timeout)
		}
		return err
	}
	if s.state.scriptError != "" {
		return fmt.Errorf("%s", s.state.scriptError)
	}
	return nil
}

const pmBootstrap = `
var pm = {};
var __expect;

function __fail(message) {
    throw new Error(message);
}

function __stringValue(value) {
    return value === undefined || value === null ? '' : String(value);
}

function __deepEqual(a, b) {
    if (a === b) return true;
    if (a === null || b === null || a === undefined || b === undefined) return false;
    if (typeof a !== typeof b) return false;
    if (typeof a !== 'object') return false;
    if (Array.isArray(a) !== Array.isArray(b)) return false;
    if (Array.isArray(a)) {
        if (a.length !== b.length) return false;
        for (var i = 0; i < a.length; i++) if (!__deepEqual(a[i], b[i])) return false;
        return true;
    }
    var ka = Object.keys(a), kb = Object.keys(b);
    if (ka.length !== kb.length) return false;
    for (var j = 0; j < ka.length; j++) {
        if (!Object.prototype.hasOwnProperty.call(b, ka[j])) return false;
        if (!__deepEqual(a[ka[j]], b[ka[j]])) return false;
    }
    return true;
}

function __inspect(v) {
    try { return JSON.stringify(v); } catch (e) { return String(v); }
}

function __makeExpect(actual, negate) {
    var chain = {};
    function check(condition, message) {
        if (negate ? condition : !condition) __fail(message);
        return chain;
    }
    function method(name, fn) {
        chain[name] = function() { return fn.apply(null, arguments); };
    }
    Object.defineProperty(chain, 'not', { get: function() { return __makeExpect(actual, !negate); } });
    chain.to = chain; chain.be = chain; chain.been = chain; chain.is = chain;
    chain.that = chain; chain.which = chain; chain.and = chain; chain.has = chain;
    chain.have = chain; chain.with = chain; chain.at = chain; chain.of = chain; chain.same = chain;
    method('equal', function(expected) { return check(actual === expected, 'expected ' + __inspect(actual) + ' to equal ' + __inspect(expected)); });
    method('equals', chain.equal);
    method('eq', chain.equal);
    method('eql', function(expected) { return check(__deepEqual(actual, expected), 'expected ' + __inspect(actual) + ' to deeply equal ' + __inspect(expected)); });
    method('eqls', chain.eql);
    method('include', function(expected) {
        var ok = false;
        if (typeof actual === 'string') ok = actual.indexOf(expected) !== -1;
        else if (Array.isArray(actual)) ok = actual.some(function(x) { return __deepEqual(x, expected); });
        return check(ok, 'expected ' + __inspect(actual) + ' to include ' + __inspect(expected));
    });
    method('contain', chain.include);
    method('property', function(name, value) {
        var exists = actual !== null && actual !== undefined && Object.prototype.hasOwnProperty.call(Object(actual), name);
        if (arguments.length > 1) return check(exists && __deepEqual(actual[name], value), 'expected property ' + name + ' to equal ' + __inspect(value));
        return check(exists, 'expected object to have property ' + name);
    });
    method('lengthOf', function(n) {
        var len = actual !== null && actual !== undefined && actual.length !== undefined ? actual.length : Object.keys(actual || {}).length;
        return check(len === n, 'expected length ' + n + ' but got ' + len);
    });
    method('length', chain.lengthOf);
    method('above', function(n) { return check(actual > n, 'expected ' + __inspect(actual) + ' to be above ' + n); });
    method('below', function(n) { return check(actual < n, 'expected ' + __inspect(actual) + ' to be below ' + n); });
    method('least', function(n) { return check(actual >= n, 'expected ' + __inspect(actual) + ' to be at least ' + n); });
    method('most', function(n) { return check(actual <= n, 'expected ' + __inspect(actual) + ' to be at most ' + n); });
    method('match', function(re) { return check(re.test(String(actual)), 'expected ' + __inspect(actual) + ' to match ' + re); });
    method('oneOf', function(values) { return check(values.some(function(x) { return __deepEqual(actual, x); }), 'expected value to be one of ' + __inspect(values)); });
    method('members', function(values) {
        var ok = Array.isArray(actual) && actual.length === values.length && values.every(function(v) { return actual.some(function(a) { return __deepEqual(a, v); }); });
        return check(ok, 'expected members to match');
    });
    method('a', function(type) { var ok = type === 'array' ? Array.isArray(actual) : typeof actual === type; return check(ok, 'expected type ' + type); });
    method('an', chain.a);
    method('instanceof', function(ctor) { return check(actual instanceof ctor, 'expected instance of ' + ctor); });
    method('instanceOf', chain.instanceof);
    Object.defineProperty(chain, 'true', { get: function() { return check(actual === true, 'expected true'); } });
    Object.defineProperty(chain, 'false', { get: function() { return check(actual === false, 'expected false'); } });
    Object.defineProperty(chain, 'null', { get: function() { return check(actual === null, 'expected null'); } });
    Object.defineProperty(chain, 'undefined', { get: function() { return check(actual === undefined, 'expected undefined'); } });
    Object.defineProperty(chain, 'ok', { get: function() { return check(!!actual, 'expected truthy value'); } });
    Object.defineProperty(chain, 'empty', { get: function() {
        var ok = actual !== null && actual !== undefined && (actual.length === 0 || (typeof actual === 'object' && Object.keys(actual).length === 0));
        return check(ok, 'expected empty value');
    } });
    return chain;
}

pm.expect = function(value) { return __makeExpect(value, false); };

pm.test = function(name, fn) {
    try {
        if (typeof fn !== 'function') __fail('pm.test requires a function');
        fn();
        __roast.recordTest(String(name), true, '');
    } catch (e) {
        __roast.recordTest(String(name), false, String(e && e.message ? e.message : e));
    }
};

pm.environment = {
    get: function(key) { return __roast.getVar('environment', String(key)); },
    set: function(key, value) { __roast.setVar('environment', String(key), __stringValue(value)); },
    unset: function(key) { __roast.unsetVar('environment', String(key)); },
    clear: function() { __roast.clearScope('environment'); },
    has: function(key) { return __roast.hasVar('environment', String(key)); },
    toObject: function() { return __roast.varsObject('environment'); },
    replaceIn: function(text) { return __roast.replaceIn(String(text)); }
};
pm.globals = {
    get: function(key) { return __roast.getVar('globals', String(key)); },
    set: function(key, value) { __roast.setVar('globals', String(key), __stringValue(value)); },
    unset: function(key) { __roast.unsetVar('globals', String(key)); },
    clear: function() { __roast.clearScope('globals'); },
    has: function(key) { return __roast.hasVar('globals', String(key)); },
    toObject: function() { return __roast.varsObject('globals'); },
    replaceIn: function(text) { return __roast.replaceIn(String(text)); }
};
pm.collectionVariables = {
    get: function(key) { return __roast.getVar('collection', String(key)); },
    set: function(key, value) { __roast.setVar('collection', String(key), __stringValue(value)); },
    unset: function(key) { __roast.unsetVar('collection', String(key)); },
    clear: function() { __roast.clearScope('collection'); },
    has: function(key) { return __roast.hasVar('collection', String(key)); },
    toObject: function() { return __roast.varsObject('collection'); },
    replaceIn: function(text) { return __roast.replaceIn(String(text)); }
};
pm.variables = {
    get: function(key) { var v = __roast.allVars()[String(key)]; return v === undefined ? undefined : v; },
    set: function(key, value) { __roast.setVar('variables', String(key), __stringValue(value)); },
    unset: function(key) { __roast.unsetVar('variables', String(key)); },
    clear: function() { __roast.clearScope('variables'); },
    has: function(key) { return __roast.allVars()[String(key)] !== undefined; },
    toObject: function() { return __roast.allVars(); },
    replaceIn: function(text) { return __roast.replaceIn(String(text)); }
};
pm.iterationData = {
    get: function(key) { return __roast.getVar('iterationData', String(key)); },
    has: function(key) { return __roast.hasVar('iterationData', String(key)); },
    toObject: function() { return __roast.varsObject('iterationData'); }
};

function __headerObject() {
    return {
        get: function(key) { var v = __roast.reqHeaderGet(String(key)); return v === undefined ? undefined : v; },
        has: function(key) { return __roast.reqHeaderHas(String(key)); },
        add: function(arg, value) {
            if (arg && typeof arg === 'object') { value = arg.value; arg = arg.key; }
            __roast.reqHeaderAdd(String(arg), String(value));
        },
        upsert: function(arg, value) {
            if (arg && typeof arg === 'object') { value = arg.value; arg = arg.key; }
            __roast.reqHeaderUpsert(String(arg), String(value));
        },
        remove: function(key) { __roast.reqHeaderRemove(String(key)); },
        toObject: function() { return __roast.reqHeadersObject(); }
    };
}
Object.defineProperty(pm, 'request', {
    get: function() {
        var req = {};
        Object.defineProperty(req, 'method', { get: function() { return __roast.reqMethod(); }, set: function(v) { __roast.reqSetMethod(String(v)); } });
        Object.defineProperty(req, 'url', { get: function() { return __roast.reqURL(); }, set: function(v) { __roast.reqSetURL(String(v)); } });
        Object.defineProperty(req, 'headers', { get: function() { return __headerObject(); } });
        req.body = { raw: __roast.reqBody() };
        return req;
    }
});

function __makeResponse(data) {
    if (!data) return undefined;
    var response = {
        code: data.code,
        status: data.status,
        responseTime: data.responseTime,
        responseSize: data.size,
        text: function() { return data.body; },
        json: function() { return JSON.parse(data.body); },
        cookies: data.cookies || [],
        headers: {
            get: function(key) {
                var lower = String(key).toLowerCase();
                for (var k in data.headers) if (k.toLowerCase() === lower) return data.headers[k];
                return undefined;
            },
            has: function(key) {
                var lower = String(key).toLowerCase();
                for (var k in data.headers) if (k.toLowerCase() === lower) return true;
                return false;
            },
            toObject: function() { return data.headers; }
        }
    };
    response.to = {
        have: {
            status: function(code) {
                if (data.code !== code) __fail('expected response to have status code ' + code + ' but got ' + data.code);
            }
        },
        be: {}
    };
    response.to.have.statusCode = response.to.have.status;
    Object.defineProperty(response.to.be, 'ok', { get: function() {
        if (data.code < 200 || data.code >= 300) __fail('expected response to be ok but got ' + data.code);
        return true;
    } });
    return response;
}
Object.defineProperty(pm, 'response', { get: function() { return __makeResponse(__roast.responseData()); } });

pm.sendRequest = function(request, callback) {
    if (typeof request === 'string') request = { url: request };
    var data = __roast.sendRequest(request);
    if (data && data.error) {
        var err = new Error(data.error);
        if (typeof callback === 'function') callback(err, undefined);
        return undefined;
    }
    var response = __makeResponse(data);
    if (typeof callback === 'function') callback(null, response);
    return response;
};

pm.execution = {
    setNextRequest: function(name) { __roast.setNextRequest(name); },
    skipRequest: function() { __roast.skipRequest(); }
};

var crypto = {
    randomUUID: function() { return __roast.randomUUID(); }
};

var CryptoJS = {
    SHA1: function(value) {
        return { toString: function() { return __roast.sha1(__stringValue(value)); } };
    },
    SHA256: function(value) {
        return { toString: function() { return __roast.sha256(__stringValue(value)); } };
    }
};

var console = {
    log: function() { __roast.console('log', Array.prototype.slice.call(arguments).join(' ')); },
    info: function() { __roast.console('info', Array.prototype.slice.call(arguments).join(' ')); },
    warn: function() { __roast.console('warn', Array.prototype.slice.call(arguments).join(' ')); },
    error: function() { __roast.console('error', Array.prototype.slice.call(arguments).join(' ')); }
};

function setTimeout(fn, ms) {
    __roast.sleep(Number(ms) || 0);
    if (typeof fn === 'function') fn();
    return 0;
}
function setInterval() { __fail('setInterval is not supported'); }
`
