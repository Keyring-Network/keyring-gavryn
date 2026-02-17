import { memo, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { ChatMessage } from "@/App";
import type { RunEvent } from "@/lib/events";
import { buildConversationItems, groupConversationItems } from "../threadItems";
import type { ConversationItem } from "../types";
import { ToolRow } from "./ToolRow";
import { ReasoningRow } from "./ReasoningRow";
import { ExploreRow } from "./ExploreRow";
import "../messages.css";

type MessagesProps = {
  messages: ChatMessage[];
  events: RunEvent[];
  isThinking: boolean;
  bottomInset?: number;
};

function formatCount(value: number, singular: string, plural: string) {
  return `${value} ${value === 1 ? singular : plural}`;
}

function isNearBottom(container: HTMLDivElement | null) {
  if (!container) return true;
  const threshold = 120;
  return container.scrollHeight - container.scrollTop - container.clientHeight <= threshold;
}

function MessageBubble({ item }: { item: Extract<ConversationItem, { kind: "message" }> }) {
  return (
    <div className={`dw-message ${item.role}`}>
      <div className="dw-bubble">
        <div className="dw-bubble-text">{item.text}</div>
      </div>
    </div>
  );
}

export const Messages = memo(function Messages({ messages, events, isThinking, bottomInset }: MessagesProps) {
  const shellRef = useRef<HTMLDivElement | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
  const [hasUnseenActivity, setHasUnseenActivity] = useState(false);
  const initializedScrollRef = useRef(false);
  const nearBottomRef = useRef(true);
  const feedVersion = useRef("");

  const items = useMemo(() => buildConversationItems({ messages, events }), [messages, events]);
  const grouped = useMemo(() => groupConversationItems(items), [items]);
  const feedFingerprint = useMemo(() => {
    const lastSeq = items.length > 0 ? items[items.length - 1].seq : 0;
    return `${lastSeq}:${grouped.length}:${isThinking ? 1 : 0}`;
  }, [grouped.length, isThinking, items]);

  useEffect(() => {
    setExpandedGroups((prev) => {
      const validIds = new Set(
        grouped
          .filter((entry) => entry.kind === "toolGroup")
          .map((entry) => (entry.kind === "toolGroup" ? entry.group.id : "")),
      );
      const next = new Set<string>();
      for (const id of prev) {
        if (validIds.has(id)) {
          next.add(id);
        }
      }
      return next.size === prev.size ? prev : next;
    });
  }, [grouped]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    if (!initializedScrollRef.current) {
      container.scrollTop = container.scrollHeight;
      nearBottomRef.current = true;
      initializedScrollRef.current = true;
      feedVersion.current = feedFingerprint;
      setHasUnseenActivity(false);
      return;
    }

    if (feedVersion.current === feedFingerprint) {
      return;
    }
    feedVersion.current = feedFingerprint;

    if (nearBottomRef.current || isNearBottom(container)) {
      container.scrollTop = container.scrollHeight;
      nearBottomRef.current = true;
      setHasUnseenActivity(false);
      return;
    }

    setHasUnseenActivity(true);
  }, [feedFingerprint]);

  const toggleExpanded = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleGroup = (id: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const jumpToLatest = () => {
    const container = containerRef.current;
    if (!container) return;
    container.scrollTop = container.scrollHeight;
    nearBottomRef.current = true;
    setHasUnseenActivity(false);
  };

  const handleScroll = () => {
    const container = containerRef.current;
    nearBottomRef.current = isNearBottom(container);
    if (nearBottomRef.current) {
      setHasUnseenActivity(false);
    }
  };

  const renderItem = (item: ConversationItem) => {
    if (item.kind === "message") return <MessageBubble key={item.id} item={item} />;
    if (item.kind === "tool") return <ToolRow key={item.id} item={item} expanded={expanded.has(item.id)} onToggle={toggleExpanded} />;
    if (item.kind === "reasoning")
      return <ReasoningRow key={item.id} item={item} expanded={expanded.has(item.id)} onToggle={toggleExpanded} />;
    if (item.kind === "explore") return <ExploreRow key={item.id} item={item} />;
    if (item.kind === "diff") {
      return (
        <div key={item.id} className="dw-item-card">
          <div className="dw-item-title">{item.title}</div>
          <pre className="dw-tool-output">{item.diff}</pre>
        </div>
      );
    }
    if (item.kind === "review") {
      return (
        <div key={item.id} className="dw-item-card">
          <div className="dw-item-title">{item.state === "started" ? "Review started" : "Review completed"}</div>
          <div className="dw-bubble-text">{item.text}</div>
        </div>
      );
    }
    return null;
  };

  return (
    <div className="dw-messages-shell" ref={shellRef}>
      <div
        className="dw-messages"
        ref={containerRef}
        onScroll={handleScroll}
        style={
          {
            "--dw-messages-bottom-inset": `${Math.max(20, Math.round(bottomInset || 20))}px`,
          } as CSSProperties
        }
      >
        {grouped.length === 0 ? (
          <div className="dw-empty">No updates yet. Waiting on agent output.</div>
        ) : (
          grouped.map((entry) => {
            if (entry.kind === "item") return renderItem(entry.item);
            const isCollapsed = !expandedGroups.has(entry.group.id);
            const summary = [
              formatCount(entry.group.toolCount, "tool call", "tool calls"),
              formatCount(entry.group.readsAndSearchesCount, "read/search", "reads/searches"),
              formatCount(entry.group.filesChangedCount, "file changed", "files changed"),
              formatCount(entry.group.commandsCount, "command", "commands"),
              formatCount(entry.group.screenshotsCount, "screenshot", "screenshots"),
              entry.group.messageCount > 0
                ? formatCount(entry.group.messageCount, "reasoning block", "reasoning blocks")
                : "",
            ]
              .filter(Boolean)
              .join(", ");
            return (
              <div key={entry.group.id} className="dw-tool-group">
                <button type="button" className="dw-tool-group-toggle" onClick={() => toggleGroup(entry.group.id)}>
                  <span>{isCollapsed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}</span>
                  <span className="dw-tool-group-label">Agent activity</span>
                  <span className="dw-tool-group-summary">{summary}</span>
                </button>
                {!isCollapsed ? <div className="dw-tool-group-body">{entry.group.items.map(renderItem)}</div> : null}
              </div>
            );
          })
        )}
      </div>
      {hasUnseenActivity ? (
        <button type="button" className="dw-jump-latest" onClick={jumpToLatest}>
          Jump to latest
        </button>
      ) : null}
    </div>
  );
});
