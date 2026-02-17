import type { ChatMessage } from "@/App";
import { normalizeEventType, type RunEvent } from "@/lib/events";
import type { ConversationItem, FeedBuildInput, MessageListEntry, ToolGroupItem } from "./types";

function safeStringify(value: unknown): string {
  const seen = new WeakSet<object>();
  return JSON.stringify(
    value,
    (_key, current) => {
      if (typeof current === "object" && current !== null) {
        if (seen.has(current)) {
          return "[Circular]";
        }
        seen.add(current);
      }
      return current;
    },
    2,
  );
}

function stringify(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null || value === undefined) return "";
  try {
    return safeStringify(value);
  } catch {
    return String(value);
  }
}

function toDisplayText(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null || value === undefined) return "";
  try {
    const text = safeStringify(value);
    return text === "{}" ? "" : text;
  } catch {
    return String(value);
  }
}

function stripAnsi(value: string): string {
  return value.replace(/\u001b\[[0-9;]*m/g, "");
}

function normalizeToolErrorText(value: string): string {
  const stripped = stripAnsi(value).replace(/\s+/g, " ").trim();
  const marker = "call log:";
  const markerIndex = stripped.toLowerCase().indexOf(marker);
  if (markerIndex === -1) return stripped;
  return stripped.slice(0, markerIndex).trim();
}

function parseToolFailure(payload: Record<string, any>) {
  const reasonCode = stringFromValue(payload.reason_code || payload.reasonCode);
  const rawError = payload.error;
  if (typeof rawError === "string") {
    const trimmed = rawError.trim();
    if (trimmed.startsWith("{") && trimmed.endsWith("}")) {
      try {
        const parsed = JSON.parse(trimmed) as Record<string, unknown>;
        const nestedReason = stringFromValue(parsed.reason_code || parsed.reasonCode);
        const nestedMessage = stripAnsi(
          stringFromValue(parsed.error || parsed.message || parsed.detail || rawError),
        );
        return {
          message: normalizeToolErrorText(nestedMessage || trimmed),
          reasonCode: reasonCode || nestedReason,
        };
      } catch {
        // Use plain text fallback below.
      }
    }
    return {
      message: normalizeToolErrorText(trimmed),
      reasonCode,
    };
  }

  return {
    message: normalizeToolErrorText(stringify(rawError || payload)),
    reasonCode,
  };
}

function stringFromValue(value: unknown): string {
  if (typeof value === "string") {
    return value.trim();
  }
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (Array.isArray(value)) {
    return value.map((entry) => stringFromValue(entry)).filter(Boolean).join(" ").trim();
  }
  if (typeof value === "object") {
    const obj = value as Record<string, unknown>;
    for (const key of ["url", "path", "command", "selector", "title", "query", "mode"]) {
      const candidate = stringFromValue(obj[key]);
      if (candidate) return candidate;
    }
    try {
      return safeStringify(obj);
    } catch {
      return "";
    }
  }
  return "";
}

function toToolSummary(toolName: string, input: Record<string, any>, output: Record<string, any>) {
  const normalized = (toolName || "").toLowerCase();
  if (normalized === "editor.write") {
    const path = stringFromValue(input.path || output.path || "file");
    return { title: `Edited ${path}`, detail: path };
  }
  if (normalized === "editor.read") {
    const path = stringFromValue(input.path || output.path || "file");
    return { title: `Read ${path}`, detail: path };
  }
  if (normalized === "editor.list") {
    const path = stringFromValue(input.path || ".");
    return { title: `Listed ${path}`, detail: path };
  }
  if (normalized === "process.exec") {
    const command = stringFromValue(output.command || input.command || "");
    const args = Array.isArray(output.args) ? output.args : Array.isArray(input.args) ? input.args : [];
    const text = [command, ...args.map((arg) => stringFromValue(arg))].filter(Boolean).join(" ").trim();
    return { title: `Run command ${text || "(unknown)"}`, detail: text };
  }
  if (normalized === "process.start") {
    const command = stringFromValue(output.command || input.command || "");
    const args = Array.isArray(output.args) ? output.args : Array.isArray(input.args) ? input.args : [];
    const text = [command, ...args.map((arg) => stringFromValue(arg))].filter(Boolean).join(" ").trim();
    return { title: `Started process ${text || "(unknown)"}`, detail: text };
  }
  if (normalized === "browser.navigate") {
    const url = stringFromValue(output.url || input.url || "");
    return { title: `Visited ${url || "page"}`, detail: url };
  }
  if (normalized === "browser.extract") {
    const mode = stringFromValue(output.mode || input.mode || "text");
    const url = stringFromValue(output.url || input.url || "");
    return { title: `Extracted ${mode}`, detail: url || mode };
  }
  if (normalized === "browser.snapshot") {
    return { title: "Captured screenshot", detail: stringFromValue(output.uri || "") };
  }
  const fallbackDetail = stringFromValue(output) || stringFromValue(input);
  return { title: toolName || "Tool call", detail: fallbackDetail };
}

function toExploreEntry(toolName: string, input: Record<string, any>) {
  const normalized = (toolName || "").toLowerCase();
  if (normalized === "editor.read") {
    const path = String(input.path || "").trim();
    if (!path) return null;
    return { kind: "read" as const, label: path };
  }
  if (normalized === "editor.list") {
    const path = String(input.path || ".").trim();
    return { kind: "list" as const, label: path || "." };
  }
  if (normalized === "process.exec") {
    const command = String(input.command || "").trim();
    const args = Array.isArray(input.args) ? input.args.map((entry) => String(entry)) : [];
    const searchText = [command, ...args].filter(Boolean).join(" ").toLowerCase();
    if (searchText.includes("rg ") || searchText.startsWith("rg") || searchText.includes("grep")) {
      return { kind: "search" as const, label: [command, ...args].filter(Boolean).join(" ") };
    }
    if (searchText.startsWith("ls") || searchText.includes(" tree")) {
      return { kind: "list" as const, label: [command, ...args].filter(Boolean).join(" ") };
    }
  }
  return null;
}

function parseArtifactArray(value: unknown) {
  if (!Array.isArray(value)) return [];
  return value
    .filter((artifact) => typeof artifact?.uri === "string" && artifact.uri.trim() !== "")
    .map((artifact) => ({
      uri: String(artifact.uri),
      type: typeof artifact.type === "string" ? artifact.type : undefined,
      contentType:
        typeof artifact.content_type === "string"
          ? artifact.content_type
          : typeof artifact.contentType === "string"
            ? artifact.contentType
            : undefined,
    }));
}

function parseToolArtifacts(payload: Record<string, any> | undefined) {
  if (!payload) return [];
  const bucket = [
    ...parseArtifactArray(payload.artifacts),
    ...parseArtifactArray(payload.output?.artifacts),
    ...parseArtifactArray(payload.result?.artifacts),
  ];
  const seen = new Set<string>();
  return bucket.filter((artifact) => {
    if (seen.has(artifact.uri)) return false;
    seen.add(artifact.uri);
    return true;
  });
}

function extractInvocationId(payload: Record<string, any>) {
  const keys = ["tool_invocation_id", "invocation_id", "idempotency_key", "tool_call_id"];
  for (const key of keys) {
    const value = stringFromValue(payload[key]);
    if (value) return value;
  }
  return "";
}

function buildToolItemFromLifecycleEvent(event: RunEvent): {
  item: Extract<ConversationItem, { kind: "tool" | "explore" }>;
  statusType: "started" | "completed" | "failed";
  toolName: string;
} | null {
  const type = normalizeEventType(event.type);
  if (type !== "tool.started" && type !== "tool.completed" && type !== "tool.failed") {
    return null;
  }

  const payload = event.payload || {};
  const toolName = typeof payload.tool_name === "string" ? payload.tool_name : "";
  const input = payload.input && typeof payload.input === "object" ? (payload.input as Record<string, any>) : {};
  const output = payload.output && typeof payload.output === "object" ? (payload.output as Record<string, any>) : {};
  const invocationId = extractInvocationId(payload);

  if (type === "tool.started") {
    const exploreEntry = toExploreEntry(toolName, input);
    if (exploreEntry) {
      return {
        statusType: "started",
        toolName,
        item: {
          id: invocationId ? `explore-${invocationId}` : `explore-${event.seq}`,
          seq: event.seq,
          kind: "explore",
          status: "exploring",
          invocationId,
          entries: [exploreEntry],
        },
      };
    }
    const summary = toToolSummary(toolName, input, output);
    return {
      statusType: "started",
      toolName,
      item: {
        id: invocationId ? `tool-${invocationId}` : `tool-${event.seq}`,
        seq: event.seq,
        kind: "tool",
        toolType: toolName || "tool",
        title: summary.title,
        detail: summary.detail,
        status: "running",
        invocationId,
      },
    };
  }

  if (type === "tool.completed") {
    const exploreEntry = toExploreEntry(toolName, input);
    if (exploreEntry) {
      return {
        statusType: "completed",
        toolName,
        item: {
          id: invocationId ? `explore-${invocationId}` : `explore-${event.seq}`,
          seq: event.seq,
          kind: "explore",
          status: "explored",
          invocationId,
          entries: [exploreEntry],
        },
      };
    }
    const summary = toToolSummary(toolName, input, output);
    return {
      statusType: "completed",
      toolName,
      item: {
        id: invocationId ? `tool-${invocationId}` : `tool-${event.seq}`,
        seq: event.seq,
        kind: "tool",
        toolType: toolName || "tool",
        title: summary.title,
        detail: summary.detail,
        output: stringify(payload.output),
        status: "completed",
        invocationId,
        artifacts: parseToolArtifacts(payload),
      },
    };
  }

  const failure = parseToolFailure(payload);
  const statusLabel = failure.reasonCode ? failure.reasonCode.replace(/_/g, " ") : "";
  return {
    statusType: "failed",
    toolName,
    item: {
      id: invocationId ? `tool-${invocationId}` : `tool-${event.seq}`,
      seq: event.seq,
      kind: "tool",
      toolType: toolName || "tool",
      title: `Failed ${toolName || "tool call"}`,
      detail: statusLabel,
      output: failure.reasonCode ? `${failure.message} (${failure.reasonCode})` : failure.message,
      status: "failed",
      invocationId,
      artifacts: parseToolArtifacts(payload),
    },
  };
}

function mergeArtifacts(
  existing: Array<{ uri: string; type?: string; contentType?: string }> | undefined,
  incoming: Array<{ uri: string; type?: string; contentType?: string }> | undefined,
) {
  const bucket = [...(existing || []), ...(incoming || [])];
  const seen = new Set<string>();
  return bucket.filter((artifact) => {
    if (!artifact.uri || seen.has(artifact.uri)) return false;
    seen.add(artifact.uri);
    return true;
  });
}

function mergeToolLifecycle(
  existing: Extract<ConversationItem, { kind: "tool" | "explore" }>,
  incoming: Extract<ConversationItem, { kind: "tool" | "explore" }>,
): Extract<ConversationItem, { kind: "tool" | "explore" }> {
  if (existing.kind === "explore") {
    if (incoming.kind === "explore") {
      const entries = [...existing.entries, ...incoming.entries];
      const seen = new Set<string>();
      const deduped = entries.filter((entry) => {
        const key = `${entry.kind}:${entry.label}:${entry.detail || ""}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      });
      return {
        ...existing,
        entries: deduped,
        status: incoming.status === "explored" ? "explored" : existing.status,
      };
    }
    if (incoming.status === "failed") {
      return {
        ...incoming,
        id: existing.id,
        seq: existing.seq,
        invocationId: existing.invocationId || incoming.invocationId,
      };
    }
    return {
      ...existing,
      status: "explored",
    };
  }

  if (incoming.kind === "explore") {
    return {
      ...existing,
      status: incoming.status === "explored" ? "completed" : existing.status,
    };
  }

  return {
    ...existing,
    toolType: incoming.toolType || existing.toolType,
    title: incoming.title || existing.title,
    detail: incoming.detail || existing.detail,
    output: incoming.output || existing.output,
    status: incoming.status || existing.status,
    invocationId: existing.invocationId || incoming.invocationId,
    artifacts: mergeArtifacts(existing.artifacts, incoming.artifacts),
  };
}

function buildEventActivityItems(events: RunEvent[]): ConversationItem[] {
  const sortedEvents = [...events].sort((left, right) => left.seq - right.seq);
  const items: ConversationItem[] = [];
  const indexByInvocation = new Map<string, number>();
  const pendingByTool = new Map<string, number[]>();

  const queuePending = (toolName: string, index: number) => {
    const normalized = (toolName || "tool").toLowerCase();
    const queue = pendingByTool.get(normalized) || [];
    queue.push(index);
    pendingByTool.set(normalized, queue);
  };

  const removePending = (toolName: string, index: number) => {
    const normalized = (toolName || "tool").toLowerCase();
    const queue = pendingByTool.get(normalized);
    if (!queue || queue.length === 0) return;
    const next = queue.filter((value) => value !== index);
    if (next.length === 0) {
      pendingByTool.delete(normalized);
      return;
    }
    pendingByTool.set(normalized, next);
  };

  const shiftPending = (toolName: string) => {
    const normalized = (toolName || "tool").toLowerCase();
    const queue = pendingByTool.get(normalized);
    if (!queue || queue.length === 0) return undefined;
    const next = [...queue];
    const value = next.shift();
    if (next.length === 0) pendingByTool.delete(normalized);
    else pendingByTool.set(normalized, next);
    return value;
  };

  for (const event of sortedEvents) {
    const type = normalizeEventType(event.type);
    const payload = event.payload || {};

    if (type === "workspace.changed") {
      const path = String(payload.path || "file");
      const change = String(payload.change || "modified");
      items.push({
        id: `workspace-${event.seq}`,
        seq: event.seq,
        kind: "tool",
        toolType: "workspace.changed",
        title: `${change === "added" ? "Created" : change === "removed" ? "Removed" : "Edited"} ${path}`,
        detail: stringify(payload.summary),
        status: "completed",
      });
      continue;
    }

    const lifecycle = buildToolItemFromLifecycleEvent(event);
    if (!lifecycle) {
      continue;
    }

    const invocationId = lifecycle.item.invocationId || "";
    const invocationKey = invocationId ? `inv:${invocationId}` : "";
    let targetIndex: number | undefined;

    if (invocationKey && indexByInvocation.has(invocationKey)) {
      targetIndex = indexByInvocation.get(invocationKey);
    } else if (lifecycle.statusType !== "started") {
      targetIndex = shiftPending(lifecycle.toolName);
    }

    if (targetIndex === undefined || targetIndex < 0 || targetIndex >= items.length) {
      const index = items.length;
      items.push(lifecycle.item);
      if (invocationKey) {
        indexByInvocation.set(invocationKey, index);
      }
      if (lifecycle.statusType === "started") {
        queuePending(lifecycle.toolName, index);
      }
      continue;
    }

    const existing = items[targetIndex];
    if (existing.kind !== "tool" && existing.kind !== "explore") {
      const index = items.length;
      items.push(lifecycle.item);
      if (invocationKey) {
        indexByInvocation.set(invocationKey, index);
      }
      if (lifecycle.statusType === "started") {
        queuePending(lifecycle.toolName, index);
      }
      continue;
    }

    const merged = mergeToolLifecycle(existing, lifecycle.item);
    items[targetIndex] = merged;

    if (invocationKey) {
      indexByInvocation.set(invocationKey, targetIndex);
    }

    if (lifecycle.statusType === "started") {
      queuePending(lifecycle.toolName, targetIndex);
    } else {
      removePending(lifecycle.toolName, targetIndex);
    }
  }

  return items;
}

function looksLikeToolJson(content: string) {
  const trimmed = content.trim();
  return (
    (trimmed.startsWith("{") && trimmed.includes("\"tool_use\"")) ||
    (trimmed.startsWith("```json") && trimmed.includes("tool_use")) ||
    (trimmed.startsWith("```tool") && trimmed.includes("tool_calls"))
  );
}

function looksLikeProtocolChatter(content: string) {
  const normalized = content.trim().toLowerCase();
  if (!normalized) return false;
  if (normalized.startsWith("tool result:")) return true;
  if (normalized.startsWith("```tool")) return true;
  if (normalized.startsWith("```json") && normalized.includes("tool_calls")) return true;
  if (normalized.includes('"tool_calls"') && normalized.includes('"tool_name"')) return true;
  if (normalized.startsWith("event:")) return true;
  if (normalized.startsWith("data:{") && normalized.includes('"seq"')) return true;
  return false;
}

function buildMessageItem(message: ChatMessage): ConversationItem | null {
  if (message.role === "system" || message.role === "tool" || message.type === "tool_output") {
    return null;
  }
  if (message.role === "assistant" && looksLikeToolJson(message.content)) {
    return null;
  }
  if (message.role !== "user" && message.role !== "assistant") {
    return null;
  }
  const text = toDisplayText(message.content || "").trim();
  if (!text || looksLikeProtocolChatter(text)) return null;
  return {
    id: message.id,
    seq: message.seq,
    kind: "message",
    role: message.role,
    text,
  };
}

function buildReasoningItems(events: RunEvent[]): ConversationItem[] {
  return events
    .filter((event) => normalizeEventType(event.type) === "model.summary")
    .map((event) => {
      const text = String(event.payload?.text || "").trim();
      if (!text) return null;
      const lines = text.split("\n");
      const summary = lines[0] || "Reasoning";
      const content = lines.slice(1).join("\n").trim() || text;
      return {
        id: `reasoning-${event.seq}`,
        seq: event.seq,
        kind: "reasoning",
        summary,
        content,
      } satisfies ConversationItem;
    })
    .filter((item): item is ConversationItem => Boolean(item));
}

function dedupeAndSort(items: ConversationItem[]) {
  const byId = new Map<string, ConversationItem>();
  for (const item of items) {
    byId.set(item.id, item);
  }
  return Array.from(byId.values()).sort((left, right) => left.seq - right.seq);
}

export function buildConversationItems(input: FeedBuildInput): ConversationItem[] {
  const eventToolItems = buildEventActivityItems(input.events as RunEvent[]);
  const reasoningItems = buildReasoningItems(input.events as RunEvent[]);

  const reasoningTextBySeq = new Map<number, string>();
  for (const item of reasoningItems) {
    if (item.kind === "reasoning") {
      reasoningTextBySeq.set(item.seq, item.content.trim());
    }
  }

  const messageItems = input.messages
    .map((message) => {
      const mapped = buildMessageItem(message);
      if (!mapped) return null;
      if (
        mapped.kind === "message" &&
        mapped.role === "assistant" &&
        reasoningTextBySeq.get(mapped.seq) &&
        reasoningTextBySeq.get(mapped.seq) === mapped.text.trim()
      ) {
        return null;
      }
      return mapped;
    })
    .filter((item): item is ConversationItem => Boolean(item));

  return dedupeAndSort([...messageItems, ...eventToolItems, ...reasoningItems]);
}

function isToolGroupItem(item: ConversationItem): item is ToolGroupItem {
  return item.kind !== "message";
}

function isImageArtifact(artifact: { uri: string; type?: string; contentType?: string }) {
  return artifact.contentType?.startsWith("image/") || artifact.type?.toLowerCase().includes("screenshot");
}

function countGroupMetrics(items: ToolGroupItem[]) {
  let toolCount = 0;
  let messageCount = 0;
  let readsAndSearchesCount = 0;
  let filesChangedCount = 0;
  let commandsCount = 0;
  let screenshotSingles = 0;
  const screenshotUris = new Set<string>();

  for (const item of items) {
    if (item.kind === "reasoning") {
      messageCount += 1;
      continue;
    }

    if (item.kind === "explore") {
      toolCount += Math.max(1, item.entries.length);
      readsAndSearchesCount += item.entries.filter((entry) => entry.kind === "read" || entry.kind === "search" || entry.kind === "list").length;
      continue;
    }

    if (item.kind !== "tool") {
      continue;
    }

    toolCount += 1;
    const normalizedTool = (item.toolType || "").toLowerCase();

    if (normalizedTool === "workspace.changed" || normalizedTool === "editor.write" || normalizedTool === "editor.delete") {
      filesChangedCount += 1;
    }

    if (normalizedTool.startsWith("process.")) {
      commandsCount += 1;
    }

    if (normalizedTool === "editor.read" || normalizedTool === "editor.list") {
      readsAndSearchesCount += 1;
    }

    if (item.artifacts?.length) {
      for (const artifact of item.artifacts) {
        if (artifact.uri && isImageArtifact(artifact)) {
          screenshotUris.add(artifact.uri);
        }
      }
    } else if (normalizedTool === "browser.snapshot") {
      screenshotSingles += 1;
    }
  }

  return {
    toolCount,
    messageCount,
    readsAndSearchesCount,
    filesChangedCount,
    commandsCount,
    screenshotsCount: screenshotSingles + screenshotUris.size,
  };
}

export function groupConversationItems(items: ConversationItem[]): MessageListEntry[] {
  const entries: MessageListEntry[] = [];
  let buffer: ToolGroupItem[] = [];

  const flush = () => {
    if (buffer.length === 0) return;
    const counts = countGroupMetrics(buffer);
    entries.push({
      kind: "toolGroup",
      group: {
        id: `activity-${buffer[0].id}`,
        items: [...buffer],
        toolCount: counts.toolCount,
        messageCount: counts.messageCount,
        readsAndSearchesCount: counts.readsAndSearchesCount,
        filesChangedCount: counts.filesChangedCount,
        commandsCount: counts.commandsCount,
        screenshotsCount: counts.screenshotsCount,
      },
    });
    buffer = [];
  };

  for (const item of items) {
    if (isToolGroupItem(item)) {
      buffer.push(item);
      continue;
    }
    flush();
    entries.push({ kind: "item", item });
  }

  flush();
  return entries;
}

export function latestReasoningLabel(items: ConversationItem[]): string | null {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.kind === "message") break;
    if (item.kind === "reasoning" && item.summary.trim()) {
      return item.summary.trim();
    }
  }
  return null;
}
