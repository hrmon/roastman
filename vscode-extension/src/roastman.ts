import { spawn, type ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";

import { parseReport, type RunReport } from "./report";

const DEFAULT_MAX_OUTPUT_BYTES = 128 * 1024 * 1024;
const MAX_STDERR_BYTES = 1024 * 1024;

export class RoastmanNotFoundError extends Error {
  constructor(readonly executable: string, cause?: unknown) {
    super(
      `roastman executable not found: ${executable}. Install roastman or set "roastman.executablePath".`,
      { cause }
    );
    this.name = "RoastmanNotFoundError";
  }
}

export class RoastmanRunError extends Error {
  constructor(
    message: string,
    readonly details: {
      exitCode: number | null;
      signal: NodeJS.Signals | null;
      stdout: string;
      stderr: string;
    }
  ) {
    super(message);
    this.name = "RoastmanRunError";
  }
}

export interface RunOptions {
  executable: string;
  collectionRoot: string;
  itemSelector: string;
  environmentPath?: string;
  exportEnvironmentPath?: string;
  timeoutSeconds: number;
  cwd?: string;
  signal?: AbortSignal;
  maxOutputBytes?: number;
}

export interface RunOutput {
  report: RunReport;
  exitCode: number | null;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
  args: string[];
  durationMs: number;
}

export function buildRunArguments(
  options: Pick<
    RunOptions,
    "collectionRoot" | "itemSelector" | "environmentPath" | "exportEnvironmentPath" | "timeoutSeconds"
  >
): string[] {
  const timeout = Math.max(1, Math.round(options.timeoutSeconds));
  const args = [
    "run",
    options.collectionRoot,
    "--item",
    options.itemSelector,
    "--report",
    "json",
    "--report-bodies",
    "--timeout",
    `${timeout}s`,
  ];
  if (options.environmentPath) {
    args.push("--environment", options.environmentPath);
  }
  if (options.exportEnvironmentPath) {
    args.push("--export-environment", options.exportEnvironmentPath);
  }
  return args;
}

/** Runs `roastman run` for one request and returns its JSON report. */
export async function runRoastman(options: RunOptions): Promise<RunOutput> {
  if (options.signal?.aborted) {
    throw abortError();
  }

  const args = buildRunArguments(options);
  const startedAt = Date.now();
  const maxOutputBytes = options.maxOutputBytes ?? DEFAULT_MAX_OUTPUT_BYTES;

  return await new Promise<RunOutput>((resolve, reject) => {
    let child: ChildProcessByStdio<null, Readable, Readable>;
    try {
      child = spawn(options.executable, args, {
        cwd: options.cwd,
        env: process.env,
        stdio: ["ignore", "pipe", "pipe"],
        windowsHide: true,
      });
    } catch (error) {
      reject(wrapSpawnError(error, options.executable));
      return;
    }

    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    let stdoutBytes = 0;
    let stderrBytes = 0;
    let aborted = false;
    let outputLimitExceeded = false;
    let settled = false;
    let killTimer: NodeJS.Timeout | undefined;

    const cleanup = () => {
      options.signal?.removeEventListener("abort", onAbort);
      if (killTimer) {
        clearTimeout(killTimer);
        killTimer = undefined;
      }
    };

    const finish = (callback: () => void) => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      callback();
    };

    const terminate = () => {
      if (child.exitCode !== null || child.signalCode !== null) {
        return;
      }
      child.kill("SIGTERM");
      killTimer = setTimeout(() => {
        if (child.exitCode === null && child.signalCode === null) {
          child.kill("SIGKILL");
        }
      }, 2000);
      killTimer.unref();
    };

    const onAbort = () => {
      aborted = true;
      terminate();
    };
    options.signal?.addEventListener("abort", onAbort, { once: true });

    child.stdout.on("data", (chunk: Buffer) => {
      if (outputLimitExceeded) {
        return;
      }
      stdoutBytes += chunk.length;
      if (stdoutBytes > maxOutputBytes) {
        outputLimitExceeded = true;
        terminate();
        return;
      }
      stdoutChunks.push(chunk);
    });

    child.stderr.on("data", (chunk: Buffer) => {
      if (stderrBytes >= MAX_STDERR_BYTES) {
        return;
      }
      stderrBytes += chunk.length;
      stderrChunks.push(chunk);
    });

    child.on("error", (error) => {
      finish(() => reject(wrapSpawnError(error, options.executable)));
    });

    child.on("close", (exitCode, signal) => {
      const stdout = Buffer.concat(stdoutChunks).toString("utf8");
      const stderr = Buffer.concat(stderrChunks).toString("utf8");

      if (aborted) {
        finish(() => reject(abortError()));
        return;
      }
      if (outputLimitExceeded) {
        finish(() =>
          reject(
            new RoastmanRunError(
              `roastman produced more than ${Math.round(
                maxOutputBytes / (1024 * 1024)
              )} MiB of output. The report was discarded.`,
              { exitCode, signal, stdout: "", stderr }
            )
          )
        );
        return;
      }

      const report = parseReport(stdout);
      if (!report) {
        const reason =
          (exitCode ?? 0) === 0
            ? "roastman exited without producing a report."
            : `roastman exited with ${describeExit(exitCode, signal)}.`;
        finish(() =>
          reject(
            new RoastmanRunError(
              stderr.trim() ? `${reason} ${stderr.trim()}` : reason,
              { exitCode, signal, stdout, stderr }
            )
          )
        );
        return;
      }

      finish(() =>
        resolve({
          report,
          exitCode,
          signal,
          stdout,
          stderr,
          args,
          durationMs: Date.now() - startedAt,
        })
      );
    });
  });
}

export function formatCommand(executable: string, args: readonly string[]): string {
  return [executable, ...args].map(quoteArgument).join(" ");
}

function quoteArgument(argument: string): string {
  if (/^[A-Za-z0-9_@%+=:,./\\-]+$/.test(argument)) {
    return argument;
  }
  return `'${argument.replace(/'/g, `'\\''`)}'`;
}

function wrapSpawnError(error: unknown, executable: string): Error {
  if (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    (error as NodeJS.ErrnoException).code === "ENOENT"
  ) {
    return new RoastmanNotFoundError(executable, error);
  }
  const message = error instanceof Error ? error.message : String(error);
  return new RoastmanRunError(`Could not start roastman: ${message}`, {
    exitCode: null,
    signal: null,
    stdout: "",
    stderr: "",
  });
}

function describeExit(exitCode: number | null, signal: NodeJS.Signals | null): string {
  if (signal) {
    return `signal ${signal}`;
  }
  return `exit code ${exitCode ?? "unknown"}`;
}

function abortError(): Error {
  const error = new Error("The roastman request was cancelled.");
  error.name = "AbortError";
  return error;
}
