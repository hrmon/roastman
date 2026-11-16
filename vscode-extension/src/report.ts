/** Types and formatting helpers for `roastman run --report json`. */

export interface TestResult {
  name: string;
  passed: boolean;
  error?: string;
}

export interface ItemReport {
  iteration?: number;
  id?: string;
  name: string;
  displayName?: string;
  folder?: string;
  method?: string;
  url?: string;
  status?: string;
  code?: number;
  responseTimeMs?: number;
  responseSize?: number;
  durationMs?: number;
  tests?: TestResult[];
  console?: string[];
  error?: string;
  skipped?: boolean;
  failed?: boolean;
  warnings?: string[];
  requestHeaders?: Record<string, string[]>;
  requestBody?: string;
  responseHeaders?: Record<string, string[]>;
  responseBody?: string;
}

export interface RunReport {
  collection: string;
  environment?: string;
  startedAt?: string;
  finishedAt?: string;
  durationMs?: number;
  iterations?: number;
  requests?: number;
  assertions?: number;
  failures?: number;
  errors?: number;
  warnings?: number;
  skipped?: number;
  items: ItemReport[];
}

export interface BodyDisplay {
  text: string;
  json: boolean;
  truncated: boolean;
}

export const MAX_DISPLAY_BODY_CHARS = 512 * 1024;

/**
 * Extracts a report from stdout. The runner exits non-zero when tests fail but
 * still writes a complete JSON report, so callers must parse stdout before
 * treating the exit code as fatal.
 */
export function parseReport(stdout: string): RunReport | undefined {
  const text = stdout.trim();
  if (!text) {
    return undefined;
  }

  const candidates = [text];
  const firstBrace = text.indexOf("{");
  const lastBrace = text.lastIndexOf("}");
  if (firstBrace >= 0 && lastBrace > firstBrace) {
    candidates.push(text.slice(firstBrace, lastBrace + 1));
  }

  for (const candidate of candidates) {
    try {
      const value: unknown = JSON.parse(candidate);
      if (
        typeof value === "object" &&
        value !== null &&
        Array.isArray((value as { items?: unknown }).items)
      ) {
        return value as RunReport;
      }
    } catch {
      // Try the next candidate.
    }
  }
  return undefined;
}

export function prettyResponseBody(body: string | undefined): BodyDisplay {
  if (!body) {
    return { text: "", json: false, truncated: false };
  }

  if (body.length > MAX_DISPLAY_BODY_CHARS) {
    return { text: body.slice(0, MAX_DISPLAY_BODY_CHARS), json: false, truncated: true };
  }

  try {
    const value: unknown = JSON.parse(body);
    if (typeof value === "object" && value !== null) {
      const pretty = JSON.stringify(value, null, 2);
      if (pretty.length > MAX_DISPLAY_BODY_CHARS) {
        return { text: pretty.slice(0, MAX_DISPLAY_BODY_CHARS), json: true, truncated: true };
      }
      return { text: pretty, json: true, truncated: false };
    }
  } catch {
    // Not JSON; show the body verbatim.
  }

  return { text: body, json: false, truncated: false };
}

export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined || !Number.isFinite(bytes) || bytes < 0) {
    return "—";
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function formatDuration(milliseconds: number | undefined): string {
  if (milliseconds === undefined || !Number.isFinite(milliseconds) || milliseconds < 0) {
    return "—";
  }
  if (milliseconds < 1000) {
    return `${Math.round(milliseconds)} ms`;
  }
  return `${(milliseconds / 1000).toFixed(1)} s`;
}

export function countTests(tests: TestResult[] | undefined): { passed: number; failed: number } {
  let passed = 0;
  let failed = 0;
  for (const test of tests ?? []) {
    if (test.passed) {
      passed++;
    } else {
      failed++;
    }
  }
  return { passed, failed };
}
