package runner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type draft struct {
	Name        string
	Method      string
	URL         string
	Headers     []Header
	Host        string
	Body        []byte
	ContentType string
}

// Response is an HTTP response snapshot used by scripts and reports.
type Response struct {
	Code          int
	Status        string
	Headers       http.Header
	Body          string
	ResponseTime  time.Duration
	Size          int64
	Truncated     bool
	Cookies       []*http.Cookie
	URL           string
	Method        string
	RequestHeader http.Header
	Timings       map[string]time.Duration
}

func (r *Runner) buildDraft(item *planItem, view *View) (*draft, error) {
	req := item.Request
	d := &draft{
		Name:    item.Name,
		Method:  strings.ToUpper(strings.TrimSpace(req.Method)),
		Headers: make([]Header, 0, len(req.Header)),
	}
	if d.Method == "" {
		d.Method = "GET"
	}

	raw := view.Replace(req.URL.String())
	if req.URL.Object != nil {
		for _, pv := range req.URL.Object.Variable {
			if pv.Key == "" {
				continue
			}
			val := view.Replace(pv.Value)
			raw = strings.ReplaceAll(raw, ":"+pv.Key, url.PathEscape(val))
		}
	}
	if req.URL.Object != nil && len(req.URL.Object.Query) > 0 {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse URL: %w", err)
		}
		var q strings.Builder
		first := true
		for _, param := range req.URL.Object.Query {
			if param.Disabled || param.Key == "" {
				continue
			}
			if !first {
				q.WriteByte('&')
			}
			first = false
			q.WriteString(url.QueryEscape(view.Replace(param.Key)))
			q.WriteByte('=')
			q.WriteString(url.QueryEscape(view.Replace(param.Value)))
		}
		u.RawQuery = q.String()
		raw = u.String()
	}
	d.URL = raw

	for _, h := range req.Header {
		if h.Disabled || strings.TrimSpace(h.Key) == "" {
			continue
		}
		key := view.Replace(h.Key)
		val := view.Replace(h.Value)
		if strings.EqualFold(key, "Host") {
			d.Host = val
			continue
		}
		d.Headers = append(d.Headers, Header{Key: key, Value: val})
	}

	if req.Body != nil && !req.Body.Disabled {
		body, contentType, err := r.buildBody(req.Body, view)
		if err != nil {
			return nil, err
		}
		d.Body = body
		d.ContentType = contentType
	}

	// Apply auth before pre-request scripts so scripts can override it.
	if len(item.Auth) > 0 {
		typ, values, err := parseAuth(item.Auth)
		if err != nil {
			return nil, err
		}
		for k, v := range values {
			values[k] = view.Replace(v)
		}
		if err := applyAuth(d, typ, values); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (r *Runner) buildBody(body *Body, view *View) ([]byte, string, error) {
	switch body.Mode {
	case "", "none":
		return nil, "", nil
	case "raw":
		return []byte(view.Replace(body.Raw)), body.contentType(), nil
	case "urlencoded":
		var b strings.Builder
		first := true
		for _, p := range body.URLEncoded {
			if p.Disabled || p.Key == "" {
				continue
			}
			if !first {
				b.WriteByte('&')
			}
			first = false
			b.WriteString(url.QueryEscape(view.Replace(p.Key)))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(view.Replace(p.Value)))
		}
		return []byte(b.String()), "application/x-www-form-urlencoded", nil
	case "formdata":
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for _, p := range body.FormData {
			if p.Disabled || p.Key == "" {
				continue
			}
			key := view.Replace(p.Key)
			if strings.EqualFold(p.Type, "file") {
				src := view.Replace(p.Src)
				if src == "" {
					src = view.Replace(p.Value)
				}
				var data []byte
				if src != "" {
					path := src
					if !filepath.IsAbs(path) {
						path = filepath.Join(r.collectionDir, filepath.FromSlash(path))
					}
					var err error
					data, err = os.ReadFile(path)
					if err != nil {
						return nil, "", fmt.Errorf("read form-data file %s: %w", src, err)
					}
				}
				part, err := mw.CreateFormFile(key, filepath.Base(src))
				if err != nil {
					return nil, "", err
				}
				if _, err := part.Write(data); err != nil {
					return nil, "", err
				}
				continue
			}
			if err := mw.WriteField(key, view.Replace(p.Value)); err != nil {
				return nil, "", err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), mw.FormDataContentType(), nil
	case "graphql":
		payload := map[string]any{"query": view.Replace(body.GraphQL.Query)}
		if body.GraphQL.Variables != "" {
			var vars any
			if err := json.Unmarshal([]byte(view.Replace(body.GraphQL.Variables)), &vars); err == nil {
				payload["variables"] = vars
			} else {
				payload["variables"] = view.Replace(body.GraphQL.Variables)
			}
		}
		if body.GraphQL.OperationName != "" {
			payload["operationName"] = view.Replace(body.GraphQL.OperationName)
		}
		data, err := json.Marshal(payload)
		return data, "application/json", err
	case "file":
		src := ""
		switch f := body.File.(type) {
		case string:
			src = f
		case map[string]any:
			src, _ = f["src"].(string)
		}
		src = view.Replace(src)
		if src == "" {
			return nil, "", nil
		}
		path := src
		if !filepath.IsAbs(path) {
			path = filepath.Join(r.collectionDir, filepath.FromSlash(path))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("read request body file %s: %w", src, err)
		}
		return data, "", nil
	default:
		return nil, "", fmt.Errorf("unsupported body mode %q", body.Mode)
	}
}

func applyAuth(d *draft, typ string, values map[string]string) error {
	setHeader := func(key, val string) {
		for i := range d.Headers {
			if strings.EqualFold(d.Headers[i].Key, key) {
				d.Headers[i].Value = val
				return
			}
		}
		d.Headers = append(d.Headers, Header{Key: key, Value: val})
	}
	switch strings.ToLower(typ) {
	case "", "noauth", "inherit":
		return nil
	case "bearer":
		setHeader("Authorization", "Bearer "+values["token"])
	case "basic":
		user := values["username"]
		pass := values["password"]
		setHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	case "apikey":
		key := values["key"]
		val := values["value"]
		in := strings.ToLower(values["in"])
		if in == "query" {
			u, err := url.Parse(d.URL)
			if err != nil {
				return err
			}
			q := u.Query()
			q.Set(key, val)
			u.RawQuery = q.Encode()
			d.URL = u.String()
		} else {
			setHeader(key, val)
		}
	case "oauth2":
		token := values["accessToken"]
		if token == "" {
			token = values["token"]
		}
		if token == "" {
			return fmt.Errorf("oauth2 auth without an access token is not supported offline; set accessToken or use a pre-request script")
		}
		setHeader("Authorization", "Bearer "+token)
	default:
		return fmt.Errorf("auth type %q is not supported yet", typ)
	}
	return nil
}

func (r *Runner) send(ctx context.Context, d *draft, settings map[string]any) (*Response, error) {
	if d.URL == "" {
		return nil, fmt.Errorf("request has an empty URL")
	}
	if !strings.Contains(d.URL, "://") {
		return nil, fmt.Errorf("URL %q has no scheme", d.URL)
	}
	parsed, err := url.Parse(d.URL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}

	var body io.Reader
	if len(d.Body) > 0 {
		body = bytes.NewReader(d.Body)
	}
	req, err := http.NewRequestWithContext(ctx, d.Method, d.URL, body)
	if err != nil {
		return nil, err
	}
	for _, h := range d.Headers {
		req.Header.Add(h.Key, h.Value)
	}
	if d.Host != "" {
		req.Host = d.Host
	}
	if d.ContentType != "" {
		if strings.HasPrefix(strings.ToLower(d.ContentType), "multipart/form-data") {
			req.Header.Set("Content-Type", d.ContentType)
		} else if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", d.ContentType)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "roastman/0.1")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}

	timings := map[string]time.Duration{}
	var start time.Time
	var dnsStart, connStart, tlsStart, firstByte time.Time
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				timings["dns"] = time.Since(dnsStart)
			}
		},
		ConnectStart: func(_, _ string) { connStart = time.Now() },
		ConnectDone: func(_, _ string, _ error) {
			if !connStart.IsZero() {
				timings["connect"] = time.Since(connStart)
			}
		},
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			if !tlsStart.IsZero() {
				timings["tls"] = time.Since(tlsStart)
			}
		},
		GotFirstResponseByte: func() {
			firstByte = time.Now()
			if !start.IsZero() {
				timings["ttfb"] = firstByte.Sub(start)
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	follow := true
	maxRedirects := 10
	if settings != nil {
		if v, ok := boolSetting(settings, "followRedirects"); ok {
			follow = v
		}
		if v, ok := intSetting(settings, "maxRedirects"); ok {
			maxRedirects = v
		}
	}
	client := &http.Client{
		Transport: r.transport,
		Jar:       r.jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !follow {
				return http.ErrUseLastResponse
			}
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
	start = time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	maxSize := r.opts.MaxResponseMB
	if maxSize <= 0 {
		maxSize = 20
	}
	limit := int64(maxSize) << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	elapsed := time.Since(start)
	out := &Response{
		Code:          resp.StatusCode,
		Status:        resp.Status,
		Headers:       resp.Header.Clone(),
		Body:          string(data),
		ResponseTime:  elapsed,
		Size:          int64(len(data)),
		Truncated:     truncated,
		URL:           d.URL,
		Method:        d.Method,
		RequestHeader: req.Header.Clone(),
		Timings:       timings,
	}
	if r.jar != nil {
		out.Cookies = r.jar.Cookies(parsed)
	}
	return out, nil
}

func boolSetting(m map[string]any, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	v, ok := m[key]
	if !ok {
		return false, false
	}
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(x)
		return b, err == nil
	}
	return false, false
}

func intSetting(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	}
	return 0, false
}
