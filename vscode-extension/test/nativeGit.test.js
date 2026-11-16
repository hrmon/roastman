"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fsp = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");

const {
  buildRequestTarget,
  findCollectionRoot,
  isRequestFile,
  readEnvironmentName,
  requestStem,
} = require("../out/nativeGit.js");

const collectionRoot = path.join(
  __dirname,
  "fixtures",
  "workspace",
  "collections",
  "Demo"
);
const usersRoot = path.join(collectionRoot, "Users");

test("recognizes native Git request files", () => {
  assert.equal(isRequestFile("Get User.request.yaml"), true);
  assert.equal(isRequestFile("Get User.request.yml"), true);
  assert.equal(isRequestFile("Get User.REQUEST.YAML"), true);
  assert.equal(isRequestFile("Get User.yaml"), false);
  assert.equal(isRequestFile("request.yaml"), false);
});

test("strips only the request suffix", () => {
  assert.equal(requestStem("Get User-2.request.yaml"), "Get User-2");
  assert.equal(requestStem("Get User.request.yml"), "Get User");
  assert.equal(requestStem("report.yaml"), "report.yaml");
});

test("finds the collection root inside a native Git workspace", async () => {
  assert.equal(
    await findCollectionRoot(path.join(usersRoot, "Get User.request.yaml")),
    collectionRoot
  );
  assert.equal(
    await findCollectionRoot(path.join(collectionRoot, "people", "Find.request.yaml")),
    collectionRoot
  );
});

test("builds an exact selector for the first duplicate request name", async () => {
  const target = await buildRequestTarget(path.join(usersRoot, "Get User.request.yaml"));

  assert.equal(target.collectionRoot, collectionRoot);
  assert.equal(target.collectionName, "Demo Collection");
  assert.equal(target.folderPath, "Users");
  assert.equal(target.requestName, "Get User");
  assert.equal(target.displayName, "Get User (1/2)");
  assert.equal(target.itemSelector, "Users/Get User (1/2)");
  assert.equal(target.duplicateIndex, 1);
  assert.equal(target.duplicateCount, 2);
  assert.equal(target.method, "GET");
});

test("builds an exact selector for the second duplicate request name", async () => {
  const target = await buildRequestTarget(path.join(usersRoot, "Get User-2.request.yaml"));

  assert.equal(target.requestName, "Get User");
  assert.equal(target.displayName, "Get User (2/2)");
  assert.equal(target.itemSelector, "Users/Get User (2/2)");
  assert.equal(target.duplicateIndex, 2);
  assert.equal(target.duplicateCount, 2);
});

test("uses a folder definition name instead of the directory name", async () => {
  const target = await buildRequestTarget(
    path.join(collectionRoot, "people", "Find.request.yaml")
  );

  assert.equal(target.folderPath, "People");
  assert.equal(target.requestName, "Find");
  assert.equal(target.itemSelector, "People/Find");
  assert.equal(target.duplicateCount, 1);
  assert.equal(target.displayName, "Find");
});

test("reads environment display names and falls back to the filename", async () => {
  const environment = path.join(
    __dirname,
    "fixtures",
    "workspace",
    "environments",
    "Local.environment.yaml"
  );
  assert.equal(await readEnvironmentName(environment), "Local Fixture");
  assert.equal(
    await readEnvironmentName(path.join(__dirname, "fixtures", "missing.environment.yaml")),
    "missing"
  );
});

test("rejects non-request files", async () => {
  await assert.rejects(
    buildRequestTarget(path.join(collectionRoot, "Users", "Get User.yaml")),
    /not a Postman v3 request file/
  );
});

test("finds a standalone collection that is not under a collections directory", async () => {
  const temporaryDirectory = await fsp.mkdtemp(path.join(os.tmpdir(), "roastman-standalone-"));
  try {
    await fsp.mkdir(path.join(temporaryDirectory, ".resources"), { recursive: true });
    await fsp.writeFile(
      path.join(temporaryDirectory, ".resources", "definition.yaml"),
      "$kind: collection\nname: Standalone\n"
    );
    const folder = path.join(temporaryDirectory, "FolderDir");
    await fsp.mkdir(path.join(folder, ".resources"), { recursive: true });
    await fsp.writeFile(
      path.join(folder, ".resources", "definition.yaml"),
      "$kind: collection\nname: Folder Display\n"
    );
    const request = path.join(folder, "Ping.request.yaml");
    await fsp.writeFile(request, "$kind: http-request\nurl: '{{base_url}}/ping'\nmethod: GET\n");

    const target = await buildRequestTarget(request);
    assert.equal(target.collectionRoot, temporaryDirectory);
    assert.equal(target.folderPath, "Folder Display");
    assert.equal(target.itemSelector, "Folder Display/Ping");
    assert.equal(target.displayName, "Ping");
  } finally {
    await fsp.rm(temporaryDirectory, { recursive: true, force: true });
  }
});
