import { useMemo, useState } from "react";
import {
  Code2,
  Download,
  ExternalLink,
  FileText,
  Image as ImageIcon,
  Link as LinkIcon,
  Search,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { LibraryItem } from "@/App";

type FileCategory = "all" | "docs" | "code" | "images" | "links" | "snapshots" | "other";

interface TaskFilesPanelProps {
  items: LibraryItem[];
}

const CATEGORIES: Array<{ id: FileCategory; label: string }> = [
  { id: "all", label: "All" },
  { id: "docs", label: "Docs" },
  { id: "code", label: "Code" },
  { id: "images", label: "Images" },
  { id: "links", label: "Links" },
  { id: "snapshots", label: "Snapshots" },
  { id: "other", label: "Other" },
];

function getExt(item: LibraryItem): string {
  const parts = item.label.split(".");
  return parts.length > 1 ? String(parts[parts.length - 1] || "").toLowerCase() : "";
}

function getCategory(item: LibraryItem): FileCategory {
  const contentType = String(item.contentType || "").toLowerCase();
  const type = String(item.type || "").toLowerCase();
  const ext = getExt(item);

  if (type === "browser.snapshot") return "snapshots";
  if (type === "link") return "links";
  if (contentType.startsWith("image/") || ["png", "jpg", "jpeg", "gif", "svg", "webp"].includes(ext)) return "images";
  if (
    contentType.includes("javascript") ||
    contentType.includes("typescript") ||
    contentType.includes("json") ||
    contentType.includes("html") ||
    contentType.includes("css") ||
    ["js", "ts", "tsx", "jsx", "json", "html", "css", "py", "go", "java", "c", "cpp", "h", "sh", "sql"].includes(ext)
  ) {
    return "code";
  }
  if (
    contentType.includes("pdf") ||
    contentType.includes("word") ||
    contentType.includes("document") ||
    contentType.includes("text/") ||
    ["md", "txt", "pdf", "doc", "docx"].includes(ext)
  ) {
    return "docs";
  }
  if (String(item.uri || "").startsWith("http")) return "links";
  return "other";
}

function getIcon(item: LibraryItem) {
  const category = getCategory(item);
  if (category === "images") return <ImageIcon className="h-4 w-4 text-purple-400" />;
  if (category === "code") return <Code2 className="h-4 w-4 text-sky-400" />;
  if (category === "links") return <LinkIcon className="h-4 w-4 text-emerald-400" />;
  return <FileText className="h-4 w-4 text-slate-400" />;
}

function safeDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function TaskFilesPanel({ items }: TaskFilesPanelProps) {
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState<FileCategory>("all");

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return [...items]
      .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
      .filter((item) => {
        if (category !== "all" && getCategory(item) !== category) {
          return false;
        }
        if (!query) return true;
        const haystack = `${item.label} ${item.type} ${item.contentType || ""} ${item.uri}`.toLowerCase();
        return haystack.includes(query);
      });
  }, [items, search, category]);

  return (
    <section className="h-full min-h-0 rounded-2xl border border-border/60 bg-card">
      <div className="border-b border-border/50 p-4">
        <div className="flex items-center justify-between gap-2">
          <div>
            <h2 className="text-sm font-semibold text-foreground">Task files</h2>
            <p className="text-xs text-muted-foreground">Files/artifacts produced for this task run.</p>
          </div>
          <div className="text-xs text-muted-foreground">{items.length} total</div>
        </div>

        <div className="mt-3 space-y-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search files..."
              className="w-full rounded-lg border border-border/60 bg-background/70 py-1.5 pl-8 pr-3 text-xs outline-none"
            />
          </div>

          <div className="flex gap-1 overflow-x-auto pb-1">
            {CATEGORIES.map((entry) => (
              <button
                key={entry.id}
                type="button"
                onClick={() => setCategory(entry.id)}
                className={cn(
                  "whitespace-nowrap rounded-full border px-2.5 py-1 text-[10px] font-medium transition",
                  category === entry.id
                    ? "border-accent/40 bg-accent/10 text-accent-foreground"
                    : "border-border/60 bg-card/50 text-muted-foreground hover:border-border"
                )}
              >
                {entry.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="h-[calc(100%-134px)] overflow-auto p-3">
        {filtered.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border/60 bg-muted/30 p-4 text-sm text-muted-foreground">
            No matching files for this task.
          </div>
        ) : (
          <div className="space-y-2">
            {filtered.map((item) => (
              <div key={item.id || item.uri} className="rounded-xl border border-border/60 bg-card/50 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      {getIcon(item)}
                      <div className="truncate text-sm font-medium text-foreground" title={item.label}>
                        {item.label}
                      </div>
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
                      <span className="rounded bg-muted/40 px-1.5 py-0.5">{item.type}</span>
                      <span className="rounded bg-muted/40 px-1.5 py-0.5">{item.contentType || "unknown"}</span>
                      <span>{safeDate(item.createdAt)}</span>
                    </div>
                    <div className="mt-1 truncate text-[11px] text-muted-foreground" title={item.uri}>
                      {item.uri}
                    </div>
                  </div>

                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-[10px]"
                      onClick={() => window.open(item.uri, "_blank", "noopener,noreferrer")}
                    >
                      <ExternalLink className="mr-1 h-3 w-3" />
                      Open
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-[10px]"
                      onClick={() => window.open(item.uri, "_blank", "noopener,noreferrer")}
                    >
                      <Download className="mr-1 h-3 w-3" />
                      Download
                    </Button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
