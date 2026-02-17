const express = require("express");
const path = require("path");
const fs = require("fs/promises");
const crypto = require("crypto");
const os = require("os");
const { spawn } = require("child_process");
const playwright = require("playwright");

// Allow dependency injection for testing
let playwrightClient = playwright;

function setPlaywrightClient(client) {
  playwrightClient = client;
}

function getPlaywrightClient() {
  return playwrightClient;
}

const app = express();
app.use(express.json({ limit: "2mb" }));

const PORT = resolvePort(process.env.BROWSER_WORKER_PORT || process.env.PORT, 8082, "browser-worker");
const CONTROL_PLANE_URL = process.env.CONTROL_PLANE_URL || "http://localhost:8080";
const BASE_URL = process.env.BROWSER_WORKER_URL || `http://localhost:${PORT}`;
const HEADLESS = process.env.BROWSER_HEADLESS !== "false";
const LIVE_FRAME_INTERVAL_MS = resolveInterval(
  process.env.BROWSER_LIVE_FRAME_INTERVAL_MS,
  750,
  "browser live frame interval"
);
const LIVE_FRAME_IDLE_MS = resolveInterval(
  process.env.BROWSER_LIVE_FRAME_IDLE_MS,
  4000,
  "browser live frame idle"
);
const LIVE_FRAME_FILENAME = "live.png";
const LIVE_ARTIFACT_ID = "browser-live";
const NAVIGATION_RETRIES = Math.max(1, resolveInterval(process.env.BROWSER_NAVIGATION_RETRIES, 2, "browser navigation retries"));
const NAVIGATION_BACKOFF_MS = Math.max(100, resolveInterval(process.env.BROWSER_NAVIGATION_BACKOFF_MS, 350, "browser navigation backoff"));
const USER_BROWSER_CDP_URL = process.env.BROWSER_USER_TAB_CDP_URL || process.env.BROWSER_CDP_URL || "";
const DEFAULT_USER_TAB_CDP_URL = "http://127.0.0.1:9222";
const USER_TAB_AUTOSTART = process.env.BROWSER_USER_TAB_AUTOSTART !== "false";
const USER_TAB_PROMPT_HANDLING_ENABLED = process.env.BROWSER_USER_TAB_PROMPT_HANDLING !== "false";
const USER_TAB_PROMPT_ATTEMPTS = Math.max(1, resolveInterval(process.env.BROWSER_USER_TAB_PROMPT_ATTEMPTS, 2, "user tab prompt attempts"));
const USER_TAB_PROMPT_DELAY_MS = Math.max(100, resolveInterval(process.env.BROWSER_USER_TAB_PROMPT_DELAY_MS, 250, "user tab prompt delay"));

const artifactsRoot = path.resolve(__dirname, "..", "artifacts");
const sessions = new Map();

const INTERACTIVE_TOOLS = new Set(["browser.click", "browser.type", "browser.evaluate"]);
const SENSITIVE_SELECTOR_PATTERNS = [
  /password/i,
  /passcode/i,
  /token/i,
  /otp/i,
  /secret/i,
  /private/i,
  /seed/i,
  /mnemonic/i,
  /api[_-]?key/i,
];

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

function resolveInterval(rawValue, fallback, label) {
  if (rawValue === undefined || rawValue === null || rawValue === "") {
    return fallback;
  }
  const parsed = Number(rawValue);
  if (Number.isFinite(parsed) && parsed > 0) {
    return parsed;
  }
  console.warn(`Invalid ${label} value "${rawValue}", falling back to ${fallback}.`);
  return fallback;
}

async function ensureDir(dir) {
  await fs.mkdir(dir, { recursive: true });
}

async function emitEvent(runId, type, payload) {
  try {
    await fetch(`${CONTROL_PLANE_URL}/runs/${runId}/events`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        type,
        source: "browser_worker",
        timestamp: new Date().toISOString(),
        payload,
      }),
    });
  } catch (err) {
    console.error("failed to emit event", err);
  }
}

function normalizeBrowserMode(value) {
  const mode = String(value || "").trim().toLowerCase();
  if (mode === "user_tab") {
    return "user_tab";
  }
  return "playwright";
}

function parseDomainAllowlist(raw) {
  if (Array.isArray(raw)) {
    return raw
      .map((entry) => String(entry || "").trim().toLowerCase())
      .filter(Boolean);
  }
  const value = String(raw || "").trim().toLowerCase();
  if (!value) return [];
  return value
    .split(/[,\s;]+/)
    .map((entry) => entry.trim().toLowerCase())
    .filter(Boolean);
}

function normalizePreferredBrowser(raw) {
  const value = String(raw || "").trim().toLowerCase();
  if (!value) return "";
  if (value.includes("brave")) return "brave";
  if (value.includes("microsoft edge") || value === "edge" || value.includes(" edg")) return "edge";
  if (value.includes("google chrome") || value === "chrome") return "chrome";
  if (value.includes("chromium")) return "chromium";
  if (value.includes("arc")) return "arc";
  if (value.includes("opera") || value.includes("opr")) return "opera";
  if (value.includes("vivaldi")) return "vivaldi";
  return "";
}

function inferPreferredBrowserFromUserAgent(userAgent) {
  const ua = String(userAgent || "").trim().toLowerCase();
  if (!ua) return "";
  if (ua.includes("brave")) return "brave";
  if (ua.includes("edg/")) return "edge";
  if (ua.includes("opr/") || ua.includes("opera")) return "opera";
  if (ua.includes("vivaldi")) return "vivaldi";
  if (ua.includes("arc/") || ua.includes(" arc")) return "arc";
  if (ua.includes("chromium")) return "chromium";
  if (ua.includes("chrome/")) return "chrome";
  return "";
}

function buildGuardrails(raw, mode) {
  const guardrails = raw && typeof raw === "object" ? raw : {};
  const interactionAllowedRaw = guardrails.interaction_allowed ?? guardrails.interactionAllowed;
  const browserUserAgent = String(guardrails.browser_user_agent ?? guardrails.browserUserAgent ?? "").trim();
  const preferredBrowser = normalizePreferredBrowser(
    guardrails.preferred_browser ?? guardrails.preferredBrowser ?? inferPreferredBrowserFromUserAgent(browserUserAgent)
  );
  const interactionAllowed =
    mode !== "user_tab" ||
    interactionAllowedRaw === true ||
    String(interactionAllowedRaw || "").toLowerCase() === "true" ||
    String(interactionAllowedRaw || "").toLowerCase() === "enabled" ||
    String(interactionAllowedRaw || "").toLowerCase() === "allow";
  return {
    interactionAllowed,
    createTabGroup: guardrails.create_tab_group !== false && guardrails.createTabGroup !== false,
    allowlistDomains: parseDomainAllowlist(guardrails.allowlist_domains ?? guardrails.allowlistDomains),
    preferredBrowser,
    browserUserAgent,
  };
}

function sanitizeToolInput(input) {
  if (!input || typeof input !== "object") {
    return {};
  }
  const next = { ...input };
  delete next._browser_mode;
  delete next._browser_guardrails;
  return next;
}

function guardrailError(reasonCode, message) {
  const err = new Error(`${message} (${reasonCode})`);
  err.reasonCode = reasonCode;
  return err;
}

function hostFromURL(rawURL) {
  try {
    return new URL(rawURL).hostname.toLowerCase();
  } catch {
    return "";
  }
}

function isDomainAllowed(rawURL, allowlistDomains) {
  if (!allowlistDomains || allowlistDomains.length === 0) {
    return true;
  }
  const host = hostFromURL(rawURL);
  if (!host) return false;
  return allowlistDomains.some((domain) => host === domain || host.endsWith(`.${domain}`));
}

function resolveUserTabCDPURL() {
  const configured = String(USER_BROWSER_CDP_URL || "").trim();
  if (configured) return configured;
  return DEFAULT_USER_TAB_CDP_URL;
}

function isLocalCDPURL(rawURL) {
  try {
    const parsed = new URL(rawURL);
    return parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" || parsed.hostname === "::1";
  } catch {
    return false;
  }
}

function cdpVersionURL(rawURL) {
  const base = String(rawURL || "").replace(/\/+$/, "");
  if (!base) return "";
  return `${base}/json/version`;
}

function parseCDPPort(rawURL) {
  try {
    const parsed = new URL(rawURL);
    if (parsed.port) {
      const port = Number(parsed.port);
      if (Number.isFinite(port) && port > 0 && port < 65536) {
        return port;
      }
    }
  } catch {
    return 9222;
  }
  return 9222;
}

async function isCDPEndpointReachable(rawURL) {
  const versionURL = cdpVersionURL(rawURL);
  if (!versionURL) return false;
  try {
    const response = await fetch(versionURL, { method: "GET" });
    return response.ok;
  } catch {
    return false;
  }
}

async function waitForCDPEndpoint(rawURL, attempts = 10, intervalMs = 250) {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    if (await isCDPEndpointReachable(rawURL)) {
      return true;
    }
    if (attempt < attempts - 1) {
      await sleep(intervalMs);
    }
  }
  return false;
}

function candidateBrowserOrder(preferred) {
  const canonical = normalizePreferredBrowser(preferred);
  const fallback = ["brave", "chrome", "edge", "arc", "chromium", "opera", "vivaldi"];
  const seen = new Set();
  const ordered = [];
  for (const item of [canonical, ...fallback]) {
    if (!item || seen.has(item)) continue;
    seen.add(item);
    ordered.push(item);
  }
  return ordered;
}

function launchBrowserWithCDP(browser, port) {
  const platform = process.platform;
  const userDataDir = path.join(os.tmpdir(), `gavryn-user-tab-${browser}`);
  const args = [`--remote-debugging-port=${port}`, `--user-data-dir=${userDataDir}`];

  if (platform === "darwin") {
    const appByBrowser = {
      brave: "Brave Browser",
      chrome: "Google Chrome",
      edge: "Microsoft Edge",
      arc: "Arc",
      chromium: "Chromium",
      opera: "Opera",
      vivaldi: "Vivaldi",
    };
    const appName = appByBrowser[browser];
    if (!appName) return false;
    try {
      const child = spawn("open", ["-na", appName, "--args", ...args], {
        detached: true,
        stdio: "ignore",
      });
      child.unref();
      return true;
    } catch {
      return false;
    }
  }

  if (platform === "linux") {
    const binByBrowser = {
      brave: ["brave-browser", "brave"],
      chrome: ["google-chrome", "chrome", "chromium-browser"],
      edge: ["microsoft-edge", "microsoft-edge-stable"],
      arc: ["arc"],
      chromium: ["chromium", "chromium-browser"],
      opera: ["opera"],
      vivaldi: ["vivaldi"],
    };
    const bins = binByBrowser[browser] || [];
    for (const bin of bins) {
      try {
        const child = spawn(bin, args, {
          detached: true,
          stdio: "ignore",
        });
        child.unref();
        return true;
      } catch {
        // Try the next candidate binary.
      }
    }
  }

  return false;
}

function isCDPConnectionFailureMessage(value) {
  const text = String(value || "").toLowerCase();
  if (!text) return false;
  return (
    (text.includes("connectovercdp") || text.includes("websocket url") || text.includes("cdp")) &&
    (text.includes("econnrefused") || text.includes("couldn't connect") || text.includes("connection refused"))
  );
}

async function ensureUserTabBrowserAvailable(rawURL, guardrails) {
  if (!USER_TAB_AUTOSTART) {
    return {
      ok: false,
      attempted: [],
      detail: "Automatic browser launch for user-tab mode is disabled (BROWSER_USER_TAB_AUTOSTART=false).",
    };
  }
  if (!isLocalCDPURL(rawURL)) {
    return {
      ok: false,
      attempted: [],
      detail: "Configured CDP endpoint is not local; automatic browser launch is skipped.",
    };
  }
  if (await isCDPEndpointReachable(rawURL)) {
    return { ok: true, attempted: [] };
  }
  const preferred = normalizePreferredBrowser(
    guardrails?.preferredBrowser || inferPreferredBrowserFromUserAgent(guardrails?.browserUserAgent || "")
  );
  const candidates = candidateBrowserOrder(preferred);
  const port = parseCDPPort(rawURL);
  for (const candidate of candidates) {
    const launched = launchBrowserWithCDP(candidate, port);
    if (!launched) continue;
    const ready = await waitForCDPEndpoint(rawURL, 12, 250);
    if (ready) {
      return { ok: true, attempted: [candidate], selected: candidate };
    }
  }
  return {
    ok: false,
    attempted: candidates,
    detail: `No local Chromium browser responded on ${rawURL} after launch attempts.`,
  };
}

async function enforceGuardrails(runId, invocationId, toolName, input, session) {
  if (session.mode !== "user_tab") return;
  const guardrails = session.guardrails || { interactionAllowed: false, allowlistDomains: [] };
  if (toolName === "browser.navigate") {
    const url = String(input?.url || "");
    if (!url) return;
    const protocol = (() => {
      try {
        return new URL(url).protocol;
      } catch {
        return "";
      }
    })();
    const allowedProtocol = protocol === "http:" || protocol === "https:" || url === "about:blank";
    if (!allowedProtocol) {
      const err = guardrailError("unsupported_navigation_scheme", `Navigation blocked for unsupported scheme: ${url}`);
      await emitEvent(runId, "browser.guardrail", {
        invocation_id: invocationId,
        tool_name: toolName,
        status: "denied",
        reason_code: err.reasonCode,
        detail: err.message,
      });
      throw err;
    }
    if (!isDomainAllowed(url, guardrails.allowlistDomains)) {
      const err = guardrailError(
        "domain_not_allowlisted",
        `Navigation blocked because ${url} is outside the allowed domain list`
      );
      await emitEvent(runId, "browser.guardrail", {
        invocation_id: invocationId,
        tool_name: toolName,
        status: "denied",
        reason_code: err.reasonCode,
        detail: err.message,
        allowlist_domains: guardrails.allowlistDomains,
      });
      throw err;
    }
  }

  if (INTERACTIVE_TOOLS.has(toolName) && !guardrails.interactionAllowed) {
    const err = guardrailError("interaction_not_allowed", `${toolName} blocked in read-only user tab mode`);
    await emitEvent(runId, "browser.guardrail", {
      invocation_id: invocationId,
      tool_name: toolName,
      status: "denied",
      reason_code: err.reasonCode,
      detail: err.message,
    });
    throw err;
  }

  if (toolName === "browser.type") {
    const selector = String(input?.selector || "");
    if (SENSITIVE_SELECTOR_PATTERNS.some((pattern) => pattern.test(selector))) {
      const err = guardrailError("sensitive_input_blocked", `Typing blocked for sensitive selector: ${selector}`);
      await emitEvent(runId, "browser.guardrail", {
        invocation_id: invocationId,
        tool_name: toolName,
        status: "denied",
        reason_code: err.reasonCode,
        detail: err.message,
      });
      throw err;
    }
  }
}

async function openUserTabSession(runId, guardrails) {
  const cdpURL = resolveUserTabCDPURL();
  let browser;
  try {
    browser = await getPlaywrightClient().chromium.connectOverCDP(cdpURL);
  } catch (err) {
    if (isCDPConnectionFailureMessage(err?.message || String(err))) {
      const ensured = await ensureUserTabBrowserAvailable(cdpURL, guardrails);
      if (ensured.ok) {
        browser = await getPlaywrightClient().chromium.connectOverCDP(cdpURL);
      } else {
        const detail = normalizeErrorMessage(err?.message || String(err));
        const attempted = ensured.attempted && ensured.attempted.length > 0
          ? ` Attempted browsers: ${ensured.attempted.join(", ")}.`
          : "";
        const message = [
          `User tab mode could not connect to ${cdpURL}.`,
          "Make sure your browser is running with remote debugging enabled, then retry.",
          attempted,
          ensured.detail ? `${ensured.detail}` : "",
          detail ? `Detail: ${detail}` : "",
        ].filter(Boolean).join(" ");
        throw guardrailError("user_tab_mode_unavailable", message);
      }
    } else {
      const detail = normalizeErrorMessage(err?.message || String(err));
      throw guardrailError("user_tab_mode_unavailable", detail || "Failed to initialize user tab mode");
    }
  }

  if (!browser) {
    const message = [
      `User tab mode could not connect to ${cdpURL}.`,
      "Make sure your browser is running with remote debugging enabled, then retry.",
    ].filter(Boolean).join(" ");
    throw guardrailError("user_tab_mode_unavailable", message);
  }
  const existingContexts = typeof browser.contexts === "function" ? browser.contexts() : [];
  const context = existingContexts[0] || await browser.newContext({ viewport: { width: 1280, height: 720 } });
  const page = await context.newPage();
  if (typeof page.bringToFront === "function") {
    await page.bringToFront();
  }
  try {
    await page.goto("about:blank", { waitUntil: "domcontentloaded" });
    await page.evaluate((label) => {
      document.title = label;
    }, `Gavryn agent tab (${runId})`);
  } catch {
    // Best effort labeling only.
  }
  await emitEvent(runId, "browser.guardrail", {
    status: "enabled",
    mode: "user_tab",
    cdp_url: cdpURL,
    preferred_browser: guardrails.preferredBrowser || undefined,
    interaction_allowed: guardrails.interactionAllowed,
    allowlist_domains: guardrails.allowlistDomains,
    tab_group_status: guardrails.createTabGroup ? "unsupported_cdp_tab_groups" : "disabled",
  });
  return {
    browser,
    context,
    page,
    mode: "user_tab",
    ownsBrowser: false,
    ownsContext: false,
    createdPage: true,
    guardrails,
    live: {
      intervalMs: LIVE_FRAME_INTERVAL_MS,
      idleMs: LIVE_FRAME_IDLE_MS,
      lastActivityAt: Date.now(),
      timer: null,
      captureInFlight: false,
      listeners: [],
    },
  };
}

async function openPlaywrightSession() {
  const browser = await getPlaywrightClient().chromium.launch({ headless: HEADLESS });
  const context = await browser.newContext({ viewport: { width: 1280, height: 720 } });
  const page = await context.newPage();
  return {
    browser,
    context,
    page,
    mode: "playwright",
    ownsBrowser: true,
    ownsContext: true,
    createdPage: true,
    guardrails: {
      interactionAllowed: true,
      createTabGroup: false,
      allowlistDomains: [],
    },
    live: {
      intervalMs: LIVE_FRAME_INTERVAL_MS,
      idleMs: LIVE_FRAME_IDLE_MS,
      lastActivityAt: Date.now(),
      timer: null,
      captureInFlight: false,
      listeners: [],
    },
  };
}

async function getSession(runId, options = {}) {
  const mode = normalizeBrowserMode(options.mode);
  const guardrails = buildGuardrails(options.guardrails, mode);
  if (sessions.has(runId)) {
    const existing = sessions.get(runId);
    if (existing.mode === mode) {
      existing.guardrails = guardrails;
      return existing;
    }
    await closeSession(runId);
  }

  const session = mode === "user_tab"
    ? await openUserTabSession(runId, guardrails)
    : await openPlaywrightSession();
  setupLiveCapture(runId, session);
  sessions.set(runId, session);
  return session;
}

async function closeSession(runId) {
  const session = sessions.get(runId);
  if (!session) return;
  try {
    await cleanupLiveCapture(runId, session);
    if (session.createdPage && typeof session.page?.close === "function") {
      await session.page.close();
    }
    if (session.ownsContext && typeof session.context?.close === "function") {
      await session.context.close();
    }
    if (session.ownsBrowser && typeof session.browser?.close === "function") {
      await session.browser.close();
    }
  } catch (err) {
    console.error("failed to close browser session", err);
  } finally {
    sessions.delete(runId);
  }
}

async function captureScreenshot(runId, invocationId, page, options = {}) {
  const artifactId = crypto.randomUUID();
  const runDir = path.join(artifactsRoot, runId);
  await ensureDir(runDir);
  const fileName = `${invocationId || artifactId}.png`;
  const filePath = path.join(runDir, fileName);
  const fullPageRequested = options.fullPage === true;
  let fullPage = fullPageRequested;
  if (fullPageRequested) {
    try {
      const metrics = await page.evaluate(() => {
        const bodyHeight = document.body?.scrollHeight || 0;
        const rootHeight = document.documentElement?.scrollHeight || 0;
        const bodyWidth = document.body?.scrollWidth || 0;
        const rootWidth = document.documentElement?.scrollWidth || 0;
        return {
          height: Math.max(bodyHeight, rootHeight),
          width: Math.max(bodyWidth, rootWidth),
        };
      });
      const pageHeight = Number(metrics?.height || 0);
      const pageWidth = Number(metrics?.width || 0);
      const tallStrip = pageWidth > 0 && pageHeight / pageWidth > 3;
      if ((Number.isFinite(pageHeight) && pageHeight > 2600) || tallStrip) {
        fullPage = false;
      }
    } catch {
      // Best effort only; keep requested mode if page metrics cannot be read.
    }
  }
  await page.screenshot({ path: filePath, fullPage });
  return {
    artifactId,
    uri: `${BASE_URL}/artifacts/${runId}/${fileName}`,
    contentType: "image/png",
    fullPageRequested,
    fullPageApplied: fullPage,
  };
}

async function captureLiveFrame(runId, page) {
  const runDir = path.join(artifactsRoot, runId);
  await ensureDir(runDir);
  const filePath = path.join(runDir, LIVE_FRAME_FILENAME);
  await page.screenshot({ path: filePath, fullPage: false });
  const cacheBuster = Date.now();
  return {
    uri: `${BASE_URL}/artifacts/${runId}/${LIVE_FRAME_FILENAME}?ts=${cacheBuster}`,
    contentType: "image/png",
  };
}

function markSessionActivity(runId, session) {
  if (!session?.live) return;
  session.live.lastActivityAt = Date.now();
  startLiveCapture(runId, session);
}

function startLiveCapture(runId, session) {
  const live = session.live;
  if (!live || live.timer) return;
  live.timer = setInterval(async () => {
    if (Date.now() - live.lastActivityAt > live.idleMs) {
      return;
    }
    if (live.captureInFlight) {
      return;
    }
    live.captureInFlight = true;
    try {
      const shot = await captureLiveFrame(runId, session.page);
      await emitEvent(runId, "browser.snapshot", {
        artifact_id: LIVE_ARTIFACT_ID,
        uri: shot.uri,
        content_type: shot.contentType,
        transient: true,
        live: true,
      });
    } catch (err) {
      console.error("failed to capture live frame", err);
    } finally {
      live.captureInFlight = false;
    }
  }, live.intervalMs);
}

function setupLiveCapture(runId, session) {
  const handler = () => markSessionActivity(runId, session);
  const events = ["domcontentloaded", "load", "framenavigated", "requestfinished"];
  for (const eventName of events) {
    session.page.on(eventName, handler);
    session.live.listeners.push([eventName, handler]);
  }
}

async function cleanupLiveCapture(runId, session) {
  const live = session.live;
  if (live?.timer) {
    clearInterval(live.timer);
    live.timer = null;
  }
  if (live?.listeners?.length) {
    for (const [eventName, handler] of live.listeners) {
      session.page.off(eventName, handler);
    }
    live.listeners = [];
  }
  try {
    await fs.unlink(path.join(artifactsRoot, runId, LIVE_FRAME_FILENAME));
  } catch (err) {
    if (err?.code !== "ENOENT") {
      console.error("failed to remove live frame", err);
    }
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function buildBrowserFailureDiagnostics({ toolName, url, selector, timeoutMs, attempt, error }) {
  return {
    tool_name: toolName,
    url: url || "",
    selector: selector || "",
    timeout_ms: timeoutMs || null,
    attempt: attempt || 1,
    timeout_class: error?.name === "TimeoutError" ? "timeout" : "runtime_error",
    error: error?.message || String(error),
  };
}

async function navigateWithRetry(runId, invocationId, page, url, timeoutMs, waitUntil) {
  let lastErr;
  for (let attempt = 1; attempt <= NAVIGATION_RETRIES; attempt += 1) {
    await emitEvent(runId, "browser.navigation", {
      invocation_id: invocationId,
      url,
      status: "attempt",
      attempt,
      wait_until: waitUntil,
    });
    try {
      await page.goto(url, { waitUntil, timeout: timeoutMs });
      return attempt;
    } catch (err) {
      lastErr = err;
      await emitEvent(runId, "browser.failure", buildBrowserFailureDiagnostics({
        toolName: "browser.navigate",
        url,
        timeoutMs,
        attempt,
        error: err,
      }));
      if (attempt >= NAVIGATION_RETRIES) {
        break;
      }
      await sleep(NAVIGATION_BACKOFF_MS * attempt);
    }
  }
  throw lastErr || new Error("navigation failed");
}

async function attemptPromptHandling(page) {
  return page.evaluate(() => {
    const normalize = (value) => String(value || "").replace(/\s+/g, " ").trim();
    const lower = (value) => normalize(value).toLowerCase();
    const isVisible = (el) => {
      if (!el || typeof el.getBoundingClientRect !== "function") return false;
      const style = window.getComputedStyle(el);
      if (!style) return false;
      if (style.display === "none" || style.visibility === "hidden" || Number(style.opacity || "1") < 0.1) return false;
      const rect = el.getBoundingClientRect();
      return rect.width > 1 && rect.height > 1;
    };
    const classifyPrompt = (text) => {
      const t = lower(text);
      if (!t) return "";
      if (
        (t.includes("privacy policy") || t.includes("privacy notice") || t.includes("terms of service") || t === "privacy" || t === "terms") &&
        !t.includes("accept") &&
        !t.includes("reject") &&
        !t.includes("agree")
      ) {
        return "";
      }
      if (
        t.includes("accept all") ||
        t.includes("accept cookies") ||
        t.includes("allow all") ||
        t.includes("agree") ||
        t.includes("i agree") ||
        t.includes("reject all") ||
        t.includes("manage choices") ||
        t.includes("manage options") ||
        t.includes("cookie settings") ||
        t.includes("save preferences") ||
        t.includes("consent")
      ) {
        return "consent";
      }
      if (
        t.includes("i am human") ||
        t.includes("i'm human") ||
        t.includes("not a robot") ||
        t.includes("verify you are human") ||
        t.includes("security check") ||
        t.includes("continue to site")
      ) {
        return "bot_prompt";
      }
      if (
        t.includes("close") ||
        t.includes("dismiss") ||
        t.includes("continue") ||
        t.includes("got it") ||
        t === "ok" ||
        t === "allow"
      ) {
        return "interstitial";
      }
      return "";
    };
    const disallowChallengeBypass = (text) => {
      const t = lower(text);
      if (!t) return false;
      return t.includes("captcha") || t.includes("recaptcha") || t.includes("hcaptcha") || t.includes("turnstile");
    };

    const selectors = [
      "#onetrust-accept-btn-handler",
      "[data-testid*='accept']",
      "[id*='consent'] button",
      "[class*='consent'] button",
      "button",
      "[role='button']",
      "a[role='button']",
      "input[type='button']",
      "input[type='submit']",
    ];
    const nodes = [];
    const seen = new Set();
    for (const selector of selectors) {
      for (const node of Array.from(document.querySelectorAll(selector))) {
        if (!node || seen.has(node)) continue;
        seen.add(node);
        if (!isVisible(node)) continue;
        nodes.push(node);
      }
    }

    const clicked = [];
    let challengeDetected = false;
    for (const node of nodes) {
      if (clicked.length >= 3) break;
      const hrefRaw = normalize(node.getAttribute?.("href") || "");
      const hrefLower = lower(hrefRaw);
      const isAnchor = String(node.tagName || "").toLowerCase() === "a";
      if (isAnchor) {
        const actionableHref =
          hrefLower === "" ||
          hrefLower === "#" ||
          hrefLower.startsWith("#") ||
          hrefLower.startsWith("javascript:");
        if (!actionableHref) {
          continue;
        }
      }
      if (
        hrefLower.includes("privacy") ||
        hrefLower.includes("terms") ||
        hrefLower.includes("policy") ||
        hrefLower.includes("legal")
      ) {
        continue;
      }
      const text = normalize(
        node.textContent ||
        node.getAttribute?.("aria-label") ||
        node.getAttribute?.("title") ||
        node.getAttribute?.("value") ||
        ""
      );
      const kind = classifyPrompt(text);
      if (!kind) continue;
      if (kind === "bot_prompt" && disallowChallengeBypass(text)) {
        challengeDetected = true;
        continue;
      }
      try {
        if (typeof node.scrollIntoView === "function") {
          node.scrollIntoView({ behavior: "instant", block: "center" });
        }
        if (typeof node.click === "function") {
          node.click();
          clicked.push({ kind, text: normalize(text).slice(0, 120) });
        }
      } catch {
        // best effort
      }
    }

    const pageFingerprint = lower(document.body?.innerText || "");
    const hardChallengePresent =
      pageFingerprint.includes("captcha") ||
      pageFingerprint.includes("verify you are human") ||
      pageFingerprint.includes("are you a robot") ||
      pageFingerprint.includes("security check") ||
      pageFingerprint.includes("checking your browser");

    return {
      clicked,
      challenge_detected: challengeDetected || hardChallengePresent,
    };
  });
}

function mergePromptActions(existing, next) {
  const merged = Array.isArray(existing) ? [...existing] : [];
  const seen = new Set(merged.map((item) => `${item.kind}:${item.text}`));
  for (const item of Array.isArray(next) ? next : []) {
    if (!item || typeof item !== "object") continue;
    const kind = normalizeWhitespace(item.kind || "");
    const text = normalizeWhitespace(item.text || "");
    if (!kind && !text) continue;
    const key = `${kind}:${text}`;
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push({ kind, text });
  }
  return merged;
}

async function maybeHandleUserTabPrompts(runId, invocationId, phase, session) {
  if (!USER_TAB_PROMPT_HANDLING_ENABLED) {
    return { actions: [], challengeDetected: false };
  }
  if (!session) {
    return { actions: [], challengeDetected: false };
  }
  if (session.mode === "user_tab" && !session.guardrails?.interactionAllowed) {
    return { actions: [], challengeDetected: false };
  }
  const page = session.page;
  const allActions = [];
  let challengeDetected = false;
  for (let attempt = 1; attempt <= USER_TAB_PROMPT_ATTEMPTS; attempt += 1) {
    let result;
    try {
      result = await attemptPromptHandling(page);
    } catch {
      break;
    }
    allActions.splice(0, allActions.length, ...mergePromptActions(allActions, result?.clicked));
    challengeDetected = challengeDetected || Boolean(result?.challenge_detected);
    if (!result?.clicked || result.clicked.length === 0) {
      break;
    }
    if (attempt < USER_TAB_PROMPT_ATTEMPTS) {
      await sleep(USER_TAB_PROMPT_DELAY_MS);
    }
  }
  if (allActions.length > 0 || challengeDetected) {
    await emitEvent(runId, "browser.prompt_handling", {
      invocation_id: invocationId,
      phase,
      status: challengeDetected ? "manual_verification_required" : "completed",
      prompt_actions: allActions,
      prompt_actions_count: allActions.length,
      reason_code: challengeDetected ? "manual_user_verification_required" : undefined,
    });
  }
  return { actions: allActions, challengeDetected };
}

function normalizeExtractionMode(mode) {
  const value = String(mode || "text").trim().toLowerCase();
  if (["text", "list", "table", "metadata"].includes(value)) {
    return value;
  }
  return "text";
}

function normalizeWhitespace(value) {
  return String(value || "").replace(/\s+/g, " ").trim();
}

function stripAnsi(value) {
  return String(value || "").replace(/\u001b\[[0-9;]*m/g, "");
}

function normalizeErrorMessage(value) {
  const message = normalizeWhitespace(stripAnsi(value));
  const marker = "call log:";
  const markerIndex = message.toLowerCase().indexOf(marker);
  if (markerIndex === -1) {
    return message;
  }
  return message.slice(0, markerIndex).trim();
}

function firstNonEmpty(...values) {
  for (const value of values) {
    const normalized = normalizeWhitespace(value);
    if (normalized) return normalized;
  }
  return "";
}

function toExtractedText(mode, extracted) {
  if (typeof extracted === "string") {
    return normalizeWhitespace(extracted);
  }
  if (Array.isArray(extracted)) {
    const flattened = extracted
      .flatMap((entry) => {
        if (typeof entry === "string") return [entry];
        if (Array.isArray(entry)) return entry.map((cell) => String(cell || ""));
        if (entry && typeof entry === "object") return [JSON.stringify(entry)];
        return [];
      })
      .join(" ");
    return normalizeWhitespace(flattened);
  }
  if (extracted && typeof extracted === "object") {
    if (mode === "metadata") {
      const parts = [
        extracted.title,
        extracted.description,
        extracted.first_paragraph,
        extracted.published_time,
        extracted.byline,
      ]
        .map((entry) => normalizeWhitespace(entry))
        .filter(Boolean);
      if (parts.length > 0) {
        return normalizeWhitespace(parts.join(" "));
      }
      return firstNonEmpty(extracted.description, extracted.first_paragraph, extracted.title, extracted.url);
    }
    return normalizeWhitespace(JSON.stringify(extracted));
  }
  return "";
}

function looksLikeArticlePath(segments) {
  if (!Array.isArray(segments) || segments.length === 0) {
    return false;
  }
  const joined = `/${segments.join("/")}`;
  if (/\/20\d{2}\/(?:0[1-9]|1[0-2])(?:\/(?:0[1-9]|[12]\d|3[01]))?/.test(joined)) {
    return true;
  }
  if (segments.length >= 3) {
    const last = String(segments[segments.length - 1] || "").trim().toLowerCase();
    return last.length >= 10;
  }
  if (segments.length === 2) {
    const second = String(segments[1] || "").trim().toLowerCase();
    return second.length >= 14 && second.includes("-");
  }
  return false;
}

async function extractMetadataFromPage(page) {
  return page.evaluate(() => {
    const normalize = (value) => String(value || "").replace(/\s+/g, " ").trim();
    const readMeta = (...keys) => {
      for (const key of keys) {
        const selector = key.startsWith("og:")
          ? `meta[property="${key}"]`
          : `meta[name="${key}"]`;
        const node = document.querySelector(selector);
        const content = node?.getAttribute("content") || "";
        if (content && content.trim()) {
          return content.trim();
        }
      }
      return "";
    };

    const pickFirstText = (...selectors) => {
      for (const selector of selectors) {
        const node = document.querySelector(selector);
        const text = normalize(node?.textContent || "");
        if (text) return text;
      }
      return "";
    };

    const readTimeElement = () => {
      const node = document.querySelector("article time[datetime], main time[datetime], time[datetime], article time, main time");
      if (!node) return "";
      return normalize(node.getAttribute("datetime") || node.textContent || "");
    };

    const headline = pickFirstText("article h1", "main h1", "h1");
    const metaTitle = readMeta("og:title", "twitter:title");
    const docTitle = normalize(document.title);
    const genericTitleSignals = ["bitcoin", "ethereum", "crypto news", "price index", "latest crypto news"];
    const docLooksGeneric = genericTitleSignals.some((signal) => docTitle.toLowerCase().includes(signal));
    const resolvedTitle = headline || (!docLooksGeneric ? docTitle : "") || metaTitle || docTitle;

    return {
      title: resolvedTitle,
      url: window.location.href,
      description: readMeta("description", "og:description", "twitter:description"),
      published_time: readMeta("article:published_time", "og:published_time", "date", "dc.date") || readTimeElement(),
      byline: pickFirstText("[rel='author']", "[itemprop='author']", ".author", ".byline", "[class*='author']"),
      canonical_url: document.querySelector("link[rel='canonical']")?.getAttribute("href") || "",
      first_paragraph: pickFirstText("article p", "main p", "p"),
    };
  });
}

async function hydratePageForTextExtraction(page) {
  for (let pass = 0; pass < 3; pass += 1) {
    const scrollState = await page.evaluate(() => {
      const doc = document.documentElement;
      const currentY = Math.max(window.scrollY || 0, doc?.scrollTop || 0);
      const viewportHeight = window.innerHeight || 0;
      const maxY = Math.max((document.body?.scrollHeight || 0) - viewportHeight, 0);
      if (maxY <= 0 || currentY >= maxY - 24) {
        return { moved: false, atBottom: true };
      }
      const nextY = Math.min(maxY, currentY + Math.max(Math.round(viewportHeight * 0.85), 720));
      window.scrollTo(0, nextY);
      return { moved: nextY > currentY + 2, atBottom: nextY >= maxY - 24 };
    });
    if (!scrollState?.moved || scrollState?.atBottom) {
      break;
    }
    await sleep(140);
  }
}

async function extractReadableTextFromPage(page) {
  return page.evaluate(() => {
    const normalize = (value) => String(value || "").replace(/\s+/g, " ").trim();
    const noisyPhrases = [
      "accept cookies",
      "manage choices",
      "cookie consent",
      "privacy policy",
      "terms of service",
      "sign in",
      "subscribe now",
      "advertisement",
    ];
    const stripNoise = (value) => {
      const normalizedText = normalize(value);
      if (!normalizedText) return "";
      const lines = normalizedText
        .split(/\n+/)
        .map((line) => normalize(line))
        .filter(Boolean)
        .filter((line) => {
          const lower = line.toLowerCase();
          return !noisyPhrases.some((phrase) => lower.includes(phrase));
        });
      return lines.join("\n");
    };
    const scoreText = (text, bonus = 0) => {
      const words = normalize(text).split(" ").filter(Boolean).length;
      return words + bonus;
    };
    const candidates = [];
    const selectors = [
      "[itemprop='articleBody']",
      "article",
      "main article",
      "article .article-body",
      "article .content",
      "main",
      ".article-body",
      ".article-content",
      ".entry-content",
      ".post-content",
      ".story-body",
      "[role='main']",
    ];

    const seen = new Set();
    for (const selector of selectors) {
      const nodes = Array.from(document.querySelectorAll(selector));
      for (const node of nodes) {
        const text = stripNoise(node?.innerText || node?.textContent || "");
        if (!text || seen.has(text)) continue;
        seen.add(text);
        const bonus = selector.includes("article") ? 120 : selector.includes("main") ? 60 : 0;
        candidates.push({ text, score: scoreText(text, bonus) });
      }
    }

    const paragraphText = Array.from(document.querySelectorAll("article p, main p, p"))
      .map((node) => stripNoise(node?.innerText || node?.textContent || ""))
      .filter((value) => value.split(" ").filter(Boolean).length >= 18)
      .slice(0, 36)
      .join("\n\n");
    if (paragraphText) {
      candidates.push({ text: paragraphText, score: scoreText(paragraphText, 160) });
    }

    const bodyText = stripNoise(document.body?.innerText || "");
    if (bodyText) {
      candidates.push({ text: bodyText, score: scoreText(bodyText, -30) });
    }

    if (candidates.length === 0) {
      return "";
    }
    candidates.sort((a, b) => b.score - a.score);
    return candidates[0].text.slice(0, 20000);
  });
}

function nonArticleReasonForUrl(rawUrl) {
  if (!rawUrl) return "";
  try {
    const parsed = new URL(rawUrl);
    const host = (parsed.hostname || "").toLowerCase();
    const path = (parsed.pathname || "").toLowerCase();
    if (path.startsWith("/search")) {
      return "search_results_page";
    }
    if (host.includes("duckduckgo.com") && parsed.searchParams.has("q")) {
      return "search_results_page";
    }
    if (path.includes("/find") && (parsed.searchParams.has("q") || parsed.searchParams.has("query") || parsed.searchParams.has("s"))) {
      return "search_results_page";
    }
    if (path.includes("search") && (parsed.searchParams.has("q") || parsed.searchParams.has("query") || parsed.searchParams.has("s") || parsed.searchParams.has("blob"))) {
      return "search_results_page";
    }
    if (!path || path === "/") {
      return "homepage_not_article";
    }
    const trimmed = path.replace(/^\/+|\/+$/g, "");
    const segments = trimmed ? trimmed.split("/") : [];
    const firstSegment = (segments[0] || "").toLowerCase();
    const policySegments = new Set([
      "privacy",
      "policy",
      "policies",
      "terms",
      "legal",
      "cookies",
      "cookie",
    ]);
    if (policySegments.has(firstSegment)) {
      return "legal_or_policy_page";
    }
    if (path.includes("terms-and-privacy") || path.includes("privacy-policy") || path.includes("terms-of-service")) {
      return "legal_or_policy_page";
    }
    if (path.includes("/author/") || path.includes("/authors/")) {
      return "section_index_page";
    }
    if (path.includes("/help/") || path.includes("/support/")) {
      return "section_index_page";
    }
    if (path.includes("/price/") || path.startsWith("/price/")) {
      return "section_index_page";
    }
    if (path.includes("/people/top-people") || path.startsWith("/people/")) {
      return "section_index_page";
    }
    if (segments.length === 1) {
      const segment = segments[0];
      if (["news", "markets", "latest", "latest-news", "latest-crypto-news", "topic", "topics", "tag", "tags", "category", "categories", "price", "prices", "video", "videos", "podcast", "podcasts", "author", "authors", "help", "support", "docs", "documentation", "faq"].includes(segment)) {
        return "section_index_page";
      }
    }
    if (path.startsWith("/news") && !looksLikeArticlePath(segments)) {
      return "section_index_page";
    }
  } catch {
    return "";
  }
  return "";
}

function buildExtractionDiagnostics({ mode, url, title, extracted, pagePreview }) {
  const extractedText = toExtractedText(mode, extracted);
  const fingerprint = normalizeWhitespace([title, pagePreview, extractedText].join(" ")).toLowerCase();
  const titleLower = normalizeWhitespace(title).toLowerCase();
  const wordCount = extractedText ? extractedText.split(/\s+/).filter(Boolean).length : 0;
  const nonArticleReason = nonArticleReasonForUrl(url);
  const signals = [];
  let parsedURL;
  try {
    parsedURL = new URL(String(url || ""));
  } catch {
    parsedURL = null;
  }

  const botIndicators = [
    "cloudflare",
    "checking your browser",
    "verify you are human",
    "attention required",
    "security check",
    "captcha",
    "just a moment",
    "access denied",
    "are you a robot",
    "not a robot",
    "detected unusual activity",
    "detected unusual traffic",
    "unusual traffic from your computer network",
    "click the box below",
  ];
  const consentIndicators = [
    "cookie consent",
    "accept cookies",
    "before you continue",
    "privacy preference",
    "manage choices",
    "consent",
  ];
  const loginIndicators = [
    "sign in to continue",
    "log in to continue",
    "subscribe to read",
    "members only",
    "paywall",
  ];
  const notFoundIndicators = [
    "page not found",
    "404",
    "this page could not be found",
    "could've sworn the page was around here somewhere",
  ];
  const errorPageIndicators = [
    "403",
    "forbidden",
    "access denied",
    "request blocked",
    "error",
  ];

  const hasAny = (terms) => terms.filter((term) => fingerprint.includes(term));
  const botMatches = hasAny(botIndicators);
  const consentMatches = hasAny(consentIndicators);
  const loginMatches = hasAny(loginIndicators);
  const extractedLower = extractedText.toLowerCase();
  const startsWithConsentBanner =
    extractedLower.startsWith("before you continue") ||
    extractedLower.startsWith("we use cookies") ||
    extractedLower.startsWith("cookie consent");
  const consentHeavy =
    consentMatches.length >= 3 ||
    (consentMatches.length >= 2 && wordCount < 260) ||
    startsWithConsentBanner ||
    (wordCount < 220 &&
      (fingerprint.includes("privacy policy") ||
        fingerprint.includes("terms of service") ||
        fingerprint.includes("terms and privacy") ||
        fingerprint.includes("manage choices") ||
        fingerprint.includes("accept all") ||
        fingerprint.includes("accept cookies")));
  const googleSorry = Boolean(
    parsedURL &&
    /(^|\.)google\./.test(String(parsedURL.hostname || "").toLowerCase()) &&
    String(parsedURL.pathname || "").toLowerCase().startsWith("/sorry")
  );

  if (botMatches.length > 0 || googleSorry) {
    signals.push(...botMatches);
    if (googleSorry) {
      signals.push("google_sorry_page");
    }
    return {
      status: "blocked",
      reason_code: "blocked_by_bot_protection",
      reason_detail: "Detected anti-bot challenge content (for example Cloudflare/captcha).",
      url,
      title,
      mode,
      extractable_content: false,
      word_count: wordCount,
      extracted_chars: extractedText.length,
      signal_matches: Array.from(new Set(signals)),
      content_preview: extractedText.slice(0, 240),
    };
  }

  if (consentMatches.length > 0 && (wordCount < 140 || consentHeavy || nonArticleReason === "legal_or_policy_page")) {
    signals.push(...consentMatches);
    return {
      status: "blocked",
      reason_code: "consent_wall",
      reason_detail: "Consent wall detected and no meaningful article body was extractable.",
      url,
      title,
      mode,
      extractable_content: false,
      word_count: wordCount,
      extracted_chars: extractedText.length,
      signal_matches: Array.from(new Set(signals)),
      content_preview: extractedText.slice(0, 240),
    };
  }

  if (loginMatches.length > 0 && (wordCount < 220 || nonArticleReason !== "")) {
    signals.push(...loginMatches);
    return {
      status: "blocked",
      reason_code: "login_wall",
      reason_detail: "Login/paywall content detected before extractable article text.",
      url,
      title,
      mode,
      extractable_content: false,
      word_count: wordCount,
      extracted_chars: extractedText.length,
      signal_matches: Array.from(new Set(signals)),
      content_preview: extractedText.slice(0, 240),
    };
  }

  if (
    titleLower.includes("page not found") ||
    titleLower.includes("404") ||
    hasAny(notFoundIndicators).length > 0
  ) {
    return {
      status: "empty",
      reason_code: "no_extractable_content",
      reason_detail: "not_found_page",
      url,
      title,
      mode,
      extractable_content: false,
      word_count: wordCount,
      extracted_chars: extractedText.length,
      signal_matches: [],
      content_preview: extractedText.slice(0, 240),
    };
  }

  if (titleLower.includes("403") || titleLower.includes("forbidden") || hasAny(errorPageIndicators).length >= 2) {
    return {
      status: "empty",
      reason_code: "no_extractable_content",
      reason_detail: "error_page",
      url,
      title,
      mode,
      extractable_content: false,
      word_count: wordCount,
      extracted_chars: extractedText.length,
      signal_matches: [],
      content_preview: extractedText.slice(0, 240),
    };
  }

  if (mode === "text" || mode === "metadata") {
    if (nonArticleReason) {
      return {
        status: "empty",
        reason_code: "no_extractable_content",
        reason_detail: nonArticleReason,
        url,
        title,
        mode,
        extractable_content: false,
        word_count: wordCount,
        extracted_chars: extractedText.length,
        signal_matches: [],
        content_preview: extractedText.slice(0, 240),
      };
    }
  }

  const minWordCount = mode === "metadata" ? 2 : mode === "table" ? 4 : 20;
  if (wordCount < minWordCount) {
    return {
      status: "empty",
      reason_code: "no_extractable_content",
      reason_detail: "Page rendered, but the selected extraction mode returned little or no usable content.",
      url,
      title,
      mode,
      extractable_content: false,
      word_count: wordCount,
      extracted_chars: extractedText.length,
      signal_matches: [],
      content_preview: extractedText.slice(0, 240),
    };
  }

  return {
    status: "ok",
    reason_code: "",
    reason_detail: "",
    url,
    title,
    mode,
    extractable_content: true,
    word_count: wordCount,
    extracted_chars: extractedText.length,
    signal_matches: [],
    content_preview: extractedText.slice(0, 240),
  };
}

function classifyExecutionFailure(err) {
  if (err && typeof err === "object" && err.reasonCode) {
    return {
      reasonCode: String(err.reasonCode),
      message: normalizeErrorMessage(err.message || String(err)),
    };
  }
  const raw = err?.message || String(err);
  const message = normalizeErrorMessage(raw);
  const lower = message.toLowerCase();
  if (
    (lower.includes("connectovercdp") || lower.includes("cdp")) &&
    (lower.includes("econnrefused") || lower.includes("websocket url"))
  ) {
    return {
      reasonCode: "user_tab_mode_unavailable",
      message: [
        `User tab mode could not connect to ${resolveUserTabCDPURL()}.`,
        "Start a Chromium-based browser with remote debugging enabled (for example --remote-debugging-port=9222), then retry.",
      ].join(" "),
    };
  }
  if (lower.includes("domain_not_allowlisted")) {
    return {
      reasonCode: "domain_not_allowlisted",
      message,
    };
  }
  if (lower.includes("interaction_not_allowed")) {
    return {
      reasonCode: "interaction_not_allowed",
      message,
    };
  }
  if (lower.includes("sensitive_input_blocked")) {
    return {
      reasonCode: "sensitive_input_blocked",
      message,
    };
  }
  if (lower.includes("user_tab_mode_unavailable")) {
    return {
      reasonCode: "user_tab_mode_unavailable",
      message,
    };
  }
  if (lower.includes("captcha") || lower.includes("cloudflare") || lower.includes("verify you are human")) {
    return {
      reasonCode: "blocked_by_bot_protection",
      message: `${message} (blocked_by_bot_protection)`,
    };
  }
  if (lower.includes("consent") || lower.includes("cookie")) {
    return {
      reasonCode: "consent_wall",
      message: `${message} (consent_wall)`,
    };
  }
  if (lower.includes("timeout")) {
    return {
      reasonCode: "navigation_timeout",
      message: `${message} (navigation_timeout)`,
    };
  }
  return {
    reasonCode: "tool_execution_failed",
    message,
  };
}

app.get("/health", (_req, res) => {
  res.json({ status: "ok" });
});

app.get("/ready", (_req, res) => {
  res.json({
    status: "ok",
    subsystems: {
      browser_engine: { status: "ok" },
      sessions: { status: "ok", active: sessions.size },
    },
  });
});

app.use("/artifacts", express.static(artifactsRoot));

app.post("/tools/execute", async (req, res) => {
  const { run_id: runId, invocation_id: invocationId, tool_name: toolName, input = {}, timeout_ms: timeoutMs } = req.body;
  const browserMode = normalizeBrowserMode(input?._browser_mode);
  const requestedGuardrails = buildGuardrails(input?._browser_guardrails, browserMode);
  const executionInput = sanitizeToolInput(input);

  if (!runId || !invocationId || !toolName) {
    res.status(400).json({ error: "run_id, invocation_id, and tool_name are required" });
    return;
  }

  const supportedTools = [
    "browser.navigate",
    "browser.snapshot",
    "browser.click",
    "browser.type",
    "browser.scroll",
    "browser.extract",
    "browser.evaluate",
    "browser.pdf",
  ];
  if (!supportedTools.includes(toolName)) {
    res.status(400).json({ error: `unsupported tool ${toolName}` });
    return;
  }

  try {
    const session = await getSession(runId, { mode: browserMode, guardrails: requestedGuardrails });
    await enforceGuardrails(runId, invocationId, toolName, executionInput, session);
    const page = session.page;
    markSessionActivity(runId, session);

    if (toolName === "browser.navigate") {
      const url = executionInput.url;
      if (!url) {
        res.status(400).json({ error: "input.url is required" });
        return;
      }
      const waitUntil = typeof executionInput.wait_until === "string" ? executionInput.wait_until : "domcontentloaded";
      const effectiveTimeoutMs = Number(timeoutMs) > 0 ? Number(timeoutMs) : 60000;
      await emitEvent(runId, "browser.navigation", { invocation_id: invocationId, url, status: "starting" });
      const completedAttempt = await navigateWithRetry(runId, invocationId, page, url, effectiveTimeoutMs, waitUntil);
      const promptHandling = await maybeHandleUserTabPrompts(runId, invocationId, "navigation", session);
      const title = await page.title();
      markSessionActivity(runId, session);
      await emitEvent(runId, "browser.navigation", {
        invocation_id: invocationId,
        url,
        status: "completed",
        title,
        attempt: completedAttempt,
        prompt_actions_count: promptHandling.actions.length,
        manual_verification_required: promptHandling.challengeDetected || undefined,
      });
      const shot = await captureScreenshot(runId, invocationId, page, { fullPage: false });
      await emitEvent(runId, "browser.snapshot", {
        invocation_id: invocationId,
        artifact_id: shot.artifactId,
        uri: shot.uri,
        content_type: shot.contentType,
      });

      res.json({
        status: "completed",
        output: {
          url,
          title,
          attempt: completedAttempt,
          wait_until: waitUntil,
          prompt_actions: promptHandling.actions,
          manual_verification_required: promptHandling.challengeDetected,
        },
        artifacts: [
          {
            artifact_id: shot.artifactId,
            type: "screenshot",
            uri: shot.uri,
            content_type: shot.contentType,
          },
        ],
      });
      return;
    }

    if (toolName === "browser.snapshot") {
      const fullPage = executionInput.full_page === true || executionInput.fullPage === true;
      const shot = await captureScreenshot(runId, invocationId, page, { fullPage });
      markSessionActivity(runId, session);
      await emitEvent(runId, "browser.snapshot", {
        invocation_id: invocationId,
        artifact_id: shot.artifactId,
        uri: shot.uri,
        content_type: shot.contentType,
      });
      res.json({
        status: "completed",
        output: { ok: true, full_page_requested: fullPage, full_page_applied: shot.fullPageApplied },
        artifacts: [
          {
            artifact_id: shot.artifactId,
            type: "screenshot",
            uri: shot.uri,
            content_type: shot.contentType,
          },
        ],
      });
      return;
    }

    if (toolName === "browser.click") {
      const selector = executionInput.selector;
      if (!selector) {
        res.status(400).json({ error: "input.selector is required" });
        return;
      }
      await emitEvent(runId, "browser.click", { invocation_id: invocationId, selector, status: "starting" });
      await page.click(selector);
      markSessionActivity(runId, session);
      await emitEvent(runId, "browser.click", { invocation_id: invocationId, selector, status: "completed" });
      const shot = await captureScreenshot(runId, invocationId, page);
      await emitEvent(runId, "browser.snapshot", {
        invocation_id: invocationId,
        artifact_id: shot.artifactId,
        uri: shot.uri,
        content_type: shot.contentType,
      });
      res.json({
        status: "completed",
        output: { clicked: true, selector },
        artifacts: [
          {
            artifact_id: shot.artifactId,
            type: "screenshot",
            uri: shot.uri,
            content_type: shot.contentType,
          },
        ],
      });
      return;
    }

    if (toolName === "browser.type") {
      const selector = executionInput.selector;
      const text = executionInput.text;
      const clear = executionInput.clear ?? false;
      if (!selector) {
        res.status(400).json({ error: "input.selector is required" });
        return;
      }
      if (text === undefined || text === null) {
        res.status(400).json({ error: "input.text is required" });
        return;
      }
      await emitEvent(runId, "browser.type", { invocation_id: invocationId, selector, status: "starting" });
      if (clear) {
        await page.fill(selector, text);
      } else {
        await page.type(selector, text);
      }
      markSessionActivity(runId, session);
      await emitEvent(runId, "browser.type", { invocation_id: invocationId, selector, status: "completed" });
      const shot = await captureScreenshot(runId, invocationId, page);
      await emitEvent(runId, "browser.snapshot", {
        invocation_id: invocationId,
        artifact_id: shot.artifactId,
        uri: shot.uri,
        content_type: shot.contentType,
      });
      res.json({
        status: "completed",
        output: { typed: true, selector, text },
        artifacts: [
          {
            artifact_id: shot.artifactId,
            type: "screenshot",
            uri: shot.uri,
            content_type: shot.contentType,
          },
        ],
      });
      return;
    }

    if (toolName === "browser.scroll") {
      const direction = executionInput.direction;
      const amount = executionInput.amount ?? 500;
      if (!direction) {
        res.status(400).json({ error: "input.direction is required (up, down, top, bottom)" });
        return;
      }
      if (!["up", "down", "top", "bottom"].includes(direction)) {
        res.status(400).json({ error: "input.direction must be one of: up, down, top, bottom" });
        return;
      }
      await emitEvent(runId, "browser.scroll", { invocation_id: invocationId, direction, status: "starting" });
      if (direction === "top") {
        await page.evaluate(() => window.scrollTo(0, 0));
      } else if (direction === "bottom") {
        await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
      } else if (direction === "up") {
        await page.evaluate((amt) => window.scrollBy(0, -amt), amount);
      } else if (direction === "down") {
        await page.evaluate((amt) => window.scrollBy(0, amt), amount);
      }
      markSessionActivity(runId, session);
      await emitEvent(runId, "browser.scroll", { invocation_id: invocationId, direction, status: "completed" });
      const shot = await captureScreenshot(runId, invocationId, page);
      await emitEvent(runId, "browser.snapshot", {
        invocation_id: invocationId,
        artifact_id: shot.artifactId,
        uri: shot.uri,
        content_type: shot.contentType,
      });
      res.json({
        status: "completed",
        output: { scrolled: true, direction },
        artifacts: [
          {
            artifact_id: shot.artifactId,
            type: "screenshot",
            uri: shot.uri,
            content_type: shot.contentType,
          },
        ],
      });
      return;
    }

    if (toolName === "browser.extract") {
      const selector = executionInput.selector;
      const attribute = executionInput.attribute;
      const mode = normalizeExtractionMode(executionInput.mode);
      const pageUrl = typeof page.url === "function" ? page.url() : "";
      const promptHandling = await maybeHandleUserTabPrompts(runId, invocationId, "extract", session);
      await emitEvent(runId, "browser.extract", { invocation_id: invocationId, selector, mode, status: "starting" });
      let extracted;
      if (mode === "text" && !selector) {
        await hydratePageForTextExtraction(page);
      }
      if (mode === "metadata") {
        extracted = await extractMetadataFromPage(page);
      } else if (mode === "table") {
        const targetSelector = selector || "table";
        extracted = await page.$$eval(targetSelector, (tables) =>
          tables.map((table) => {
            const rows = Array.from(table.querySelectorAll("tr"));
            return rows.map((row) =>
              Array.from(row.querySelectorAll("th,td")).map((cell) => (cell.textContent || "").trim())
            );
          })
        );
      } else if (mode === "list") {
        const targetSelector = selector || "li";
        extracted = await page.$$eval(targetSelector, (els) => els.map((el) => el.textContent?.trim() || ""));
      } else if (selector) {
        if (attribute) {
          extracted = await page.$$eval(
            selector,
            (els, attr) => {
              const normalizedAttr = String(attr || "").trim().toLowerCase();
              return els.map((el) => {
                if (normalizedAttr === "textcontent" || normalizedAttr === "innertext" || normalizedAttr === "text") {
                  return (el.textContent || "").trim();
                }
                if (normalizedAttr === "href" && "href" in el) {
                  return el.href || el.getAttribute("href");
                }
                return el.getAttribute(attr);
              });
            },
            attribute
          );
        } else {
          extracted = await page.$$eval(selector, (els) => els.map((el) => el.textContent?.trim() || ""));
        }
      } else {
        extracted = await extractReadableTextFromPage(page);
      }
      markSessionActivity(runId, session);
      const title = await page.title();
      const pagePreview = await page.evaluate(() => {
        const body = document.body?.innerText || "";
        return body.slice(0, 6000);
      });
      const diagnostics = buildExtractionDiagnostics({
        mode,
        url: pageUrl,
        title,
        extracted,
        pagePreview,
      });
      const count =
        Array.isArray(extracted) ? extracted.length : extracted && typeof extracted === "object" ? Object.keys(extracted).length : 1;
      await emitEvent(runId, "browser.extract", {
        invocation_id: invocationId,
        selector,
        mode,
        status: "completed",
        count,
        url: pageUrl,
        title,
        extraction_status: diagnostics.status,
        reason_code: diagnostics.reason_code || undefined,
        diagnostics,
      });
      const artifacts = [];
      const shouldCaptureExtractSnapshot = mode !== "metadata" || diagnostics.status !== "ok";
      if (shouldCaptureExtractSnapshot) {
        try {
          const shot = await captureScreenshot(runId, invocationId, page, { fullPage: false });
          await emitEvent(runId, "browser.snapshot", {
            invocation_id: invocationId,
            artifact_id: shot.artifactId,
            uri: shot.uri,
            content_type: shot.contentType,
          });
          artifacts.push({
            artifact_id: shot.artifactId,
            type: "screenshot",
            uri: shot.uri,
            content_type: shot.contentType,
          });
        } catch (snapshotErr) {
          console.error("failed to capture extract snapshot", snapshotErr);
        }
      }
      res.json({
        status: "completed",
        output: {
          mode,
          url: pageUrl,
          title,
          extracted,
          diagnostics,
          prompt_actions: promptHandling.actions,
          manual_verification_required: promptHandling.challengeDetected,
        },
        artifacts,
      });
      return;
    }

    if (toolName === "browser.evaluate") {
      const script = executionInput.script || executionInput.expression;
      if (!script) {
        res.status(400).json({ error: "input.script (or input.expression) is required" });
        return;
      }
      await emitEvent(runId, "browser.evaluate", { invocation_id: invocationId, status: "starting" });
      const result = await page.evaluate((scriptText) => {
        const source = String(scriptText || "").trim();
        if (!source) return null;
        try {
          // Support expression-style payloads such as IIFEs and literals.
          // eslint-disable-next-line no-new-func
          const expressionFn = new Function(`return (${source});`);
          return expressionFn();
        } catch (_expressionErr) {
          // Fallback for statement-style scripts.
          // eslint-disable-next-line no-new-func
          const statementFn = new Function(source);
          return statementFn();
        }
      }, script);
      markSessionActivity(runId, session);
      await emitEvent(runId, "browser.evaluate", { invocation_id: invocationId, status: "completed" });
      res.json({
        status: "completed",
        output: { result },
      });
      return;
    }

    if (toolName === "browser.pdf") {
      const format = executionInput.format || "A4";
      const filename = executionInput.filename || `${invocationId || crypto.randomUUID()}.pdf`;
      if (!["A4", "Letter"].includes(format)) {
        res.status(400).json({ error: "input.format must be one of: A4, Letter" });
        return;
      }
      await emitEvent(runId, "browser.pdf", { invocation_id: invocationId, filename, status: "starting" });
      const runDir = path.join(artifactsRoot, runId);
      await ensureDir(runDir);
      const filePath = path.join(runDir, filename);
      await page.pdf({ path: filePath, format });
      markSessionActivity(runId, session);
      await emitEvent(runId, "browser.pdf", {
        invocation_id: invocationId,
        filename,
        status: "completed",
        pdf_url: `${BASE_URL}/artifacts/${runId}/${filename}`,
      });
      res.json({
        status: "completed",
        output: { pdf_url: `${BASE_URL}/artifacts/${runId}/${filename}` },
        artifacts: [
          {
            artifact_id: filename,
            type: "pdf",
            uri: `${BASE_URL}/artifacts/${runId}/${filename}`,
            content_type: "application/pdf",
          },
        ],
      });
      return;
    }
  } catch (err) {
    const classified = classifyExecutionFailure(err);
    console.error("browser worker error", err);
    await emitEvent(runId, "browser.failure", {
      ...buildBrowserFailureDiagnostics({
        toolName,
        url: executionInput?.url,
        selector: executionInput?.selector,
        timeoutMs: Number(timeoutMs) || null,
        error: err,
      }),
      reason_code: classified.reasonCode,
      actionable_message: classified.message,
    });
    await emitEvent(runId, "warning", {
      invocation_id: invocationId,
      error: classified.message,
      reason_code: classified.reasonCode,
    });
    res.status(500).json({
      status: "failed",
      error: classified.message,
      reason_code: classified.reasonCode,
    });
  }
});

app.post("/cancel", async (req, res) => {
  const { run_id: runId } = req.body;
  if (!runId) {
    res.status(400).json({ error: "run_id is required" });
    return;
  }
  await closeSession(runId);
  await emitEvent(runId, "browser.navigation", { status: "cancelled" });
  res.json({ status: "cancelled" });
});

let server;
// Only start server if this file is run directly (not required by tests)
if (require.main === module) {
  server = app.listen(PORT, () => {
    console.log(`browser worker listening on ${PORT}`);
  });

  process.on("SIGINT", async () => {
    server.close();
    const ids = Array.from(sessions.keys());
    await Promise.all(ids.map((id) => closeSession(id)));
    process.exit(0);
  });
}

module.exports = {
  app,
  setPlaywrightClient,
  getPlaywrightClient,
  resolvePort,
  resolveInterval,
  ensureDir,
  emitEvent,
  getSession,
  closeSession,
  captureScreenshot,
  captureLiveFrame,
  markSessionActivity,
  startLiveCapture,
  setupLiveCapture,
  cleanupLiveCapture,
  sessions,
  artifactsRoot,
  BASE_URL,
  CONTROL_PLANE_URL,
  HEADLESS,
  LIVE_FRAME_INTERVAL_MS,
  LIVE_FRAME_IDLE_MS,
};
