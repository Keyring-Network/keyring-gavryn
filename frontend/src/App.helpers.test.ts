import { describe, it, expect } from "vitest";
import {
  getTagColorIndex,
  normalizeTagName,
  normalizeSearch,
  isSubsequence,
  matchesQuery,
  createId,
  formatTime,
  formatDate,
  titleFromArtifact,
  mergeLibraryItems,
  collectLibraryItems,
  applyEvent,
  tagColors,
  encodeBase64,
  decodeBase64,
} from "./App";
import type { RunEvent } from "./lib/events";

describe("App Helpers", () => {
  describe("Base64 Helpers", () => {
    it("encodes and decodes correctly", () => {
      const original = "Hello World";
      const encoded = encodeBase64(original);
      const decoded = decodeBase64(encoded);
      expect(decoded).toBe(original);
    });
  });

  describe("getTagColorIndex", () => {
    it("returns consistent index for same tag", () => {
      const idx1 = getTagColorIndex("test");
      const idx2 = getTagColorIndex("test");
      expect(idx1).toBe(idx2);
      expect(idx1).toBeGreaterThanOrEqual(0);
      expect(idx1).toBeLessThan(tagColors.length);
    });

    it("returns different index for different tags (likely)", () => {
      const idx1 = getTagColorIndex("test");
      const idx2 = getTagColorIndex("other");
      expect(idx1).not.toBe(idx2);
    });
  });

  describe("normalizeTagName", () => {
    it("trims and lowercases", () => {
      expect(normalizeTagName("  Test  ")).toBe("test");
      expect(normalizeTagName("TAG")).toBe("tag");
    });
  });

  describe("normalizeSearch", () => {
    it("trims and lowercases", () => {
      expect(normalizeSearch("  Query  ")).toBe("query");
    });
  });

  describe("isSubsequence", () => {
    it("returns true for subsequence", () => {
      expect(isSubsequence("abc", "aabbcc")).toBe(true);
      expect(isSubsequence("test", "testing")).toBe(true);
    });

    it("returns false for non-subsequence", () => {
      expect(isSubsequence("acb", "aabbcc")).toBe(false); // order matters
      expect(isSubsequence("xyz", "aabbcc")).toBe(false);
    });
  });

  describe("matchesQuery", () => {
    it("returns true if query is empty", () => {
      expect(matchesQuery("", "anything")).toBe(true);
    });

    it("returns false if value is empty", () => {
      expect(matchesQuery("test", "")).toBe(false);
    });

    it("returns true for substring match", () => {
      expect(matchesQuery("test", "testing")).toBe(true);
    });

    it("returns true for subsequence match", () => {
      expect(matchesQuery("tst", "testing")).toBe(true);
    });

    it("returns false for no match", () => {
      expect(matchesQuery("xyz", "testing")).toBe(false);
    });
  });

  describe("createId", () => {
    it("returns a string", () => {
      expect(typeof createId()).toBe("string");
    });

    it("uses crypto.randomUUID if available", () => {
      // crypto is mocked in setup
      expect(createId()).toMatch(/^test-uuid-/);
    });
  });

  describe("formatTime", () => {
    it("formats time correctly", () => {
      const date = new Date("2023-01-01T12:34:56");
      // Output depends on locale, but should contain 12:34
      expect(formatTime(date.toISOString())).toMatch(/12:34/);
    });
  });

  describe("formatDate", () => {
    it("formats date correctly", () => {
      const date = new Date("2023-01-01T12:34:56");
      // Output depends on locale, but should contain Jan
      expect(formatDate(date.toISOString())).toMatch(/Jan/);
    });
  });

  describe("titleFromArtifact", () => {
    it("handles pdf", () => {
      expect(titleFromArtifact("file", "application/pdf")).toBe("PDF document");
    });

    it("handles presentation", () => {
      expect(titleFromArtifact("file", "application/vnd.ms-powerpoint")).toBe("Presentation");
    });

    it("handles image", () => {
      expect(titleFromArtifact("image", "image/png")).toBe("Image");
    });

    it("handles generic type", () => {
      expect(titleFromArtifact("code", "text/plain")).toBe("Code");
      expect(titleFromArtifact("my-file", "text/plain")).toBe("My file");
    });

    it("handles missing type", () => {
      expect(titleFromArtifact(undefined, "text/plain")).toBe("Artifact");
    });
  });

  describe("mergeLibraryItems", () => {
    it("merges items by id or uri", () => {
      const existing = [
        { id: "1", uri: "u1", label: "1", type: "t", createdAt: "", runId: "r1" },
      ];
      const incoming = [
        { id: "1", uri: "u1", label: "1-new", type: "t", createdAt: "", runId: "r1" }, // Duplicate
        { id: "2", uri: "u2", label: "2", type: "t", createdAt: "", runId: "r1" },
      ];
      const merged = mergeLibraryItems(existing, incoming);
      expect(merged).toHaveLength(2);
      expect(merged.find(i => i.id === "1")?.label).toBe("1"); // Keeps existing
    });
  });

  describe("collectLibraryItems", () => {
    it("collects browser snapshots", () => {
      const event: RunEvent = {
        type: "browser.snapshot",
        run_id: "r1",
        seq: 1,
        timestamp: "2023-01-01T00:00:00Z",
        payload: {
          uri: "http://example.com/snap.png",
          content_type: "image/png",
          artifact_id: "a1",
        },
      };
      const items = collectLibraryItems(event);
      expect(items).toHaveLength(1);
      expect(items[0].type).toBe("browser.snapshot");
      expect(items[0].uri).toBe("http://example.com/snap.png");
    });

    it("skips transient browser snapshots", () => {
      const event: RunEvent = {
        type: "browser.snapshot",
        run_id: "r1",
        seq: 2,
        timestamp: "2023-01-01T00:00:00Z",
        payload: {
          uri: "http://example.com/live.png",
          transient: true,
        },
      };
      const items = collectLibraryItems(event);
      expect(items).toHaveLength(0);
    });

    it("collects tool outputs", () => {
      const event: RunEvent = {
        type: "tool.completed",
        run_id: "r1",
        seq: 1,
        timestamp: "2023-01-01T00:00:00Z",
        payload: {
          artifacts: [
            { artifact_id: "a1", type: "code", uri: "file.ts", content_type: "text/plain" },
          ],
        },
      };
      const items = collectLibraryItems(event);
      expect(items).toHaveLength(1);
      expect(items[0].type).toBe("code");
    });

    it("uses artifact filename as label when available", () => {
      const event: RunEvent = {
        type: "tool.completed",
        run_id: "r1",
        seq: 2,
        timestamp: "2023-01-01T00:00:00Z",
        payload: {
          artifacts: [
            {
              artifact_id: "a-docx",
              type: "artifact",
              uri: "http://localhost:8080/artifacts/RWA-summary.docx",
              content_type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
            },
          ],
        },
      };
      const items = collectLibraryItems(event);
      expect(items).toHaveLength(1);
      expect(items[0].label).toBe("RWA-summary.docx");
    });

    it("ignores other events", () => {
      const event: RunEvent = {
        type: "run.started",
        run_id: "r1",
        seq: 1,
        timestamp: "2023-01-01T00:00:00Z",
        payload: {},
      };
      const items = collectLibraryItems(event);
      expect(items).toHaveLength(0);
    });
  });

  describe("applyEvent", () => {
    it("handles message.added", () => {
      const prev = [];
      const event: RunEvent = {
        type: "message.added",
        run_id: "r1",
        seq: 1,
        payload: { role: "user", content: "hello", message_id: "m1" },
      };
      const next = applyEvent(prev, event);
      expect(next).toHaveLength(1);
      expect(next[0].content).toBe("hello");
    });

    it("ignores empty and system message.added payloads", () => {
      const prev = [];
      const emptyEvent: RunEvent = {
        type: "message.added",
        run_id: "r1",
        seq: 1,
        payload: { role: "assistant", content: "   ", message_id: "m-empty" },
      };
      const systemEvent: RunEvent = {
        type: "message.added",
        run_id: "r1",
        seq: 2,
        payload: { role: "system", content: "internal", message_id: "m-system" },
      };
      expect(applyEvent(prev, emptyEvent)).toHaveLength(0);
      expect(applyEvent(prev, systemEvent)).toHaveLength(0);
    });

    it("ignores protocol-style assistant chatter in message.added", () => {
      const prev = [];
      const protocolEvent: RunEvent = {
        type: "message.added",
        run_id: "r1",
        seq: 3,
        payload: { role: "assistant", content: "Tool result: {\"tool_calls\":[{\"tool_name\":\"browser.navigate\"}]}" },
      };
      expect(applyEvent(prev, protocolEvent)).toHaveLength(0);
    });

    it("handles model.token", () => {
      const prev = [];
      const event1: RunEvent = {
        type: "model.token",
        run_id: "r1",
        seq: 1,
        payload: { text: "He" },
      };
      const next1 = applyEvent(prev, event1);
      expect(next1).toHaveLength(1);
      expect(next1[0].streaming).toBe(true);
      expect(next1[0].content).toBe("He");

      const event2: RunEvent = {
        type: "model.token",
        run_id: "r1",
        seq: 2,
        payload: { text: "llo" },
      };
      const next2 = applyEvent(next1, event2);
      expect(next2).toHaveLength(1);
      expect(next2[0].content).toBe("Hello");
    });

    it("handles model.summary", () => {
      const prev = [{ id: "s1", role: "assistant", content: "He", seq: 1, streaming: true }];
      const event: RunEvent = {
        type: "model.summary",
        run_id: "r1",
        seq: 2,
        payload: { text: "Hello world" },
      };
      // Should finalize streaming without replacing text unless stream is empty.
      const next = applyEvent(prev as any, event);
      expect(next).toHaveLength(1);
      expect(next[0].content).toBe("He");
      expect(next[0].streaming).toBe(false);
    });

    it("handles run.failed", () => {
      const prev = [];
      const event: RunEvent = {
        type: "run.failed",
        run_id: "r1",
        seq: 1,
        payload: {},
      };
      const next = applyEvent(prev, event);
      expect(next).toHaveLength(1);
      expect(next[0].role).toBe("assistant");
      expect(next[0].content).toContain("Run failed");
    });

    it("handles run.failed with backend error detail", () => {
      const prev = [];
      const event: RunEvent = {
        type: "run.failed",
        run_id: "r1",
        seq: 2,
        payload: { error: "opencode request failed: 502 Bad Gateway" },
      };
      const next = applyEvent(prev, event);
      expect(next).toHaveLength(1);
      expect(next[0].role).toBe("assistant");
      expect(next[0].content).toContain("Run failed:");
      expect(next[0].content).toContain("502 Bad Gateway");
    });

    it("keeps non-conversation operational events out of chat", () => {
      const prev = [{ id: "m1", role: "assistant", content: "hello", seq: 1 }];
      const phaseChanged: RunEvent = {
        type: "run.phase.changed",
        run_id: "r1",
        seq: 2,
        payload: { phase: "executing" },
      };
      const planned: RunEvent = {
        type: "step.planned",
        run_id: "r1",
        seq: 3,
        payload: { name: "Collect sources" },
      };
      const partial: RunEvent = {
        type: "run.partial",
        run_id: "r1",
        seq: 4,
        payload: { completion_reason: "llm_transient_error" },
      };
      const workspaceChanged: RunEvent = {
        type: "workspace.changed",
        run_id: "r1",
        seq: 5,
        payload: { path: "index.ts" },
      };
      expect(applyEvent(prev as any, phaseChanged)).toEqual(prev);
      expect(applyEvent(prev as any, planned)).toEqual(prev);
      expect(applyEvent(prev as any, partial)).toEqual(prev);
      expect(applyEvent(prev as any, workspaceChanged)).toEqual(prev);
    });
  });
});
