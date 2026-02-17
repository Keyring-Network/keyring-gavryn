import { useState, useMemo } from "react";
import { 
  Search, FileText, Image as ImageIcon, Link as LinkIcon, 
  Code2, File, Download, ExternalLink, Calendar,
  MoreHorizontal, Eye, Copy
} from "lucide-react";
import { type LibraryItem } from "@/App";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface ArtifactsPanelProps {
  artifacts: LibraryItem[];
  onPreview: (url: string) => void;
  onOpenFile: (path: string) => void;
}

const CATEGORIES = [
  { id: "all", label: "All" },
  { id: "code", label: "Code" },
  { id: "images", label: "Images" },
  { id: "docs", label: "Docs" },
  { id: "links", label: "Links" },
];

export function ArtifactsPanel({ artifacts, onPreview, onOpenFile }: ArtifactsPanelProps) {
  const [search, setSearch] = useState("");
  const [activeCategory, setActiveCategory] = useState("all");

  const getArtifactType = (item: LibraryItem) => {
    const type = (item.contentType || "").toLowerCase();
    const ext = item.label.split('.').pop()?.toLowerCase() || "";

    if (item.type === "link" || (!type && item.uri.startsWith("http"))) return "link";
    if (type.startsWith("image/") || ["png", "jpg", "jpeg", "gif", "svg", "webp"].includes(ext)) return "image";
    if (type.includes("javascript") || type.includes("typescript") || type.includes("json") || 
        type.includes("html") || type.includes("css") || type.includes("xml") || type.includes("yaml") ||
        ["js", "ts", "tsx", "jsx", "json", "html", "css", "py", "go", "java", "c", "cpp", "h", "sh", "sql"].includes(ext)) return "code";
    if (type.includes("pdf") || type.includes("document") || type.includes("text/plain") || 
        ["md", "txt", "pdf", "doc", "docx"].includes(ext)) return "doc";
    return "other";
  };

  const filteredArtifacts = useMemo(() => {
    return artifacts.filter(item => {
      // Search Filter
      if (search && !item.label.toLowerCase().includes(search.toLowerCase())) {
        return false;
      }

      // Category Filter
      if (activeCategory === "all") return true;
      
      const type = getArtifactType(item);
      if (activeCategory === "code") return type === "code";
      if (activeCategory === "images") return type === "image";
      if (activeCategory === "docs") return type === "doc";
      if (activeCategory === "links") return type === "link";
      
      return false;
    });
  }, [artifacts, search, activeCategory]);

  const getIcon = (item: LibraryItem) => {
    const type = getArtifactType(item);
    switch (type) {
      case "code": return <Code2 className="h-5 w-5 text-blue-400" />;
      case "image": return <ImageIcon className="h-5 w-5 text-purple-400" />;
      case "doc": return <FileText className="h-5 w-5 text-slate-400" />;
      case "link": return <LinkIcon className="h-5 w-5 text-emerald-400" />;
      default: return <File className="h-5 w-5 text-muted-foreground" />;
    }
  };

  const formatDate = (dateString: string) => {
    try {
      const date = new Date(dateString);
      return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(date);
    } catch (e) {
      return "";
    }
  };

  return (
    <div className="absolute inset-0 flex flex-col bg-background">
      {/* Search & Categories */}
      <div className="p-3 border-b border-border/40 space-y-3">
        <div className="relative">
          <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" />
          <input 
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter artifacts..."
            className="w-full bg-muted/30 border border-border/40 rounded-lg pl-8 pr-3 py-1.5 text-xs outline-none focus:bg-muted/50 transition-colors"
          />
        </div>
        <div className="flex gap-1 overflow-x-auto pb-1 scrollbar-none">
          {CATEGORIES.map(cat => (
            <button
              key={cat.id}
              onClick={() => setActiveCategory(cat.id)}
              className={cn(
                "px-2.5 py-1 text-[10px] font-medium rounded-full border transition-colors whitespace-nowrap",
                activeCategory === cat.id
                  ? "bg-primary/10 border-primary/20 text-primary"
                  : "bg-transparent border-border/40 text-muted-foreground hover:bg-muted/50"
              )}
            >
              {cat.label}
            </button>
          ))}
        </div>
      </div>

      {/* Artifacts List */}
      <div className="flex-1 overflow-auto p-3 space-y-2">
        {filteredArtifacts.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-muted-foreground opacity-60">
            <div className="p-3 rounded-full bg-muted/30 mb-2">
              <FileText className="h-6 w-6" />
            </div>
            <span className="text-xs">No artifacts found</span>
          </div>
        ) : (
          filteredArtifacts.map((item) => (
            <div 
              key={item.id || item.uri} 
              className="group flex flex-col gap-2 rounded-xl border border-border/40 bg-card p-3 shadow-sm hover:shadow-md hover:border-border/60 transition-all"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-start gap-3 min-w-0">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-accent/5 mt-0.5">
                    {getIcon(item)}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-xs font-medium text-foreground leading-snug" title={item.label}>
                      {item.label}
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      <span className="text-[10px] text-muted-foreground bg-muted/30 px-1.5 py-0.5 rounded">
                        {item.contentType || "Unknown"}
                      </span>
                      {item.createdAt && (
                         <span className="text-[10px] text-muted-foreground flex items-center gap-0.5">
                           <Calendar className="h-2.5 w-2.5" />
                           {formatDate(item.createdAt)}
                         </span>
                      )}
                    </div>
                  </div>
                </div>
                
                {/* Actions */}
                <div className="flex flex-col gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                   <Button variant="ghost" size="sm" className="h-6 w-6 p-0" title="Download" onClick={() => window.open(item.uri, "_blank")}>
                     <Download className="h-3.5 w-3.5" />
                   </Button>
                </div>
              </div>
              
              {/* Context/Source info if available */}
              {item.taskTitle && (
                 <div className="text-[10px] text-muted-foreground border-t border-border/20 pt-2 mt-1 truncate">
                    From: {item.taskTitle}
                 </div>
              )}

              {/* Inline Actions based on type */}
              <div className="grid grid-cols-2 gap-2 mt-1">
                {getArtifactType(item) === "link" && (
                   <Button 
                     variant="outline" 
                     size="sm" 
                     className="h-7 text-[10px] gap-1.5 w-full"
                     onClick={() => onPreview(item.uri)}
                   >
                     <Eye className="h-3 w-3" />
                     Preview
                   </Button>
                )}
                 {getArtifactType(item) === "code" && (
                   <Button 
                     variant="outline" 
                     size="sm" 
                     className="h-7 text-[10px] gap-1.5 w-full"
                     onClick={() => {
                        // If it's a workspace file path or relative, we can try to open it
                        // Assuming label or uri might contain path
                        const path = item.uri.startsWith("file://") ? item.uri.replace("file://", "") : item.label;
                        onOpenFile(path);
                     }}
                   >
                     <Code2 className="h-3 w-3" />
                     Open in Editor
                   </Button>
                )}
                 <Button 
                   variant="ghost" 
                   size="sm" 
                   className="h-7 text-[10px] gap-1.5 w-full bg-muted/20 hover:bg-muted/40"
                   onClick={() => window.open(item.uri, "_blank")}
                 >
                   <ExternalLink className="h-3 w-3" />
                   Open External
                 </Button>
              </div>
              
              {/* Inline Image Preview */}
              {getArtifactType(item) === "image" && (
                 <div className="relative mt-1 rounded-lg overflow-hidden border border-border/20 bg-black/5 h-24 group-hover:h-32 transition-all">
                    <img src={item.uri} alt={item.label} className="w-full h-full object-cover" />
                 </div>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
}
