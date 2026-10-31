# roastman

`roastman` converts between a Postman Collection v2.1 JSON file and Postman's
git-native local file layout (the v3 YAML format used by **Postman Native Git**
/ local project folders), in both directions. It also includes an offline
collection runner that reads the local file format directly.

The important property: converting an unchanged collection

```
collection.json  ->  local files  ->  collection.json
```

produces a **byte-for-byte identical JSON file**. Small, readable YAML files
can live in Git without re-exporting the collection and creating a noisy diff.

## Install

```sh
go build -o roastman .
# or
go install .
```

Requires Go 1.26+. The runner embeds Sobek (a pure-Go JavaScript engine), so no Node.js or npm installation is needed.

## Usage

### JSON -> local files

```sh
roastman to-files data/test.postman_collection.json -o ./example-collection
```

The output is a normal native Git collection directory:

```text
example-collection/
├── .resources/
│   ├── definition.yaml
│   └── .roastman.json              # collection metadata + formatting
├── Admin/
│   ├── .resources/
│   │   ├── definition.yaml
│   │   └── .roastman.json          # folder metadata, when needed
│   ├── Authenticate.request.yaml
│   └── Authenticate/
│       ├── .resources/
│       │   ├── definition.yaml
│       │   └── .roastman/
│       │       └── Admin - Authenticate - 200.json  # request metadata
│       └── Admin - Authenticate - 200.request.yaml
```

If `-o` is omitted, the directory is named after the collection. Use
`--force` to replace a non-empty output directory.

`-o` can also point at a workspace root (containing `postman/collections`) or
at the `postman/` / `collections/` directory itself; the collection is placed
in the appropriate subdirectory automatically. A standalone collection
directory works too, which is useful when you just want to inspect the files.

### Local files -> JSON

```sh
roastman to-json ./example-collection -o example-collection.postman_collection.json
```

`to-json` accepts any of these paths:

- a collection directory containing `.resources/definition.yaml`
- a workspace `postman/` directory
- a `postman/collections/` directory
- the `.resources/definition.yaml` file itself

When a workspace contains more than one collection, select one:

```sh
roastman to-json ./postman --collection example-collection -o example-collection.json
```

Without `-o`, the JSON is written to stdout, which is handy for piping:

```sh
roastman to-json ./example-collection | jq '.info.name'
```

### Aliases and help

| Command | Aliases |
| --- | --- |
| `to-files` | `split`, `export` |
| `to-json` | `join`, `import` |

```sh
roastman help
roastman version
```

## Run a collection offline

```sh
roastman run ./example-collection \
  --environment Local \
  --data data.csv \
  --iterations 3 \
  --timeout 30s
```

`run` accepts a native Git collection directory, a workspace/`postman`
directory, or a collection JSON file. It executes requests sequentially, applies
collection/folder/request scripts, and keeps all state in memory. It makes no
Postman Cloud, telemetry, or update requests.

Common flags:

```text
    --collection NAME            Select a collection when the workspace has several
-e, --environment NAME_OR_FILE   Environment YAML, or a name found in the workspace
-g, --globals FILE               Globals YAML (workspace globals are auto-detected)
-d, --data FILE.csv|FILE.json    Data-driven iterations
-n, --iterations N               Number of iterations (default: one per data row)
-i, --item NAME_UID_OR_PATH     Run only this request/folder, in the given order (repeatable)
    --folder NAME                Filter execution to a folder (repeatable)
    --request NAME_OR_PATH       Filter execution to a request (repeatable)
    --env-var KEY=VALUE          Override an environment value (repeatable)
    --export-environment FILE    Write the final in-memory environment after the run
    --export-globals FILE        Write the final in-memory globals after the run
    --delay MS                   Delay between requests
    --timeout DURATION           Per-request timeout (default 30s)
    --bail                       Stop at the first failure/error
    --insecure                   Skip TLS certificate verification
    --no-scripts                 Execute requests without JavaScript
    --report cli|json|junit      Report format
    --report-bodies              Include request/response headers and bodies
-o, --output FILE                Write the report to a file
    --verbose                    Print every request and console log
```

`-i/--item` follows Postman CLI semantics: it accepts a request/folder name,
request/folder UID, or a `folder/request` path, and multiple `-i` flags run the
selected items in the order given. When names are duplicated, the first match is
used. The older `--folder`/`--request` flags are filters in collection order.

The CLI report is tree/table oriented like Newman. When writing the CLI report
to stdout, results stream as each request completes, and failed tests are repeated in a
final `Failures:` recap after the summary table. `--verbose` adds test details
and console output; `--quiet` suppresses the per-request stream.
`--report-bodies` additionally includes the final request and response headers
and bodies for every request (it also enables full per-request output). Bodies
can contain secrets, so use it deliberately.

Unresolved variables are warnings, not errors. If a URL still contains
`{{variable}}` after pre-request scripts, that request is skipped with a warning.
Unresolved variables in headers or bodies produce warnings but the request is
still sent. Warnings don't affect the process exit code.

Exit code is `0` when all requests and tests pass and nonzero when a test,
request, or runner error occurs. JSON and JUnit reports are suitable for CI.

Environment/globals mutations made by scripts stay in memory during the run.
Use `--export-environment` / `--export-globals` to write their final state as
native YAML for the next run or for inspection.

### Why not `postman-runtime`?

`postman-runtime` is the reference implementation, but it is a low-level Node.js
library that expects Collection SDK v2 objects. Using it would require a Node
runtime, npm dependencies, and an extra v3-to-v2 transform. `roastman` uses a
native Go HTTP client and an embedded pure-Go JavaScript engine instead, so the
runner remains a single offline binary. It is not a byte-for-byte replacement
for the full Node sandbox.

### Script support

The runner embeds Sobek and exposes the common Postman sandbox API:

- `pm.test`, `pm.expect` with common Chai-style matchers
- `pm.response` (`code`, `status`, `headers`, `json()`, `text()`, `cookies`)
- `pm.environment`, `pm.globals`, `pm.collectionVariables`, `pm.variables`,
  `pm.iterationData`
- `pm.request` headers/URL mutation
- `pm.sendRequest` (synchronous in the current implementation)
- `pm.execution.setNextRequest`, `pm.execution.skipRequest`
- `console.*`, `setTimeout` (implemented as a blocking timer)
- legacy `responseBody`, `responseCode`, `responseHeaders`, `tests`, and
  `postman.setEnvironmentVariable`-style helpers

Scripts using async/await, promises with asynchronous callbacks, `require`,
or Postman packages are not fully supported. Unsupported script APIs produce a
clear error; asynchronous callbacks may need to be rewritten synchronously.
This is the deliberate trade-off for a single binary with no Node dependency.

Core request auth in the runner: inherit, noauth, bearer, basic, API key and
offline OAuth2 access tokens. OAuth flows that fetch tokens, OAuth1, AWS
signature v4, digest and NTLM are not implemented yet.

## What is preserved

The converter implements the native Git HTTP collection schema:

- collections, nested folders, requests, descriptions, IDs and ordering
- headers, query parameters and path variables (including disabled entries and
  descriptions)
- request bodies: raw (`json`, `text`, `xml`, `html`, `javascript`), form-data,
  URL-encoded, file and GraphQL
- pre-request and test scripts (`beforeRequest` / `afterResponse`), including
  collection- and folder-level scripts
- auth: `noauth`, `bearer`, `basic`, `apikey`, and the other v2 auth types
- collection/folder variables
- saved request examples/responses (`.example.yaml` files)
- request settings and `protocolProfileBehavior`
- other v2 fields that have no native representation, via metadata

### Round-trip metadata

Native Git has less information than Postman's v2.1 JSON: it stores object
members in a fixed order, has no place for a few compatibility-only fields
(such as `_exporter_id`, non-empty legacy `packages` maps or form-data
`uuid`s), and doesn't record whether the original JSON used a mixed inline
layout.

`roastman` stores that information in scoped `.roastman` sidecars instead of
adding unsupported custom fields to the v3 YAML:

- collection root: `.resources/.roastman.json`
- each folder: `<folder>/.resources/.roastman.json`
- each request: `<folder>/.resources/.roastman/<request-stem>.json`

Paths inside a request sidecar are relative to that request, for example
`/request/header/0`, not global paths like `/item/3/item/7/...`. Adding or
removing a folder/request therefore does not shift metadata onto another item.
Request sidecars keep the metadata local to the request, so Git diffs stay
localized.

The metadata contains:

- object member order (`orderSets` / `keyOrders`)
- pretty/minified layout, indentation, line endings, final newline, and
  single-line nested objects/arrays
- unusual JSON values that can't be expressed in native Git (`extras`)
- whether a URL was a string or an object, and a few similar presence flags

Postman ignores these files. If you delete a sidecar, conversion still works;
the affected scope uses canonical order/style. If you edit YAML files, the
sidecar only fills back fields that are still unrepresented.

Metadata created by older `roastman` versions is still readable for unchanged
collections. Re-run `to-files` to upgrade a collection to the scoped format
before structurally editing it.

## Development

```sh
go test ./...
go vet ./...
```

The tests include a large integration round trip when
`data/test.postman_collection.json` is available, plus a feature-rich fixture
and tests for reading hand-written native Git layouts without metadata.

## Notes / limitations

- Supported input is Postman Collection v2.1 (the format exported by Postman
  today) with HTTP requests, including GraphQL bodies. Native GraphQL, gRPC,
  WebSocket, MQTT, Socket.IO, LLM and MCP request kinds are not converted as
  native request types.
- `POST`/`PUT` etc. method casing and string values are preserved exactly.
- JSON formatting preservation covers standard pretty-printed and minified
  documents, including mixed inline containers. Highly irregular whitespace
  (for example extra spaces around a specific comma) is normalized.
- The `.resources/.roastman.json` file is local to this tool. Postman can read
  the collection without it.
- The runner executes HTTP requests only and does not support gRPC, WebSocket,
  Socket.IO, MQTT, LLM, or MCP request kinds.
- `pm.sendRequest` and `setTimeout` are synchronous approximations. Postman's
  full asynchronous sandbox lifecycle and npm package resolution are out of
  scope for the built-in engine.
