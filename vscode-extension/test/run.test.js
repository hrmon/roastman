"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");

const { buildRequestTarget } = require("../out/nativeGit.js");
const { runRoastman } = require("../out/roastman.js");

const repositoryRoot = path.resolve(__dirname, "..", "..");
const fixtureRoot = path.join(__dirname, "fixtures", "workspace");
const collectionRoot = path.join(fixtureRoot, "collections", "Demo");

function findRoastmanBinary() {
  const candidates = [
    process.env.ROASTMAN_BIN,
    path.join(repositoryRoot, process.platform === "win32" ? "roastman.exe" : "roastman"),
    "roastman",
  ].filter(Boolean);

  for (const candidate of candidates) {
    if (candidate === "roastman") {
      return candidate;
    }
    try {
      fs.accessSync(candidate, fs.constants.X_OK);
      if (fs.statSync(candidate).isFile()) {
        return candidate;
      }
    } catch {
      // Try the next candidate.
    }
  }
  return undefined;
}

function startServer() {
  return new Promise((resolve, reject) => {
    const server = http.createServer((request, response) => {
      const payload = JSON.stringify({
        ok: true,
        path: request.url,
        method: request.method,
      });
      response.writeHead(200, {
        "content-type": "application/json",
        "content-length": Buffer.byteLength(payload),
      });
      response.end(payload);
    });
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      resolve(server);
    });
  });
}

test("runs the selected request file through roastman and returns test results", async (t) => {
  const executable = findRoastmanBinary();
  if (!executable) {
    t.skip("roastman binary not found; set ROASTMAN_BIN to run this test");
    return;
  }

  const server = await startServer();
  const temporaryDirectory = await fsp.mkdtemp(
    path.join(os.tmpdir(), "roastman-vscode-test-")
  );

  try {
    const { port } = server.address();
    const environmentPath = path.join(temporaryDirectory, "Local.environment.yaml");
    await fsp.writeFile(
      environmentPath,
      [
        "name: Test Environment",
        "values:",
        "  - key: base_url",
        `    value: http://127.0.0.1:${port}`,
        "    enabled: true",
        "  - key: keep",
        "    value: unchanged",
        "    enabled: true",
        "  - key: stale",
        "    value: remove-me",
        "    enabled: true",
        "",
      ].join("\n")
    );

    const second = await buildRequestTarget(
      path.join(collectionRoot, "Users", "Get User-2.request.yaml")
    );
    assert.equal(second.itemSelector, "Users/Get User (2/2)");

    const secondRun = await runRoastman({
      executable,
      collectionRoot: second.collectionRoot,
      itemSelector: second.itemSelector,
      environmentPath,
      timeoutSeconds: 10,
      cwd: repositoryRoot,
    });

    assert.equal(secondRun.report.environment, "Test Environment");
    assert.equal(secondRun.report.requests, 1);
    assert.equal(secondRun.report.errors, 0);
    assert.equal(secondRun.report.failures, 0);

    const secondItem = secondRun.report.items[0];
    assert.equal(secondItem.name, "Get User");
    assert.equal(secondItem.displayName, "Get User (2/2)");
    assert.equal(secondItem.code, 200);
    assert.equal(
      secondItem.url,
      `http://127.0.0.1:${port}/users/2`
    );
    assert.deepEqual(
      (secondItem.tests || []).map((result) => [result.name, result.passed]),
      [
        ["Status is 200", true],
        ["Path is /users/2", true],
      ]
    );
    assert.equal(JSON.parse(secondItem.responseBody).path, "/users/2");

    const first = await buildRequestTarget(
      path.join(collectionRoot, "Users", "Get User.request.yaml")
    );
    assert.equal(first.itemSelector, "Users/Get User (1/2)");

    const firstRun = await runRoastman({
      executable,
      collectionRoot: first.collectionRoot,
      itemSelector: first.itemSelector,
      environmentPath,
      timeoutSeconds: 10,
      cwd: repositoryRoot,
    });

    assert.equal(firstRun.report.failures, 0);
    assert.equal(firstRun.report.errors, 0);
    assert.equal(firstRun.report.items[0].url, `http://127.0.0.1:${port}/users/1`);
    assert.equal(JSON.parse(firstRun.report.items[0].responseBody).path, "/users/1");

    const failing = await buildRequestTarget(
      path.join(collectionRoot, "Users", "Failing.request.yaml")
    );
    assert.equal(failing.itemSelector, "Users/Failing");

    const failingRun = await runRoastman({
      executable,
      collectionRoot: failing.collectionRoot,
      itemSelector: failing.itemSelector,
      environmentPath,
      timeoutSeconds: 10,
      cwd: repositoryRoot,
    });

    // A test failure exits non-zero, but the JSON report is still complete.
    assert.equal(failingRun.exitCode, 1);
    assert.equal(failingRun.report.failures, 1);
    assert.equal(failingRun.report.errors, 0);
    assert.equal(failingRun.report.items[0].code, 200);
    assert.deepEqual(failingRun.report.items[0].tests, [
      {
        name: "Expects 201",
        passed: false,
        error: "expected response to have status code 201 but got 200",
      },
    ]);

    const mutate = await buildRequestTarget(
      path.join(collectionRoot, "Users", "Mutate.request.yaml")
    );
    const exportPath = path.join(temporaryDirectory, "exported.environment.yaml");
    const mutateRun = await runRoastman({
      executable,
      collectionRoot: mutate.collectionRoot,
      itemSelector: mutate.itemSelector,
      environmentPath,
      exportEnvironmentPath: exportPath,
      timeoutSeconds: 10,
      cwd: repositoryRoot,
    });

    assert.equal(mutateRun.report.failures, 0);
    assert.equal(mutateRun.report.errors, 0);
    const exportedEnvironment = await fsp.readFile(exportPath, "utf8");
    assert.match(exportedEnvironment, /key: token/);
    assert.match(exportedEnvironment, /value: new-token/);
    assert.match(exportedEnvironment, /key: keep/);
    assert.doesNotMatch(exportedEnvironment, /key: stale/);

    // The selected environment itself is only changed by the extension's
    // write-back step, not by running roastman with an export path.
    const untouchedEnvironment = await fsp.readFile(environmentPath, "utf8");
    assert.match(untouchedEnvironment, /key: stale/);
  } finally {
    await new Promise((resolve) => server.close(resolve));
    await fsp.rm(temporaryDirectory, { recursive: true, force: true });
  }
});
