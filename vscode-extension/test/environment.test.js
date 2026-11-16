"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  mergeEnvironmentText,
  parseEnvironmentEntries,
} = require("../out/environment.js");

const originalYaml = [
  "# environment metadata comment",
  'owner: "3014857"',
  'team: "11669667"',
  "name: Callingow",
  "values:",
  "  # base url comment",
  "  - key: base_url",
  '    value: "http://localhost:8080"',
  "    enabled: true",
  "  - key: keep",
  "    value: unchanged",
  "    enabled: true",
  "  - key: stale",
  "    value: remove me",
  "    enabled: true",
  "",
].join("\n");

const exportedYaml = [
  "name: Callingow",
  "values:",
  "  - key: base_url",
  "    value: http://localhost:8080",
  "    enabled: true",
  "  - key: keep",
  "    value: unchanged",
  "    enabled: true",
  "  - key: access_token",
  "    value: new-token",
  "    enabled: true",
  "    type: default",
  "",
].join("\n");

test("merges exported YAML values while preserving metadata and comments", () => {
  const result = mergeEnvironmentText(originalYaml, "Callingow.environment.yaml", exportedYaml);

  assert.equal(result.changed, true);
  assert.match(result.text, /# environment metadata comment/);
  assert.match(result.text, /owner: "3014857"/);
  assert.match(result.text, /team: "11669667"/);
  assert.match(result.text, /# base url comment/);
  assert.match(result.text, /value: "http:\/\/localhost:8080"/);
  assert.match(result.text, /key: keep/);
  assert.match(result.text, /key: access_token/);
  assert.match(result.text, /value: new-token/);
  assert.match(result.text, /type: default/);
  assert.doesNotMatch(result.text, /key: stale/);
  assert.ok(result.text.endsWith("\n"));
});

test("does not rewrite an unchanged environment", () => {
  const result = mergeEnvironmentText(
    originalYaml,
    "Callingow.environment.yaml",
    originalYaml.replace(/^#.*\n/, "").replace('owner: "3014857"\n', "").replace('team: "11669667"\n', "")
  );

  assert.equal(result.changed, false);
  assert.equal(result.text, originalYaml);
});

test("ignores type-only differences when deciding whether to rewrite", () => {
  const entries = parseEnvironmentEntries(originalYaml, "environment.yaml");
  const exported = [
    "name: Callingow",
    "values:",
    ...entries.flatMap((entry) => [
      `  - key: ${entry.key}`,
      `    value: ${JSON.stringify(entry.value)}`,
      `    enabled: ${entry.enabled}`,
      "    type: any",
    ]),
    "",
  ].join("\n");

  const result = mergeEnvironmentText(originalYaml, "environment.yaml", exported);
  assert.equal(result.changed, false);
});

test("merges exported values into a JSON environment while preserving other fields", () => {
  const originalJson = JSON.stringify(
    {
      id: "env-id",
      name: "Local",
      owner: "team",
      values: [
        { key: "base_url", value: "http://localhost", enabled: true },
        { key: "token", value: "old-token", enabled: true, extra: "keep-me" },
        { key: "stale", value: "remove", enabled: true },
      ],
    },
    null,
    2
  ) + "\n";

  const exportedJson = JSON.stringify({
    name: "Local",
    values: [
      { key: "base_url", value: "http://localhost", enabled: true },
      { key: "token", value: "new-token", enabled: true, type: "default" },
      { key: "new_key", value: "new value", enabled: true },
    ],
  });

  const result = mergeEnvironmentText(originalJson, "Local.postman_environment.json", exportedJson);
  assert.equal(result.changed, true);

  const parsed = JSON.parse(result.text);
  assert.equal(parsed.id, "env-id");
  assert.equal(parsed.name, "Local");
  assert.equal(parsed.owner, "team");
  assert.deepEqual(
    parsed.values.map((entry) => entry.key),
    ["base_url", "token", "new_key"]
  );
  assert.equal(parsed.values[1].value, "new-token");
  assert.equal(parsed.values[1].extra, "keep-me");
  assert.equal(parsed.values[1].type, "default");
  assert.equal(parsed.values[2].value, "new value");
  assert.ok(result.text.endsWith("\n"));
});

test("adds a values sequence when the environment has none", () => {
  const original = "# just a name\nname: Empty\n";
  const exported = [
    "name: Empty",
    "values:",
    "  - key: token",
    "    value: abc",
    "    enabled: true",
    "",
  ].join("\n");

  const result = mergeEnvironmentText(original, "Empty.environment.yaml", exported);
  assert.equal(result.changed, true);
  assert.match(result.text, /# just a name/);
  assert.match(result.text, /values:/);
  assert.match(result.text, /key: token/);
});

test("normalizes values like the runner", () => {
  const entries = parseEnvironmentEntries(
    ["name: Numbers", "values:", "  - key: one", "    value: 1", "    enabled: false", ""].join("\n"),
    "Numbers.environment.yaml"
  );
  assert.deepEqual(entries, [{ key: "one", value: "1", enabled: false, type: undefined }]);
});
