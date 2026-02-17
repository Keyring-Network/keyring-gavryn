import { useEffect, useMemo, useState } from "react";
import { Clock, Globe, MousePointer, Type, RotateCw, ExternalLink } from "lucide-react";
import { formatTime } from "@/App";
import { normalizeEventType, type RunEvent } from "@/lib/events";
import { Button } from "@/components/ui/button";

interface BrowserPanelProps {
  events: RunEvent[];
  latestSnapshotUri?: string;
}

function snapshotIdentity(uri: string) {
  const raw = String(uri || "").trim();
  if (!raw) return "";
  try {
    const parsed = new URL(raw);
    return `${parsed.origin}${parsed.pathname}`.toLowerCase();
  } catch {
    return raw.split("?")[0].split("#")[0].toLowerCase();
  }
}

export function BrowserPanel({ events, latestSnapshotUri }: BrowserPanelProps) {
  const normalizedEvents = useMemo(
    () => events.map((event) => ({ ...event, type: normalizeEventType(event.type) })),
    [events],
  );
  const browserEvents = useMemo(
    () =>
      normalizedEvents
        .filter((event) => {
          if (event.payload?.transient) return false;
          if (event.type.startsWith("browser.")) return true;
          if (event.type !== "tool.completed" && event.type !== "tool.started") return false;
          const toolName = event.payload?.tool_name;
          return typeof toolName === "string" && toolName.startsWith("browser.");
        })
        .sort((a, b) => b.seq - a.seq),
    [normalizedEvents],
  );

  const lastNav = browserEvents.find((e) => {
    if (e.type === "browser.navigation" || e.type === "browser.navigate") return true;
    const toolName = e.payload?.tool_name;
    return e.type === "tool.completed" && toolName === "browser.navigate";
  });
  const currentUrl =
    lastNav?.payload?.url ||
    (typeof lastNav?.payload?.output?.url === "string" ? lastNav.payload.output.url : "") ||
    "about:blank";
  const interactionSeqs = normalizedEvents
    .filter((e) => e.type === "browser.click" || e.type === "browser.type")
    .map((e) => e.seq);
  const lastInteractionSeq = interactionSeqs.length > 0 ? Math.max(...interactionSeqs) : -1;
  const isLoading =
    lastInteractionSeq > -1 &&
    !normalizedEvents.some((e) => e.type === "browser.snapshot" && e.seq > lastInteractionSeq);

  const snapshots = useMemo(() => {
    const fromBrowserEvents = normalizedEvents
      .filter((event) => event.type === "browser.snapshot" && typeof event.payload?.uri === "string" && !event.payload?.transient)
      .map((event) => ({
        seq: event.seq,
        uri: String(event.payload?.uri),
        id: snapshotIdentity(String(event.payload?.uri)),
      }));
    const fromToolArtifacts = normalizedEvents
      .filter((event) => event.type === "tool.completed" && typeof event.payload?.tool_name === "string" && event.payload.tool_name.startsWith("browser."))
      .flatMap((event) => {
        const candidates = Array.isArray(event.payload?.artifacts)
          ? event.payload.artifacts
          : Array.isArray(event.payload?.output?.artifacts)
            ? event.payload.output.artifacts
            : [];
        return candidates
          .filter((artifact: any) => typeof artifact?.uri === "string" && artifact.uri.trim() !== "")
          .map((artifact: any, index: number) => ({
            seq: event.seq + index / 1000,
            uri: String(artifact.uri),
            id: snapshotIdentity(String(artifact.uri)),
          }));
      });
    const seen = new Set<string>();
    return [...fromBrowserEvents, ...fromToolArtifacts]
      .filter((snapshot) => {
        const key = snapshot.id || snapshot.uri;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      })
      .sort((left, right) => right.seq - left.seq);
  }, [normalizedEvents]);
  const [selectedSnapshotId, setSelectedSnapshotId] = useState<string>("");
  useEffect(() => {
    if (snapshots.length === 0) {
      setSelectedSnapshotId("");
      return;
    }
    const selectedStillExists = snapshots.some((snapshot) => snapshot.id === selectedSnapshotId);
    if (selectedStillExists) {
      return;
    }
    setSelectedSnapshotId(snapshots[0].id || snapshots[0].uri);
  }, [selectedSnapshotId, snapshots]);
  const activeSnapshot = snapshots.find((snapshot) => snapshot.id === selectedSnapshotId) || snapshots[0];
  const activeSnapshotUri = activeSnapshot?.uri || latestSnapshotUri;

  const getIcon = (type: string) => {
    if (type.includes("click")) return <MousePointer className="h-3 w-3" />;
    if (type.includes("type") || type.includes("input")) return <Type className="h-3 w-3" />;
    if (type.includes("nav")) return <Globe className="h-3 w-3" />;
    return <Clock className="h-3 w-3" />;
  };

  const formatEventType = (type: string, payload: any) => {
    if (type === "tool.completed" || type === "tool.started") {
      const toolName = typeof payload?.tool_name === "string" ? payload.tool_name : "";
      return toolName ? toolName.replace("browser.", "").replace(/\./g, " ") : "tool";
    }
    if (type === "browser.navigation") return "navigate";
    return type.replace("browser.", "").replace(/\./g, " ");
  };

  return (
    <div className="flex flex-col h-full min-h-0 bg-muted/10">
      {/* Browser Header / URL Bar */}
      <div className="flex items-center gap-2 border-b border-border/60 bg-card/60 px-3 py-2 flex-shrink-0">
        <div className="flex-1 flex items-center gap-2 rounded-lg border border-border/60 bg-background/50 px-3 py-1.5 min-w-0">
          {isLoading ? (
            <RotateCw className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          ) : (
            <Globe className="h-3.5 w-3.5 text-muted-foreground" />
          )}
          <span className="flex-1 truncate text-xs font-mono text-foreground/80 selection:bg-accent/20">
            {currentUrl}
          </span>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 p-0"
          onClick={() => window.open(currentUrl, "_blank")}
          disabled={currentUrl === "about:blank"}
          title="Open in new tab"
        >
          <ExternalLink className="h-4 w-4 text-muted-foreground" />
        </Button>
      </div>

      {/* Main Viewport */}
      <div className="flex-1 min-h-0 overflow-auto bg-muted/5 p-4 flex flex-col items-center justify-center">
        {activeSnapshotUri ? (
          <div className="relative overflow-hidden rounded-xl border border-border/60 bg-card shadow-sm max-w-full">
            <img
              src={activeSnapshotUri}
              alt="Browser snapshot"
              className="h-auto w-full max-h-[60vh] object-contain"
            />
          </div>
        ) : (
          <div className="flex flex-col items-center gap-3 text-muted-foreground p-8 rounded-2xl border border-dashed border-border/60 bg-muted/20">
            <Globe className="h-10 w-10 opacity-20" />
            <div className="text-sm font-medium">No active view</div>
            <p className="text-xs text-center max-w-[200px] opacity-70">
              Browser will appear here when the agent navigates to a page.
            </p>
          </div>
        )}
      </div>

      {snapshots.length > 1 && (
        <div className="border-t border-border/60 bg-card/70 px-3 py-2">
          <div className="mb-2 text-[10px] uppercase tracking-wider font-semibold text-muted-foreground">
            Snapshots ({snapshots.length})
          </div>
          <div className="flex gap-2 overflow-x-auto pb-1">
            {snapshots.map((snapshot) => {
              const targetId = snapshot.id || snapshot.uri;
              const isActive = targetId === selectedSnapshotId || (!selectedSnapshotId && snapshot === activeSnapshot);
              return (
                <button
                  key={targetId}
                  type="button"
                  onClick={() => setSelectedSnapshotId(targetId)}
                  className={`h-14 w-20 shrink-0 overflow-hidden rounded-md border ${
                    isActive ? "border-accent" : "border-border/60"
                  }`}
                  aria-pressed={isActive}
                  title={`Snapshot #${snapshot.seq}`}
                >
                  <img src={snapshot.uri} alt={`Snapshot ${snapshot.seq}`} className="h-full w-full object-cover" />
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Activity Timeline (Collapsible or just small) */}
      <div className="flex-shrink-0 border-t border-border/60 bg-card">
        <div className="px-4 py-2 text-[10px] uppercase tracking-wider font-semibold text-muted-foreground border-b border-border/40">
          Activity Log
        </div>
        <div className="h-32 overflow-auto p-2 space-y-1">
          {browserEvents.length === 0 ? (
            <div className="p-4 text-center text-xs text-muted-foreground italic">
              No activity recorded
            </div>
          ) : (
            browserEvents.map((event) => (
              <div
                key={`${event.run_id}-${event.seq}`}
                className="flex items-start gap-3 rounded-lg px-2 py-1.5 text-xs transition hover:bg-muted/50 group"
              >
                <div className="mt-0.5 text-muted-foreground group-hover:text-foreground">
                  {getIcon(event.type)}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium text-foreground capitalize">
                      {formatEventType(event.type, event.payload)}
                    </span>
                    <span className="text-[10px] text-muted-foreground tabular-nums opacity-0 group-hover:opacity-100 transition-opacity">
                      {(event.ts || event.timestamp) ? formatTime(event.ts || event.timestamp || "") : ""}
                    </span>
                  </div>
                  {event.payload && (
                    <div className="text-muted-foreground truncate mt-0.5 font-mono text-[10px]">
                      {event.payload.selector && <span className="text-accent-foreground">{event.payload.selector} </span>}
                      {event.payload.text && <span>"{event.payload.text}" </span>}
                      {event.payload.url && <span>{event.payload.url}</span>}
                      {event.payload.error && <span className="text-rose-400">{event.payload.error}</span>}
                    </div>
                  )}
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
