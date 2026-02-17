import { Search, Terminal } from "lucide-react";
import type { ConversationItem } from "../types";

type ExploreRowProps = {
  item: Extract<ConversationItem, { kind: "explore" }>;
};

export function ExploreRow({ item }: ExploreRowProps) {
  return (
    <div className="dw-tool-inline dw-explore-inline">
      <div className="dw-tool-bar-toggle" aria-hidden />
      <div className="dw-tool-content">
        <div className="dw-explore-header">
          {item.status === "exploring" ? <Search className="dw-tool-icon processing" size={14} /> : <Terminal className="dw-tool-icon completed" size={14} />}
          <span className="dw-explore-title">{item.status === "exploring" ? "Exploring" : "Explored"}</span>
        </div>
        <div className="dw-explore-list">
          {item.entries.map((entry, index) => (
            <div key={`${entry.kind}-${entry.label}-${index}`} className="dw-explore-item">
              <span className="dw-explore-kind">{entry.kind}</span>
              <span className="dw-explore-label">{entry.label}</span>
              {entry.detail ? <span className="dw-explore-detail">{entry.detail}</span> : null}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

