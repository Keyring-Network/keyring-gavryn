import { Brain, ChevronDown, ChevronRight } from "lucide-react";
import type { ConversationItem } from "../types";

type ReasoningRowProps = {
  item: Extract<ConversationItem, { kind: "reasoning" }>;
  expanded: boolean;
  onToggle: (id: string) => void;
};

export function ReasoningRow({ item, expanded, onToggle }: ReasoningRowProps) {
  return (
    <div className="dw-tool-inline dw-reasoning-inline">
      <button type="button" className="dw-tool-bar-toggle" onClick={() => onToggle(item.id)} aria-expanded={expanded} />
      <div className="dw-tool-content">
        <button type="button" className="dw-tool-summary" onClick={() => onToggle(item.id)} aria-expanded={expanded}>
          <Brain className="dw-tool-icon completed" size={14} />
          <span className="dw-tool-value">{item.summary || "Reasoning"}</span>
          <span className="dw-tool-chevron">{expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</span>
        </button>
        {expanded ? <div className="dw-reasoning-content">{item.content}</div> : null}
      </div>
    </div>
  );
}

