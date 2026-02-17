import { CheckCircle2, ChevronDown, ChevronRight, CircleAlert, Loader2 } from "lucide-react";
import { useMemo } from "react";
import type { ConversationItem } from "../types";

type ToolRowProps = {
  item: Extract<ConversationItem, { kind: "tool" }>;
  expanded: boolean;
  onToggle: (id: string) => void;
};

function getTone(status?: string) {
  const normalized = (status || "").toLowerCase();
  if (normalized.includes("fail")) return "failed";
  if (normalized.includes("run") || normalized.includes("start")) return "processing";
  return "completed";
}

export function ToolRow({ item, expanded, onToggle }: ToolRowProps) {
  const tone = getTone(item.status);
  const imageArtifacts = useMemo(
    () =>
      (item.artifacts || []).filter(
        (artifact) =>
          artifact.contentType?.startsWith("image/") ||
          artifact.type?.toLowerCase().includes("screenshot"),
      ),
    [item.artifacts],
  );

  return (
    <div className="dw-tool-inline">
      <button type="button" className="dw-tool-bar-toggle" onClick={() => onToggle(item.id)} aria-expanded={expanded} />
      <div className="dw-tool-content">
        <button type="button" className="dw-tool-summary" onClick={() => onToggle(item.id)} aria-expanded={expanded}>
          {tone === "completed" ? <CheckCircle2 className="dw-tool-icon completed" size={14} /> : null}
          {tone === "processing" ? <Loader2 className="dw-tool-icon processing animate-spin" size={14} /> : null}
          {tone === "failed" ? <CircleAlert className="dw-tool-icon failed" size={14} /> : null}
          <span className="dw-tool-value">{item.title}</span>
          <span className="dw-tool-chevron">{expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</span>
        </button>
        {expanded && item.detail ? <div className="dw-tool-detail">{item.detail}</div> : null}
        {expanded && item.output ? <pre className="dw-tool-output">{item.output}</pre> : null}
        {expanded && imageArtifacts.length > 0 ? (
          <div className="dw-artifact-grid">
            {imageArtifacts.map((artifact) => (
              <a key={artifact.uri} href={artifact.uri} target="_blank" rel="noreferrer" className="dw-artifact-thumb">
                <img src={artifact.uri} alt={artifact.type || "Artifact"} loading="lazy" />
              </a>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

