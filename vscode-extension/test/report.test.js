"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  countTests,
  formatBytes,
  formatDuration,
  parseReport,
  prettyResponseBody,
} = require("../out/report.js");

test("parses the JSON report from stdout", () => {
  const report = {
    collection: "Demo",
    environment: "Local",
    requests: 1,
    failures: 0,
    errors: 0,
    warnings: 0,
    skipped: 0,
    items: [{ name: "Get User", code: 200 }],
  };
  const parsed = parseReport(`\n${JSON.stringify(report)}\n`);
  assert.equal(parsed.collection, "Demo");
  assert.equal(parsed.items[0].name, "Get User");
});

test("parses a report surrounded by unrelated output", () => {
  const parsed = parseReport(
    'starting\n{"collection":"Demo","items":[{"name":"A"}]}\ntrailing\n'
  );
  assert.equal(parsed.items.length, 1);
});

test("returns undefined for invalid reports", () => {
  assert.equal(parseReport(""), undefined);
  assert.equal(parseReport("not json"), undefined);
  assert.equal(parseReport('{"collection":"Demo"}'), undefined);
});

test("pretty-prints object JSON bodies", () => {
  const body = prettyResponseBody('{"ok":true,"items":[1,2]}');
  assert.equal(body.json, true);
  assert.match(body.text, /\n {2}"ok": true/);
});

test("keeps non-object bodies verbatim", () => {
  assert.deepEqual(prettyResponseBody("plain text"), {
    text: "plain text",
    json: false,
    truncated: false,
  });
  assert.deepEqual(prettyResponseBody('"hello"'), {
    text: '"hello"',
    json: false,
    truncated: false,
  });
  assert.deepEqual(prettyResponseBody(undefined), { text: "", json: false, truncated: false });
});

test("formats sizes and durations", () => {
  assert.equal(formatBytes(0), "0 B");
  assert.equal(formatBytes(512), "512 B");
  assert.equal(formatBytes(2048), "2.0 KB");
  assert.equal(formatBytes(5 * 1024 * 1024), "5.0 MB");
  assert.equal(formatDuration(18), "18 ms");
  assert.equal(formatDuration(1500), "1.5 s");
  assert.equal(formatDuration(undefined), "—");
});

test("counts passed and failed tests", () => {
  assert.deepEqual(
    countTests([
      { name: "a", passed: true },
      { name: "b", passed: false },
      { name: "c", passed: true },
    ]),
    { passed: 2, failed: 1 }
  );
  assert.deepEqual(countTests(undefined), { passed: 0, failed: 0 });
});
