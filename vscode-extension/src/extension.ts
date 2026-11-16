import * as crypto from "node:crypto";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import * as vscode from "vscode";

import { mergeEnvironmentText, writeFileAtomically } from "./environment";
import {
  buildRequestTarget,
  isRequestFile,
  readEnvironmentName,
  type RequestTarget,
} from "./nativeGit";
import {
  buildRunArguments,
  formatCommand,
  RoastmanNotFoundError,
  RoastmanRunError,
  runRoastman,
} from "./roastman";
import { renderErrorHtml, renderLoadingHtml, renderReportHtml } from "./webview";

const ENVIRONMENT_STATE_KEY = "roastman.environmentPath";
const OUTPUT_CHANNEL_NAME = "Roastman";

let extensionContext: vscode.ExtensionContext;
let output: vscode.OutputChannel;
let environmentStatus: vscode.StatusBarItem;
let currentPanel: vscode.WebviewPanel | undefined;
let currentRun: AbortController | undefined;
let runGeneration = 0;
let statusUpdateGeneration = 0;

export function activate(context: vscode.ExtensionContext): void {
  extensionContext = context;
  output = vscode.window.createOutputChannel(OUTPUT_CHANNEL_NAME);
  context.subscriptions.push(output);

  environmentStatus = vscode.window.createStatusBarItem(
    "roastman.environment",
    vscode.StatusBarAlignment.Right,
    100
  );
  environmentStatus.command = "roastman.selectEnvironment";
  context.subscriptions.push(environmentStatus);

  context.subscriptions.push(
    vscode.commands.registerCommand("roastman.runRequest", (uri?: unknown) =>
      runRequest(uri)
    ),
    vscode.commands.registerCommand("roastman.selectEnvironment", () =>
      selectEnvironment()
    ),
    vscode.languages.registerCodeLensProvider(
      { language: "yaml", scheme: "file" },
      new RunRequestCodeLensProvider()
    ),
    vscode.window.onDidChangeActiveTextEditor(() => void updateEnvironmentStatus()),
    vscode.workspace.onDidChangeWorkspaceFolders(() => void updateEnvironmentStatus())
  );

  void updateEnvironmentStatus();
}

export function deactivate(): void {
  currentRun?.abort();
}

class RunRequestCodeLensProvider implements vscode.CodeLensProvider {
  provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
    if (document.uri.scheme !== "file" || !isRequestFile(path.basename(document.uri.fsPath))) {
      return [];
    }
    return [
      new vscode.CodeLens(new vscode.Range(0, 0, 0, 0), {
        title: "$(play) Run request",
        command: "roastman.runRequest",
        arguments: [document.uri],
      }),
    ];
  }
}

async function runRequest(argument?: unknown): Promise<void> {
  const uri = requestUriFromArgument(argument);
  if (!uri || uri.scheme !== "file") {
    void vscode.window.showErrorMessage(
      "Roastman: open a Postman v3 .request.yaml file before running a request."
    );
    return;
  }

  const fileName = path.basename(uri.fsPath);
  if (!isRequestFile(fileName)) {
    void vscode.window.showErrorMessage(
      `Roastman: ${fileName} is not a Postman v3 request file.`
    );
    return;
  }

  if (!(await saveOpenDocument(uri.fsPath))) {
    return;
  }

  let target: RequestTarget;
  try {
    target = await buildRequestTarget(uri.fsPath);
  } catch (error) {
    const message = errorMessage(error);
    output?.appendLine(`[error] ${message}`);
    showErrorPage("Cannot run this request", message);
    return;
  }

  const environment = await ensureEnvironment();
  if (!environment) {
    return;
  }
  if (environment.path && !(await saveOpenDocument(environment.path))) {
    return;
  }

  let executable: string;
  try {
    executable = await resolveExecutable();
  } catch (error) {
    const message = errorMessage(error);
    output?.appendLine(`[error] ${message}`);
    showErrorPage("Cannot start roastman", message);
    await vscode.window.showErrorMessage(`Roastman: ${message}`, "Open Settings").then(
      (choice) => {
        if (choice) {
          void vscode.commands.executeCommand(
            "workbench.action.openSettings",
            "roastman.executablePath"
          );
        }
      }
    );
    return;
  }

  const generation = ++runGeneration;
  currentRun?.abort();
  const abort = new AbortController();
  currentRun = abort;

  const environmentLabel = environment.path
    ? await readEnvironmentName(environment.path)
    : "No environment";
  const requestFileLabel =
    path.relative(target.collectionRoot, target.requestFile) || path.basename(target.requestFile);
  const panel = getOrCreatePanel();

  panel.title = `Roastman: ${target.requestName}`;
  panel.webview.html = renderLoadingHtml({
    name: target.displayName,
    method: target.method,
    url: target.url,
    environmentLabel,
    collectionName: target.collectionName,
    requestFileLabel,
    nonce: createNonce(),
    cspSource: panel.webview.cspSource,
  });

  const runningMessage = vscode.window.setStatusBarMessage(
    `$(sync~spin) Roastman: running ${target.requestName}…`
  );

  let exportDirectory: string | undefined;
  let exportPath: string | undefined;
  let environmentSnapshot: string | undefined;
  let updateEnvironment = false;
  let command = "";

  try {
    if (environment.path && updateEnvironmentFileEnabled()) {
      try {
        environmentSnapshot = await fs.promises.readFile(environment.path, "utf8");
        exportDirectory = await fs.promises.mkdtemp(
          path.join(os.tmpdir(), "roastman-vscode-env-")
        );
        const extension =
          path.extname(environment.path).toLowerCase() === ".json" ? ".json" : ".yaml";
        exportPath = path.join(exportDirectory, `environment${extension}`);
        updateEnvironment = true;
      } catch (error) {
        const message = errorMessage(error);
        output?.appendLine(
          `[warning] Environment write-back disabled for this run: ${message}`
        );
        void vscode.window.showWarningMessage(
          `Roastman: could not prepare environment write-back. ${truncate(message, 300)}`
        );
        if (exportDirectory) {
          await fs.promises
            .rm(exportDirectory, { recursive: true, force: true })
            .catch(() => undefined);
        }
        exportDirectory = undefined;
        exportPath = undefined;
        environmentSnapshot = undefined;
      }
    }

    const timeoutSeconds = requestTimeoutSeconds();
    const args = buildRunArguments({
      collectionRoot: target.collectionRoot,
      itemSelector: target.itemSelector,
      environmentPath: environment.path,
      exportEnvironmentPath: updateEnvironment ? exportPath : undefined,
      timeoutSeconds,
    });
    command = formatCommand(executable, args);
    output?.appendLine(`[run] ${command}`);

    const result = await runRoastman({
      executable,
      collectionRoot: target.collectionRoot,
      itemSelector: target.itemSelector,
      environmentPath: environment.path,
      exportEnvironmentPath: updateEnvironment ? exportPath : undefined,
      timeoutSeconds,
      cwd: target.collectionRoot,
      signal: abort.signal,
    });

    if (generation !== runGeneration) {
      return;
    }

    if (result.stderr.trim()) {
      output?.appendLine(result.stderr.trim());
    }
    output?.appendLine(
      `[done] ${result.report.requests ?? result.report.items.length} request(s), ` +
        `${result.report.failures} failure(s), ${result.report.errors} error(s), ` +
        `exit ${result.exitCode ?? "unknown"} in ${result.durationMs} ms`
    );

    if (
      updateEnvironment &&
      environment.path &&
      exportPath &&
      environmentSnapshot !== undefined
    ) {
      try {
        await writeBackEnvironmentFile(environment.path, exportPath, environmentSnapshot);
      } catch (error) {
        const message = `Could not update the environment file: ${errorMessage(error)}`;
        output?.appendLine(`[warning] ${message}`);
        void vscode.window.showWarningMessage(`Roastman: ${truncate(message, 300)}`);
      }
    }

    const item = result.report.items[0];
    if (!item) {
      showErrorPage(
        "No request was run",
        "roastman returned an empty report. The request may not have been selected correctly."
      );
      return;
    }

    const completedPanel = getOrCreatePanel();
    completedPanel.title = `Roastman: ${item.displayName || item.name || target.requestName}`;
    completedPanel.webview.html = renderReportHtml({
      report: result.report,
      item,
      requestFileLabel,
      environmentLabel,
      nonce: createNonce(),
      cspSource: completedPanel.webview.cspSource,
    });
  } catch (error) {
    runningMessage.dispose();
    if (exportDirectory) {
      await fs.promises
        .rm(exportDirectory, { recursive: true, force: true })
        .catch(() => undefined);
      exportDirectory = undefined;
    }
    if (error instanceof Error && error.name === "AbortError") {
      return;
    }
    if (generation !== runGeneration) {
      return;
    }

    const message = errorMessage(error);
    output?.appendLine(`[error] ${message}`);
    const details = formatErrorDetails(error, command);

    if (error instanceof RoastmanRunError) {
      if (error.details.stderr.trim()) {
        output?.appendLine("stderr:");
        output?.appendLine(error.details.stderr.trim());
      }
      if (error.details.stdout.trim()) {
        output?.appendLine("stdout:");
        output?.appendLine(truncate(error.details.stdout.trim(), 20000));
      }
    }

    showErrorPage("Request failed to run", message, details);

    if (error instanceof RoastmanNotFoundError) {
      const choice = await vscode.window.showErrorMessage(
        `Roastman: ${error.message}`,
        "Open Settings"
      );
      if (choice) {
        await vscode.commands.executeCommand(
          "workbench.action.openSettings",
          "roastman.executablePath"
        );
      }
    } else {
      const choice = await vscode.window.showErrorMessage(
        `Roastman: ${truncate(message, 300)}`,
        "Show Output"
      );
      if (choice) {
        output?.show();
      }
    }
  } finally {
    if (exportDirectory) {
      await fs.promises
        .rm(exportDirectory, { recursive: true, force: true })
        .catch(() => undefined);
    }
    runningMessage.dispose();
    if (generation === runGeneration) {
      currentRun = undefined;
    }
  }
}

async function writeBackEnvironmentFile(
  environmentPath: string,
  exportPath: string,
  originalText: string
): Promise<void> {
  if (isOpenDocumentDirty(environmentPath)) {
    output?.appendLine(
      `[env] skipped ${environmentPath}: the file has unsaved editor changes`
    );
    void vscode.window.showWarningMessage(
      "Roastman: the environment file has unsaved changes, so it was not updated."
    );
    return;
  }

  const currentText = await fs.promises.readFile(environmentPath, "utf8");
  if (currentText !== originalText) {
    output?.appendLine(
      `[env] skipped ${environmentPath}: the file changed while the request was running`
    );
    void vscode.window.showWarningMessage(
      "Roastman: the environment file changed while the request was running, so it was not updated."
    );
    return;
  }

  const exportedText = await fs.promises.readFile(exportPath, "utf8");
  const merged = mergeEnvironmentText(originalText, environmentPath, exportedText);
  if (!merged.changed) {
    output?.appendLine(`[env] no environment changes for ${environmentPath}`);
    return;
  }

  await writeFileAtomically(environmentPath, merged.text);
  output?.appendLine(`[env] updated ${environmentPath}`);
  vscode.window.setStatusBarMessage("$(check) Roastman: environment updated", 3000);
}

function isOpenDocumentDirty(filePath: string): boolean {
  const absolute = path.resolve(filePath);
  return vscode.workspace.textDocuments.some(
    (document) =>
      document.uri.scheme === "file" &&
      path.resolve(document.uri.fsPath) === absolute &&
      document.isDirty
  );
}

function updateEnvironmentFileEnabled(): boolean {
  return (
    vscode.workspace
      .getConfiguration("roastman")
      .get<boolean>("updateEnvironmentFile", false) === true
  );
}

async function selectEnvironment(): Promise<void> {
  await pickEnvironment();
}

async function ensureEnvironment(): Promise<{ path?: string } | undefined> {
  const selection = extensionContext.workspaceState.get<string>(ENVIRONMENT_STATE_KEY);
  if (selection === undefined) {
    return await pickEnvironment();
  }
  if (selection === "") {
    return {};
  }

  try {
    await fs.promises.access(selection, fs.constants.R_OK);
    return { path: selection };
  } catch {
    const choice = await vscode.window.showWarningMessage(
      `Roastman: the selected environment file is missing: ${selection}`,
      "Select Environment"
    );
    if (choice) {
      return await pickEnvironment();
    }
    return undefined;
  }
}

async function pickEnvironment(): Promise<{ path?: string } | undefined> {
  const items = await environmentQuickPickItems();
  const current = extensionContext.workspaceState.get<string>(ENVIRONMENT_STATE_KEY);
  const currentLabel =
    current === undefined
      ? "none selected"
      : current === ""
        ? "no environment"
        : await readEnvironmentName(current);

  const choice = await vscode.window.showQuickPick(items, {
    placeHolder: `Select an environment for Roastman requests (current: ${currentLabel})`,
    matchOnDescription: true,
    ignoreFocusOut: true,
  });
  if (!choice) {
    return undefined;
  }

  if (choice.action === "separator") {
    return undefined;
  }

  if (choice.action === "browse") {
    const picked = await vscode.window.showOpenDialog({
      canSelectMany: false,
      openLabel: "Use environment",
      filters: { "Postman environments": ["yaml", "yml", "json"] },
      defaultUri:
        current && path.isAbsolute(current) && fs.existsSync(current)
          ? vscode.Uri.file(path.dirname(current))
          : vscode.workspace.workspaceFolders?.[0]?.uri,
    });
    const selected = picked?.[0];
    if (!selected) {
      return undefined;
    }
    await extensionContext.workspaceState.update(ENVIRONMENT_STATE_KEY, selected.fsPath);
    void updateEnvironmentStatus();
    return { path: selected.fsPath };
  }

  if (choice.action === "none") {
    await extensionContext.workspaceState.update(ENVIRONMENT_STATE_KEY, "");
    void updateEnvironmentStatus();
    return {};
  }

  if (!choice.path) {
    return undefined;
  }
  await extensionContext.workspaceState.update(ENVIRONMENT_STATE_KEY, choice.path);
  void updateEnvironmentStatus();
  return { path: choice.path };
}

interface EnvironmentQuickPickItem extends vscode.QuickPickItem {
  action: "env" | "none" | "browse" | "separator";
  path?: string;
}

async function environmentQuickPickItems(): Promise<EnvironmentQuickPickItem[]> {
  const current = extensionContext.workspaceState.get<string>(ENVIRONMENT_STATE_KEY);
  const uris = await discoverEnvironmentFiles();
  const seen = new Set<string>();
  const entries: Array<{
    path: string;
    name: string;
    relative: string;
  }> = [];

  for (const uri of uris) {
    if (seen.has(uri.fsPath)) {
      continue;
    }
    seen.add(uri.fsPath);
    entries.push({
      path: uri.fsPath,
      name: await readEnvironmentName(uri.fsPath),
      relative: vscode.workspace.asRelativePath(uri, true),
    });
  }

  if (current && !seen.has(current) && fs.existsSync(current)) {
    entries.push({
      path: current,
      name: await readEnvironmentName(current),
      relative: "outside the workspace",
    });
  }

  entries.sort((a, b) => a.name.localeCompare(b.name) || a.path.localeCompare(b.path));

  const items: EnvironmentQuickPickItem[] = entries.map((entry) => ({
    label: `$(file) ${entry.name}`,
    description:
      entry.path === current ? `current · ${entry.relative}` : entry.relative,
    action: "env",
    path: entry.path,
  }));

  items.push({
    label: "$(circle-slash) No environment",
    description: current === "" ? "current" : "Run the request without an environment",
    action: "none",
  });
  items.push({
    label: "$(folder-opened) Browse for environment file…",
    action: "browse",
  });

  return items;
}

async function discoverEnvironmentFiles(): Promise<vscode.Uri[]> {
  const patterns = [
    "**/*.environment.yaml",
    "**/*.environment.yml",
    "**/*.postman_environment.json",
    "**/environments/**/*.yaml",
    "**/environments/**/*.yml",
    "**/environments/**/*.json",
  ];

  try {
    const groups = await Promise.all(
      patterns.map((pattern) =>
        vscode.workspace.findFiles(pattern, "**/{node_modules,.git}/**", 500)
      )
    );
    const seen = new Set<string>();
    const files: vscode.Uri[] = [];
    for (const group of groups) {
      for (const uri of group) {
        if (!seen.has(uri.toString())) {
          seen.add(uri.toString());
          files.push(uri);
        }
      }
    }
    return files;
  } catch (error) {
    output?.appendLine(`[warning] Could not scan for environment files: ${errorMessage(error)}`);
    return [];
  }
}

async function updateEnvironmentStatus(): Promise<void> {
  const generation = ++statusUpdateGeneration;
  const editor = vscode.window.activeTextEditor;
  const hasRequestEditor =
    (vscode.window.visibleTextEditors ?? []).some((visible) => isRequestDocument(visible.document)) ||
    (editor !== undefined && isRequestDocument(editor.document));
  if (!hasRequestEditor) {
    environmentStatus.hide();
    return;
  }

  const selection = extensionContext.workspaceState.get<string>(ENVIRONMENT_STATE_KEY);
  if (selection === undefined) {
    environmentStatus.text = "$(layers) Env: select…";
    environmentStatus.tooltip = "Select a Postman environment for Roastman requests";
    environmentStatus.color = undefined;
    environmentStatus.show();
    return;
  }

  if (selection === "") {
    environmentStatus.text = "$(layers) Env: none";
    environmentStatus.tooltip = "No environment is used. Click to select one.";
    environmentStatus.color = undefined;
    environmentStatus.show();
    return;
  }

  let name: string;
  let missing = false;
  try {
    await fs.promises.access(selection, fs.constants.R_OK);
    name = await readEnvironmentName(selection);
  } catch {
    name = "missing";
    missing = true;
  }
  if (generation !== statusUpdateGeneration) {
    return;
  }

  environmentStatus.text = missing ? "$(warning) Env: missing" : `$(layers) Env: ${name}`;
  environmentStatus.tooltip = `Environment: ${selection}\nClick to change`;
  environmentStatus.color = missing
    ? new vscode.ThemeColor("statusBarItem.warningForeground")
    : undefined;
  environmentStatus.show();
}

async function resolveExecutable(): Promise<string> {
  const configured = vscode.workspace
    .getConfiguration("roastman")
    .get<string>("executablePath", "")
    .trim();
  const workspaceRoot = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;

  if (configured) {
    if (path.isAbsolute(configured)) {
      if (!(await isExecutableFile(configured))) {
        throw new Error(
          `roastman executable not found at ${configured}. Update "roastman.executablePath" or install roastman in PATH.`
        );
      }
      return configured;
    }

    if (configured.includes("/") || configured.includes("\\")) {
      const executable = path.resolve(workspaceRoot ?? process.cwd(), configured);
      if (!(await isExecutableFile(executable))) {
        throw new Error(
          `roastman executable not found at ${executable}. Update "roastman.executablePath" or install roastman in PATH.`
        );
      }
      return executable;
    }

    if (workspaceRoot) {
      for (const name of executableNames()) {
        const executable = path.join(workspaceRoot, name);
        if (await isExecutableFile(executable)) {
          return executable;
        }
      }
    }
    return configured;
  }

  for (const folder of vscode.workspace.workspaceFolders ?? []) {
    for (const name of executableNames()) {
      const candidate = path.join(folder.uri.fsPath, name);
      if (await isExecutableFile(candidate)) {
        return candidate;
      }
    }
  }

  return executableNames()[0];
}

function executableNames(): string[] {
  return process.platform === "win32" ? ["roastman.exe", "roastman"] : ["roastman"];
}

async function isExecutableFile(filePath: string): Promise<boolean> {
  try {
    const stat = await fs.promises.stat(filePath);
    if (!stat.isFile()) {
      return false;
    }
    if (process.platform !== "win32") {
      await fs.promises.access(filePath, fs.constants.X_OK);
    }
    return true;
  } catch {
    return false;
  }
}

function requestTimeoutSeconds(): number {
  const value = vscode.workspace
    .getConfiguration("roastman")
    .get<number>("requestTimeoutSeconds", 30);
  return Number.isFinite(value) ? Math.min(3600, Math.max(1, Math.round(value))) : 30;
}

async function saveOpenDocument(filePath: string): Promise<boolean> {
  const absolute = path.resolve(filePath);
  const document = vscode.workspace.textDocuments.find(
    (candidate) =>
      candidate.uri.scheme === "file" && path.resolve(candidate.uri.fsPath) === absolute
  );
  if (!document?.isDirty) {
    return true;
  }
  const saved = await document.save();
  if (!saved) {
    void vscode.window.showErrorMessage(
      `Roastman: save ${path.basename(filePath)} before running the request.`
    );
  }
  return saved;
}

function isRequestDocument(document: vscode.TextDocument): boolean {
  return (
    document.uri.scheme === "file" &&
    isRequestFile(path.basename(document.uri.fsPath))
  );
}

function requestUriFromArgument(argument: unknown): vscode.Uri | undefined {
  if (argument instanceof vscode.Uri) {
    return argument;
  }
  return vscode.window.activeTextEditor?.document.uri;
}

function getOrCreatePanel(): vscode.WebviewPanel {
  if (currentPanel) {
    currentPanel.reveal(undefined, true);
    return currentPanel;
  }

  const panel = vscode.window.createWebviewPanel(
    "roastman.output",
    "Roastman",
    { viewColumn: vscode.ViewColumn.Beside, preserveFocus: true },
    {
      enableScripts: true,
      enableFindWidget: true,
      retainContextWhenHidden: true,
      localResourceRoots: [],
    }
  );
  panel.webview.onDidReceiveMessage(async (message: unknown) => {
    if (
      typeof message === "object" &&
      message !== null &&
      (message as { type?: unknown }).type === "copy" &&
      typeof (message as { text?: unknown }).text === "string"
    ) {
      try {
        await vscode.env.clipboard.writeText((message as { text: string }).text);
      } catch (error) {
        output?.appendLine(`[warning] Could not copy to the clipboard: ${errorMessage(error)}`);
      }
    }
  });
  panel.onDidDispose(() => {
    if (currentPanel === panel) {
      currentPanel = undefined;
    }
  });
  currentPanel = panel;
  return panel;
}

function showErrorPage(title: string, message: string, details?: string): void {
  const panel = getOrCreatePanel();
  panel.title = `Roastman: ${title}`;
  panel.webview.html = renderErrorHtml({
    title,
    message,
    details,
    nonce: createNonce(),
    cspSource: panel.webview.cspSource,
  });
}

function formatErrorDetails(error: unknown, command: string): string {
  const lines: string[] = [`Command: ${command}`];
  if (error instanceof RoastmanRunError) {
    lines.push(`Exit code: ${error.details.exitCode ?? "unknown"}`);
    if (error.details.signal) {
      lines.push(`Signal: ${error.details.signal}`);
    }
    if (error.details.stderr.trim()) {
      lines.push("", "stderr:", error.details.stderr.trim());
    }
    if (error.details.stdout.trim()) {
      lines.push("", "stdout:", truncate(error.details.stdout.trim(), 20000));
    }
  } else if (error instanceof Error) {
    if (error.stack) {
      lines.push("", truncate(error.stack, 20000));
    }
  }
  return lines.join("\n");
}

function createNonce(): string {
  return crypto.randomBytes(16).toString("base64");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function truncate(value: string, maxLength: number): string {
  return value.length <= maxLength ? value : `${value.slice(0, maxLength)}\n…`;
}
