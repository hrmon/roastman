"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  renderErrorHtml,
  renderLoadingHtml,
  renderReportHtml,
} = require("../out/webview.js");

const nonce = "test-nonce";
const cspSource = "vscode-webview://test";

function sampleReport() {
  return {
    collection: "Demo Collection",
    environment: "Local Fixture",
    requests: 1,
    failures: 1,
    errors: 0,
    warnings: 1,
    skipped: 0,
    items: [
      {
        name: "Get User",
        displayName: "Get User (2/2)",
        folder: "Users",
        method: "GET",
        url: "http://127.0.0.1:3000/users/2",
        status: "200 OK",
        code: 200,
        responseTimeMs: 18,
        responseSize: 42,
        durationMs: 20,
        warnings: ["A harmless warning"],
        tests: [
          { name: "Status is 200", passed: true },
          {
            name: "Body shape",
            passed: false,
            error: "expected <ok> to be false",
          },
        ],
        console: ["log line <one>"],
        responseHeaders: {
          "Content-Type": ["application/json"],
        },
        requestHeaders: {
          Accept: ["application/json"],
        },
        responseBody: '{"ok":true,"name":"<script>alert(1)</script>"}',
      },
    ],
  };
}

test("renders a human-readable successful/failing request result", () => {
  const report = sampleReport();
  const html = renderReportHtml({
    report,
    item: report.items[0],
    requestFileLabel: "Users/Get User-2.request.yaml",
    environmentLabel: "Local Fixture",
    nonce,
    cspSource,
  });

  assert.match(html, /Get User \(2\/2\)/);
  assert.match(html, /Status is 200/);
  assert.match(html, /Body shape/);
  assert.match(html, /1 passed/);
  assert.match(html, /1 failed/);
  assert.match(html, /expected &lt;ok&gt; to be false/);
  assert.match(html, /A harmless warning/);
  assert.match(html, /Copy body/);
  assert.match(html, /log line &lt;one&gt;/);
  assert.match(html, /Users\/Get User-2\.request\.yaml/);
  assert.match(html, /200 OK/);
  assert.match(html, /18 ms/);
  assert.match(html, /Local Fixture/);
  assert.ok(html.includes(`nonce-${nonce}`));
  assert.match(html, /&quot;ok&quot;: true/);
  assert.ok(!html.includes("<script>alert(1)</script>"), "response body must be escaped");
  assert.ok(!html.includes("expected <ok>"), "test errors must be escaped");
});

test("renders skipped requests and request errors", () => {
  const report = sampleReport();
  report.items[0] = {
    name: "Get User",
    method: "GET",
    url: "{{base_url}}/users/2",
    skipped: true,
    warnings: ["unresolved URL variable(s): base_url"],
    failed: true,
  };
  const html = renderReportHtml({
    report,
    item: report.items[0],
    requestFileLabel: "Users/Get User.request.yaml",
    environmentLabel: "No environment",
    nonce,
    cspSource,
  });
  assert.match(html, /Skipped/);
  assert.match(html, /unresolved URL variable/);
  assert.match(html, /No response: the request was skipped/);

  report.items[0] = {
    name: "Broken",
    method: "GET",
    url: "http://127.0.0.1:1/",
    error: 'dial tcp: connect: connection refused',
    failed: true,
  };
  const errorHtml = renderReportHtml({
    report,
    item: report.items[0],
    requestFileLabel: "Broken.request.yaml",
    environmentLabel: "No environment",
    nonce,
    cspSource,
  });
  assert.match(errorHtml, /Request failed/);
  assert.match(errorHtml, /connection refused/);
  assert.match(errorHtml, /No response body was received/);
});

test("renders the loading state", () => {
  const html = renderLoadingHtml({
    name: "Get User (2/2)",
    method: "GET",
    url: "http://127.0.0.1:3000/users/2",
    environmentLabel: "Local Fixture",
    collectionName: "Demo Collection",
    requestFileLabel: "Users/Get User-2.request.yaml",
    nonce,
    cspSource,
  });
  assert.match(html, /Running request…/);
  assert.match(html, /Get User \(2\/2\)/);
  assert.match(html, /class="spinner"/);
});

test("renders and escapes error details", () => {
  const html = renderErrorHtml({
    title: "Request failed to run",
    message: "roastman <failed> & stopped",
    details: "stderr:\n<boom>",
    nonce,
    cspSource,
  });
  assert.match(html, /Request failed to run/);
  assert.match(html, /roastman &lt;failed&gt; &amp; stopped/);
  assert.match(html, /&lt;boom&gt;/);
  assert.ok(html.includes(`nonce-${nonce}`));
});
