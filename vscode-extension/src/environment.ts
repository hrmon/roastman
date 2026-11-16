import { promises as fs } from "node:fs";
import * as path from "node:path";
import {
  isMap,
  isScalar,
  isSeq,
  parse,
  parseDocument,
  type YAMLMap,
} from "yaml";

export interface EnvironmentEntry {
  key: string;
  value: string;
  enabled: boolean;
  type?: string;
}

export interface MergeEnvironmentResult {
  text: string;
  changed: boolean;
}

/**
 * Parses the `values` array of a Postman environment file. Values are
 * normalized the same way the Go runner does so that an unchanged environment
 * can be detected without rewriting the file.
 */
export function parseEnvironmentEntries(text: string, filePath: string): EnvironmentEntry[] {
  const source = stripBom(text);
  const document = isJsonEnvironment(source, filePath)
    ? parseJsonEnvironment(source, filePath)
    : parseYamlEnvironment(source, filePath);

  if (!isRecord(document)) {
    throw new Error(`${filePath} is not a Postman environment file.`);
  }

  const values = Array.isArray(document.values) ? document.values : [];
  return values.map((entry, index) => {
    if (!isRecord(entry)) {
      throw new Error(`${filePath}: values[${index}] is not an object.`);
    }
    const key = scalarString(entry.key);
    if (key === undefined) {
      throw new Error(`${filePath}: values[${index}] has no usable key.`);
    }
    return {
      key,
      value: environmentValueString(entry.value),
      enabled: entry.enabled === false || entry.enabled === "false" ? false : true,
      type: typeof entry.type === "string" && entry.type.length > 0 ? entry.type : undefined,
    };
  });
}

/**
 * Merges the values written by `roastman --export-environment` into the
 * selected environment file.
 *
 * Only the `values` array is changed. Top-level fields, comments, per-entry
 * fields, and formatting of unchanged values are preserved. If the effective
 * values are identical, the original text is returned unchanged.
 */
export function mergeEnvironmentText(
  originalText: string,
  filePath: string,
  exportedText: string
): MergeEnvironmentResult {
  const source = stripBom(originalText);
  const originalEntries = parseEnvironmentEntries(source, filePath);
  const exportedEntries = parseEnvironmentEntries(exportedText, filePath);

  if (!environmentEntriesChanged(originalEntries, exportedEntries)) {
    return { text: originalText, changed: false };
  }

  const merged = isJsonEnvironment(source, filePath)
    ? mergeJsonEnvironment(source, filePath, exportedEntries)
    : mergeYamlEnvironment(source, filePath, exportedEntries);

  return { text: (originalText.startsWith("\uFEFF") ? "\uFEFF" : "") + merged, changed: true };
}

/** Writes a file next to the original and renames it over the target. */
export async function writeFileAtomically(filePath: string, content: string): Promise<void> {
  let target = filePath;
  try {
    target = await fs.realpath(filePath);
  } catch {
    // The caller normally verifies the file still exists; use the given path.
  }

  const stat = await fs.stat(target);
  const temporary = path.join(
    path.dirname(target),
    `.${path.basename(target)}.roastman-${process.pid}-${Date.now()}.tmp`
  );

  await fs.writeFile(temporary, content, { mode: stat.mode & 0o777 });
  try {
    await fs.rename(temporary, target);
  } catch (error) {
    await fs.rm(temporary, { force: true }).catch(() => undefined);
    if (process.platform === "win32") {
      // Windows can refuse to rename over an existing file in some setups.
      await fs.writeFile(target, content, { mode: stat.mode & 0o777 });
      return;
    }
    throw error;
  }
}

function environmentEntriesChanged(
  before: EnvironmentEntry[],
  after: EnvironmentEntry[]
): boolean {
  if (before.length !== after.length) {
    return true;
  }
  for (let index = 0; index < before.length; index++) {
    const previous = before[index];
    const next = after[index];
    if (
      previous.key !== next.key ||
      previous.value !== next.value ||
      previous.enabled !== next.enabled
    ) {
      return true;
    }
  }
  return false;
}

function mergeJsonEnvironment(
  source: string,
  filePath: string,
  exportedEntries: EnvironmentEntry[]
): string {
  let document: unknown;
  try {
    document = JSON.parse(source);
  } catch (error) {
    throw new Error(`Cannot parse ${filePath} as JSON: ${errorMessage(error)}`);
  }
  if (!isRecord(document)) {
    throw new Error(`${filePath} is not a Postman environment file.`);
  }

  const existingValues = Array.isArray(document.values) ? document.values : [];
  const byKey = new Map<string, Record<string, unknown>>();
  for (const value of existingValues) {
    if (isRecord(value)) {
      const key = scalarString(value.key);
      if (key !== undefined) {
        byKey.set(key, value);
      }
    }
  }

  document.values = exportedEntries.map((entry) => {
    const existing = byKey.get(entry.key);
    const merged: Record<string, unknown> = existing ? { ...existing } : {};
    merged.key = entry.key;
    merged.value = entry.value;
    merged.enabled = entry.enabled;
    if (entry.type) {
      merged.type = entry.type;
    }
    return merged;
  });

  const indent = jsonIndentOf(source);
  let output = JSON.stringify(document, null, indent);
  if (source.includes("\r\n")) {
    output = output.replace(/\n/g, "\r\n");
  }
  if (source.endsWith("\n")) {
    output += source.endsWith("\r\n") ? "\r\n" : "\n";
  }
  return output;
}

function mergeYamlEnvironment(
  source: string,
  filePath: string,
  exportedEntries: EnvironmentEntry[]
): string {
  const document = parseDocument(source);
  if (document.errors.length > 0) {
    throw new Error(`Cannot parse ${filePath}: ${document.errors[0].message}`);
  }
  if (!isMap(document.contents)) {
    throw new Error(`${filePath} is not a Postman environment file.`);
  }

  const values = document.get("values", true);
  const existingByKey = new Map<string, YAMLMap>();
  if (isSeq(values)) {
    for (const item of values.items) {
      if (!isMap(item)) {
        continue;
      }
      const keyNode = item.get("key", true);
      if (!isScalar(keyNode)) {
        continue;
      }
      const key = scalarString(keyNode.value);
      if (key !== undefined) {
        existingByKey.set(key, item);
      }
    }
  }

  const items = exportedEntries.map((entry) => {
    const existing = existingByKey.get(entry.key);
    if (!existing) {
      return document.createNode({
        key: entry.key,
        value: entry.value,
        enabled: entry.enabled,
        ...(entry.type ? { type: entry.type } : {}),
      });
    }

    const valueNode = existing.get("value", true);
    if (!isScalar(valueNode) || environmentValueString(valueNode.value) !== entry.value) {
      existing.set("value", entry.value);
    }

    if (existing.has("enabled")) {
      const enabledNode = existing.get("enabled", true);
      if (isScalar(enabledNode)) {
        const enabled = enabledNode.value !== false && enabledNode.value !== "false";
        if (enabled !== entry.enabled) {
          existing.set("enabled", entry.enabled);
        }
      } else {
        existing.set("enabled", entry.enabled);
      }
    } else if (!entry.enabled) {
      existing.set("enabled", false);
    }

    if (entry.type) {
      const typeNode = existing.get("type", true);
      if (!isScalar(typeNode) || typeNode.value !== entry.type) {
        existing.set("type", entry.type);
      }
    }

    return existing;
  });

  if (isSeq(values)) {
    values.items = items;
  } else {
    document.set(
      "values",
      document.createNode(
        exportedEntries.map((entry) => ({
          key: entry.key,
          value: entry.value,
          enabled: entry.enabled,
          ...(entry.type ? { type: entry.type } : {}),
        }))
      )
    );
  }

  let output = document.toString({ lineWidth: 0 });
  if (!source.endsWith("\n")) {
    output = output.replace(/\n$/, "");
  }
  if (source.includes("\r\n")) {
    output = output.replace(/\r?\n/g, "\r\n");
  }
  return output;
}

function parseJsonEnvironment(source: string, filePath: string): unknown {
  try {
    return JSON.parse(source);
  } catch (error) {
    throw new Error(`Cannot parse ${filePath} as JSON: ${errorMessage(error)}`);
  }
}

function parseYamlEnvironment(source: string, filePath: string): unknown {
  try {
    return parse(source);
  } catch (error) {
    throw new Error(`Cannot parse ${filePath}: ${errorMessage(error)}`);
  }
}

function isJsonEnvironment(source: string, filePath: string): boolean {
  return (
    path.extname(filePath).toLowerCase() === ".json" || source.trimStart().startsWith("{")
  );
}

function jsonIndentOf(source: string): string | number | undefined {
  if (!source.includes("\n")) {
    return undefined;
  }
  const match = /\n([ \t]+)"/.exec(source);
  return match ? match[1] : 2;
}

function environmentValueString(value: unknown): string {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value);
}

function scalarString(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value);
  }
  if (typeof value === "boolean") {
    return value ? "true" : "false";
  }
  return undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stripBom(text: string): string {
  return text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
