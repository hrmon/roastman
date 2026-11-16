"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const http = require("node:http");
const Module = require("node:module");
const { spawnSync } = require("node:child_process");
const os = require("node:os");
const path = require("node:path");
const { parse: parseYaml } = require("yaml");

const repositoryRoot = path.resolve(__dirname, "..", "..");
const requestFile = path.join(
  __dirname,
  "fixtures",
  "workspace",
  "collections",
  "Demo",
  "Users",
  "Get User-2.request.yaml"
);
const mutateRequestFile = path.join(
  __dirname,
  "fixtures",
  "workspace",
  "collections",
  "Demo",
  "Users",
  "Mutate.request.yaml"
);

function disposable() {
  return { dispose() {} };
}

function createVscodeMock() {
  class Uri {
    constructor(fsPath) {
      this.fsPath = fsPath;
      this.scheme = "file";
    }
    static file(fsPath) {
      return new Uri(fsPath);
    }
    toString() {
      return `file://${this.fsPath}`;
    }
  }

  class Range {
    constructor(startLine, startCharacter, endLine, endCharacter) {
      this.start = { line: startLine, character: startCharacter };
      this.end = { line: endLine, character: endCharacter };
    }
  }

  class CodeLens {
    constructor(range, command) {
      this.range = range;
      this.command = command;
    }
  }

  class ThemeColor {
    constructor(id) {
      this.id = id;
    }
  }

  const workspaceState = new Map();
  const configuration = {};
  const commands = new Map();
  const outputLines = [];
  const errors = [];
  const warnings = [];
  const quickPicks = [];
  const statusMessages = [];
  let codeLensProvider;
  let panel;

  const statusItem = {
    text: "",
    tooltip: "",
    command: "",
    color: undefined,
    visible: false,
    show() {
      this.visible = true;
    },
    hide() {
      this.visible = false;
    },
    dispose() {},
  };

  const mock = {
    Uri,
    Range,
    CodeLens,
    ThemeColor,
    StatusBarAlignment: { Left: 1, Right: 2 },
    ViewColumn: { Active: -1, Beside: -2, One: 1 },
    window: {
      activeTextEditor: undefined,
      visibleTextEditors: [],
      createOutputChannel() {
        return {
          appendLine(line) {
            outputLines.push(line);
          },
          append(line) {
            outputLines.push(line);
          },
          show() {},
          dispose() {},
        };
      },
      createStatusBarItem() {
        return statusItem;
      },
      onDidChangeActiveTextEditor(handler) {
        mock._activeEditorHandler = handler;
        return disposable();
      },
      setStatusBarMessage(message) {
        statusMessages.push(message);
        return disposable();
      },
      async showErrorMessage(message) {
        errors.push(message);
        return undefined;
      },
      async showWarningMessage(message) {
        warnings.push(message);
        return undefined;
      },
      async showQuickPick(items) {
        quickPicks.push(items);
        return undefined;
      },
      async showOpenDialog() {
        return undefined;
      },
      createWebviewPanel() {
        const webview = {
          html: "",
          cspSource: "vscode-webview://test",
          onDidReceiveMessage() {
            return disposable();
          },
        };
        panel = {
          title: "Roastman",
          webview,
          reveal() {},
          onDidDispose() {
            return disposable();
          },
        };
        return panel;
      },
    },
    workspace: {
      workspaceFolders: [{ uri: Uri.file(repositoryRoot), name: "roastman", index: 0 }],
      textDocuments: [],
      getConfiguration(section) {
        return {
          get(key, defaultValue) {
            const fullKey = section ? `${section}.${key}` : key;
            return Object.prototype.hasOwnProperty.call(configuration, fullKey)
              ? configuration[fullKey]
              : defaultValue;
          },
        };
      },
      async findFiles() {
        return [];
      },
      asRelativePath(uri) {
        return path.relative(repositoryRoot, uri.fsPath);
      },
      onDidChangeWorkspaceFolders() {
        return disposable();
      },
    },
    commands: {
      registerCommand(name, handler) {
        commands.set(name, handler);
        return disposable();
      },
      async executeCommand() {
        return undefined;
      },
    },
    languages: {
      registerCodeLensProvider(_selector, provider) {
        codeLensProvider = provider;
        return disposable();
      },
    },
    env: {
      clipboard: {
        async writeText() {},
      },
    },
    _workspaceState: workspaceState,
    _configuration: configuration,
    _commands: commands,
    _outputLines: outputLines,
    _errors: errors,
    _warnings: warnings,
    _quickPicks: quickPicks,
    _statusMessages: statusMessages,
    _statusItem: statusItem,
    _codeLensProvider() {
      return codeLensProvider;
    },
    _panel() {
      return panel;
    },
  };
  return mock;
}

async function waitFor(predicate, timeoutMs = 5000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error("Timed out waiting for the extension");
}

function findRoastmanExecutable() {
  const candidates = [
    process.env.ROASTMAN_BIN,
    path.join(repositoryRoot, process.platform === "win32" ? "roastman.exe" : "roastman"),
  ].filter(Boolean);
  for (const candidate of candidates) {
    try {
      fs.accessSync(candidate, fs.constants.X_OK);
      if (fs.statSync(candidate).isFile()) {
        return candidate;
      }
    } catch {
      // Try the next candidate.
    }
  }

  const onPath = spawnSync("roastman", ["version"], { stdio: "ignore" });
  return onPath.status === 0 ? "roastman" : undefined;
}

test("activates the bundled extension and runs the request with a selected environment", async (t) => {
  const executable = findRoastmanExecutable();
  if (!executable) {
    t.skip("roastman is not available; set ROASTMAN_BIN or put it on PATH");
    return;
  }

  const server = http.createServer((request, response) => {
    const payload = JSON.stringify({ ok: true, path: request.url, method: request.method });
    response.writeHead(200, {
      "content-type": "application/json",
      "content-length": Buffer.byteLength(payload),
    });
    response.end(payload);
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const temporaryDirectory = await fsp.mkdtemp(
    path.join(os.tmpdir(), "roastman-extension-test-")
  );

  const vscodeMock = createVscodeMock();
  const originalLoad = Module._load;
  Module._load = function (request, parent, isMain) {
    if (request === "vscode") {
      return vscodeMock;
    }
    return originalLoad.call(this, request, parent, isMain);
  };

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
        "",
      ].join("\n")
    );
    vscodeMock._workspaceState.set("roastman.environmentPath", environmentPath);
    vscodeMock._configuration["roastman.executablePath"] = executable;

    const extension = require("../dist/extension.js");
    const context = {
      subscriptions: [],
      workspaceState: {
        get(key) {
          return vscodeMock._workspaceState.get(key);
        },
        async update(key, value) {
          vscodeMock._workspaceState.set(key, value);
        },
      },
    };

    extension.activate(context);

    const command = vscodeMock._commands.get("roastman.runRequest");
    assert.equal(typeof command, "function");
    assert.ok(vscodeMock._commands.has("roastman.selectEnvironment"));

    const lenses = vscodeMock._codeLensProvider().provideCodeLenses({
      uri: vscodeMock.Uri.file(requestFile),
    });
    assert.equal(lenses.length, 1);
    assert.equal(lenses[0].command.command, "roastman.runRequest");

    vscodeMock.window.activeTextEditor = {
      document: { uri: vscodeMock.Uri.file(requestFile), isDirty: false },
    };
    vscodeMock._activeEditorHandler();
    await waitFor(() => vscodeMock._statusItem.text.includes("Test Environment"));
    assert.equal(vscodeMock._statusItem.visible, true);
    assert.match(vscodeMock._statusItem.text, /Test Environment/);

    await command(vscodeMock.Uri.file(requestFile));

    assert.equal(vscodeMock._errors.length, 0, `unexpected errors: ${vscodeMock._errors.join("; ")}`);
    assert.equal(vscodeMock._quickPicks.length, 0, "environment should not be prompted");

    const panel = vscodeMock._panel();
    assert.ok(panel, "a webview panel should be created");
    assert.match(panel.webview.html, /Get User \(2\/2\)/);
    assert.match(panel.webview.html, /Path is \/users\/2/);
    assert.match(panel.webview.html, /2 passed/);
    assert.match(panel.webview.html, /users\/2/);
    assert.match(panel.title, /Get User/);
    assert.ok(
      vscodeMock._outputLines.some((line) => line.includes("[run]")),
      "the command should be logged"
    );
    assert.ok(
      vscodeMock._statusMessages.some((message) => message.includes("running Get User")),
      "the running status should be shown"
    );

    // Enable the opt-in write-back and run a request whose scripts mutate the
    // environment. Only the values should change; metadata and comments stay.
    vscodeMock._configuration["roastman.updateEnvironmentFile"] = true;
    await fsp.writeFile(
      environmentPath,
      [
        "# keep this comment",
        'owner: "fixture-owner"',
        "name: Test Environment",
        "values:",
        "  - key: base_url",
        `    value: http://127.0.0.1:${port}`,
        "    enabled: true",
        "  - key: stale",
        "    value: remove-me",
        "    enabled: true",
        "  - key: keep",
        "    value: unchanged",
        "    enabled: true",
        "",
      ].join("\n")
    );

    await command(vscodeMock.Uri.file(mutateRequestFile));

    const updatedEnvironment = await fsp.readFile(environmentPath, "utf8");
    assert.match(updatedEnvironment, /# keep this comment/);
    const parsedEnvironment = parseYaml(updatedEnvironment);
    assert.equal(parsedEnvironment.owner, "fixture-owner");
    assert.equal(parsedEnvironment.name, "Test Environment");
    const values = Object.fromEntries(
      parsedEnvironment.values.map((entry) => [entry.key, entry.value])
    );
    assert.equal(values.keep, "unchanged");
    assert.equal(values.token, "new-token");
    assert.equal(values.base_url, `http://127.0.0.1:${port}`);
    assert.equal(values.stale, undefined);
    assert.ok(
      vscodeMock._statusMessages.some((message) => message.includes("environment updated")),
      "the environment update should be confirmed"
    );
  } finally {
    Module._load = originalLoad;
    extensionDeactivate();
    await new Promise((resolve) => server.close(resolve));
    await fsp.rm(temporaryDirectory, { recursive: true, force: true });
  }

  function extensionDeactivate() {
    try {
      require("../dist/extension.js").deactivate();
    } catch {
      // The module may not have loaded if the test failed early.
    }
  }
});
