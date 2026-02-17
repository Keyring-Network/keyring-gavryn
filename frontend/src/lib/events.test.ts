import { describe, it, expect } from "vitest";
import { dedupeEvents, derivePanels, type RunEvent } from "./events";

describe("dedupeEvents", () => {
  it("dedupes by seq and preserves ordering", () => {
    const base: RunEvent[] = [
      { run_id: "r1", seq: 1, type: "run.started", timestamp: "t", source: "cp" },
      { run_id: "r1", seq: 2, type: "message.added", timestamp: "t", source: "cp" },
    ];
    const incoming: RunEvent[] = [
      { run_id: "r1", seq: 2, type: "message.added", timestamp: "t", source: "cp" },
      { run_id: "r1", seq: 3, type: "browser.snapshot", timestamp: "t", source: "worker" },
    ];

    const result = dedupeEvents(base, incoming);
    expect(result.map((event) => event.seq)).toEqual([1, 2, 3]);
  });
});

describe("derivePanels", () => {
  it("shows browser panel and captures latest uri", () => {
    const events: RunEvent[] = [
      {
        run_id: "r1",
        seq: 1,
        type: "browser.snapshot",
        timestamp: "t",
        source: "worker",
        payload: { uri: "http://localhost:8082/artifacts/r1/shot.png" },
      },
    ];
    const panels = derivePanels(events);
    expect(panels.showBrowser).toBe(true);
    expect(panels.latestBrowserUri).toContain("shot.png");
  });

  it("prefers latest live snapshot for browser uri", () => {
    const events: RunEvent[] = [
      {
        run_id: "r1",
        seq: 1,
        type: "browser.snapshot",
        timestamp: "t",
        source: "worker",
        payload: { uri: "http://localhost:8082/artifacts/r1/static.png" },
      },
      {
        run_id: "r1",
        seq: 2,
        type: "browser.snapshot",
        timestamp: "t",
        source: "worker",
        payload: { uri: "http://localhost:8082/artifacts/r1/live.png?ts=123", transient: true },
      },
    ];
    const panels = derivePanels(events);
    expect(panels.latestBrowserUri).toContain("live.png");
  });

  it("shows editor panel when editor tool outputs", () => {
    const events: RunEvent[] = [
      {
        run_id: "r1",
        seq: 1,
        type: "tool.completed",
        timestamp: "t",
        source: "worker",
        payload: { tool_name: "editor.apply_patch" },
      },
    ];
    const panels = derivePanels(events);
    expect(panels.showEditor).toBe(true);
  });
});
