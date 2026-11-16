import { constants as fsConstants, promises as fs } from "node:fs";
import * as path from "node:path";
import { parse as parseYaml } from "yaml";

export const REQUEST_SUFFIXES = [".request.yaml", ".request.yml"] as const;
const DEFINITION_NAMES = ["definition.yaml", "definition.yml"] as const;
const SKIPPED_DIRECTORIES = new Set([".resources", ".git", ".postman", "node_modules"]);

export interface RequestTarget {
  /** Absolute path of the request file. */
  requestFile: string;
  /** Absolute path of the native Git collection root. */
  collectionRoot: string;
  collectionName: string;
  /** Folder names (native Git item names), relative to the collection root. */
  folderPath: string;
  requestName: string;
  /** Request name with a `(n/total)` suffix when the name is duplicated. */
  displayName: string;
  /** Value passed to `roastman run -i`. */
  itemSelector: string;
  /** Request method from the YAML, for the loading/error panel. */
  method: string;
  /** Request URL from the YAML, for the loading/error panel. */
  url: string;
  duplicateIndex: number;
  duplicateCount: number;
}

interface Definition {
  name?: string;
  order?: number;
  kind?: string;
}

interface SiblingRequest {
  fileName: string;
  filePath: string;
  name: string;
  order: number;
}

export function isRequestFile(fileName: string): boolean {
  const lower = fileName.toLowerCase();
  return REQUEST_SUFFIXES.some((suffix) => lower.endsWith(suffix));
}

export function requestStem(fileName: string): string {
  const lower = fileName.toLowerCase();
  for (const suffix of REQUEST_SUFFIXES) {
    if (lower.endsWith(suffix)) {
      return fileName.slice(0, fileName.length - suffix.length);
    }
  }
  return fileName;
}

export function environmentNameFromPath(filePath: string): string {
  let base = path.basename(filePath);
  for (const suffix of [
    ".postman_environment.json",
    ".environment.yaml",
    ".environment.yml",
    ".json",
    ".yaml",
    ".yml",
  ]) {
    if (base.toLowerCase().endsWith(suffix)) {
      base = base.slice(0, base.length - suffix.length);
      break;
    }
  }
  return base || path.basename(filePath);
}

/** Reads the display name of a Postman environment file (YAML or JSON). */
export async function readEnvironmentName(filePath: string): Promise<string> {
  try {
    const text = await fs.readFile(filePath, "utf8");
    const document = filePath.toLowerCase().endsWith(".json")
      ? JSON.parse(text)
      : parseYaml(text);
    const name = asScalarString(asRecord(document)?.name);
    if (name) {
      return name;
    }
  } catch {
    // A missing or unparseable environment is handled by the caller; the
    // filename is still a usable label.
  }
  return environmentNameFromPath(filePath);
}

/**
 * Finds the native Git collection root containing a request file.
 *
 * A native Git workspace stores collections under `collections/<name>`, while
 * every folder inside a collection also has a `.resources/definition.yaml`.
 * The nearest `collections` ancestor is therefore preferred; a standalone
 * collection is found by walking up to the highest consecutive definition.
 */
export async function findCollectionRoot(requestFile: string): Promise<string> {
  const absolute = path.resolve(requestFile);
  const parent = path.dirname(absolute);

  const segments = parent.split(path.sep);
  for (let i = segments.length - 1; i >= 0; i--) {
    if (segments[i] !== "collections") {
      continue;
    }
    const candidate = segments.slice(0, i + 2).join(path.sep);
    if (candidate && isInside(candidate, absolute) && (await hasDefinition(candidate))) {
      return candidate;
    }
  }

  let directory = parent;
  let found: string | undefined;
  for (;;) {
    if (await hasDefinition(directory)) {
      found = directory;
    } else if (found) {
      break;
    }
    const next = path.dirname(directory);
    if (next === directory) {
      break;
    }
    directory = next;
  }

  if (!found) {
    throw new Error(
      `Not a Postman v3 request: no .resources/definition.yaml was found above ${absolute}.`
    );
  }
  return found;
}

/** Builds everything needed to run one request file. */
export async function buildRequestTarget(requestFile: string): Promise<RequestTarget> {
  const absolute = path.resolve(requestFile);
  const fileName = path.basename(absolute);
  if (!isRequestFile(fileName)) {
    throw new Error(
      `"${fileName}" is not a Postman v3 request file (expected a .request.yaml or .request.yml file).`
    );
  }

  const collectionRoot = await findCollectionRoot(absolute);
  const collectionDefinition = await readDefinition(collectionRoot);
  const document = await readYamlDocument(absolute);
  const kind = asScalarString(document.$kind);
  if (kind && kind !== "http-request") {
    throw new Error(
      `This file is a "${kind}" item. Only Postman HTTP requests can be run by roastman.`
    );
  }

  const requestName = asScalarString(document.name) ?? requestStem(fileName);
  const folderNames = await folderNamesFor(collectionRoot, path.dirname(absolute));
  const siblings = await readSiblingRequests(path.dirname(absolute));
  const sameName = siblings.filter((sibling) => sibling.name === requestName);
  const duplicateIndex = sameName.findIndex((sibling) => sibling.filePath === absolute) + 1;
  const duplicateCount = sameName.length;
  const displayName =
    duplicateCount > 1 ? `${requestName} (${duplicateIndex}/${duplicateCount})` : requestName;

  if (duplicateIndex === 0) {
    throw new Error(`Could not locate ${absolute} among its sibling requests.`);
  }

  const rawUrl = document.url;
  const url =
    typeof rawUrl === "string"
      ? rawUrl
      : asScalarString(asRecord(rawUrl)?.raw) ?? "";

  return {
    requestFile: absolute,
    collectionRoot,
    collectionName:
      asScalarString(collectionDefinition?.name) ?? path.basename(collectionRoot),
    folderPath: folderNames.join("/"),
    requestName,
    displayName,
    itemSelector: [...folderNames, displayName].join("/"),
    method: (asScalarString(document.method) ?? "REQUEST").toUpperCase(),
    url,
    duplicateIndex,
    duplicateCount,
  };
}

async function folderNamesFor(collectionRoot: string, requestDirectory: string): Promise<string[]> {
  const relative = path.relative(collectionRoot, requestDirectory);
  if (relative === "") {
    return [];
  }
  if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    throw new Error(`Request directory ${requestDirectory} is outside ${collectionRoot}.`);
  }

  const names: string[] = [];
  let directory = collectionRoot;
  for (const segment of relative.split(path.sep)) {
    if (SKIPPED_DIRECTORIES.has(segment)) {
      throw new Error(
        `The request is not part of a collection item (it is inside ${segment}).`
      );
    }
    directory = path.join(directory, segment);
    const definition = await readDefinition(directory);
    names.push(asScalarString(definition?.name) ?? segment);
  }
  return names;
}

async function readSiblingRequests(directory: string): Promise<SiblingRequest[]> {
  const entries = await fs.readdir(directory, { withFileTypes: true });
  const siblings: SiblingRequest[] = [];

  for (const entry of entries) {
    if (entry.isDirectory() || !isRequestFile(entry.name)) {
      continue;
    }
    if (!entry.isFile() && !entry.isSymbolicLink()) {
      continue;
    }
    const filePath = path.join(directory, entry.name);
    const document = await readYamlDocument(filePath);
    siblings.push({
      fileName: entry.name,
      filePath,
      name: asScalarString(document.name) ?? requestStem(entry.name),
      order: asNumber(document.order) ?? 0,
    });
  }

  // The Go converter sorts its directory entries by filename first (os.ReadDir
  // order), then stable-sorts siblings by `order` and name.
  siblings.sort((a, b) => compareStrings(a.fileName, b.fileName));
  siblings.sort((a, b) => a.order - b.order || compareStrings(a.name, b.name));
  return siblings;
}

async function hasDefinition(directory: string): Promise<boolean> {
  return (await readDefinition(directory)) !== undefined;
}

async function readDefinition(directory: string): Promise<Definition | undefined> {
  for (const name of DEFINITION_NAMES) {
    const definitionPath = path.join(directory, ".resources", name);
    let text: string;
    try {
      await fs.access(definitionPath, fsConstants.R_OK);
      text = await fs.readFile(definitionPath, "utf8");
    } catch (error) {
      if (isFileNotFound(error)) {
        continue;
      }
      throw error;
    }

    const document = parseYamlDocument(text, definitionPath);
    return {
      name: asScalarString(document.name),
      order: asNumber(document.order),
      kind: asScalarString(document.$kind),
    };
  }
  return undefined;
}

async function readYamlDocument(filePath: string): Promise<Record<string, unknown>> {
  const text = await fs.readFile(filePath, "utf8");
  return parseYamlDocument(text, filePath);
}

function parseYamlDocument(text: string, filePath: string): Record<string, unknown> {
  let document: unknown;
  try {
    document = parseYaml(text);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`Cannot parse ${filePath}: ${message}`);
  }
  return asRecord(document) ?? {};
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function asScalarString(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value.length > 0 ? value : undefined;
  }
  if (typeof value === "number") {
    return Number.isFinite(value) ? String(value) : undefined;
  }
  if (typeof value === "boolean") {
    return value ? "true" : "false";
  }
  return undefined;
}

function asNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return undefined;
}

function compareStrings(a: string, b: string): number {
  // The Go converter compares UTF-8 bytes. Matching that exactly matters when
  // duplicate request names need an occurrence suffix.
  return Buffer.compare(Buffer.from(a, "utf8"), Buffer.from(b, "utf8"));
}

function isInside(parent: string, child: string): boolean {
  const relative = path.relative(parent, child);
  return (
    relative === "" ||
    (relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative))
  );
}

function isFileNotFound(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    (error as NodeJS.ErrnoException).code === "ENOENT"
  );
}
