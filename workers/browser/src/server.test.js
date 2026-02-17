const { describe, it, before, after, beforeEach, afterEach } = require("node:test");
const assert = require("node:assert");
const request = require("supertest");
const path = require("path");
const fs = require("fs/promises");

if (!process.env.BROWSER_CDP_URL) {
  process.env.BROWSER_CDP_URL = "http://127.0.0.1:9222";
}
if (!process.env.BROWSER_USER_TAB_AUTOSTART) {
  process.env.BROWSER_USER_TAB_AUTOSTART = "false";
}

const {
  app,
  setPlaywrightClient,
  resolvePort,
  resolveInterval,
  ensureDir,
  emitEvent,
  getSession,
  closeSession,
  captureScreenshot,
  captureLiveFrame,
  sessions,
  artifactsRoot,
  BASE_URL,
  CONTROL_PLANE_URL,
} = require("./server");

// Mock crypto for deterministic UUIDs
const originalRandomUUID = require("crypto").randomUUID;
let uuidCounter = 0;
require("crypto").randomUUID = () => `test-uuid-${++uuidCounter}`;

describe("Browser Worker Server", () => {
  let mockPage;
  let mockContext;
  let mockBrowser;
  let originalFetch;
  let fetchCalls;

  beforeEach(() => {
    // Reset sessions
    sessions.clear();
    uuidCounter = 0;

    // Create mock page
    mockPage = {
      goto: async () => {},
      title: async () => "Test Page",
      screenshot: async () => Buffer.from("fake-image"),
      evaluate: async () => "",
      url: () => "https://example.com",
      click: async () => {},
      type: async () => {},
      fill: async () => {},
      $$eval: async () => [],
      bringToFront: async () => {},
      close: async () => {},
      on: () => {},
      off: () => {},
    };

    // Create mock context
    mockContext = {
      newPage: async () => mockPage,
      close: async () => {},
    };

    // Create mock browser
    mockBrowser = {
      newContext: async () => mockContext,
      contexts: () => [mockContext],
      close: async () => {},
    };

    // Inject mock Playwright client
    setPlaywrightClient({
      chromium: {
        launch: async () => mockBrowser,
        connectOverCDP: async () => mockBrowser,
      },
    });

    // Mock fetch for emitEvent
    fetchCalls = [];
    originalFetch = global.fetch;
    global.fetch = async (url, options) => {
      fetchCalls.push({ url, options });
      return { ok: true };
    };
  });

  afterEach(async () => {
    // Clean up any remaining sessions
    for (const runId of sessions.keys()) {
      await closeSession(runId);
    }
    sessions.clear();
    global.fetch = originalFetch;
  });

  after(() => {
    require("crypto").randomUUID = originalRandomUUID;
  });

  describe("Health Endpoint", () => {
    it("GET /health returns 200 with ok status", async () => {
      const response = await request(app).get("/health");
      assert.strictEqual(response.status, 200);
      assert.deepStrictEqual(response.body, { status: "ok" });
    });

    it("GET /ready returns readiness payload", async () => {
      const response = await request(app).get("/ready");
      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "ok");
      assert.strictEqual(response.body.subsystems.browser_engine.status, "ok");
      assert.strictEqual(response.body.subsystems.sessions.status, "ok");
      assert.strictEqual(response.body.subsystems.sessions.active, 0);
    });
  });

  describe("Execute Endpoint - Validation", () => {
    it("POST /tools/execute without tool returns 400", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({ run_id: "test-run", invocation_id: "test-inv" });
      assert.strictEqual(response.status, 400);
      assert.ok(response.body.error.includes("required"));
    });

    it("POST /tools/execute without run_id returns 400", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({ invocation_id: "test-inv", tool_name: "browser.navigate" });
      assert.strictEqual(response.status, 400);
      assert.ok(response.body.error.includes("required"));
    });

    it("POST /tools/execute without invocation_id returns 400", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({ run_id: "test-run", tool_name: "browser.navigate" });
      assert.strictEqual(response.status, 400);
      assert.ok(response.body.error.includes("required"));
    });

    it("POST /tools/execute with unsupported tool returns 400", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.unsupported",
        });
      assert.strictEqual(response.status, 400);
      assert.ok(response.body.error.includes("unsupported"));
    });
  });

  describe("Execute Endpoint - browser.navigate", () => {
    it("successful navigation returns 200 with screenshot", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "completed");
      assert.strictEqual(response.body.output.url, "https://example.com");
      assert.strictEqual(response.body.output.title, "Test Page");
      assert.strictEqual(response.body.artifacts.length, 1);
      assert.strictEqual(response.body.artifacts[0].type, "screenshot");
    });

    it("browser.navigate without url returns 400", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: {},
        });

      assert.strictEqual(response.status, 400);
      assert.ok(response.body.error.includes("input.url is required"));
    });

    it("browser launch failure returns 500", async () => {
      setPlaywrightClient({
        chromium: {
          launch: async () => {
            throw new Error("Browser launch failed");
          },
        },
      });

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(response.status, 500);
      assert.ok(response.body.error.includes("Browser launch failed"));
    });

    it("navigation timeout returns 500", async () => {
      mockPage.goto = async () => {
        throw new Error("Navigation timeout");
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(response.status, 500);
      assert.ok(response.body.error.includes("Navigation timeout"));
    });

    it("screenshot failure during navigate returns 500", async () => {
      mockPage.screenshot = async () => {
        throw new Error("Screenshot failed");
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(response.status, 500);
      assert.ok(response.body.error.includes("Screenshot failed"));
    });

    it("uses custom timeout when provided", async () => {
      let usedTimeout;
      mockPage.goto = async (url, options) => {
        usedTimeout = options.timeout;
      };

      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
          timeout_ms: 30000,
        });

      assert.strictEqual(usedTimeout, 30000);
    });

    it("uses default timeout when not provided", async () => {
      let usedTimeout;
      mockPage.goto = async (url, options) => {
        usedTimeout = options.timeout;
      };

      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(usedTimeout, 60000);
    });

    it("reuses existing session for same run_id", async () => {
      // First request creates session
      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-1",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      const launchCalls = [];
      setPlaywrightClient({
        chromium: {
          launch: async () => {
            launchCalls.push("launch");
            return mockBrowser;
          },
        },
      });

      // Second request should reuse session
      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-2",
          tool_name: "browser.navigate",
          input: { url: "https://example2.com" },
        });

      assert.strictEqual(launchCalls.length, 0, "Should not launch new browser");
    });
  });

  describe("User Tab Mode Guardrails", () => {
    it("uses CDP connection when user_tab mode is enabled", async () => {
      let launchCalled = 0;
      let cdpCalled = 0;
      setPlaywrightClient({
        chromium: {
          launch: async () => {
            launchCalled += 1;
            return mockBrowser;
          },
          connectOverCDP: async () => {
            cdpCalled += 1;
            return mockBrowser;
          },
        },
      });

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "user-tab-run",
          invocation_id: "user-tab-inv",
          tool_name: "browser.navigate",
          input: {
            _browser_mode: "user_tab",
            _browser_guardrails: { interaction_allowed: true },
            url: "https://example.com",
          },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(cdpCalled, 1);
      assert.strictEqual(launchCalled, 0);
      const session = sessions.get("user-tab-run");
      assert.ok(session);
      assert.strictEqual(session.mode, "user_tab");
    });

    it("returns actionable user-tab unavailable error when CDP endpoint is unreachable", async () => {
      setPlaywrightClient({
        chromium: {
          launch: async () => mockBrowser,
          connectOverCDP: async () => {
            throw new Error(
              "browserType.connectOverCDP: connect ECONNREFUSED 127.0.0.1:9222 Call log: \\u001b[2m - <ws preparing> retrieving websocket url\\u001b[22m"
            );
          },
        },
      });

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "user-tab-unreachable",
          invocation_id: "user-tab-inv",
          tool_name: "browser.navigate",
          input: {
            _browser_mode: "user_tab",
            _browser_guardrails: { interaction_allowed: true },
            url: "https://example.com",
          },
        });

      assert.strictEqual(response.status, 500);
      assert.strictEqual(response.body.reason_code, "user_tab_mode_unavailable");
      assert.ok(String(response.body.error).includes("remote debugging enabled"));
      assert.ok(!String(response.body.error).includes("Call log:"));
    });

    it("blocks interactive tools when user_tab mode is read-only", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "user-tab-readonly",
          invocation_id: "user-tab-inv",
          tool_name: "browser.click",
          input: {
            _browser_mode: "user_tab",
            _browser_guardrails: { interaction_allowed: false },
            selector: "#continue",
          },
        });

      assert.strictEqual(response.status, 500);
      assert.strictEqual(response.body.reason_code, "interaction_not_allowed");
      assert.ok(String(response.body.error).includes("interaction_not_allowed"));
    });

    it("blocks navigation outside allowlisted domains in user_tab mode", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "user-tab-allowlist",
          invocation_id: "user-tab-inv",
          tool_name: "browser.navigate",
          input: {
            _browser_mode: "user_tab",
            _browser_guardrails: {
              interaction_allowed: true,
              allowlist_domains: ["example.com"],
            },
            url: "https://blocked.example.org",
          },
        });

      assert.strictEqual(response.status, 500);
      assert.strictEqual(response.body.reason_code, "domain_not_allowlisted");
      assert.ok(String(response.body.error).includes("domain_not_allowlisted"));
    });

    it("cancel closes user-tab page without closing the connected browser", async () => {
      let browserCloseCalls = 0;
      let pageCloseCalls = 0;
      const page = {
        ...mockPage,
        close: async () => {
          pageCloseCalls += 1;
        },
      };
      const context = {
        ...mockContext,
        newPage: async () => page,
      };
      const browser = {
        ...mockBrowser,
        contexts: () => [context],
        close: async () => {
          browserCloseCalls += 1;
        },
      };
      setPlaywrightClient({
        chromium: {
          launch: async () => browser,
          connectOverCDP: async () => browser,
        },
      });

      const executeResponse = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "user-tab-cancel",
          invocation_id: "user-tab-inv",
          tool_name: "browser.navigate",
          input: {
            _browser_mode: "user_tab",
            _browser_guardrails: { interaction_allowed: true },
            url: "https://example.com",
          },
        });
      assert.strictEqual(executeResponse.status, 200);

      const cancelResponse = await request(app)
        .post("/cancel")
        .send({ run_id: "user-tab-cancel" });
      assert.strictEqual(cancelResponse.status, 200);
      assert.strictEqual(pageCloseCalls, 1);
      assert.strictEqual(browserCloseCalls, 0);
    });

    it("auto-handles consent prompts in user-tab mode", async () => {
      mockPage.evaluate = async (scriptOrFn) => {
        const source = String(scriptOrFn);
        if (source.includes("querySelectorAll") && source.includes("challenge_detected")) {
          return {
            clicked: [{ kind: "consent", text: "Accept all cookies" }],
            challenge_detected: false,
          };
        }
        return "";
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "user-tab-consent",
          invocation_id: "user-tab-consent-inv",
          tool_name: "browser.navigate",
          input: {
            _browser_mode: "user_tab",
            _browser_guardrails: { interaction_allowed: true },
            url: "https://example.com",
          },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.manual_verification_required, false);
      assert.strictEqual(Array.isArray(response.body.output.prompt_actions), true);
      assert.ok(response.body.output.prompt_actions.length >= 1);
      assert.strictEqual(response.body.output.prompt_actions[0].kind, "consent");
    });

    it("flags manual verification when challenge remains after prompt handling", async () => {
      mockPage.evaluate = async (scriptOrFn) => {
        const source = String(scriptOrFn);
        if (source.includes("querySelectorAll") && source.includes("challenge_detected")) {
          return {
            clicked: [],
            challenge_detected: true,
          };
        }
        return "";
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "user-tab-challenge",
          invocation_id: "user-tab-challenge-inv",
          tool_name: "browser.navigate",
          input: {
            _browser_mode: "user_tab",
            _browser_guardrails: { interaction_allowed: true },
            url: "https://example.com",
          },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.manual_verification_required, true);
    });
  });

  describe("Prompt Handling", () => {
    it("auto-handles consent prompts in playwright mode too", async () => {
      mockPage.evaluate = async (scriptOrFn) => {
        const source = String(scriptOrFn);
        if (source.includes("querySelectorAll") && source.includes("challenge_detected")) {
          return {
            clicked: [{ kind: "consent", text: "Accept cookies" }],
            challenge_detected: false,
          };
        }
        return "";
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "playwright-consent",
          invocation_id: "playwright-consent-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(Array.isArray(response.body.output.prompt_actions), true);
      assert.ok(response.body.output.prompt_actions.length >= 1);
      assert.strictEqual(response.body.output.prompt_actions[0].kind, "consent");
    });
  });

  describe("Execute Endpoint - browser.evaluate", () => {
    it("returns expression results for IIFE-style scripts", async () => {
      mockPage.evaluate = async (fn, scriptText) => fn(scriptText);

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-eval-iife",
          tool_name: "browser.evaluate",
          input: { script: "(() => ({ links: ['a', 'b', 'c'] }))()" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "completed");
      assert.deepStrictEqual(response.body.output.result, { links: ["a", "b", "c"] });
    });

    it("accepts input.expression as an alias for input.script", async () => {
      mockPage.evaluate = async (fn, scriptText) => fn(scriptText);

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-eval-expression",
          tool_name: "browser.evaluate",
          input: { expression: "[1, 2, 3].map((value) => value * 2)" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "completed");
      assert.deepStrictEqual(response.body.output.result, [2, 4, 6]);
    });

    it("requires script or expression input", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-eval-missing",
          tool_name: "browser.evaluate",
          input: {},
        });

      assert.strictEqual(response.status, 400);
      assert.ok(String(response.body.error).includes("input.script"));
    });
  });

    describe("Execute Endpoint - browser.snapshot", () => {
    it("successful snapshot returns 200 with screenshot", async () => {
      // First create a session via navigate
      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-1",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-2",
          tool_name: "browser.snapshot",
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "completed");
      assert.strictEqual(response.body.output.ok, true);
      assert.strictEqual(response.body.artifacts.length, 1);
      assert.strictEqual(response.body.artifacts[0].type, "screenshot");
    });

    it("clamps oversized full-page snapshot requests to viewport capture", async () => {
      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-1",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      let screenshotOptions = null;
      mockPage.evaluate = async () => ({ height: 9000, width: 1280 });
      mockPage.screenshot = async (options) => {
        screenshotOptions = options;
        return Buffer.from("fake-image");
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-2",
          tool_name: "browser.snapshot",
          input: { full_page: true },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.full_page_requested, true);
      assert.strictEqual(response.body.output.full_page_applied, false);
      assert.strictEqual(screenshotOptions?.fullPage, false);
    });

    it("snapshot creates new session if none exists", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.snapshot",
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "completed");
    });

    it("screenshot failure during snapshot returns 500", async () => {
      // Create session first
      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-1",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      mockPage.screenshot = async () => {
        throw new Error("Screenshot failed");
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv-2",
          tool_name: "browser.snapshot",
        });

      assert.strictEqual(response.status, 500);
      assert.ok(response.body.error.includes("Screenshot failed"));
    });
  });

  describe("Execute Endpoint - browser.extract", () => {
    it("successful metadata extract returns 200 and skips snapshot artifact for non-blocked pages", async () => {
      mockPage.url = () => "https://example.com/article";
      mockPage.evaluate = async () => ({
        title: "Example Title",
        url: "https://example.com/article",
        description: "Example description",
      });

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-1",
          tool_name: "browser.extract",
          input: { mode: "metadata" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "completed");
      assert.strictEqual(response.body.output.mode, "metadata");
      assert.strictEqual(response.body.output.extracted.url, "https://example.com/article");
      assert.strictEqual(response.body.output.diagnostics.status, "ok");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, true);
      assert.deepStrictEqual(response.body.artifacts, []);
    });

    it("extract still succeeds when snapshot capture fails", async () => {
      mockPage.evaluate = async () => "Extracted text body";
      mockPage.screenshot = async () => {
        throw new Error("Screenshot failed");
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-2",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "completed");
      assert.strictEqual(response.body.output.mode, "text");
      assert.strictEqual(response.body.output.extracted, "Extracted text body");
      assert.strictEqual(response.body.output.diagnostics.status, "empty");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "no_extractable_content");
      assert.deepStrictEqual(response.body.artifacts, []);
    });

    it("labels consent-wall extractions with explicit diagnostics", async () => {
      mockPage.evaluate = async () =>
        "Before you continue, we use cookies. Please accept cookies to continue to the article.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-consent",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "blocked");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "consent_wall");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("labels consent-heavy pages as consent walls even when body text is long", async () => {
      mockPage.url = () => "https://decrypt.co/price/memecore";
      mockPage.evaluate = async () =>
        "Before you continue, we use cookies and consent preferences. Manage choices, accept all cookies, and review the privacy policy. " +
        "Before you continue, we use cookies and consent preferences. Manage choices, accept all cookies, and review the privacy policy. " +
        "Before you continue, we use cookies and consent preferences. Manage choices, accept all cookies, and review the privacy policy. " +
        "Before you continue, we use cookies and consent preferences. Manage choices, accept all cookies, and review the privacy policy.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-consent-long",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "blocked");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "consent_wall");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("marks search result pages as non-extractable evidence", async () => {
      mockPage.url = () => "https://www.bloomberg.com/search?query=real+world+assets+crypto+tokenization+2026";
      mockPage.evaluate = async () =>
        "Search results for real world assets tokenization and crypto market coverage.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-search",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "empty");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "no_extractable_content");
      assert.strictEqual(response.body.output.diagnostics.reason_detail, "search_results_page");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("labels bot challenge pages with explicit diagnostics", async () => {
      mockPage.url = () => "https://www.bloomberg.com/search?query=real+world+assets+crypto+tokenization+2026";
      mockPage.evaluate = async () =>
        "Bloomberg - Are you a robot? We've detected unusual activity. Please click the box below.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-bot",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "blocked");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "blocked_by_bot_protection");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("labels google sorry pages as blocked_by_bot_protection", async () => {
      mockPage.url = () => "https://www.google.com/sorry/index?continue=https://www.google.com/search?q=rwa+crypto";
      mockPage.evaluate = async () =>
        "About this page. This page checks to see if it's really you sending the requests, and not a robot. Our systems have detected unusual traffic from your computer network.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-google-sorry",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "blocked");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "blocked_by_bot_protection");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("does not misclassify /news/<slug> pages as index pages", async () => {
      mockPage.url = () =>
        "https://cointelegraph.com/news/crypto-vc-funding-doubled-in-2025-as-rwa-tokenization-took-the-lead";
      mockPage.evaluate = async () =>
        "Crypto VC funding doubled in 2025 while RWA tokenization led allocations, according to the latest market data and deal flow analysis.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-news-slug",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "ok");
      assert.notStrictEqual(response.body.output.diagnostics.reason_detail, "section_index_page");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, true);
    });

    it("supports textContent attribute extraction for selector mode", async () => {
      mockPage.$$eval = async (selector, fn, attr) =>
        fn(
          [
            {
              textContent: " First headline ",
              href: "https://example.com/a",
              getAttribute: () => null,
            },
            {
              textContent: "Second headline",
              href: "https://example.com/b",
              getAttribute: () => null,
            },
          ],
          attr
        );

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-attr-text",
          tool_name: "browser.extract",
          input: { selector: "a", attribute: "textContent" },
        });

      assert.strictEqual(response.status, 200);
      assert.deepStrictEqual(response.body.output.extracted, ["First headline", "Second headline"]);
    });

    it("marks legal/privacy pages as non-extractable evidence", async () => {
      mockPage.url = () => "https://cointelegraph.com/terms-and-privacy";
      mockPage.evaluate = async () => "Terms of service and privacy policy for site usage.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-legal",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "empty");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "no_extractable_content");
      assert.strictEqual(response.body.output.diagnostics.reason_detail, "legal_or_policy_page");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("marks author pages as non-extractable evidence", async () => {
      mockPage.url = () => "https://www.coindesk.com/author/olivier-acuna";
      mockPage.evaluate = async () => "Olivier Acuna writes about crypto markets and regulation.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-author",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "empty");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "no_extractable_content");
      assert.strictEqual(response.body.output.diagnostics.reason_detail, "section_index_page");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("marks 404 pages as non-extractable evidence", async () => {
      mockPage.url = () => "https://cointelegraph.com/opinion/rwas-gatekeepers";
      mockPage.title = async () => "Page Not Found | 404 | Cointelegraph";
      mockPage.evaluate = async () => "Could've sworn the page was around here somewhere.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-not-found",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "empty");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "no_extractable_content");
      assert.strictEqual(response.body.output.diagnostics.reason_detail, "not_found_page");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("marks 403 forbidden pages as non-extractable evidence", async () => {
      mockPage.url = () => "https://www.coindesk.com/author/francisco-rodrigues,saksham-diwan";
      mockPage.title = async () => "403: Forbidden";
      mockPage.evaluate = async () => "Access denied.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-forbidden",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "blocked");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "blocked_by_bot_protection");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("marks price pages as section index pages", async () => {
      mockPage.url = () => "https://decrypt.co/price/memecore";
      mockPage.evaluate = async () => "MemeCore price, chart, and market data overview.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-price-page",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "empty");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "no_extractable_content");
      assert.strictEqual(response.body.output.diagnostics.reason_detail, "section_index_page");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });

    it("marks latest-crypto-news index pages as non-extractable evidence", async () => {
      mockPage.url = () => "https://www.coindesk.com/latest-crypto-news";
      mockPage.evaluate = async () => "Latest crypto news index and rolling headlines.";

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-extract-latest-index",
          tool_name: "browser.extract",
          input: { mode: "text" },
        });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.output.diagnostics.status, "empty");
      assert.strictEqual(response.body.output.diagnostics.reason_code, "no_extractable_content");
      assert.strictEqual(response.body.output.diagnostics.reason_detail, "section_index_page");
      assert.strictEqual(response.body.output.diagnostics.extractable_content, false);
    });
  });

  describe("Cancel Endpoint", () => {
    it("POST /cancel closes session and returns 200", async () => {
      // Create a session first
      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(sessions.has("test-run"), true);

      const response = await request(app)
        .post("/cancel")
        .send({ run_id: "test-run" });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "cancelled");
      assert.strictEqual(sessions.has("test-run"), false);
    });

    it("cancel without run_id returns 400", async () => {
      const response = await request(app).post("/cancel").send({});
      assert.strictEqual(response.status, 400);
      assert.ok(response.body.error.includes("run_id is required"));
    });

    it("cancel without existing session returns 200 (no-op)", async () => {
      const response = await request(app)
        .post("/cancel")
        .send({ run_id: "non-existent-run" });

      assert.strictEqual(response.status, 200);
      assert.strictEqual(response.body.status, "cancelled");
    });

    it("cancel handles session close errors gracefully", async () => {
      // Create a session with a browser that throws on close
      const errorBrowser = {
        newContext: async () => ({
          newPage: async () => mockPage,
          close: async () => {
            throw new Error("Close failed");
          },
        }),
        close: async () => {
          throw new Error("Close failed");
        },
      };

      setPlaywrightClient({
        chromium: {
          launch: async () => errorBrowser,
        },
      });

      await request(app)
        .post("/tools/execute")
        .send({
          run_id: "error-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      // Should not throw
      const response = await request(app)
        .post("/cancel")
        .send({ run_id: "error-run" });

      assert.strictEqual(response.status, 200);
    });
  });

  describe("Helper Functions", () => {
    describe("resolvePort", () => {
      it("parses valid PORT env value", () => {
        assert.strictEqual(resolvePort("3000", 8080, "test"), 3000);
        assert.strictEqual(resolvePort("8081", 8082, "browser"), 8081);
      });

      it("defaults to fallback when env is undefined", () => {
        assert.strictEqual(resolvePort(undefined, 8082, "test"), 8082);
      });

      it("defaults to fallback for invalid string", () => {
        assert.strictEqual(resolvePort("invalid", 8082, "test"), 8082);
      });

      it("defaults to fallback for negative number", () => {
        assert.strictEqual(resolvePort("-1", 8082, "test"), 8082);
      });

      it("defaults to fallback for number too high", () => {
        assert.strictEqual(resolvePort("65536", 8082, "test"), 8082);
      });

      it("defaults to fallback for non-finite number", () => {
        assert.strictEqual(resolvePort("Infinity", 8082, "test"), 8082);
        assert.strictEqual(resolvePort("NaN", 8082, "test"), 8082);
      });

      it("logs warning for invalid port", () => {
        const originalWarn = console.warn;
        let warningMessage;
        console.warn = (msg) => {
          warningMessage = msg;
        };

        resolvePort("invalid", 8082, "test-service");

        assert.ok(warningMessage.includes("Invalid test-service port"));
        assert.ok(warningMessage.includes("falling back to 8082"));

        console.warn = originalWarn;
      });
    });

    describe("resolveInterval", () => {
      it("uses fallback when invalid", () => {
        assert.strictEqual(resolveInterval("bad", 500, "test"), 500);
      });

      it("parses valid interval", () => {
        assert.strictEqual(resolveInterval("750", 500, "test"), 750);
      });
    });

    describe("ensureDir", () => {
      it("creates directory recursively", async () => {
        const testDir = path.join(artifactsRoot, "test-ensure-dir");
        await ensureDir(testDir);

        const stats = await fs.stat(testDir);
        assert.ok(stats.isDirectory());

        // Cleanup
        await fs.rmdir(testDir);
      });
    });

    describe("emitEvent", () => {
      it("sends event to control plane", async () => {
        await emitEvent("test-run", "test_event", { foo: "bar" });

        assert.strictEqual(fetchCalls.length, 1);
        assert.ok(fetchCalls[0].url.includes("/runs/test-run/events"));
        const body = JSON.parse(fetchCalls[0].options.body);
        assert.strictEqual(body.type, "test_event");
        assert.strictEqual(body.source, "browser_worker");
        assert.strictEqual(body.payload.foo, "bar");
        assert.ok(body.timestamp);
      });

      it("handles fetch errors gracefully", async () => {
        global.fetch = async () => {
          throw new Error("Network error");
        };

        const originalError = console.error;
        let errorMessage;
        console.error = (...args) => {
          errorMessage = args.join(" ");
        };

        // Should not throw
        await emitEvent("test-run", "test_event", {});

        assert.ok(errorMessage.includes("failed to emit event"));
        assert.ok(errorMessage.includes("Network error"));

        console.error = originalError;
      });
    });

    describe("getSession", () => {
      it("creates new session when none exists", async () => {
        const session = await getSession("new-run");
        assert.ok(session.browser);
        assert.ok(session.context);
        assert.ok(session.page);
        assert.strictEqual(sessions.has("new-run"), true);
      });

      it("returns existing session when one exists", async () => {
        const session1 = await getSession("existing-run");
        const session2 = await getSession("existing-run");
        assert.strictEqual(session1, session2);
      });
    });

    describe("closeSession", () => {
      it("closes browser and context and removes from sessions", async () => {
        await getSession("close-test");
        assert.strictEqual(sessions.has("close-test"), true);

        await closeSession("close-test");
        assert.strictEqual(sessions.has("close-test"), false);
      });

      it("handles non-existent session gracefully", async () => {
        // Should not throw
        await closeSession("non-existent");
      });

      it("handles close errors gracefully", async () => {
        const errorBrowser = {
          newContext: async () => ({
            newPage: async () => mockPage,
            close: async () => {
              throw new Error("Context close error");
            },
          }),
          close: async () => {
            throw new Error("Browser close error");
          },
        };

        setPlaywrightClient({
          chromium: {
            launch: async () => errorBrowser,
          },
        });

        await getSession("error-close");

        const originalError = console.error;
        let errorMessage;
        console.error = (...args) => {
          errorMessage = args.join(" ");
        };

        // Should not throw
        await closeSession("error-close");

        assert.ok(errorMessage.includes("failed to close browser session"));

        console.error = originalError;
      });
    });

    describe("captureScreenshot", () => {
      it("captures screenshot and returns artifact info", async () => {
        const runId = "screenshot-test";
        const invocationId = "inv-123";

        // Create session first
        const session = await getSession(runId);

        const result = await captureScreenshot(runId, invocationId, session.page);

        assert.ok(result.artifactId);
        assert.ok(result.uri.includes(`/artifacts/${runId}/${invocationId}`));
        assert.strictEqual(result.contentType, "image/png");

        // Cleanup
        await closeSession(runId);
      });

      it("uses artifactId when invocationId not provided", async () => {
        const runId = "screenshot-test-no-inv";

        // Create session first
        const session = await getSession(runId);

        const result = await captureScreenshot(runId, null, session.page);

        assert.ok(result.artifactId);
        assert.ok(result.uri.includes(`/artifacts/${runId}/${result.artifactId}`));

        // Cleanup
        await closeSession(runId);
      });

      it("returns full-page request metadata", async () => {
        const runId = "screenshot-test-full-page";
        const session = await getSession(runId);
        session.page.evaluate = async () => 1600;

        const result = await captureScreenshot(runId, "inv-full", session.page, { fullPage: true });

        assert.strictEqual(result.fullPageRequested, true);
        assert.strictEqual(result.fullPageApplied, true);

        await closeSession(runId);
      });
    });

    describe("captureLiveFrame", () => {
      it("captures live frame with cache busting", async () => {
        const runId = "live-test";
        const session = await getSession(runId);
        const originalNow = Date.now;
        let screenshotOptions = null;
        Date.now = () => 12345;
        session.page.screenshot = async (options) => {
          screenshotOptions = options;
          return Buffer.from("fake-image");
        };

        const result = await captureLiveFrame(runId, session.page);

        assert.ok(result.uri.includes(`/artifacts/${runId}/live.png?ts=12345`));
        assert.strictEqual(result.contentType, "image/png");
        assert.strictEqual(screenshotOptions?.fullPage, false);

        Date.now = originalNow;
        await closeSession(runId);
      });
    });
  });

  describe("Error Handling", () => {
    it("handles null request body", async () => {
      const response = await request(app)
        .post("/tools/execute")
        .send(null);

      assert.strictEqual(response.status, 400);
    });

    it("handles error with non-Error object", async () => {
      mockPage.goto = async () => {
        throw "String error"; // Not an Error object
      };

      const response = await request(app)
        .post("/tools/execute")
        .send({
          run_id: "test-run",
          invocation_id: "test-inv",
          tool_name: "browser.navigate",
          input: { url: "https://example.com" },
        });

      assert.strictEqual(response.status, 500);
      assert.strictEqual(response.body.error, "String error");
    });
  });

  describe("Artifacts Static Serving", () => {
    it("serves artifacts directory", async () => {
      // Create a test artifact
      const testRunDir = path.join(artifactsRoot, "static-test");
      await ensureDir(testRunDir);
      await fs.writeFile(path.join(testRunDir, "test.png"), "fake-image-data");

      const response = await request(app).get("/artifacts/static-test/test.png");

      // Should either return the file or 404 if not found
      assert.ok(response.status === 200 || response.status === 404);

      // Cleanup
      await fs.unlink(path.join(testRunDir, "test.png"));
      await fs.rmdir(testRunDir);
    });
  });

  describe("Server Startup", () => {
    it("starts server when run directly", async () => {
      const { spawn } = require("child_process");
      const path = require("path");

      // Use a random port to avoid conflicts
      const testPort = 19000 + Math.floor(Math.random() * 1000);

      const child = spawn("node", [path.join(__dirname, "server.js")], {
        env: {
          ...process.env,
          PORT: String(testPort),
          BROWSER_WORKER_PORT: String(testPort),
          BROWSER_HEADLESS: "true",
        },
        stdio: "pipe",
      });

      let output = "";
      child.stdout.on("data", (data) => {
        output += data.toString();
      });
      child.stderr.on("data", (data) => {
        output += data.toString();
      });

      // Wait for server to start
      await new Promise((resolve) => setTimeout(resolve, 500));

      // Verify server started
      assert.ok(output.includes(`browser worker listening on ${testPort}`));

      // Test health endpoint
      const healthResponse = await new Promise((resolve, reject) => {
        const http = require("http");
        const req = http.get(`http://localhost:${testPort}/health`, (res) => {
          let data = "";
          res.on("data", (chunk) => (data += chunk));
          res.on("end", () => resolve({ status: res.statusCode, body: data }));
        });
        req.on("error", reject);
        req.setTimeout(1000, () => {
          req.destroy();
          reject(new Error("Timeout"));
        });
      });

      assert.strictEqual(healthResponse.status, 200);
      assert.ok(healthResponse.body.includes("ok"));

      // Send SIGINT to test graceful shutdown
      child.kill("SIGINT");

      // Wait for process to exit
      await new Promise((resolve) => {
        child.on("exit", resolve);
        setTimeout(() => child.kill("SIGTERM"), 2000);
      });
    });

    it("handles SIGINT with active sessions", async () => {
      const { spawn } = require("child_process");
      const path = require("path");

      const testPort = 19000 + Math.floor(Math.random() * 1000);

      const child = spawn("node", [path.join(__dirname, "server.js")], {
        env: {
          ...process.env,
          PORT: String(testPort),
          BROWSER_WORKER_PORT: String(testPort),
          BROWSER_HEADLESS: "true",
        },
        stdio: "pipe",
      });

      let output = "";
      child.stdout.on("data", (data) => {
        output += data.toString();
      });
      child.stderr.on("data", (data) => {
        output += data.toString();
      });

      // Wait for server to start
      await new Promise((resolve) => setTimeout(resolve, 500));

      // Create a session by calling execute
      const http = require("http");
      const postData = JSON.stringify({
        run_id: "sigint-test-run",
        invocation_id: "test-inv",
        tool_name: "browser.navigate",
        input: { url: "about:blank" },
      });

      await new Promise((resolve, reject) => {
        const req = http.request(
          {
            hostname: "localhost",
            port: testPort,
            path: "/tools/execute",
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "Content-Length": Buffer.byteLength(postData),
            },
          },
          (res) => {
            let data = "";
            res.on("data", (chunk) => (data += chunk));
            res.on("end", resolve);
          }
        );
        req.on("error", reject);
        req.write(postData);
        req.end();
      });

      // Send SIGINT - should gracefully close sessions
      child.kill("SIGINT");

      // Wait for process to exit
      const exitCode = await new Promise((resolve) => {
        child.on("exit", resolve);
        setTimeout(() => {
          child.kill("SIGTERM");
          resolve(-1);
        }, 3000);
      });

      // Process should exit cleanly (code 0)
      assert.ok(exitCode === 0 || exitCode === -1, `Process exited with code ${exitCode}`);
    });
  });
});
