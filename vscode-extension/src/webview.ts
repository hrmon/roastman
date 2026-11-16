import {
  MAX_DISPLAY_BODY_CHARS,
  countTests,
  formatBytes,
  formatDuration,
  prettyResponseBody,
  type ItemReport,
  type RunReport,
} from "./report";

export interface ReportPageInput {
  report: RunReport;
  item: ItemReport;
  requestFileLabel: string;
  environmentLabel: string;
  nonce: string;
  cspSource: string;
}

export interface LoadingPageInput {
  name: string;
  method: string;
  url: string;
  environmentLabel: string;
  collectionName: string;
  requestFileLabel: string;
  nonce: string;
  cspSource: string;
}

export interface ErrorPageInput {
  title: string;
  message: string;
  details?: string;
  nonce: string;
  cspSource: string;
}

/** Renders a completed request: response, test results, headers, and console. */
export function renderReportHtml(input: ReportPageInput): string {
  const { report, item } = input;
  const title = item.displayName || item.name;
  const method = (item.method || "REQUEST").toUpperCase();
  const environment = report.environment?.trim() || input.environmentLabel || "No environment";
  const tests = item.tests ?? [];
  const counts = countTests(tests);
  const responseBody = prettyResponseBody(item.responseBody);
  const consoleLines = item.console ?? [];
  const warnings = item.warnings ?? [];

  const status = renderStatus(item);
  const timing = formatDuration(item.responseTimeMs ?? item.durationMs);
  const size =
    item.responseSize === undefined ? "" : `<span>${escapeHtml(formatBytes(item.responseSize))}</span>`;

  const banners = [
    item.error ? `<div class="banner error"><strong>Request failed</strong><div>${escapeHtml(item.error)}</div></div>` : "",
    item.skipped ? renderWarningBanner(warnings, "Request skipped", true) : renderWarningBanner(warnings, "Warnings", false),
    report.items.length > 1
      ? `<div class="banner info">The runner returned ${report.items.length} requests; showing the first one.</div>`
      : "",
  ].filter(Boolean);

  const testsSection = `
    <section aria-labelledby="tests-heading">
      <div class="section-head">
        <h2 id="tests-heading">Tests</h2>
        <div class="test-counts">
          ${counts.passed > 0 ? `<span class="count pass">${counts.passed} passed</span>` : ""}
          ${counts.failed > 0 ? `<span class="count fail">${counts.failed} failed</span>` : ""}
        </div>
      </div>
      ${
        tests.length === 0
          ? `<p class="muted">No tests were defined for this request.</p>`
          : `<ul class="tests">${tests.map(renderTest).join("")}</ul>`
      }
    </section>`;

  const responseSection = `
    <section aria-labelledby="response-heading">
      <div class="section-head">
        <h2 id="response-heading">Response</h2>
        ${responseBody.text ? `<button class="button" data-copy-target="response-body">Copy body</button>` : ""}
      </div>
      ${
        responseBody.text
          ? `<pre id="response-body" class="body" aria-label="Response body">${escapeHtml(
              responseBody.text
            )}</pre>
             ${
               responseBody.truncated
                 ? `<p class="note">Showing the first ${Math.round(
                     MAX_DISPLAY_BODY_CHARS / 1024
                   )} KB of the response body.</p>`
                 : ""
             }`
          : item.skipped
            ? `<p class="muted">No response: the request was skipped.</p>`
            : item.error
              ? `<p class="muted">No response body was received.</p>`
              : `<p class="muted">The response body is empty.</p>`
      }
      ${renderHeadersDetails(item.responseHeaders, "Response headers")}
    </section>`;

  const consoleSection =
    consoleLines.length > 0
      ? `<section aria-labelledby="console-heading">
           <h2 id="console-heading">Console</h2>
           <pre class="body console">${escapeHtml(consoleLines.join("\n"))}</pre>
         </section>`
      : "";

  const requestSection = `
    <details class="details">
      <summary>Request</summary>
      <div class="details-body">
        <p class="request-line">
          <span class="method ${methodClass(method)}">${escapeHtml(method)}</span>
          <span class="request-url">${escapeHtml(item.url || "—")}</span>
        </p>
        <h3>Headers</h3>
        ${renderHeadersTable(item.requestHeaders)}
        <h3>Body</h3>
        ${renderRequestBody(item.requestBody)}
      </div>
    </details>`;

  const content = `
    <header class="header">
      <div class="title-row">
        <span class="method ${methodClass(method)}">${escapeHtml(method)}</span>
        <h1>${escapeHtml(title)}</h1>
      </div>
      <div class="url">${escapeHtml(item.url || "No URL")}</div>
      <div class="meta">
        ${status}
        <span title="Response time">${escapeHtml(timing)}</span>
        ${size}
        <span>Env: ${escapeHtml(environment)}</span>
        ${item.folder ? `<span>Folder: ${escapeHtml(item.folder.replace(/\//g, " › "))}</span>` : ""}
      </div>
    </header>
    ${banners.join("")}
    ${testsSection}
    ${responseSection}
    ${consoleSection}
    <section aria-label="Request details">${requestSection}</section>
    <footer class="footer">
      <span>${escapeHtml(report.collection || "Collection")}</span>
      <span>${escapeHtml(input.requestFileLabel)}</span>
      <span>${escapeHtml(environment)}</span>
    </footer>`;

  return page({ title, nonce: input.nonce, cspSource: input.cspSource, content });
}

/** Renders the panel while a request is running. */
export function renderLoadingHtml(input: LoadingPageInput): string {
  const method = (input.method || "REQUEST").toUpperCase();
  const content = `
    <header class="header">
      <div class="title-row">
        <span class="method ${methodClass(method)}">${escapeHtml(method)}</span>
        <h1>${escapeHtml(input.name)}</h1>
      </div>
      <div class="url">${escapeHtml(input.url || "No URL")}</div>
      <div class="meta"><span>Env: ${escapeHtml(input.environmentLabel)}</span></div>
    </header>
    <div class="banner info loading" role="status">
      <span class="spinner" aria-hidden="true"></span>
      <div>
        <strong>Running request…</strong>
        <div class="muted">The response and test results will appear here.</div>
      </div>
    </div>
    <footer class="footer">
      <span>${escapeHtml(input.collectionName)}</span>
      <span>${escapeHtml(input.requestFileLabel)}</span>
    </footer>`;
  return page({
    title: `Running ${input.name}`,
    nonce: input.nonce,
    cspSource: input.cspSource,
    content,
  });
}

/** Renders an actionable error in the output panel. */
export function renderErrorHtml(input: ErrorPageInput): string {
  const content = `
    <header class="header">
      <h1>${escapeHtml(input.title)}</h1>
    </header>
    <div class="banner error">${escapeHtml(input.message)}</div>
    ${
      input.details
        ? `<details class="details" open><summary>Details</summary><div class="details-body"><pre class="body">${escapeHtml(
            input.details
          )}</pre></div></details>`
        : ""
    }`;
  return page({
    title: input.title,
    nonce: input.nonce,
    cspSource: input.cspSource,
    content,
  });
}

export function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function page(options: {
  title: string;
  nonce: string;
  cspSource: string;
  content: string;
}): string {
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${options.cspSource} 'unsafe-inline'; script-src 'nonce-${options.nonce}';">
<title>${escapeHtml(options.title)}</title>
<style>${CSS}</style>
</head>
<body>
<main>${options.content}</main>
<script nonce="${options.nonce}">${SCRIPT}</script>
</body>
</html>`;
}

function renderTest(test: { name: string; passed: boolean; error?: string }): string {
  return `<li class="test ${test.passed ? "pass" : "fail"}">
    <span class="test-icon" aria-hidden="true">${test.passed ? "✓" : "✗"}</span>
    <div class="test-content">
      <div class="test-name">${escapeHtml(test.name || "(unnamed test)")}</div>
      ${test.error ? `<pre class="test-error">${escapeHtml(test.error)}</pre>` : ""}
    </div>
  </li>`;
}

function renderWarningBanner(warnings: string[], heading: string, skipped: boolean): string {
  if (warnings.length === 0) {
    return skipped ? `<div class="banner warning"><strong>${heading}</strong></div>` : "";
  }
  return `<div class="banner warning">
    <strong>${escapeHtml(heading)}</strong>
    <ul class="warning-list">${warnings.map((warning) => `<li>${escapeHtml(warning)}</li>`).join("")}</ul>
  </div>`;
}

function renderHeadersDetails(
  headers: Record<string, string[]> | undefined,
  title: string
): string {
  const count = Object.keys(headers ?? {}).length;
  if (count === 0) {
    return "";
  }
  return `<details class="details">
    <summary>${escapeHtml(title)} (${count})</summary>
    <div class="details-body">${renderHeadersTable(headers)}</div>
  </details>`;
}

function renderHeadersTable(headers: Record<string, string[]> | undefined): string {
  const entries = Object.entries(headers ?? {});
  if (entries.length === 0) {
    return `<p class="muted">No headers.</p>`;
  }
  return `<table>
    <tbody>
      ${entries
        .map(
          ([key, values]) =>
            `<tr><th scope="row">${escapeHtml(key)}</th><td>${escapeHtml(
              Array.isArray(values) ? values.join(", ") : String(values)
            )}</td></tr>`
        )
        .join("")}
    </tbody>
  </table>`;
}

function renderRequestBody(body: string | undefined): string {
  if (!body) {
    return `<p class="muted">No request body.</p>`;
  }
  const truncated = body.length > MAX_DISPLAY_BODY_CHARS;
  const text = truncated ? body.slice(0, MAX_DISPLAY_BODY_CHARS) : body;
  return `<pre class="body">${escapeHtml(text)}</pre>${
    truncated
      ? `<p class="note">Showing the first ${Math.round(MAX_DISPLAY_BODY_CHARS / 1024)} KB.</p>`
      : ""
  }`;
}

function renderStatus(item: ItemReport): string {
  if (item.skipped) {
    return `<span class="badge neutral">Skipped</span>`;
  }
  if (item.code !== undefined) {
    const text = item.status?.trim() || String(item.code);
    return `<span class="badge ${statusClass(item.code)}">${escapeHtml(text)}</span>`;
  }
  if (item.error) {
    return `<span class="badge bad">Error</span>`;
  }
  return "";
}

function statusClass(code: number): string {
  if (code >= 200 && code < 300) {
    return "ok";
  }
  if (code >= 300 && code < 400) {
    return "redirect";
  }
  if (code >= 400) {
    return "bad";
  }
  return "neutral";
}

function methodClass(method: string): string {
  switch (method.toUpperCase()) {
    case "GET":
      return "get";
    case "POST":
      return "post";
    case "PUT":
      return "put";
    case "PATCH":
      return "patch";
    case "DELETE":
      return "delete";
    default:
      return "other";
  }
}

const SCRIPT = `
(function () {
  const vscode = acquireVsCodeApi();
  document.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest("[data-copy-target]");
    if (!button) {
      return;
    }
    const source = document.getElementById(button.getAttribute("data-copy-target") || "");
    if (!source) {
      return;
    }
    vscode.postMessage({ type: "copy", text: source.textContent || "" });
    const original = button.textContent;
    button.textContent = "Copied";
    window.setTimeout(() => {
      button.textContent = original;
    }, 1200);
  });
})();`;

const CSS = `
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: var(--vscode-font-family, sans-serif);
  font-size: var(--vscode-font-size, 13px);
  color: var(--vscode-foreground);
  background: var(--vscode-editor-background);
}
main { max-width: 1000px; margin: 0 auto; padding: 24px 28px 48px; }
.header {
  border-bottom: 1px solid var(--vscode-panel-border);
  padding-bottom: 16px;
  margin-bottom: 20px;
}
.title-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
h1 {
  margin: 0;
  font-size: 1.35rem;
  font-weight: 600;
  line-height: 1.3;
  overflow-wrap: anywhere;
}
h2 { margin: 0 0 10px; font-size: 1rem; font-weight: 600; }
h3 {
  margin: 16px 0 6px;
  font-size: 0.82rem;
  font-weight: 600;
  color: var(--vscode-descriptionForeground);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
section { margin-top: 22px; }
.url {
  margin-top: 10px;
  font-family: var(--vscode-editor-font-family, monospace);
  font-size: 0.92em;
  word-break: break-all;
  color: var(--vscode-foreground);
}
.meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 16px;
  margin-top: 12px;
  color: var(--vscode-descriptionForeground);
}
.method {
  display: inline-block;
  padding: 2px 7px;
  border: 1px solid currentColor;
  border-radius: 4px;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  line-height: 1.5;
}
.method.get { color: var(--vscode-charts-green, #3fb950); }
.method.post { color: var(--vscode-charts-blue, #58a6ff); }
.method.put { color: var(--vscode-charts-orange, #d29922); }
.method.patch { color: var(--vscode-charts-purple, #bc8cff); }
.method.delete { color: var(--vscode-charts-red, #f85149); }
.method.other { color: var(--vscode-descriptionForeground); }
.badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border: 1px solid transparent;
  border-radius: 999px;
  font-size: 0.82em;
  font-weight: 600;
}
.badge.ok { color: var(--vscode-testing-iconPassed, #3fb950); border-color: currentColor; }
.badge.redirect { color: var(--vscode-charts-blue, #58a6ff); border-color: currentColor; }
.badge.bad { color: var(--vscode-testing-iconFailed, #f85149); border-color: currentColor; }
.badge.neutral { color: var(--vscode-descriptionForeground); border-color: currentColor; }
.banner {
  border-left: 3px solid var(--vscode-panel-border);
  border-radius: 3px;
  padding: 10px 12px;
  margin: 16px 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.banner.error {
  border-left-color: var(--vscode-inputValidation-errorBorder, #f85149);
  background: var(--vscode-inputValidation-errorBackground, rgba(248, 81, 73, 0.1));
  color: var(--vscode-errorForeground, #f85149);
}
.banner.warning {
  border-left-color: var(--vscode-inputValidation-warningBorder, #d29922);
  background: var(--vscode-inputValidation-warningBackground, rgba(210, 153, 34, 0.1));
}
.banner.info {
  border-left-color: var(--vscode-charts-blue, #58a6ff);
  background: var(--vscode-textBlockQuote-background, rgba(88, 166, 255, 0.08));
}
.banner.loading { display: flex; gap: 12px; align-items: flex-start; }
.warning-list { margin: 6px 0 0; padding-left: 20px; }
.warning-list li { margin: 3px 0; }
.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}
.section-head h2 { margin: 0; }
.test-counts { display: flex; gap: 8px; flex-wrap: wrap; }
.count { font-size: 0.82em; font-weight: 600; }
.count.pass { color: var(--vscode-testing-iconPassed, #3fb950); }
.count.fail { color: var(--vscode-testing-iconFailed, #f85149); }
.tests {
  list-style: none;
  margin: 0;
  padding: 0;
  border: 1px solid var(--vscode-panel-border);
  border-radius: 6px;
  overflow: hidden;
}
.test {
  display: flex;
  gap: 10px;
  padding: 10px 12px;
  border-top: 1px solid var(--vscode-panel-border);
}
.test:first-child { border-top: 0; }
.test.fail { background: var(--vscode-inputValidation-errorBackground, rgba(248, 81, 73, 0.07)); }
.test-icon { font-weight: 700; line-height: 1.5; }
.test.pass .test-icon { color: var(--vscode-testing-iconPassed, #3fb950); }
.test.fail .test-icon { color: var(--vscode-testing-iconFailed, #f85149); }
.test-name { font-weight: 500; }
.test-error {
  margin: 6px 0 0;
  font-family: var(--vscode-editor-font-family, monospace);
  font-size: 0.9em;
  color: var(--vscode-errorForeground, #f85149);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.body {
  margin: 0;
  padding: 12px;
  border: 1px solid var(--vscode-panel-border);
  border-radius: 6px;
  background: var(--vscode-textCodeBlock-background, rgba(127, 127, 127, 0.08));
  font-family: var(--vscode-editor-font-family, monospace);
  font-size: var(--vscode-editor-font-size, 13px);
  line-height: 1.5;
  white-space: pre;
  overflow: auto;
  max-height: 65vh;
}
.body.console { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 40vh; }
.muted { color: var(--vscode-descriptionForeground); margin: 0; }
.note { color: var(--vscode-descriptionForeground); font-size: 0.85em; margin: 6px 0 0; }
.button {
  border: 0;
  border-radius: 3px;
  padding: 4px 10px;
  background: var(--vscode-button-secondaryBackground, var(--vscode-button-background));
  color: var(--vscode-button-secondaryForeground, var(--vscode-button-foreground));
  font-family: inherit;
  font-size: 0.82em;
  cursor: pointer;
}
.button:hover { background: var(--vscode-button-secondaryHoverBackground, var(--vscode-button-hoverBackground)); }
.details {
  margin-top: 14px;
  border: 1px solid var(--vscode-panel-border);
  border-radius: 6px;
}
.details > summary {
  cursor: pointer;
  padding: 9px 12px;
  font-weight: 600;
  user-select: none;
}
.details[open] > summary { border-bottom: 1px solid var(--vscode-panel-border); }
.details-body { padding: 12px; }
table { width: 100%; border-collapse: collapse; }
th, td {
  text-align: left;
  vertical-align: top;
  padding: 5px 8px;
  border-top: 1px solid var(--vscode-panel-border);
  font-size: 0.92em;
  overflow-wrap: anywhere;
}
tr:first-child th, tr:first-child td { border-top: 0; }
th { width: 30%; font-weight: 600; color: var(--vscode-descriptionForeground); }
.request-line { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; margin: 0; }
.request-url { font-family: var(--vscode-editor-font-family, monospace); overflow-wrap: anywhere; }
.footer {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 18px;
  margin-top: 32px;
  padding-top: 12px;
  border-top: 1px solid var(--vscode-panel-border);
  color: var(--vscode-descriptionForeground);
  font-size: 0.85em;
}
.spinner {
  width: 16px;
  height: 16px;
  margin-top: 2px;
  border: 2px solid var(--vscode-descriptionForeground);
  border-right-color: transparent;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  flex: 0 0 auto;
}
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 600px) {
  main { padding: 16px 16px 32px; }
  .section-head { align-items: flex-start; }
}
`;
