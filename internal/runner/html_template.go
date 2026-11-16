package runner

// htmlReportTemplateSource is the self-contained report document. Styles and
// scripts are inline so a report can be opened from disk without network
// access. Keep the markup free of external assets.
const htmlReportTemplateSource = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'">
<title>{{.DocumentTitle}}</title>
<link rel="icon" href="data:,">
<script>
(function () {
  var root = document.documentElement;
  root.classList.add('js');
  try {
    var saved = localStorage.getItem('roastman-theme');
    if (saved === 'light' || saved === 'dark') { root.setAttribute('data-theme', saved); }
  } catch (e) {}
})();
</script>
<style>
:root {
  color-scheme: light dark;
  --font: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  --mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  --bg: #f4f5f7;
  --panel: #ffffff;
  --code-bg: #f7f8fa;
  --text: #181b1f;
  --muted: #5f6873;
  --border: #dde1e6;
  --line: #eef0f3;
  --accent: #b45309;
  --green: #166534;
  --green-bg: #e8f5ec;
  --red: #b42318;
  --red-bg: #fdeceb;
  --amber: #8a5a00;
  --amber-bg: #fff7e0;
  --blue: #1d4ed8;
  --blue-bg: #e9effc;
  --violet: #6b21a8;
  --violet-bg: #f3e8ff;
  --shadow: 0 1px 2px rgb(15 23 42 / 0.05), 0 10px 30px rgb(15 23 42 / 0.06);
}
@supports (color: light-dark(#000, #fff)) {
  :root {
    --bg: light-dark(#f4f5f7, #0d1013);
    --panel: light-dark(#ffffff, #15191e);
    --code-bg: light-dark(#f7f8fa, #0f1317);
    --text: light-dark(#181b1f, #e6e9ed);
    --muted: light-dark(#5f6873, #9aa4af);
    --border: light-dark(#dde1e6, #262c33);
    --line: light-dark(#eef0f3, #1e242a);
    --accent: light-dark(#b45309, #f59e0b);
    --green: light-dark(#166534, #4ade80);
    --green-bg: light-dark(#e8f5ec, #12301d);
    --red: light-dark(#b42318, #f87171);
    --red-bg: light-dark(#fdeceb, #3a1717);
    --amber: light-dark(#8a5a00, #fbbf24);
    --amber-bg: light-dark(#fff7e0, #33260a);
    --blue: light-dark(#1d4ed8, #93b4ff);
    --blue-bg: light-dark(#e9effc, #14213d);
    --violet: light-dark(#6b21a8, #c4a5f5);
    --violet-bg: light-dark(#f3e8ff, #251538);
    --shadow: 0 1px 2px light-dark(rgb(15 23 42 / 0.05), rgb(0 0 0 / 0.4)), 0 10px 30px light-dark(rgb(15 23 42 / 0.06), rgb(0 0 0 / 0.35));
  }
}
:root[data-theme="light"] { color-scheme: light; }
:root[data-theme="dark"] { color-scheme: dark; }
*, *::before, *::after { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body {
  margin: 0;
  font-family: var(--font);
  font-size: 15px;
  line-height: 1.5;
  background: var(--bg);
  color: var(--text);
}
h1, h2, h3, h4, h5, p, ul, ol { margin: 0; }
button, input { font: inherit; color: inherit; }
button { cursor: pointer; }
a { color: var(--accent); }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.wrap { width: min(1180px, calc(100% - 40px)); margin-inline: auto; }
.sr-only {
  position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px;
  overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0;
}
.masthead {
  background:
    radial-gradient(900px 420px at 88% -30%, rgb(249 115 22 / 0.38), transparent 60%),
    linear-gradient(135deg, #1c1917 0%, #292524 58%, #431407 100%);
  color: #fafaf9;
  padding: 34px 0 52px;
}
.masthead-top { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; }
.eyebrow {
  margin-bottom: 7px;
  color: #fdba74;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.17em;
  text-transform: uppercase;
}
.masthead h1 {
  font-size: clamp(24px, 4vw, 36px);
  line-height: 1.15;
  letter-spacing: -0.025em;
  overflow-wrap: anywhere;
}
.verdict {
  flex: none;
  padding: 7px 14px;
  border: 1px solid transparent;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.02em;
  white-space: nowrap;
}
.verdict.good { color: #bbf7d0; background: rgb(20 83 45 / 0.55); border-color: rgb(74 222 128 / 0.45); }
.verdict.warn { color: #fde68a; background: rgb(120 53 15 / 0.55); border-color: rgb(251 191 36 / 0.45); }
.verdict.bad { color: #fecaca; background: rgb(127 29 29 / 0.55); border-color: rgb(248 113 113 / 0.5); }
.run-meta { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 20px; }
.meta-chip {
  display: inline-flex;
  align-items: baseline;
  gap: 7px;
  padding: 5px 10px;
  border: 1px solid rgb(255 255 255 / 0.14);
  border-radius: 8px;
  background: rgb(255 255 255 / 0.07);
  color: #d6d3d1;
  font-size: 12px;
}
.meta-chip strong { color: #fafaf9; font-weight: 600; }
.stats {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(155px, 1fr));
  gap: 12px;
  margin-top: -28px;
}
.stat {
  padding: 15px 16px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--panel);
  box-shadow: var(--shadow);
}
.stat-value {
  display: block;
  font-size: 27px;
  font-weight: 700;
  line-height: 1.1;
  letter-spacing: -0.03em;
  font-variant-numeric: tabular-nums;
}
.stat-label {
  display: block;
  margin-top: 3px;
  color: var(--muted);
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.09em;
  text-transform: uppercase;
}
.stat-detail { display: block; margin-top: 5px; color: var(--muted); font-size: 12px; }
.stat.bad .stat-value { color: var(--red); }
.stat.warn .stat-value { color: var(--amber); }
.stat.muted .stat-value { color: var(--muted); }
.recap {
  margin-top: 22px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--panel);
  box-shadow: var(--shadow);
}
.recap-head {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--red-bg);
}
.recap-head h2 { color: var(--red); font-size: 12px; letter-spacing: 0.09em; text-transform: uppercase; }
.recap-icon { color: var(--red); font-weight: 700; }
.recap ol { margin: 0; padding: 0; list-style: none; }
.recap li { padding: 11px 16px; border-top: 1px solid var(--line); }
.recap li:first-child { border-top: 0; }
.recap a {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
}
.recap a:hover .recap-path { text-decoration: underline; }
.recap-path { overflow-wrap: anywhere; }
.recap-error {
  margin-top: 5px;
  color: var(--red);
  font-family: var(--mono);
  font-size: 12px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.recap-test { display: flex; align-items: flex-start; gap: 7px; margin-top: 8px; font-size: 13px; }
.recap-test-body { display: flex; flex: 1 1 auto; flex-direction: column; gap: 4px; min-width: 0; }
.toolbar {
  display: none;
  position: sticky;
  top: 0;
  z-index: 30;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 20px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}
.js .toolbar { display: flex; }
@supports (backdrop-filter: blur(1px)) {
  .toolbar {
    background: color-mix(in srgb, var(--bg) 86%, transparent);
    backdrop-filter: blur(10px);
  }
}
.search { position: relative; flex: 1 1 240px; min-width: 190px; }
.search::before {
  content: "⌕";
  position: absolute;
  top: 50%;
  left: 11px;
  color: var(--muted);
  font-size: 17px;
  transform: translateY(-52%);
  pointer-events: none;
}
.search input {
  width: 100%;
  padding: 9px 12px 9px 33px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--panel);
  font-size: 14px;
}
.search input::placeholder { color: var(--muted); }
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
  padding: 3px;
  border-radius: 10px;
  background: var(--line);
}
.filters button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--muted);
  font-size: 12.5px;
  font-weight: 600;
}
.filters button:hover { color: var(--text); }
.filters button.active { background: var(--panel); color: var(--text); box-shadow: 0 1px 2px rgb(0 0 0 / 0.08); }
.filters .count {
  padding: 1px 5px;
  border-radius: 5px;
  background: var(--bg);
  color: var(--muted);
  font-size: 10.5px;
  font-variant-numeric: tabular-nums;
}
.toolbar-actions { display: flex; gap: 6px; margin-left: auto; }
.toolbar-actions button {
  padding: 7px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
}
.toolbar-actions button:hover { border-color: var(--muted); color: var(--text); }
#theme-toggle { min-width: 34px; font-size: 14px; }
.result-count {
  display: none;
  margin-top: 8px;
  color: var(--muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.js .result-count { display: block; }
.empty {
  margin-top: 16px;
  padding: 34px 20px;
  border: 1px dashed var(--border);
  border-radius: 12px;
  color: var(--muted);
  text-align: center;
}
.empty[hidden] { display: none !important; }
.group { margin-top: 22px; }
.group[hidden] { display: none !important; }
.group-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  padding: 0 4px 8px;
  border-bottom: 1px solid var(--border);
}
.group-head h2 { font-size: 15px; letter-spacing: -0.01em; overflow-wrap: anywhere; }
.group-meta { color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.items { display: grid; gap: 8px; }
.item {
  overflow: hidden;
  border: 1px solid var(--border);
  border-left: 3px solid transparent;
  border-radius: 11px;
  background: var(--panel);
  box-shadow: 0 1px 2px rgb(0 0 0 / 0.03);
  scroll-margin-top: var(--toolbar-offset, 76px);
}
.item[hidden] { display: none !important; }
.item[data-state="passed"] { border-left-color: var(--green); }
.item[data-state="failed"] { border-left-color: var(--red); }
.item[data-state="warning"] { border-left-color: var(--amber); }
.item[data-state="skipped"] { border-left-color: var(--muted); }
.item summary {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 14px;
  list-style: none;
  cursor: pointer;
}
.item summary::-webkit-details-marker { display: none; }
.item summary:hover { background: var(--line); }
.item[open] summary { border-bottom: 1px solid var(--line); }
.chev { flex: none; width: 10px; color: var(--muted); font-size: 9px; text-align: center; transition: transform 0.15s ease; }
.item[open] .chev { transform: rotate(90deg); }
.state-icon { flex: none; width: 18px; font-size: 14px; font-weight: 700; text-align: center; }
.state-icon.passed { color: var(--green); }
.state-icon.failed { color: var(--red); }
.state-icon.warning { color: var(--amber); }
.state-icon.skipped { color: var(--muted); }
.summary-main { display: flex; flex: 1 1 auto; flex-direction: column; gap: 1px; min-width: 0; }
.item-name { overflow: hidden; font-size: 14px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.item-url {
  overflow: hidden;
  color: var(--muted);
  font-family: var(--mono);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.iter { flex: none; color: var(--muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.method {
  flex: none;
  padding: 3px 6px;
  border-radius: 5px;
  background: var(--line);
  color: var(--muted);
  font-family: var(--mono);
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.04em;
}
.method.m-get { background: var(--blue-bg); color: var(--blue); }
.method.m-post { background: var(--green-bg); color: var(--green); }
.method.m-put { background: var(--amber-bg); color: var(--amber); }
.method.m-patch { background: var(--violet-bg); color: var(--violet); }
.method.m-delete { background: var(--red-bg); color: var(--red); }
.code {
  flex: none;
  padding: 3px 7px;
  border-radius: 999px;
  background: var(--line);
  color: var(--muted);
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.code.c2 { background: var(--green-bg); color: var(--green); }
.code.c3 { background: var(--blue-bg); color: var(--blue); }
.code.c4 { background: var(--amber-bg); color: var(--amber); }
.code.c5 { background: var(--red-bg); color: var(--red); }
.code.none { background: var(--line); color: var(--muted); }
.duration { flex: none; min-width: 56px; color: var(--muted); font-size: 12px; text-align: right; font-variant-numeric: tabular-nums; }
.item-body { display: grid; gap: 14px; padding: 14px 16px 16px; }
.callout { padding: 10px 12px; border: 1px solid transparent; border-radius: 9px; font-size: 13px; }
.callout.error {
  border-color: color-mix(in srgb, var(--red) 28%, var(--border));
  background: var(--red-bg);
  color: var(--red);
  font-family: var(--mono);
  font-size: 12px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.callout.warnings {
  border-color: color-mix(in srgb, var(--amber) 28%, var(--border));
  background: var(--amber-bg);
  color: var(--amber);
}
.callout.warnings ul { margin: 5px 0 0; padding-left: 19px; }
.detail-title { display: flex; align-items: baseline; flex-wrap: wrap; gap: 10px; margin-bottom: 8px; }
.detail-title h3 {
  color: var(--muted);
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.exchange { padding: 12px 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--panel); }
@supports (background: color-mix(in srgb, #fff 50%, #000)) {
  .exchange { background: color-mix(in srgb, var(--panel) 72%, var(--bg)); }
}
.exchange h4 {
  margin: 12px 0 6px;
  color: var(--muted);
  font-size: 10.5px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.detail-line { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px; font-size: 13px; }
.detail-line code { font-family: var(--mono); font-size: 12px; overflow-wrap: anywhere; }
.tests { display: grid; gap: 7px; padding: 0; list-style: none; }
.test { display: flex; align-items: flex-start; gap: 8px; font-size: 13px; }
.test-icon { flex: none; width: 15px; font-weight: 700; text-align: center; }
.test-icon.pass { color: var(--green); }
.test-icon.fail { color: var(--red); }
.test.fail .test-name { color: var(--red); font-weight: 600; }
.test-body { display: flex; flex: 1 1 auto; flex-direction: column; gap: 4px; min-width: 0; }
.test-name { overflow-wrap: anywhere; }
.test-error { margin: 0; color: var(--red); }
.code-block {
  max-height: 28rem;
  margin: 0;
  padding: 10px 12px;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--code-bg);
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.headers { width: 100%; border-collapse: collapse; font-size: 12px; }
.headers th, .headers td { padding: 4px 8px 4px 0; border-top: 1px solid var(--line); text-align: left; vertical-align: top; }
.headers tr:first-child th, .headers tr:first-child td { border-top: 0; }
.headers th { width: 1%; padding-right: 16px; color: var(--muted); font-family: var(--mono); font-weight: 600; white-space: nowrap; }
.headers td { width: 99%; font-family: var(--mono); overflow-wrap: anywhere; }
.footer { margin-top: 40px; margin-bottom: 34px; padding-top: 14px; border-top: 1px solid var(--border); color: var(--muted); font-size: 12px; }
.footer strong { color: var(--text); }
@media (max-width: 760px) {
  .wrap { width: calc(100% - 24px); }
  .masthead { padding: 26px 0 48px; }
  .masthead-top { flex-direction: column; gap: 12px; }
  .stats { grid-template-columns: repeat(auto-fit, minmax(132px, 1fr)); }
  .item summary { gap: 8px; padding: 10px 12px; }
  .item-url { display: none; }
  .duration { display: none; }
  .toolbar { position: static; }
  .item { scroll-margin-top: 16px; }
  .filters { width: 100%; overflow-x: auto; }
  .toolbar-actions { margin-left: 0; }
}
@media (max-width: 480px) {
  .iter { display: none; }
  .filters button { padding: 6px 8px; font-size: 12px; }
  .filters .count { font-size: 10px; }
}
</style>
</head>
<body>
<header class="masthead">
  <div class="wrap">
    <div class="masthead-top">
      <div>
        <p class="eyebrow">roastman run report</p>
        <h1>{{.Collection}}</h1>
      </div>
      <span class="verdict {{.VerdictClass}}">{{.Verdict}}</span>
    </div>
    <div class="run-meta">
      {{if .Environment}}<span class="meta-chip"><span>Environment</span><strong>{{.Environment}}</strong></span>{{end}}
      <span class="meta-chip"><span>Iterations</span><strong>{{.Iterations}}</strong></span>
      {{if .Started}}<span class="meta-chip"><span>Started</span><strong><time datetime="{{.StartedISO}}">{{.Started}}</time></strong></span>{{end}}
      <span class="meta-chip"><span>Duration</span><strong>{{.Duration}}</strong></span>
      {{if .AvgResponse}}<span class="meta-chip"><span>Average response</span><strong>{{.AvgResponse}}</strong></span>{{end}}
      {{if .DataReceived}}<span class="meta-chip"><span>Data received</span><strong>{{.DataReceived}}</strong></span>{{end}}
    </div>
  </div>
</header>

<main class="wrap">
  <section class="stats" aria-label="Run summary">
    {{range .Stats}}
    <div class="stat {{.Tone}}">
      <span class="stat-value">{{.Value}}</span>
      <span class="stat-label">{{.Label}}</span>
      <span class="stat-detail">{{.Detail}}</span>
    </div>
    {{end}}
  </section>

  {{if .Recap}}
  <section class="recap" aria-labelledby="failures-title">
    <div class="recap-head">
      <span class="recap-icon" aria-hidden="true">✗</span>
      <h2 id="failures-title">Failures &amp; errors</h2>
      <span class="group-meta">{{len .Recap}} request{{if ne (len .Recap) 1}}s{{end}}</span>
    </div>
    <ol>
      {{range .Recap}}
      <li>
        <a class="recap-link" href="#{{.Target}}">
          <span class="recap-path">{{.Path}}</span>
          {{if .Method}}<span class="method {{.MethodClass}}">{{.Method}}</span>{{end}}
          {{if .CodeText}}<span class="code {{.CodeClass}}">{{.CodeText}}</span>{{end}}
        </a>
        {{if .Error}}<p class="recap-error">{{.Error}}</p>{{end}}
        {{range .FailedTests}}
        <div class="recap-test">
          <span class="test-icon fail" aria-hidden="true">✗</span>
          <div class="recap-test-body">
            <span class="test-name">{{.Name}}</span>
            {{if .Error}}<pre class="code-block test-error">{{.Error}}</pre>{{end}}
          </div>
        </div>
        {{end}}
      </li>
      {{end}}
    </ol>
  </section>
  {{end}}

  {{if .Total}}
  <div class="toolbar">
    <div class="search">
      <label class="sr-only" for="search-input">Search requests</label>
      <input id="search-input" type="search" placeholder="Search requests, folders, tests…" autocomplete="off" spellcheck="false">
    </div>
    <div class="filters" role="group" aria-label="Filter by result">
      <button type="button" class="active" data-filter="all" aria-pressed="true">All <span class="count">{{.Total}}</span></button>
      <button type="button" data-filter="failed" aria-pressed="false">Failed <span class="count">{{.FailedCount}}</span></button>
      <button type="button" data-filter="passed" aria-pressed="false">Passed <span class="count">{{.PassedCount}}</span></button>
      <button type="button" data-filter="warnings" aria-pressed="false">Warnings <span class="count">{{.WarningCount}}</span></button>
      <button type="button" data-filter="skipped" aria-pressed="false">Skipped <span class="count">{{.SkippedCount}}</span></button>
    </div>
    <div class="toolbar-actions">
      <button type="button" id="expand-all">Expand all</button>
      <button type="button" id="collapse-all">Collapse all</button>
      <button type="button" id="theme-toggle" title="Toggle light/dark theme" aria-label="Toggle light/dark theme">◐</button>
    </div>
  </div>
  <p class="result-count" id="visible-count" aria-live="polite">{{.Total}} of {{.Total}} requests</p>
  <p class="empty" id="empty-filter" hidden>No requests match the current filter.</p>
  {{else}}
  <p class="empty">No requests were executed in this run.</p>
  {{end}}

  {{range .Groups}}
  <section class="group" data-group>
    {{if .Label}}
    <div class="group-head">
      <h2>{{.Label}}</h2>
      <span class="group-meta">{{.Summary}}</span>
    </div>
    {{end}}
    <div class="items">
      {{range .Items}}
      <details class="item" id="{{.ID}}" data-state="{{.State}}" data-warnings="{{if .Warnings}}true{{else}}false{{end}}" data-search="{{.Search}}">
        <summary title="{{.Path}}">
          <span class="chev" aria-hidden="true">▶</span>
          <span class="state-icon {{.State}}" aria-hidden="true">{{.StateIcon}}</span>
          <span class="sr-only">Status: {{.StateLabel}}.</span>
          <span class="summary-main">
            <span class="item-name">{{.Name}}</span>
            {{if .URL}}<span class="item-url">{{.URL}}</span>{{end}}
          </span>
          {{if .ShowIteration}}<span class="iter">{{.IterationLabel}}</span>{{end}}
          {{if .Method}}<span class="method {{.MethodClass}}">{{.Method}}</span>{{end}}
          {{if .CodeText}}<span class="code {{.CodeClass}}">{{.CodeText}}</span>{{end}}
          <span class="duration">{{.Duration}}</span>
        </summary>
        <div class="item-body">
          {{if .Error}}<div class="callout error"><strong>Error:</strong> {{.Error}}</div>{{end}}
          {{if .Warnings}}
          <div class="callout warnings">
            <strong>Warnings</strong>
            <ul>{{range .Warnings}}<li>{{.}}</li>{{end}}</ul>
          </div>
          {{end}}
          {{if .Tests}}
          <section aria-label="Tests">
            <div class="detail-title"><h3>Tests</h3><span class="group-meta">{{.TestsSummary}}</span></div>
            <ul class="tests">
              {{range .Tests}}
              <li class="test {{if .Passed}}pass{{else}}fail{{end}}">
                <span class="test-icon {{if .Passed}}pass{{else}}fail{{end}}" aria-hidden="true">{{if .Passed}}✓{{else}}✗{{end}}</span>
                <span class="sr-only">{{if .Passed}}passed{{else}}failed{{end}}:</span>
                <div class="test-body">
                  <span class="test-name">{{.Name}}</span>
                  {{if .Error}}<pre class="code-block test-error">{{.Error}}</pre>{{end}}
                </div>
              </li>
              {{end}}
            </ul>
          </section>
          {{end}}
          {{if .ConsoleText}}
          <section aria-label="Console output">
            <div class="detail-title"><h3>Console</h3></div>
            <pre class="code-block">{{.ConsoleText}}</pre>
          </section>
          {{end}}
          {{if .HasRequestDetail}}
          <section class="exchange" aria-label="Request">
            <div class="detail-title">
              <h3>Request</h3>
              {{if .ShowIteration}}<span class="group-meta">{{.IterationLabel}}</span>{{end}}
            </div>
            <p class="detail-line">
              {{if .Method}}<span class="method {{.MethodClass}}">{{.Method}}</span>{{end}}
              <code>{{.URL}}</code>
            </p>
            {{if .RequestHeaders}}
            <h4>Headers</h4>
            <table class="headers"><tbody>
              {{range .RequestHeaders}}
              <tr><th scope="row">{{.Name}}</th><td>{{range $i, $v := .Values}}{{if $i}}<br>{{end}}{{$v}}{{end}}</td></tr>
              {{end}}
            </tbody></table>
            {{end}}
            {{if .RequestBody}}<h4>Body</h4><pre class="code-block">{{.RequestBody}}</pre>{{end}}
          </section>
          {{end}}
          {{if .ShowResponse}}
          <section class="exchange" aria-label="Response">
            <div class="detail-title">
              <h3>Response</h3>
              {{if .ResponseMeta}}<span class="group-meta">{{.ResponseMeta}}</span>{{end}}
            </div>
            <p class="detail-line">
              {{if .CodeText}}<span class="code {{.CodeClass}}">{{.CodeText}}</span>{{end}}
              {{if .Status}}<span>{{.Status}}</span>{{end}}
            </p>
            {{if .ResponseHeaders}}
            <h4>Headers</h4>
            <table class="headers"><tbody>
              {{range .ResponseHeaders}}
              <tr><th scope="row">{{.Name}}</th><td>{{range $i, $v := .Values}}{{if $i}}<br>{{end}}{{$v}}{{end}}</td></tr>
              {{end}}
            </tbody></table>
            {{end}}
            {{if .ResponseBody}}<h4>Body</h4><pre class="code-block">{{.ResponseBody}}</pre>{{end}}
          </section>
          {{end}}
        </div>
      </details>
      {{end}}
    </div>
  </section>
  {{end}}
</main>

<footer class="footer wrap">
  <p>Generated by <strong>roastman</strong> · offline Postman-compatible collection runner</p>
</footer>

<script>
(function () {
  var doc = document;
  var items = Array.prototype.slice.call(doc.querySelectorAll('.item'));
  var groups = Array.prototype.slice.call(doc.querySelectorAll('.group'));
  var search = doc.getElementById('search-input');
  var count = doc.getElementById('visible-count');
  var empty = doc.getElementById('empty-filter');
  var buttons = Array.prototype.slice.call(doc.querySelectorAll('[data-filter]'));
  var current = 'all';

  function matchesFilter(item) {
    if (current === 'all') { return true; }
    if (current === 'warnings') { return item.getAttribute('data-warnings') === 'true'; }
    var state = item.getAttribute('data-state');
    if (current === 'passed') { return state === 'passed' || state === 'warning'; }
    return state === current;
  }

  function apply() {
    var visible = 0;
    items.forEach(function (item) {
      var stateMatch = matchesFilter(item);
      var term = search ? (search.value || '').trim().toLowerCase() : '';
      var searchMatch = !term || (item.getAttribute('data-search') || '').indexOf(term) !== -1;
      var show = stateMatch && searchMatch;
      item.hidden = !show;
      if (show) { visible += 1; }
    });
    groups.forEach(function (group) {
      group.hidden = group.querySelector('.item:not([hidden])') === null;
    });
    if (count) { count.textContent = visible + ' of ' + items.length + ' requests'; }
    if (empty) { empty.hidden = visible !== 0; }
    buttons.forEach(function (button) {
      var active = button.getAttribute('data-filter') === current;
      button.classList.toggle('active', active);
      button.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
  }

  buttons.forEach(function (button) {
    button.addEventListener('click', function () {
      current = button.getAttribute('data-filter') || 'all';
      apply();
    });
  });
  if (search) { search.addEventListener('input', apply); }

  doc.addEventListener('keydown', function (event) {
    var target = event.target;
    var typing = target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' ||
      target.tagName === 'SELECT' || target.isContentEditable);
    if (event.key === '/' && !typing) {
      event.preventDefault();
      if (search) { search.focus(); search.select(); }
    } else if (event.key === 'Escape' && target === search) {
      search.value = '';
      apply();
      search.blur();
    }
  });

  var expand = doc.getElementById('expand-all');
  var collapse = doc.getElementById('collapse-all');
  if (expand) {
    expand.addEventListener('click', function () {
      items.forEach(function (item) { if (!item.hidden) { item.open = true; } });
    });
  }
  if (collapse) {
    collapse.addEventListener('click', function () {
      items.forEach(function (item) { item.open = false; });
    });
  }

  var theme = doc.getElementById('theme-toggle');
  if (theme) {
    theme.addEventListener('click', function () {
      var root = doc.documentElement;
      var stored = root.getAttribute('data-theme');
      var dark = stored ? stored === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches;
      var next = dark ? 'light' : 'dark';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem('roastman-theme', next); } catch (e) {}
    });
  }

  Array.prototype.forEach.call(doc.querySelectorAll('.recap-link'), function (link) {
    link.addEventListener('click', function () {
      // The target may be hidden by an active filter or search, so reveal the
      // full list before expanding it.
      current = 'all';
      if (search) { search.value = ''; }
      apply();
      var href = link.getAttribute('href');
      var target = href ? doc.querySelector(href) : null;
      if (target) { target.open = true; }
    });
  });

  function syncToolbarOffset() {
    var toolbar = doc.querySelector('.toolbar');
    if (toolbar) {
      doc.documentElement.style.setProperty('--toolbar-offset', (toolbar.offsetHeight + 12) + 'px');
    }
  }
  window.addEventListener('resize', syncToolbarOffset);
  syncToolbarOffset();

  apply();
})();
</script>
</body>
</html>
`
