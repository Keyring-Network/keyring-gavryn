export type RunEvent = {
  run_id: string;
  seq: number;
  type: string;
  ts?: string;
  timestamp?: string;
  source: string;
  trace_id?: string;
  payload?: Record<string, any>;
};

export function normalizeEventType(type: string) {
  const value = (type || "").trim().toLowerCase();
  return value;
}

export function normalizeEvent(event: RunEvent): RunEvent {
  return {
    ...event,
    type: normalizeEventType(event.type),
    ts: event.ts || event.timestamp || "",
  };
}

export function dedupeEvents(existing: RunEvent[], incoming: RunEvent[]) {
  const bySeq = new Map<number, RunEvent>();
  for (const event of existing) {
    bySeq.set(event.seq, event);
  }
  for (const event of incoming) {
    if (!bySeq.has(event.seq)) {
      bySeq.set(event.seq, event);
    }
  }
  return Array.from(bySeq.values()).sort((a, b) => a.seq - b.seq);
}

export function derivePanels(events: RunEvent[]) {
  const showBrowser = events.some((event) => normalizeEventType(event.type).startsWith("browser."));
  const showEditor = events.some((event) => {
    if (normalizeEventType(event.type) !== "tool.completed") return false;
    const toolName = event.payload?.tool_name;
    return typeof toolName === "string" && toolName.includes("editor");
  });

  let latestBrowserUri: string | undefined;
  for (let i = events.length - 1; i >= 0; i -= 1) {
    const event = events[i];
    if (normalizeEventType(event.type) === "browser.snapshot") {
      const uri = event.payload?.uri;
      if (typeof uri === "string") {
        latestBrowserUri = uri;
        break;
      }
    }
  }

  return { showBrowser, showEditor, latestBrowserUri };
}
