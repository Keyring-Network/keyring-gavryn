const express = require("express");
const path = require("path");
const fs = require("fs/promises");
const crypto = require("crypto");
const { spawn } = require("child_process");
const PptxGenJS = require("pptxgenjs");
const docx = require("docx");
const { PDFDocument, rgb, StandardFonts } = require("pdf-lib");
const Papa = require("papaparse");

const app = express();
app.use(express.json({ limit: "2mb" }));

const PORT = resolvePort(process.env.TOOL_RUNNER_PORT || process.env.PORT, 8081, "tool-runner");
const CONTROL_PLANE_URL = process.env.CONTROL_PLANE_URL || "http://localhost:8080";
const BROWSER_WORKER_URL = process.env.BROWSER_WORKER_URL || "http://localhost:8082";
const BASE_URL = process.env.TOOL_RUNNER_URL || `http://localhost:${PORT}`;

const artifactsRoot = path.resolve(__dirname, "..", "artifacts");
const workspaceRoot = path.resolve(__dirname, "..", "workspaces");

const browserToolNames = [
  "browser.navigate",
  "browser.snapshot",
  "browser.click",
  "browser.type",
  "browser.scroll",
  "browser.extract",
  "browser.evaluate",
  "browser.pdf",
];
const documentToolNames = [
  "document.create_pptx",
  "document.create_docx",
  "document.create_pdf",
  "document.create_csv",
];
const editorToolNames = ["editor.list", "editor.read", "editor.write", "editor.delete", "editor.stat"];
const processToolNames = ["process.exec", "process.start", "process.status", "process.logs", "process.stop", "process.list"];
const defaultAllowlist = [...browserToolNames, ...documentToolNames, ...editorToolNames, ...processToolNames].join(",");

const allowlist = (process.env.ALLOWED_TOOLS || defaultAllowlist)
  .split(",")
  .map((tool) => tool.trim())
  .filter(Boolean);

const defaultProcessAllowlist = [
  "echo",
  "ls",
  "pwd",
  "whoami",
  "cat",
  "cp",
  "mv",
  "rm",
  "mkdir",
  "touch",
  "find",
  "grep",
  "sed",
  "awk",
  "git",
  "node",
  "npm",
  "npx",
  "pnpm",
  "yarn",
  "bun",
  "go",
  "python",
  "python3",
  "pip",
  "pip3",
  "uv",
  "sh",
  "bash",
].join(",");

const commandAllowlist = (process.env.PROCESS_ALLOWLIST || defaultProcessAllowlist)
  .split(",")
  .map((cmd) => cmd.trim())
  .filter(Boolean);

const maxReadBytes = parseEnvInt("EDITOR_MAX_READ_BYTES", 1024 * 1024);
const maxWriteBytes = parseEnvInt("EDITOR_MAX_WRITE_BYTES", 1024 * 1024);
const maxListEntries = parseEnvInt("EDITOR_MAX_LIST_ENTRIES", 2000);
const maxProcessOutputBytes = parseEnvInt("PROCESS_MAX_OUTPUT_BYTES", 200 * 1024);
const maxProcessTimeoutMs = parseEnvInt("PROCESS_MAX_TIMEOUT_MS", 600000);
const defaultProcessTimeoutMs = parseEnvInt("PROCESS_DEFAULT_TIMEOUT_MS", 120000);
const managedProcessMaxLogBytes = parseEnvInt("PROCESS_MANAGED_MAX_LOG_BYTES", 1024 * 1024);
const managedProcessMaxLogEntries = parseEnvInt("PROCESS_MANAGED_MAX_LOG_ENTRIES", 4000);
const managedProcessRetentionMs = parseEnvInt("PROCESS_MANAGED_RETENTION_MS", 10 * 60 * 1000);
const processStopGraceMs = parseEnvInt("PROCESS_STOP_GRACE_MS", 5000);
const invocationCacheTTLms = parseEnvInt("TOOL_INVOCATION_CACHE_TTL_MS", 30 * 60 * 1000);
const invocationCacheMaxEntries = parseEnvInt("TOOL_INVOCATION_CACHE_MAX_ENTRIES", 2000);

const managedProcesses = new Map();
const invocationResultCache = new Map();
const localURLPattern = /\bhttps?:\/\/(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::\d+)?[^\s)'"`]*|\bhttp:\/\/\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?[^\s)'"`]*/gi;

function resolvePort(rawValue, fallback, label) {
  const parsed = Number(rawValue);
  if (Number.isFinite(parsed) && parsed >= 0 && parsed < 65536) {
    return parsed;
  }
  if (rawValue !== undefined) {
    console.warn(`Invalid ${label} port "${rawValue}", falling back to ${fallback}.`);
  }
  return fallback;
}

function parseEnvInt(key, fallback) {
  const raw = process.env[key];
  if (raw === undefined || raw === null || raw === "") return fallback;
  const parsed = Number(raw);
  if (Number.isFinite(parsed) && parsed >= 0) {
    return Math.floor(parsed);
  }
  return fallback;
}

function normalizeToolName(toolName) {
  const normalized = String(toolName || "").trim().toLowerCase();
  return normalized;
}

function invocationCacheKey(runId, invocationId, toolName) {
  return `${runId}:${invocationId}:${toolName}`;
}

function cloneResultBody(body) {
  try {
    return JSON.parse(JSON.stringify(body));
  } catch (_err) {
    return body;
  }
}

function getCachedInvocationResult(key) {
  const entry = invocationResultCache.get(key);
  if (!entry) return null;
  if (entry.expiresAt <= Date.now()) {
    invocationResultCache.delete(key);
    return null;
  }
  return {
    statusCode: entry.statusCode,
    body: cloneResultBody(entry.body),
  };
}

function rememberInvocationResult(key, statusCode, body) {
  if (invocationCacheTTLms <= 0 || statusCode >= 500) {
    return;
  }
  invocationResultCache.set(key, {
    statusCode,
    body: cloneResultBody(body),
    createdAt: Date.now(),
    expiresAt: Date.now() + invocationCacheTTLms,
  });
  pruneInvocationCache();
}

function pruneInvocationCache() {
  const now = Date.now();
  for (const [key, entry] of invocationResultCache.entries()) {
    if (entry.expiresAt <= now) {
      invocationResultCache.delete(key);
    }
  }
  if (invocationResultCache.size <= invocationCacheMaxEntries) {
    return;
  }
  const sortedByAge = Array.from(invocationResultCache.entries())
    .sort((left, right) => left[1].createdAt - right[1].createdAt);
  const removeCount = invocationResultCache.size - invocationCacheMaxEntries;
  for (let index = 0; index < removeCount; index += 1) {
    invocationResultCache.delete(sortedByAge[index][0]);
  }
}

function normalizeEditorInput(input) {
  const normalized = input && typeof input === "object" ? { ...input } : {};
  const primaryPath = normalized.path;
  if (primaryPath !== undefined && primaryPath !== null && String(primaryPath).trim() !== "") {
    return normalized;
  }
  const aliases = [normalized.file_path, normalized.filepath];
  for (const aliasValue of aliases) {
    if (aliasValue === undefined || aliasValue === null) {
      continue;
    }
    if (String(aliasValue).trim() === "") {
      continue;
    }
    normalized.path = aliasValue;
    break;
  }
  return normalized;
}

async function ensureDir(dir) {
  await fs.mkdir(dir, { recursive: true });
}

function isSubpath(root, target) {
  const normalizedRoot = root.endsWith(path.sep) ? root : root + path.sep;
  return target === root || target.startsWith(normalizedRoot);
}

async function resolveRunPath(runId, inputPath, { allowMissing = false } = {}) {
  const runRoot = path.resolve(workspaceRoot, runId);
  await ensureDir(runRoot);
  const relativePath = normalizeWorkspacePath(inputPath);
  const targetPath = path.resolve(runRoot, relativePath);
  if (!isSubpath(runRoot, targetPath)) {
    throw new Error("path escapes run root");
  }
  const runRootReal = await fs.realpath(runRoot);
  if (allowMissing) {
    const existingAncestor = await resolveExistingAncestor(targetPath);
    const ancestorReal = await fs.realpath(existingAncestor);
    if (!isSubpath(runRootReal, ancestorReal)) {
      throw new Error("path escapes run root");
    }
    return { runRoot: runRootReal, targetPath, relativePath };
  }
  const targetReal = await fs.realpath(targetPath);
  if (!isSubpath(runRootReal, targetReal)) {
    throw new Error("path escapes run root");
  }
  return { runRoot: runRootReal, targetPath, relativePath };
}

function normalizeWorkspacePath(inputPath) {
  if (inputPath === undefined || inputPath === null) {
    return ".";
  }
  let value = String(inputPath).trim();
  if (!value) {
    return ".";
  }
  value = value.replaceAll("\\", "/");
  if (value === "/context" || value === "context") {
    return ".";
  }
  if (value.startsWith("/context/")) {
    value = value.slice("/context/".length);
  } else if (value.startsWith("context/")) {
    value = value.slice("context/".length);
  }
  if (value.startsWith("/")) {
    value = value.replace(/^\/+/, "");
  }
  if (!value) {
    return ".";
  }
  return value;
}

async function resolveExistingAncestor(targetPath) {
  let current = targetPath;
  while (true) {
    try {
      await fs.lstat(current);
      return current;
    } catch (err) {
      if (err.code !== "ENOENT") {
        throw err;
      }
      const parent = path.dirname(current);
      if (parent === current) {
        throw err;
      }
      current = parent;
    }
  }
}

function sanitizeCommand(command) {
  if (!command || typeof command !== "string") {
    throw new Error("command is required");
  }
  if (command.includes("/") || command.includes("\\")) {
    throw new Error("command must be allowlisted name");
  }
  const trimmed = command.trim();
  if (!trimmed) {
    throw new Error("command is required");
  }
  if (!commandAllowlist.includes(trimmed)) {
    throw new Error(`command not allowlisted: ${trimmed}`);
  }
  return trimmed;
}

function normalizeArgs(args) {
  if (!args) return [];
  if (!Array.isArray(args)) {
    throw new Error("args must be an array");
  }
  return args.map((arg) => {
    if (typeof arg !== "string") {
      throw new Error("args must be strings");
    }
    return arg;
  });
}

async function ensureSafePathArg(runRoot, cwd, arg) {
  if (typeof arg !== "string") return;
  const candidate = arg.trim();
  if (!candidate) return;
  if (candidate.startsWith("~") || candidate.startsWith("/")) {
    throw new Error("absolute paths are not allowed in args");
  }
  if (candidate.startsWith("-") && candidate.includes("=")) {
    const value = candidate.split("=").slice(1).join("=");
    if (value && (value.includes("/") || value.startsWith("."))) {
      await assertPathWithinRoot(runRoot, path.resolve(cwd, value));
    }
    return;
  }
  if (candidate.includes("/") || candidate.includes("\\") || candidate.startsWith(".")) {
    await assertPathWithinRoot(runRoot, path.resolve(cwd, candidate));
  }
}

async function assertPathWithinRoot(runRoot, targetPath) {
  const runRootReal = await fs.realpath(runRoot);
  try {
    const targetReal = await fs.realpath(targetPath);
    if (!isSubpath(runRootReal, targetReal)) {
      throw new Error("path escapes run root");
    }
    return;
  } catch (err) {
    if (err.code !== "ENOENT") {
      throw err;
    }
  }
  const parentReal = await fs.realpath(path.dirname(targetPath));
  if (!isSubpath(runRootReal, parentReal)) {
    throw new Error("path escapes run root");
  }
}

async function listWorkspace(runId, inputPath) {
  const { runRoot, targetPath } = await resolveRunPath(runId, inputPath);
  const stats = await fs.lstat(targetPath);
  if (stats.isSymbolicLink()) {
    throw new Error("symlinks are not allowed");
  }
  if (!stats.isDirectory()) {
    throw new Error("path is not a directory");
  }
  const entries = await fs.readdir(targetPath, { withFileTypes: true });
  if (entries.length > maxListEntries) {
    throw new Error(`directory has too many entries (max ${maxListEntries})`);
  }
  const output = [];
  for (const entry of entries) {
    const entryPath = path.join(targetPath, entry.name);
    const entryStats = await fs.lstat(entryPath);
    let type = "file";
    if (entryStats.isDirectory()) type = "directory";
    if (entryStats.isSymbolicLink()) type = "symlink";
    output.push({
      name: entry.name,
      path: path.relative(runRoot, entryPath),
      type,
      size_bytes: entryStats.isFile() ? entryStats.size : 0,
      modified_at: entryStats.mtime.toISOString(),
    });
  }
  return { root: runRoot, entries: output };
}

function inferPreviewURLs(text) {
  if (!text || typeof text !== "string") {
    return [];
  }
  const matches = text.match(localURLPattern);
  if (!matches || matches.length === 0) {
    return [];
  }
  return [...new Set(matches)];
}

function isManagedProcessActive(processInfo) {
  if (!processInfo) return false;
  return processInfo.status === "running" || processInfo.status === "starting";
}

function pruneManagedProcesses() {
  if (managedProcessRetentionMs <= 0) {
    return;
  }
  const now = Date.now();
  for (const [processId, processInfo] of managedProcesses.entries()) {
    if (isManagedProcessActive(processInfo)) {
      continue;
    }
    const endedAt = Date.parse(processInfo.ended_at || processInfo.started_at || "");
    if (!Number.isFinite(endedAt)) {
      continue;
    }
    if (now-endedAt >= managedProcessRetentionMs) {
      managedProcesses.delete(processId);
    }
  }
}

function toSerializableProcess(processInfo, includeLogs = false, tail = 200) {
  const serializable = {
    process_id: processInfo.process_id,
    run_id: processInfo.run_id,
    command: processInfo.command,
    args: processInfo.args,
    cwd: processInfo.cwd,
    status: processInfo.status,
    pid: processInfo.pid,
    started_at: processInfo.started_at,
    ended_at: processInfo.ended_at || null,
    exit_code: processInfo.exit_code,
    signal: processInfo.signal || null,
    output_bytes: processInfo.output_bytes,
    preview_urls: [...processInfo.preview_urls],
  };
  if (includeLogs) {
    serializable.logs = processInfo.logs.slice(-Math.max(1, tail));
  }
  return serializable;
}

function appendManagedProcessLog(processInfo, stream, chunk) {
  const text = chunk.toString("utf8");
  if (!text) return;
  const previewURLs = inferPreviewURLs(text);
  for (const url of previewURLs) {
    processInfo.preview_urls.add(url);
  }
  const bytes = Buffer.byteLength(text, "utf8");
  processInfo.output_bytes += bytes;
  processInfo.logs.push({
    stream,
    ts: new Date().toISOString(),
    text,
    bytes,
  });
  while (processInfo.logs.length > managedProcessMaxLogEntries) {
    const removed = processInfo.logs.shift();
    processInfo.output_bytes -= removed ? removed.bytes : 0;
  }
  while (processInfo.output_bytes > managedProcessMaxLogBytes && processInfo.logs.length > 0) {
    const removed = processInfo.logs.shift();
    processInfo.output_bytes -= removed ? removed.bytes : 0;
  }
}

async function startManagedProcess(runId, input = {}) {
  pruneManagedProcesses();
  const safeCommand = sanitizeCommand(input.command);
  const safeArgs = normalizeArgs(input.args);
  const cwdInput = typeof input.cwd === "string" ? input.cwd : ".";
  const { runRoot, targetPath } = await resolveRunPath(runId, cwdInput, { allowMissing: false });
  const cwdStats = await fs.lstat(targetPath);
  if (!cwdStats.isDirectory()) {
    throw new Error("cwd must be a directory");
  }
  for (const arg of safeArgs) {
    await ensureSafePathArg(runRoot, targetPath, arg);
  }
  const safeEnv = {};
  if (input.env && typeof input.env === "object") {
    for (const [key, value] of Object.entries(input.env)) {
      if (typeof value === "string") {
        safeEnv[key] = value;
      }
    }
  }
  const processId = crypto.randomUUID();
  const child = spawn(safeCommand, safeArgs, {
    cwd: targetPath,
    env: { ...process.env, ...safeEnv },
    shell: false,
  });
  const processInfo = {
    process_id: processId,
    run_id: runId,
    command: safeCommand,
    args: safeArgs,
    cwd: path.relative(runRoot, targetPath) || ".",
    status: "running",
    pid: child.pid,
    started_at: new Date().toISOString(),
    ended_at: "",
    exit_code: null,
    signal: "",
    logs: [],
    output_bytes: 0,
    preview_urls: new Set(),
    child,
  };
  child.stdout.on("data", (chunk) => appendManagedProcessLog(processInfo, "stdout", chunk));
  child.stderr.on("data", (chunk) => appendManagedProcessLog(processInfo, "stderr", chunk));
  child.on("close", (code, signal) => {
    processInfo.status = "exited";
    processInfo.ended_at = new Date().toISOString();
    processInfo.exit_code = code;
    processInfo.signal = signal || "";
    processInfo.child = null;
  });
  child.on("error", (err) => {
    processInfo.status = "failed";
    processInfo.ended_at = new Date().toISOString();
    processInfo.signal = "";
    processInfo.exit_code = null;
    appendManagedProcessLog(processInfo, "stderr", Buffer.from(String(err?.message || err || "process error")));
    processInfo.child = null;
  });
  managedProcesses.set(processId, processInfo);
  return toSerializableProcess(processInfo);
}

function getManagedProcess(runId, processId) {
  const processInfo = managedProcesses.get(processId);
  if (!processInfo || processInfo.run_id !== runId) {
    return null;
  }
  return processInfo;
}

function listManagedProcesses(runId) {
  pruneManagedProcesses();
  const results = [];
  for (const processInfo of managedProcesses.values()) {
    if (processInfo.run_id !== runId) continue;
    results.push(toSerializableProcess(processInfo));
  }
  results.sort((a, b) => (a.started_at < b.started_at ? 1 : -1));
  return results;
}

async function stopManagedProcess(runId, processId, force = false) {
  const processInfo = getManagedProcess(runId, processId);
  if (!processInfo) {
    throw new Error("process not found");
  }
  if (!processInfo.child || processInfo.status !== "running") {
    return toSerializableProcess(processInfo);
  }
  const signal = force ? "SIGKILL" : "SIGTERM";
  processInfo.child.kill(signal);
  if (force) {
    return toSerializableProcess(processInfo);
  }
  await new Promise((resolve) => {
    const timeout = setTimeout(() => {
      if (processInfo.child && processInfo.status === "running") {
        processInfo.child.kill("SIGKILL");
      }
      resolve();
    }, processStopGraceMs);
    const interval = setInterval(() => {
      if (processInfo.status !== "running") {
        clearTimeout(timeout);
        clearInterval(interval);
        resolve();
      }
    }, 50);
  });
  return toSerializableProcess(processInfo);
}

async function cleanupManagedProcessesForRun(runId, force = true) {
  pruneManagedProcesses();
  const processIds = [];
  for (const processInfo of managedProcesses.values()) {
    if (processInfo.run_id !== runId) continue;
    if (!isManagedProcessActive(processInfo)) continue;
    processIds.push(processInfo.process_id);
  }

  let stopped = 0;
  const errors = [];
  for (const processId of processIds) {
    try {
      await stopManagedProcess(runId, processId, force);
      stopped += 1;
    } catch (err) {
      errors.push({
        process_id: processId,
        error: err?.message || String(err),
      });
    }
  }
	return {
		run_id: runId,
		stopped,
		attempted: processIds.length,
		errors,
	};
}

async function cancelBrowserSessionForRun(runId) {
  if (!runId) {
    return { cancelled: false, skipped: true };
  }
  try {
    const response = await fetch(`${BROWSER_WORKER_URL}/cancel`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ run_id: runId }),
    });
    if (!response.ok) {
      const details = await response.text();
      return {
        cancelled: false,
        error: details || `browser cancel failed with status ${response.status}`,
      };
    }
    return { cancelled: true };
  } catch (err) {
    return { cancelled: false, error: err?.message || String(err) };
  }
}

async function readWorkspaceFile(runId, inputPath, encoding) {
  const { targetPath } = await resolveRunPath(runId, inputPath);
  const stats = await fs.lstat(targetPath);
  if (stats.isSymbolicLink()) {
    throw new Error("symlinks are not allowed");
  }
  if (!stats.isFile()) {
    throw new Error("path is not a file");
  }
  if (stats.size > maxReadBytes) {
    throw new Error(`file exceeds max read size (${maxReadBytes} bytes)`);
  }
  if (encoding === "base64") {
    const buffer = await fs.readFile(targetPath);
    return { path: inputPath, encoding: "base64", content: buffer.toString("base64"), size_bytes: stats.size };
  }
  const content = await fs.readFile(targetPath, "utf8");
  return { path: inputPath, encoding: "utf-8", content, size_bytes: stats.size };
}

async function writeWorkspaceFile(runId, inputPath, content, encoding) {
	if (content === undefined || content === null) {
		throw new Error("content is required");
	}
	const { targetPath } = await resolveRunPath(runId, inputPath, { allowMissing: true });
  const parent = path.dirname(targetPath);
  await ensureDir(parent);
  let buffer;
  if (encoding === "base64") {
    buffer = Buffer.from(content, "base64");
  } else {
    buffer = Buffer.from(String(content), "utf8");
  }
  if (buffer.length > maxWriteBytes) {
    throw new Error(`content exceeds max write size (${maxWriteBytes} bytes)`);
  }
  try {
    const existing = await fs.lstat(targetPath);
    if (existing.isSymbolicLink()) {
      throw new Error("symlinks are not allowed");
    }
    if (existing.isDirectory()) {
      throw new Error("path is a directory");
    }
  } catch (err) {
    if (err.code !== "ENOENT") throw err;
  }
  await fs.writeFile(targetPath, buffer);
	const stats = await fs.stat(targetPath);
	return { path: inputPath, size_bytes: stats.size };
}

async function getWorkspaceSnapshot(runId, inputPath) {
	const { targetPath } = await resolveRunPath(runId, inputPath, { allowMissing: true });
	try {
		const stats = await fs.lstat(targetPath);
		if (stats.isSymbolicLink()) {
			return { exists: true, type: "symlink" };
		}
		if (stats.isDirectory()) {
			return { exists: true, type: "directory" };
		}
		if (stats.isFile()) {
			let lineCount;
			if (stats.size <= maxReadBytes) {
				const content = await fs.readFile(targetPath, "utf8");
				lineCount = content === "" ? 0 : content.split(/\r?\n/).length;
			}
			return { exists: true, type: "file", size_bytes: stats.size, line_count: lineCount };
		}
		return { exists: true, type: "other" };
	} catch (err) {
		if (err.code === "ENOENT") {
			return { exists: false };
		}
		throw err;
	}
}

function buildWorkspaceChangeSummary(before, after) {
	const summary = {};
	if (before && before.exists && typeof before.size_bytes === "number") {
		summary.before_bytes = before.size_bytes;
	}
	if (after && after.exists && typeof after.size_bytes === "number") {
		summary.after_bytes = after.size_bytes;
	}
	if (typeof summary.before_bytes === "number" && typeof summary.after_bytes === "number") {
		summary.delta_bytes = summary.after_bytes - summary.before_bytes;
	}
	if (before && typeof before.line_count === "number") {
		summary.before_lines = before.line_count;
	}
	if (after && typeof after.line_count === "number") {
		summary.after_lines = after.line_count;
	}
	if (typeof summary.before_lines === "number" && typeof summary.after_lines === "number") {
		summary.delta_lines = summary.after_lines - summary.before_lines;
	}
	summary.type = (after && after.type) || (before && before.type);
	return summary;
}

async function deleteWorkspacePath(runId, inputPath, recursive) {
  const { targetPath } = await resolveRunPath(runId, inputPath);
  const stats = await fs.lstat(targetPath);
  if (stats.isSymbolicLink()) {
    await fs.unlink(targetPath);
    return { path: inputPath, deleted: true };
  }
  if (stats.isDirectory()) {
    if (!recursive) {
      throw new Error("directory delete requires recursive=true");
    }
    await fs.rm(targetPath, { recursive: true, force: true });
    return { path: inputPath, deleted: true };
  }
  await fs.unlink(targetPath);
  return { path: inputPath, deleted: true };
}

async function statWorkspacePath(runId, inputPath) {
  const { targetPath } = await resolveRunPath(runId, inputPath);
  const stats = await fs.lstat(targetPath);
  if (stats.isSymbolicLink()) {
    throw new Error("symlinks are not allowed");
  }
  let type = "file";
  if (stats.isDirectory()) type = "directory";
  return {
    path: inputPath,
    type,
    size_bytes: stats.isFile() ? stats.size : 0,
    modified_at: stats.mtime.toISOString(),
  };
}

async function execWorkspaceProcess(runId, command, args, cwd, env, timeoutMs) {
  const safeCommand = sanitizeCommand(command);
  const safeArgs = normalizeArgs(args);
  const { runRoot, targetPath } = await resolveRunPath(runId, cwd || ".");
  const cwdStats = await fs.lstat(targetPath);
  if (!cwdStats.isDirectory()) {
    throw new Error("cwd must be a directory");
  }
  for (const arg of safeArgs) {
    await ensureSafePathArg(runRoot, targetPath, arg);
  }
  const safeEnv = {};
  if (env && typeof env === "object") {
    for (const [key, value] of Object.entries(env)) {
      if (typeof value === "string") {
        safeEnv[key] = value;
      }
    }
  }
  const maxTimeout = Math.max(1000, maxProcessTimeoutMs);
  const requestedTimeout = Number(timeoutMs) || defaultProcessTimeoutMs;
  const effectiveTimeout = Math.min(Math.max(requestedTimeout, 1000), maxTimeout);

  return new Promise((resolve, reject) => {
    let stdout = "";
    let stderr = "";
    let outputBytes = 0;
    let outputLimit = false;
    let timedOut = false;

    const child = spawn(safeCommand, safeArgs, {
      cwd: targetPath,
      env: { ...process.env, ...safeEnv },
      shell: false,
    });

    const timeout = setTimeout(() => {
      timedOut = true;
      child.kill("SIGKILL");
    }, effectiveTimeout);

    child.stdout.on("data", (chunk) => {
      outputBytes += chunk.length;
      if (outputBytes > maxProcessOutputBytes) {
        outputLimit = true;
        child.kill("SIGKILL");
        return;
      }
      stdout += chunk.toString("utf8");
    });

    child.stderr.on("data", (chunk) => {
      outputBytes += chunk.length;
      if (outputBytes > maxProcessOutputBytes) {
        outputLimit = true;
        child.kill("SIGKILL");
        return;
      }
      stderr += chunk.toString("utf8");
    });

    child.on("error", (err) => {
      clearTimeout(timeout);
      reject(err);
    });

    child.on("close", (code, signal) => {
      clearTimeout(timeout);
      if (timedOut) {
        reject(new Error("process exceeded timeout"));
        return;
      }
      if (outputLimit) {
        reject(new Error("process output exceeded limit"));
        return;
      }
      resolve({
        stdout,
        stderr,
        exit_code: code,
        signal,
      });
    });
  });
}

async function emitEvent(runId, type, payload) {
  try {
    await fetch(`${CONTROL_PLANE_URL}/runs/${runId}/events`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type,
        source: "tool_runner",
        timestamp: new Date().toISOString(),
        payload,
      }),
    });
  } catch (err) {
    console.error("failed to emit event", err);
  }
}

app.get("/health", (_req, res) => {
  res.json({ status: "ok" });
});

app.get("/ready", async (_req, res) => {
  const capabilities = await buildCapabilities();
  const browserRequired = capabilities.browser?.enabled === true;
  const browserHealthy = capabilities.browser?.healthy === true;
  const ready = !browserRequired || browserHealthy;
  const statusCode = ready ? 200 : 503;
  res.status(statusCode).json({
    status: ready ? "ok" : "degraded",
    subsystems: {
      browser_worker: {
        status: browserRequired ? (browserHealthy ? "ok" : "error") : "skipped",
      },
    },
  });
});

async function buildCapabilities() {
  const browserEnabled = allowlist.some((toolName) => browserToolNames.includes(toolName));
  let browserHealthy = false;
  if (browserEnabled) {
    try {
      let response = await fetch(`${BROWSER_WORKER_URL}/ready`);
      if (!response.ok && response.status === 404) {
        response = await fetch(`${BROWSER_WORKER_URL}/health`);
      }
      browserHealthy = response.ok;
    } catch (_readyErr) {
      try {
        const healthResponse = await fetch(`${BROWSER_WORKER_URL}/health`);
        browserHealthy = healthResponse.ok;
      } catch (_healthErr) {
        browserHealthy = false;
      }
    }
  }
  return {
    status: "ok",
    tools: allowlist,
    browser: {
      enabled: browserEnabled,
      healthy: browserHealthy,
    },
    document: {
      enabled: allowlist.some((toolName) => documentToolNames.includes(toolName)),
    },
    editor: {
      enabled: allowlist.some((toolName) => editorToolNames.includes(toolName)),
    },
    process: {
      enabled: allowlist.some((toolName) => processToolNames.includes(toolName)),
      limits: {
        max_timeout_ms: maxProcessTimeoutMs,
        default_timeout_ms: defaultProcessTimeoutMs,
        max_output_bytes: maxProcessOutputBytes,
        max_managed_log_bytes: managedProcessMaxLogBytes,
      },
    },
    idempotency: {
      enabled: true,
      ttl_ms: invocationCacheTTLms,
      max_entries: invocationCacheMaxEntries,
    },
    contract_version: "tool_contract_v2",
  };
}

app.get("/tools/capabilities", async (_req, res) => {
  res.json(await buildCapabilities());
});

app.post("/runs/:runId/processes/cleanup", async (req, res) => {
  const runId = String(req.params.runId || "").trim();
  if (!runId) {
    res.status(400).json({ error: "run id required" });
    return;
  }
  const force = req.body?.force !== false;
  try {
    const processCleanup = await cleanupManagedProcessesForRun(runId, force);
    const browserCleanup = await cancelBrowserSessionForRun(runId);
    res.json({
      status: "completed",
      output: {
        ...processCleanup,
        browser: browserCleanup,
      },
      artifacts: [],
    });
  } catch (err) {
    res.status(500).json({ status: "failed", error: err?.message || String(err) });
  }
});


app.use("/artifacts", express.static(artifactsRoot));

app.post("/tools/execute", async (req, res) => {
  const {
    contract_version: contractVersion,
    run_id: runId,
    invocation_id: rawInvocationID,
    idempotency_key: idempotencyKey,
    tool_name: requestedToolName,
    input = {},
    timeout_ms: timeoutMs,
    policy_context: policyContext = {},
  } = req.body || {};
  const invocationId = String(idempotencyKey || rawInvocationID || "").trim();
  const toolName = normalizeToolName(requestedToolName);

  if (contractVersion && contractVersion !== "tool_contract_v2") {
    res.status(400).json({ error: "unsupported contract_version" });
    return;
  }

  if (!runId || !invocationId || !requestedToolName) {
    res.status(400).json({ error: "run_id, idempotency_key, and tool_name are required" });
    return;
  }

  if (!allowlist.includes(toolName)) {
    await emitEvent(runId, "policy.denied", {
      reason_code: "tool_not_allowlisted",
      tool_name: requestedToolName,
      policy_profile: String(policyContext?.profile || "default"),
    });
    res.status(403).json({ error: `tool not allowlisted: ${requestedToolName}`, reason_code: "tool_not_allowlisted" });
    return;
  }

  const cacheKey = invocationCacheKey(runId, invocationId, toolName);
  const cached = getCachedInvocationResult(cacheKey);
  if (cached) {
    await emitEvent(runId, "tool.deduped", {
      tool_invocation_id: invocationId,
      tool_name: toolName,
      deduped: true,
    });
    res.status(cached.statusCode).json({ ...cached.body, deduped: true });
    return;
  }

  const originalJson = res.json.bind(res);
  res.json = (body) => {
    rememberInvocationResult(cacheKey, res.statusCode || 200, body);
    return originalJson(body);
  };

  await emitEvent(runId, "tool.started", {
    tool_invocation_id: invocationId,
    tool_name: toolName,
    input,
  });

  try {
    if (toolName.startsWith("browser.")) {
      const response = await fetch(`${BROWSER_WORKER_URL}/tools/execute`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          contract_version: "tool_contract_v2",
          run_id: runId,
          invocation_id: invocationId,
          idempotency_key: invocationId,
          tool_name: toolName,
          input,
          timeout_ms: timeoutMs,
          policy_context: policyContext,
        }),
      });

      if (!response.ok) {
        const text = await response.text();
        let message = text || "browser worker failed";
        let reasonCode = "";
        let diagnostics;
        if (text) {
          try {
            const parsed = JSON.parse(text);
            if (parsed && typeof parsed === "object") {
              message = String(parsed.error || parsed.message || parsed.detail || message).trim() || message;
              reasonCode = String(parsed.reason_code || parsed.reasonCode || "").trim();
              if (parsed.diagnostics && typeof parsed.diagnostics === "object") {
                diagnostics = parsed.diagnostics;
              }
            }
          } catch {
            // Keep plain-text response body as the error message.
          }
        }
        const proxyErr = new Error(message || "browser worker failed");
        if (reasonCode) {
          proxyErr.reasonCode = reasonCode;
        }
        if (diagnostics) {
          proxyErr.diagnostics = diagnostics;
        }
        throw proxyErr;
      }

      const result = await response.json();
      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output: result.output,
        artifacts: result.artifacts || [],
      });
      res.json(result);
      return;
    }

    if (toolName.startsWith("editor.")) {
      const normalizedInput = normalizeEditorInput(input);
      const rawPath = normalizedInput.path;
      if ((rawPath === undefined || rawPath === null || String(rawPath).trim() === "") && toolName !== "editor.list") {
        res.status(400).json({ error: "input.path is required" });
        return;
      }

      const normalizedPath = normalizeWorkspacePath(rawPath);
      let output;
      if (toolName === "editor.list") {
        output = await listWorkspace(runId, normalizedPath);
      } else if (toolName === "editor.read") {
        output = await readWorkspaceFile(runId, normalizedPath, normalizedInput.encoding);
      } else if (toolName === "editor.write") {
        const before = await getWorkspaceSnapshot(runId, normalizedPath);
        output = await writeWorkspaceFile(runId, normalizedPath, normalizedInput.content, normalizedInput.encoding);
        const after = await getWorkspaceSnapshot(runId, normalizedPath);
        await emitEvent(runId, "workspace.changed", {
          path: normalizedPath,
          change: before.exists ? "modified" : "added",
          summary: buildWorkspaceChangeSummary(before, after),
        });
      } else if (toolName === "editor.delete") {
        const before = await getWorkspaceSnapshot(runId, normalizedPath);
        output = await deleteWorkspacePath(runId, normalizedPath, normalizedInput.recursive === true);
        await emitEvent(runId, "workspace.changed", {
          path: normalizedPath,
          change: "removed",
          summary: buildWorkspaceChangeSummary(before, { exists: false }),
        });
      } else if (toolName === "editor.stat") {
        output = await statWorkspacePath(runId, normalizedPath);
      } else {
        throw new Error(`unsupported tool ${toolName}`);
      }

      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output,
      });

      res.json({
        status: "completed",
        output,
        artifacts: [],
      });
      return;
    }

    if (toolName === "process.exec") {
      const output = await execWorkspaceProcess(runId, input.command, input.args, input.cwd, input.env, timeoutMs);
      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output,
      });
      res.json({
        status: "completed",
        output,
        artifacts: [],
      });
      return;
    }

    if (toolName === "process.start") {
      const output = await startManagedProcess(runId, input);
      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output,
      });
      res.json({
        status: "completed",
        output,
        artifacts: [],
      });
      return;
    }

    if (toolName === "process.status") {
      const processId = String(input.process_id || "").trim();
      if (!processId) {
        res.status(400).json({ error: "input.process_id is required" });
        return;
      }
      const processInfo = getManagedProcess(runId, processId);
      if (!processInfo) {
        res.status(404).json({ error: "process not found" });
        return;
      }
      const output = toSerializableProcess(processInfo);
      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output,
      });
      res.json({
        status: "completed",
        output,
        artifacts: [],
      });
      return;
    }

    if (toolName === "process.logs") {
      const processId = String(input.process_id || "").trim();
      if (!processId) {
        res.status(400).json({ error: "input.process_id is required" });
        return;
      }
      const processInfo = getManagedProcess(runId, processId);
      if (!processInfo) {
        res.status(404).json({ error: "process not found" });
        return;
      }
      const tail = Number.isFinite(Number(input.tail)) ? Number(input.tail) : 200;
      const output = toSerializableProcess(processInfo, true, tail);
      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output: { process_id: processId, logs_count: output.logs.length },
      });
      res.json({
        status: "completed",
        output,
        artifacts: [],
      });
      return;
    }

    if (toolName === "process.stop") {
      const processId = String(input.process_id || "").trim();
      if (!processId) {
        res.status(400).json({ error: "input.process_id is required" });
        return;
      }
      const output = await stopManagedProcess(runId, processId, input.force === true);
      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output,
      });
      res.json({
        status: "completed",
        output,
        artifacts: [],
      });
      return;
    }

    if (toolName === "process.list") {
      const output = { processes: listManagedProcesses(runId) };
      await emitEvent(runId, "tool.completed", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        output: { process_count: output.processes.length },
      });
      res.json({
        status: "completed",
        output,
        artifacts: [],
      });
      return;
    }

    // Document generation tools
    if (toolName === "document.create_pptx") {
      const { slides = [] } = input;
      const filename = `${crypto.randomUUID()}.pptx`;
      const filePath = path.join(artifactsRoot, filename);
      await ensureDir(artifactsRoot);

      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "starting",
      });

      const pptx = new PptxGenJS();
      
      for (const slide of slides) {
        const pptxSlide = pptx.addSlide();
        if (slide.title) {
          pptxSlide.addText(slide.title, { x: 0.5, y: 0.5, w: 9, h: 1, fontSize: 24, bold: true });
        }
        if (slide.content) {
          pptxSlide.addText(slide.content, { x: 0.5, y: 1.5, w: 9, h: 4, fontSize: 14 });
        }
        if (slide.bullets && slide.bullets.length > 0) {
          const bulletText = slide.bullets.map(b => ({ text: b, options: { bullet: true } }));
          pptxSlide.addText(bulletText, { x: 0.5, y: 2.5, w: 9, h: 3, fontSize: 14 });
        }
      }

      await pptx.writeFile({ fileName: filePath });

      const uri = `${BASE_URL}/artifacts/${filename}`;
      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "completed",
        uri,
      });

      res.json({
        status: "completed",
        output: { filename, slides: slides.length },
        artifacts: [
          {
            artifact_id: invocationId,
            type: "file",
            uri,
            content_type: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
          },
        ],
      });
      return;
    }

    if (toolName === "document.create_docx") {
      const { title, sections = [] } = input;
      const filename = `${crypto.randomUUID()}.docx`;
      const filePath = path.join(artifactsRoot, filename);
      await ensureDir(artifactsRoot);

      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "starting",
      });

      const children = [];
      
      if (title) {
        children.push(
          new docx.Paragraph({
            text: title,
            heading: docx.HeadingLevel.TITLE,
            spacing: { after: 200 },
          })
        );
      }

      for (const section of sections) {
        if (section.heading) {
          children.push(
            new docx.Paragraph({
              text: section.heading,
              heading: docx.HeadingLevel.HEADING_1,
              spacing: { before: 200, after: 100 },
            })
          );
        }
        if (section.content) {
          children.push(
            new docx.Paragraph({
              text: section.content,
              spacing: { after: 200 },
            })
          );
        }
      }

      const doc = new docx.Document({
        sections: [{ children }],
      });

      const buffer = await docx.Packer.toBuffer(doc);
      await fs.writeFile(filePath, buffer);

      const uri = `${BASE_URL}/artifacts/${filename}`;
      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "completed",
        uri,
      });

      res.json({
        status: "completed",
        output: { filename, sections: sections.length },
        artifacts: [
          {
            artifact_id: invocationId,
            type: "file",
            uri,
            content_type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
          },
        ],
      });
      return;
    }

    if (toolName === "document.create_pdf") {
      const { content, title } = input;
      const filename = `${crypto.randomUUID()}.pdf`;
      const filePath = path.join(artifactsRoot, filename);
      await ensureDir(artifactsRoot);

      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "starting",
      });

      const pdfDoc = await PDFDocument.create();
      const page = pdfDoc.addPage();
      const { width, height } = page.getSize();
      const font = await pdfDoc.embedFont(StandardFonts.Helvetica);
      const boldFont = await pdfDoc.embedFont(StandardFonts.HelveticaBold);

      let y = height - 50;
      const fontSize = 12;
      const lineHeight = fontSize + 4;
      const margin = 50;
      const maxWidth = width - 2 * margin;

      if (title) {
        page.drawText(title, {
          x: margin,
          y,
          size: 18,
          font: boldFont,
          color: rgb(0, 0, 0),
        });
        y -= 30;
      }

      if (content) {
        const lines = content.split("\n");
        for (const line of lines) {
          if (y < margin) {
            pdfDoc.addPage();
            y = height - 50;
          }
          
          const words = line.split(" ");
          let currentLine = "";
          
          for (const word of words) {
            const testLine = currentLine ? `${currentLine} ${word}` : word;
            const testWidth = font.widthOfTextAtSize(testLine, fontSize);
            
            if (testWidth > maxWidth && currentLine) {
              page.drawText(currentLine, {
                x: margin,
                y,
                size: fontSize,
                font,
                color: rgb(0, 0, 0),
              });
              y -= lineHeight;
              currentLine = word;
            } else {
              currentLine = testLine;
            }
          }
          
          if (currentLine) {
            if (y < margin) {
              pdfDoc.addPage();
              y = height - 50;
            }
            page.drawText(currentLine, {
              x: margin,
              y,
              size: fontSize,
              font,
              color: rgb(0, 0, 0),
            });
            y -= lineHeight;
          }
          
          y -= 5;
        }
      }

      const pdfBytes = await pdfDoc.save();
      await fs.writeFile(filePath, pdfBytes);

      const uri = `${BASE_URL}/artifacts/${filename}`;
      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "completed",
        uri,
      });

      res.json({
        status: "completed",
        output: { filename, has_content: !!content },
        artifacts: [
          {
            artifact_id: invocationId,
            type: "file",
            uri,
            content_type: "application/pdf",
          },
        ],
      });
      return;
    }

    if (toolName === "document.create_csv") {
      const { headers, rows = [] } = input;
      const filename = `${crypto.randomUUID()}.csv`;
      const filePath = path.join(artifactsRoot, filename);
      await ensureDir(artifactsRoot);

      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "starting",
      });

      const csvContent = Papa.unparse({
        fields: headers || [],
        data: rows,
      });

      await fs.writeFile(filePath, csvContent);

      const uri = `${BASE_URL}/artifacts/${filename}`;
      await emitEvent(runId, "document.created", {
        tool_invocation_id: invocationId,
        tool_name: toolName,
        filename,
        status: "completed",
        uri,
      });

      res.json({
        status: "completed",
        output: { filename, rows: rows.length },
        artifacts: [
          {
            artifact_id: invocationId,
            type: "file",
            uri,
            content_type: "text/csv",
          },
        ],
      });
      return;
    }

    throw new Error(`unsupported tool ${toolName}`);
  } catch (err) {
    const reasonCode = typeof err?.reasonCode === "string" ? err.reasonCode.trim() : "";
    const diagnostics = err?.diagnostics && typeof err.diagnostics === "object" ? err.diagnostics : undefined;
    await emitEvent(runId, "tool.failed", {
      tool_invocation_id: invocationId,
      tool_name: toolName,
      error: err.message || String(err),
      reason_code: reasonCode || undefined,
      diagnostics,
    });
    res.status(500).json({
      status: "failed",
      error: err.message || String(err),
      reason_code: reasonCode || undefined,
      diagnostics,
    });
  }
});


// Only start the server if this file is run directly (not required for tests)
/* c8 ignore start */
if (require.main === module) {
  app.listen(PORT, () => {
    console.log(`tool runner listening on ${PORT}`);
  });
}
/* c8 ignore stop */

module.exports = { app };
