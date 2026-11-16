# Roastman Request Runner

A focused VS Code extension for Postman v3 (native Git) request files. Open a
`.request.yaml` file, run it with the environment you select, and see the
response and test results in a readable panel.

It does one thing: it runs the request file you are looking at. It shells out to
the [`roastman`](../README.md) CLI, so all body types, authentication, and
test scripts are handled by the same offline runner as the command line.

## Requirements

- VS Code 1.90 or newer.
- `roastman` 0.3.0 or newer, available in one of these places:
  - a `roastman` binary at the root of the workspace, or
  - `roastman` on `PATH`, or
  - a path configured in `roastman.executablePath`.
- The request must be part of a native Git collection: a directory tree with a
  `.resources/definition.yaml` at the collection root.

## Install

From a prebuilt VSIX (from the repository root):

```sh
code --install-extension vscode-extension/roastman-vscode.vsix
```

From source:

```sh
cd vscode-extension
npm install
npm run package
code --install-extension roastman-vscode.vsix
```

## Use

1. Open a `.request.yaml` (or `.request.yml`) file.
2. Run it with any of:
   - the ▶ button in the editor title bar,
   - the **Run request** CodeLens on the first line,
   - right-click → **Run Request**,
   - `Cmd+Alt+R` on macOS / `Ctrl+Alt+R` elsewhere,
   - **Roastman: Run Request** in the command palette.
3. If no environment has been chosen yet, pick one from the list. You can also
   choose **No environment**, or browse to any Postman environment YAML/JSON file.
4. The result opens in a panel beside the editor. It shows:
   - HTTP status, response time, and size,
   - every test with a pass/fail marker and assertion error,
   - the response body (pretty-printed when it is JSON),
   - response headers, request headers, request body, and console output.

Use the environment item in the status bar to switch environments at any time.
The choice is remembered per workspace.

The request file and the selected environment file are saved automatically if
they have unsaved edits before a run.

### Environment discovery

The picker finds environment files matching:

- `**/*.environment.yaml` / `**/*.environment.yml`
- `**/*.postman_environment.json`
- `**/environments/**/*.{yaml,yml,json}`

Anything else can be selected with **Browse for environment file…**.

### Writing environment changes back

By default every run starts from the selected environment file, and values set
or unset by scripts are discarded. To keep them, enable:

```json
"roastman.updateEnvironmentFile": true
```

The extension then asks `roastman` for its final in-memory environment and merges
only the `values` list back into the selected file. Top-level fields such as
`owner`, `team`, and `lastRevision`, comments, and the formatting of unchanged
values are preserved. New keys are added and keys removed by scripts are removed.
The file is only rewritten when values actually changed, and it is skipped if it
has unsaved editor changes or was changed on disk while the request was running.
Both YAML and JSON environment files are supported.

### Duplicate request names

Postman allows several requests in one folder to share a name. The native Git
file names are still unique, and this extension computes the exact occurrence
the runner expects (for example `Organization/Update/... (9/11)`), so the file
you opened is the file that runs.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `roastman.executablePath` | empty | Path to the `roastman` executable. Empty means: use a `roastman` binary at the workspace root, then `roastman` from `PATH`. A relative path is resolved against the first workspace folder. A bare command name is looked up on `PATH` if it is not present in the workspace. |
| `roastman.requestTimeoutSeconds` | `30` | Per-request timeout passed to `roastman`. |
| `roastman.updateEnvironmentFile` | `false` | After a run, merge values changed by scripts back into the selected environment file. |

## What it does not do

This is intentionally not a collection runner UI:

- It runs one request at a time, not a folder or collection.
- It only supports native Git HTTP requests (`$kind: http-request`). Other
  native request kinds (gRPC, WebSocket, Socket.IO, MQTT, and so on) are not
  supported by the `roastman` runner. GraphQL bodies on HTTP requests do work.
- Environment changes made by scripts are not written back unless
  `roastman.updateEnvironmentFile` is enabled. Even then only the selected
  environment file is updated; globals and other environment files are not.
- Very large response bodies are displayed up to 512 KB. The complete report is
  still received and validated by the extension.

## Development

```sh
npm install
npm run compile   # type-check and emit out/ (used by the tests)
npm test          # unit tests plus a real roastman execution against a local server
npm run bundle    # bundle dist/extension.js for VS Code
npm run package   # produce roastman-vscode.vsix
```

The tests need `roastman` at the repository root or on `PATH`; set
`ROASTMAN_BIN=/path/to/roastman` to point at a specific binary.

Open this folder in VS Code and press F5 to launch an Extension Development
Host with the repository root as the workspace.
