import { describe, expect, it } from "vitest";
import { buildConversationItems, groupConversationItems } from "./threadItems";

describe("threadItems", () => {
  it("builds semantic tool items from tool events", () => {
    const items = buildConversationItems({
      messages: [],
      events: [
        {
          run_id: "run-1",
          seq: 1,
          type: "tool.completed",
          source: "tool_runner",
          payload: {
            tool_name: "browser.navigate",
            input: { url: "https://example.com" },
            output: { url: "https://example.com", title: "Example" },
            artifacts: [{ uri: "https://example.com/snap.png", type: "screenshot", content_type: "image/png" }],
          },
        },
      ],
    });

    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe("tool");
    if (items[0].kind === "tool") {
      expect(items[0].title).toContain("Visited");
      expect(items[0].artifacts?.[0]?.uri).toBe("https://example.com/snap.png");
    }
  });

  it("dedupes assistant message that matches reasoning content on same seq", () => {
    const items = buildConversationItems({
      messages: [
        { id: "m1", role: "assistant", content: "Thinking process", seq: 2 },
      ],
      events: [
        {
          run_id: "run-1",
          seq: 2,
          type: "model.summary",
          source: "llm",
          payload: { text: "Thinking process" },
        },
      ],
    });

    expect(items.filter((item) => item.kind === "reasoning")).toHaveLength(1);
    expect(items.filter((item) => item.kind === "message")).toHaveLength(0);
  });

  it("groups contiguous tool and explore items", () => {
    const grouped = groupConversationItems([
      { id: "u1", seq: 1, kind: "message", role: "user", text: "hello" },
      { id: "e1", seq: 2, kind: "explore", status: "exploring", entries: [{ kind: "search", label: "defi" }] },
      { id: "t1", seq: 3, kind: "tool", toolType: "browser.navigate", title: "Visited x", status: "completed" },
      { id: "a1", seq: 4, kind: "message", role: "assistant", text: "done" },
    ]);

    expect(grouped).toHaveLength(3);
    expect(grouped[1].kind).toBe("toolGroup");
    if (grouped[1].kind === "toolGroup") {
      expect(grouped[1].group.toolCount).toBe(2);
      expect(grouped[1].group.readsAndSearchesCount).toBe(1);
      expect(grouped[1].group.filesChangedCount).toBe(0);
      expect(grouped[1].group.commandsCount).toBe(0);
    }
  });

  it("reconciles started/completed lifecycle by invocation id", () => {
    const items = buildConversationItems({
      messages: [],
      events: [
        {
          run_id: "run-1",
          seq: 2,
          type: "tool.started",
          source: "tool_runner",
          payload: {
            tool_invocation_id: "inv-1",
            tool_name: "process.exec",
            input: { command: "npm", args: ["test"] },
          },
        },
        {
          run_id: "run-1",
          seq: 3,
          type: "tool.completed",
          source: "tool_runner",
          payload: {
            tool_invocation_id: "inv-1",
            tool_name: "process.exec",
            input: { command: "npm", args: ["test"] },
            output: { command: "npm", args: ["test"], stdout: "ok" },
          },
        },
      ],
    });

    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe("tool");
    if (items[0].kind === "tool") {
      expect(items[0].status).toBe("completed");
      expect(items[0].output).toContain("stdout");
    }
  });

  it("reconciles started/failed lifecycle by invocation id", () => {
    const items = buildConversationItems({
      messages: [],
      events: [
        {
          run_id: "run-1",
          seq: 2,
          type: "tool.started",
          source: "tool_runner",
          payload: {
            tool_invocation_id: "inv-fail",
            tool_name: "browser.extract",
            input: { mode: "text" },
          },
        },
        {
          run_id: "run-1",
          seq: 3,
          type: "tool.failed",
          source: "llm",
          payload: {
            tool_invocation_id: "inv-fail",
            tool_name: "browser.extract",
            error: "blocked_by_bot_protection",
          },
        },
      ],
    });

    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe("tool");
    if (items[0].kind === "tool") {
      expect(items[0].status).toBe("failed");
      expect(items[0].title.toLowerCase()).toContain("failed");
    }
  });

  it("formats nested JSON tool failure payloads into readable output", () => {
    const items = buildConversationItems({
      messages: [],
      events: [
        {
          run_id: "run-1",
          seq: 2,
          type: "tool.failed",
          source: "llm",
          payload: {
            tool_invocation_id: "inv-fail-json",
            tool_name: "browser.navigate",
            error: "{\"status\":\"failed\",\"error\":\"browserType.connectOverCDP: connect ECONNREFUSED 127.0.0.1:9222 Call log: \\u001b[2m - <ws preparing>\\u001b[22m\",\"reason_code\":\"user_tab_mode_unavailable\"}",
          },
        },
      ],
    });

    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe("tool");
    if (items[0].kind === "tool") {
      expect(items[0].status).toBe("failed");
      expect(items[0].detail).toContain("user tab mode unavailable");
      expect(items[0].output || "").not.toContain("Call log:");
    }
  });
});
