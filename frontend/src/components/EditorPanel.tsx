import { useState, useEffect, useCallback, useRef } from "react";
import { 
  File, Folder, FolderOpen, RefreshCw, Save, Terminal, Play, 
  FileText, Download, Globe, ExternalLink, ChevronRight, ChevronDown,
  Code2, FileCode2, FileJson, FileType, Image as ImageIcon, LayoutTemplate,
  X, Split, Columns, PanelRight, PanelLeft, MoreHorizontal, Monitor, Layers, Wrench,
  ChevronRight as ChevronRightIcon
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { type LibraryItem } from "@/App";
import Editor from "@monaco-editor/react";
import { cn } from "@/lib/utils";
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle, useDefaultLayout } from "react-resizable-panels";
import { ArtifactsPanel } from "./ArtifactsPanel";
import {
  buildFixNowPrompt,
  buildPreviewStrategies,
  type PreviewAttemptFailure,
  type PreviewStrategy,
  type WorkspaceFileNode,
} from "./editorPreview";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "http://localhost:8080";

interface FileNode extends WorkspaceFileNode {}

interface EditorPanelProps {
  runId: string;
  artifacts: LibraryItem[];
  onFixNow?: (prompt: string) => void;
}

interface OpenFile {
  path: string;
  name: string;
  isModified: boolean;
  content?: string;
  language: string;
}

interface ManagedProcessLog {
  stream: "stdout" | "stderr";
  ts: string;
  text: string;
}

interface ManagedProcessOutput {
  process_id: string;
  status: string;
  preview_urls?: string[];
  logs?: ManagedProcessLog[];
}

interface ToolRunnerResponse {
  status?: string;
  output?: Record<string, unknown>;
  error?: string;
}

const getLanguage = (path: string) => {
  const lower = path.toLowerCase();
  if (lower.endsWith(".ts") || lower.endsWith(".tsx")) return "typescript";
  if (lower.endsWith(".js") || lower.endsWith(".jsx") || lower.endsWith(".cjs") || lower.endsWith(".mjs")) return "javascript";
  if (lower.endsWith(".json")) return "json";
  if (lower.endsWith(".html")) return "html";
  if (lower.endsWith(".css")) return "css";
  if (lower.endsWith(".md") || lower.endsWith(".mdx")) return "markdown";
  if (lower.endsWith(".go")) return "go";
  if (lower.endsWith(".py")) return "python";
  if (lower.endsWith(".sh") || lower.endsWith(".bash")) return "shell";
  if (lower.endsWith(".yml") || lower.endsWith(".yaml")) return "yaml";
  if (lower.endsWith(".sql")) return "sql";
  if (lower.endsWith(".dockerfile") || lower.includes("dockerfile")) return "dockerfile";
  return "plaintext";
};

const getFileIcon = (name: string, className?: string) => {
  const lower = name.toLowerCase();
  const cn = className || "h-3.5 w-3.5";
  if (lower.endsWith(".ts") || lower.endsWith(".tsx")) return <FileCode2 className={`${cn} text-blue-400`} />;
  if (lower.endsWith(".js") || lower.endsWith(".jsx")) return <FileCode2 className={`${cn} text-yellow-400`} />;
  if (lower.endsWith(".json")) return <FileJson className={`${cn} text-amber-400`} />;
  if (lower.endsWith(".css") || lower.endsWith(".scss")) return <FileType className={`${cn} text-sky-400`} />;
  if (lower.endsWith(".html")) return <Code2 className={`${cn} text-orange-400`} />;
  if (lower.endsWith(".md")) return <FileText className={`${cn} text-slate-400`} />;
  if (lower.endsWith(".png") || lower.endsWith(".jpg") || lower.endsWith(".svg")) return <ImageIcon className={`${cn} text-purple-400`} />;
  if (lower.includes("config") || lower.startsWith(".")) return <LayoutTemplate className={`${cn} text-slate-500`} />;
  return <File className={`${cn} text-muted-foreground`} />;
};

export function EditorPanel({ runId, artifacts, onFixNow }: EditorPanelProps) {
  // --- Workspace State ---
  const [files, setFiles] = useState<FileNode[]>([]);
  const [loadingFiles, setLoadingFiles] = useState(false);
  const [fileError, setFileError] = useState<string | null>(null);
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set());
  
  // --- Editor State ---
  const [openFiles, setOpenFiles] = useState<OpenFile[]>([]);
  const [activeFile, setActiveFile] = useState<string | null>(null);
  const [activeFileContent, setActiveFileContent] = useState<string>("");
  const [fileContentCache, setFileContentCache] = useState<Record<string, string>>({});
  const [isSaving, setIsSaving] = useState(false);
  
  // --- Terminal/Exec State ---
  const [command, setCommand] = useState("");
  const [executing, setExecuting] = useState(false);
  const [execOutput, setExecOutput] = useState<string | null>(null);
  const [activeProcessId, setActiveProcessId] = useState<string | null>(null);
  const [activeProcessStatus, setActiveProcessStatus] = useState<string | null>(null);
  const [previewFailures, setPreviewFailures] = useState<PreviewAttemptFailure[]>([]);
  
  // --- Layout/Utility State ---
  const [showRightPanel, setShowRightPanel] = useState(false);
  const [rightPanelTab, setRightPanelTab] = useState<"preview" | "artifacts">("preview");
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [iframeKey, setIframeKey] = useState(0);

  // Layout Persistence
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "ide-main-layout",
    // We must provide panelIds matching the Panel id props below for reliable restoration
    panelIds: ["explorer", "editor", "utility"]
  });

  // Auto-expand root folders on load
  useEffect(() => {
    if (files.length > 0 && expandedFolders.size === 0) {
      const topLevel = new Set<string>();
      files.forEach(f => {
        if (f.type === "directory") topLevel.add(f.path);
      });
      setExpandedFolders(topLevel);
    }
  }, [files]);

  // Detect Preview URL
  useEffect(() => {
    if (!execOutput) return;
    const match = execOutput.match(/http:\/\/localhost:[0-9]+/);
    if (match) {
      const url = match[0];
      if (url !== previewUrl) {
        setPreviewUrl(url);
        setRightPanelTab("preview");
        setShowRightPanel(true);
      }
    }
  }, [execOutput, previewUrl]);

  useEffect(() => {
    if (!runId || !activeProcessId) return;
    let cancelled = false;
    const poll = async () => {
      try {
        const statusRes = await fetch(`${API_BASE_URL}/runs/${runId}/processes/${activeProcessId}`);
        if (!statusRes.ok) {
          if (!cancelled) {
            setActiveProcessStatus("failed");
          }
          return;
        }
        const statusData = await statusRes.json();
        const processOutput = (statusData.output || {}) as ManagedProcessOutput;
        if (!cancelled) {
          setActiveProcessStatus(processOutput.status || null);
          const urls = processOutput.preview_urls || [];
          if (urls.length > 0 && urls[0] && urls[0] !== previewUrl) {
            setPreviewUrl(urls[0]);
            setShowRightPanel(true);
            setRightPanelTab("preview");
          }
        }

        const logsRes = await fetch(`${API_BASE_URL}/runs/${runId}/processes/${activeProcessId}/logs?tail=200`);
        if (logsRes.ok) {
          const logsData = await logsRes.json();
          const logsOutput = (logsData.output || {}) as ManagedProcessOutput;
          if (Array.isArray(logsOutput.logs)) {
            const combined = logsOutput.logs.map((entry) => entry.text).join("");
            if (!cancelled) {
              setExecOutput(combined || "No output");
            }
          }
        }

        if (!cancelled && processOutput.status === "running") {
          window.setTimeout(poll, 1500);
        }
      } catch {
        if (!cancelled) {
          setActiveProcessStatus("failed");
        }
      }
    };
    poll();
    return () => {
      cancelled = true;
    };
  }, [runId, activeProcessId, previewUrl]);

  useEffect(() => {
    if (activeProcessStatus !== "failed") return;
    setPreviewFailures((prev) => {
      if (prev.length > 0) return prev;
      return [
        {
          strategy: { command: "process.start", args: [], cwd: ".", label: "Current preview process" },
          error: "Preview process exited with status failed.",
          logs: execOutput || undefined,
        },
      ];
    });
  }, [activeProcessStatus, execOutput]);

  const loadFiles = useCallback(async () => {
    if (!runId) return;
    setLoadingFiles(true);
    setFileError(null);
    try {
      const res = await fetch(`${API_BASE_URL}/runs/${runId}/workspace/tree`);
      if (!res.ok) {
        if (res.status === 404) {
             setFiles([]); 
             return; 
        }
        throw new Error("Failed to load workspace files");
      }
      const data = await res.json();
      setFiles(data.files || []);
    } catch (err) {
      console.error(err);
      setFileError("Could not load workspace files.");
    } finally {
      setLoadingFiles(false);
    }
  }, [runId]);

  useEffect(() => {
    loadFiles();
  }, [loadFiles]);

  const toggleFolder = (path: string) => {
    setExpandedFolders((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  const openFile = async (path: string, name: string) => {
    // Check if already open
    if (!openFiles.find(f => f.path === path)) {
      const newFile: OpenFile = {
        path,
        name,
        isModified: false,
        language: getLanguage(path)
      };
      setOpenFiles(prev => [...prev, newFile]);
    }
    
    setActiveFile(path);
    
    // Check cache first
    if (fileContentCache[path] !== undefined) {
      setActiveFileContent(fileContentCache[path]);
      return;
    }

    // Load content
    setActiveFileContent("// Loading...");
    try {
      const res = await fetch(`${API_BASE_URL}/runs/${runId}/workspace/file?path=${encodeURIComponent(path)}`);
      if (!res.ok) throw new Error("Failed to load file content");
      const data = (await res.json()) as ToolRunnerResponse;
      const content = typeof data.output?.content === "string" ? data.output.content : "";
      setFileContentCache(prev => ({ ...prev, [path]: content }));
      setActiveFileContent(content);
    } catch (err) {
      setActiveFileContent("// Error loading file content.");
    }
  };

  const closeFile = (e: React.MouseEvent, path: string) => {
    e.stopPropagation();
    const newOpenFiles = openFiles.filter(f => f.path !== path);
    setOpenFiles(newOpenFiles);
    
    // If we closed the active file, switch to the last one
    if (activeFile === path) {
      if (newOpenFiles.length > 0) {
        const last = newOpenFiles[newOpenFiles.length - 1];
        setActiveFile(last.path);
        setActiveFileContent(fileContentCache[last.path] || "");
      } else {
        setActiveFile(null);
        setActiveFileContent("");
      }
    }
    
    // Cleanup cache if needed? Nah, keep it for now.
  };

  const handleEditorChange = (value: string | undefined) => {
    if (value === undefined || !activeFile) return;
    setActiveFileContent(value);
    setFileContentCache(prev => ({ ...prev, [activeFile]: value }));
    // Mark as modified logic could go here
  };

  const saveCurrentFile = async () => {
    if (!activeFile) return;
    setIsSaving(true);
    try {
      const res = await fetch(`${API_BASE_URL}/runs/${runId}/workspace/file`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: activeFile, content: activeFileContent }),
      });
      if (!res.ok) throw new Error("Failed to save file");
      // Could show toast success here
    } catch (err) {
      console.error(err);
      alert("Failed to save file");
    } finally {
      setIsSaving(false);
    }
  };

  const runCommand = async (cmdOverride?: string) => {
    const cmdToRun = cmdOverride || command;
    if (!cmdToRun.trim()) return;
    setCommand(cmdToRun);
    setExecuting(true);
    setExecOutput(null);
    setActiveProcessId(null);
    setActiveProcessStatus(null);
    setPreviewUrl(null); // Reset preview on new run? Maybe not if it's the same port.
    try {
      const res = await fetch(`${API_BASE_URL}/runs/${runId}/processes/exec`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ command: cmdToRun }),
      });
      const data = await res.json();
      const output = (data?.output || {}) as Record<string, unknown>;
      const stdout = typeof output.stdout === "string" ? output.stdout : "";
      const stderr = typeof output.stderr === "string" ? output.stderr : "";
      const exitCode = typeof output.exit_code === "number" ? output.exit_code : null;
      const combined = `${stdout}${stderr}`.trim();
      if (combined) {
        setExecOutput(combined);
      } else if (exitCode !== null) {
        setExecOutput(`Command finished with exit code ${exitCode}`);
      } else {
        setExecOutput(data.error || "Command executed");
      }
      loadFiles();
    } catch (err) {
      setExecOutput("Failed to execute command");
    } finally {
      setExecuting(false);
    }
  };

  const parseResponsePayload = async (response: Response): Promise<any> => {
    const text = await response.text();
    if (!text) return {};
    try {
      return JSON.parse(text);
    } catch {
      return { error: text };
    }
  };

  const stopProcess = async (processId: string) => {
    try {
      await fetch(`${API_BASE_URL}/runs/${runId}/processes/${processId}/stop`, {
        method: "POST",
      });
    } catch {
      // no-op
    }
  };

  const pollPreviewStartup = async (processId: string, timeoutMs: number) => {
    const startedAt = Date.now();
    let logs = "";
    while (Date.now() - startedAt < timeoutMs) {
      const statusRes = await fetch(`${API_BASE_URL}/runs/${runId}/processes/${processId}`);
      const statusData = await parseResponsePayload(statusRes);
      const processOutput = (statusData.output || {}) as ManagedProcessOutput;
      const status = processOutput.status || "unknown";

      const logsRes = await fetch(`${API_BASE_URL}/runs/${runId}/processes/${processId}/logs?tail=200`);
      const logsData = await parseResponsePayload(logsRes);
      const logsOutput = (logsData.output || {}) as ManagedProcessOutput;
      if (Array.isArray(logsOutput.logs)) {
        logs = logsOutput.logs.map((entry) => entry.text).join("");
      }

      const previewUrls = processOutput.preview_urls || logsOutput.preview_urls || [];
      if (Array.isArray(previewUrls) && previewUrls.length > 0 && previewUrls[0]) {
        return { ok: true as const, status, previewUrl: String(previewUrls[0]), logs };
      }
      if (status !== "running" && status !== "starting") {
        return {
          ok: false as const,
          status,
          error: logs || `Process exited with status ${status}`,
          logs,
        };
      }
      await new Promise((resolve) => window.setTimeout(resolve, 1200));
    }
    return { ok: true as const, status: "running", previewUrl: "", logs };
  };

  const startPreviewProcess = async (strategy: PreviewStrategy) => {
    const response = await fetch(`${API_BASE_URL}/runs/${runId}/processes/start`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ command: strategy.command, args: strategy.args, cwd: strategy.cwd }),
    });
    const payload = await parseResponsePayload(response);
    if (!response.ok) {
      return {
        ok: false as const,
        error: typeof payload?.error === "string" ? payload.error : `Failed to start ${strategy.label}`,
      };
    }
    const output = (payload.output || {}) as ManagedProcessOutput;
    if (!output.process_id) {
      return { ok: false as const, error: "process.start returned no process_id" };
    }
    return { ok: true as const, processId: output.process_id };
  };

  const handleRunAndPreview = () => {
    void (async () => {
      setExecuting(true);
      setExecOutput(null);
      setPreviewUrl(null);
      setActiveProcessId(null);
      setActiveProcessStatus(null);
      setPreviewFailures([]);
      try {
        const strategies = buildPreviewStrategies(files);
        if (strategies.length === 0) {
          setExecOutput("No preview strategy could be inferred from workspace files.");
          return;
        }
        const failures: PreviewAttemptFailure[] = [];

        for (const strategy of strategies) {
          const started = await startPreviewProcess(strategy);
          if (!started.ok) {
            failures.push({ strategy, error: started.error });
            continue;
          }
          const probe = await pollPreviewStartup(started.processId, 12000);
          if (!probe.ok) {
            await stopProcess(started.processId);
            failures.push({
              strategy,
              error: probe.error,
              logs: probe.logs,
            });
            continue;
          }

          setActiveProcessId(started.processId);
          setActiveProcessStatus(probe.status);
          if (probe.previewUrl) {
            setPreviewUrl(probe.previewUrl);
            setShowRightPanel(true);
            setRightPanelTab("preview");
          }
          const header = `Started preview via ${strategy.label}`;
          const previewLine = probe.previewUrl ? `Preview URL: ${probe.previewUrl}` : "Waiting for preview URL...";
          const logSection = probe.logs ? `\n\nRecent logs:\n${probe.logs}` : "";
          setExecOutput(`${header}\n${previewLine}${logSection}`);
          setPreviewFailures([]);
          return;
        }

        setPreviewFailures(failures);
        const attemptSummary = failures
          .map((failure, index) => `${index + 1}. ${failure.strategy.label}: ${failure.error}`)
          .join("\n");
        setExecOutput(`Preview startup failed after ${failures.length} attempt(s).\n\n${attemptSummary}`);
      } catch {
        setExecOutput("Failed to start dev server");
      } finally {
        setExecuting(false);
      }
    })();
  };

  const stopRunAndPreview = async () => {
    if (!activeProcessId) return;
    try {
      await fetch(`${API_BASE_URL}/runs/${runId}/processes/${activeProcessId}/stop`, {
        method: "POST",
      });
    } catch {
      // no-op
    } finally {
      setActiveProcessStatus("stopped");
      setActiveProcessId(null);
    }
  };

  const handleFixNow = () => {
    if (!onFixNow || previewFailures.length === 0) return;
    const prompt = buildFixNowPrompt({
      runId,
      attempts: previewFailures,
      terminalOutput: execOutput || "",
      activeFile,
    });
    onFixNow(prompt);
  };

  const renderFileTree = (nodes: FileNode[], depth = 0) => {
    return nodes.map((node) => {
      const isDir = node.type === "directory";
      const isExpanded = expandedFolders.has(node.path);
      const isSelected = activeFile === node.path;
      
      return (
        <div key={node.path}>
          <button
            onClick={() => (isDir ? toggleFolder(node.path) : openFile(node.path, node.name))}
            className={cn(
              "flex w-full items-center gap-1.5 px-2 py-1 text-xs transition-colors select-none",
              isSelected
                ? "bg-accent/50 text-accent-foreground font-medium"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
            )}
            style={{ paddingLeft: `${depth * 12 + 8}px` }}
          >
            <span className="shrink-0 text-muted-foreground/50 w-3.5 flex justify-center">
              {isDir ? (
                isExpanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRightIcon className="h-3 w-3" />
              ) : null}
            </span>
            
            {isDir ? (
              isExpanded ? (
                <FolderOpen className="h-3.5 w-3.5 text-amber-400/80 shrink-0" />
              ) : (
                <Folder className="h-3.5 w-3.5 text-amber-400/80 shrink-0" />
              )
            ) : (
              getFileIcon(node.name)
            )}
            <span className="truncate">{node.name}</span>
          </button>
          {isDir && isExpanded && node.children && (
            <div>{renderFileTree(node.children, depth + 1)}</div>
          )}
        </div>
      );
    });
  };

  const activeFileObj = openFiles.find(f => f.path === activeFile);

  return (
    <div className="flex flex-col h-full min-h-0 bg-background/50 overflow-hidden">
      {/* Top Bar: simplified, maybe just global actions if needed, or rely on internal panes */}
      
      <PanelGroup 
        orientation="horizontal" 
        className="flex-1 min-h-0" 
        defaultLayout={defaultLayout} 
        onLayoutChanged={onLayoutChanged}
      >
        {/* Left Pane: Explorer */}
        <Panel 
          id="explorer"
          defaultSize={20} 
          minSize={15} 
          collapsible 
          className="flex flex-col min-w-0 bg-card/10 border-r border-border/40"
        >
           <div className="flex items-center justify-between p-2 border-b border-border/20">
             <span className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Explorer</span>
             <Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={loadFiles} disabled={loadingFiles}>
               <RefreshCw className={cn("h-3 w-3", loadingFiles && "animate-spin")} />
             </Button>
           </div>
           <div className="flex-1 overflow-y-auto overflow-x-auto py-2 custom-scrollbar">
             {files.length === 0 && !loadingFiles ? (
               <div className="px-4 py-8 text-center text-xs text-muted-foreground">
                 No files found
               </div>
             ) : (
               renderFileTree(files)
             )}
           </div>
        </Panel>

        <PanelResizeHandle className="w-1.5 bg-border/40 hover:bg-accent/80 transition-colors cursor-col-resize active:bg-accent" />

        {/* Center Pane: Editor */}
        <Panel id="editor" defaultSize={60} minSize={30} className="flex flex-col min-w-0 bg-[#1e1e1e]">
          {/* Editor Tabs */}
          {openFiles.length > 0 ? (
            <div className="flex bg-[#252526] overflow-x-auto custom-scrollbar border-b border-black/20">
              {openFiles.map(file => (
                <div 
                  key={file.path}
                  className={cn(
                    "group flex items-center gap-2 px-3 py-2 text-xs cursor-pointer border-r border-white/5 min-w-[120px] max-w-[200px]",
                    activeFile === file.path 
                      ? "bg-[#1e1e1e] text-zinc-100 border-t-2 border-t-emerald-500" 
                      : "bg-[#2d2d2d] text-zinc-400 hover:bg-[#2a2a2b]"
                  )}
                  onClick={() => {
                     setActiveFile(file.path);
                     if (fileContentCache[file.path]) setActiveFileContent(fileContentCache[file.path]);
                  }}
                >
                  {getFileIcon(file.name)}
                  <span className="truncate flex-1">{file.name}</span>
                  <button 
                    onClick={(e) => closeFile(e, file.path)}
                    className="opacity-0 group-hover:opacity-100 hover:bg-white/10 rounded p-0.5 transition-opacity"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <div className="h-9 bg-[#252526] border-b border-black/20" /> 
          )}

          {/* Breadcrumbs / Toolbar */}
          {activeFile ? (
            <div className="flex items-center justify-between px-4 py-1.5 bg-[#1e1e1e] text-zinc-500 text-xs border-b border-white/5">
              <div className="flex items-center gap-1.5 overflow-hidden">
                {activeFile.split('/').map((part, i, arr) => (
                  <div key={i} className="flex items-center gap-1">
                    <span className={cn(i === arr.length - 1 ? "text-zinc-300" : "")}>{part}</span>
                    {i < arr.length - 1 && <ChevronRight className="h-3 w-3 opacity-50" />}
                  </div>
                ))}
              </div>
              <Button 
                variant="ghost" 
                size="sm" 
                className="h-6 gap-1.5 text-[10px] text-zinc-400 hover:text-zinc-100 hover:bg-white/5" 
                onClick={saveCurrentFile} 
                disabled={isSaving}
              >
                <Save className="h-3 w-3" />
                {isSaving ? "Saving..." : "Save"}
              </Button>
            </div>
          ) : null}

          {/* Editor Content */}
          <div className="flex-1 relative min-h-0">
             {activeFile ? (
               <Editor
                 height="100%"
                 language={activeFileObj?.language || "plaintext"}
                 value={activeFileContent}
                 onChange={handleEditorChange}
                 theme="vs-dark"
                 options={{
                   minimap: { enabled: false },
                   fontSize: 13,
                   wordWrap: "on",
                   scrollBeyondLastLine: false,
                   automaticLayout: true,
                   padding: { top: 16 },
                   fontFamily: "'JetBrains Mono', 'Fira Code', Consolas, monospace",
                   renderLineHighlight: "all",
                   smoothScrolling: true,
                 }}
               />
             ) : (
               <div className="flex-1 flex flex-col items-center justify-center text-zinc-600 gap-3 h-full">
                  <div className="p-4 rounded-full bg-white/5">
                    <Code2 className="h-8 w-8 opacity-40" />
                  </div>
                  <div className="text-sm font-medium">Select a file to start editing</div>
                  <div className="flex gap-2 text-xs">
                     <span className="px-2 py-1 rounded bg-white/5">⌘ P</span>
                     <span>to search files</span>
                  </div>
               </div>
             )}
          </div>
          
          {/* Bottom Terminal (Fixed height for now, could be resizable in V2) */}
          <div className="h-[140px] border-t border-border/20 bg-[#1e1e1e] flex flex-col">
            <div className="flex items-center justify-between px-2 py-1 bg-[#252526] border-b border-black/20">
              <div className="flex items-center gap-2 text-xs text-zinc-400">
                <Terminal className="h-3 w-3" />
                <span>Terminal</span>
              </div>
              <div className="flex items-center gap-2">
                 <button 
                   onClick={() => setShowRightPanel(!showRightPanel)}
                   className={cn("text-xs flex items-center gap-1 px-2 py-0.5 rounded transition-colors", showRightPanel ? "bg-accent/20 text-accent" : "text-muted-foreground hover:bg-white/5")}
                   title="Toggle Right Panel"
                 >
                   <PanelRight className="h-3 w-3" />
                   {showRightPanel ? "Hide Panel" : "Show Panel"}
                 </button>
              </div>
            </div>
            <div className="flex-1 flex flex-col min-h-0">
               <div className="flex-1 overflow-auto p-2 text-[11px] font-mono text-zinc-300 custom-scrollbar">
                 {execOutput ? (
                   <pre className="whitespace-pre-wrap">{execOutput}</pre>
                 ) : (
                   <span className="text-zinc-600 italic">No output</span>
                 )}
               </div>
               <div className="flex items-center gap-2 p-2 border-t border-white/5 bg-[#1e1e1e]">
                 <Terminal className="h-3.5 w-3.5 text-zinc-500 shrink-0" />
                 <input
                   value={command}
                   onChange={(e) => setCommand(e.target.value)}
                   onKeyDown={(e) => e.key === "Enter" && runCommand()}
                   placeholder="Run command..."
                   className="flex-1 bg-transparent text-xs text-zinc-300 outline-none placeholder:text-zinc-600 font-mono"
                 />
                 <Button
                    variant="ghost"
                    size="sm"
                    onClick={handleRunAndPreview}
                    disabled={executing}
                    className="h-6 gap-1.5 text-[10px] text-emerald-400 hover:text-emerald-300 hover:bg-emerald-400/10 px-2"
                  >
                    <Play className="h-3 w-3" />
                    Run & Preview
                  </Button>
                  {previewFailures.length > 0 && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={handleFixNow}
                      disabled={!onFixNow}
                      className="h-6 gap-1.5 text-[10px] text-amber-300 hover:text-amber-200 hover:bg-amber-400/10 px-2"
                    >
                      <Wrench className="h-3 w-3" />
                      Fix now
                    </Button>
                  )}
                  {activeProcessId && activeProcessStatus === "running" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={stopRunAndPreview}
                      className="h-6 gap-1.5 text-[10px] text-rose-400 hover:text-rose-300 hover:bg-rose-400/10 px-2"
                    >
                      <X className="h-3 w-3" />
                      Stop
                    </Button>
                  )}
               </div>
            </div>
          </div>
        </Panel>

        {showRightPanel && (
          <>
            <PanelResizeHandle className="w-1.5 bg-border/40 hover:bg-accent/80 transition-colors cursor-col-resize active:bg-accent" />
            <Panel id="utility" defaultSize={30} minSize={20} className="flex flex-col min-w-0 bg-background/50 border-l border-border/40">
               {/* Right Panel Tabs */}
               <div className="flex items-center border-b border-border/40 bg-card/20">
                 <button 
                   onClick={() => setRightPanelTab("preview")}
                   className={cn(
                     "flex-1 px-3 py-2 text-xs font-medium border-b-2 transition-colors flex items-center justify-center gap-2",
                     rightPanelTab === "preview" 
                       ? "border-emerald-500 text-emerald-500 bg-emerald-500/5" 
                       : "border-transparent text-muted-foreground hover:bg-accent/5"
                   )}
                 >
                   <Globe className="h-3.5 w-3.5" />
                   Preview
                 </button>
                 <button 
                   onClick={() => setRightPanelTab("artifacts")}
                   className={cn(
                     "flex-1 px-3 py-2 text-xs font-medium border-b-2 transition-colors flex items-center justify-center gap-2",
                     rightPanelTab === "artifacts" 
                       ? "border-amber-500 text-amber-500 bg-amber-500/5" 
                       : "border-transparent text-muted-foreground hover:bg-accent/5"
                   )}
                 >
                   <Layers className="h-3.5 w-3.5" />
                   Artifacts
                 </button>
               </div>
               
               <div className="flex-1 relative min-h-0 bg-background">
                 {rightPanelTab === "preview" && (
                   <div className="absolute inset-0 flex flex-col">
                      {!previewUrl ? (
                         <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-3 p-4 text-center">
                            <Globe className="h-10 w-10 opacity-20" />
                            <div className="text-sm font-medium">No server running</div>
                            <div className="text-xs opacity-70">
                                Run `npm run dev` to start the development server.
                            </div>
                         </div>
                      ) : (
                         <div className="flex flex-col h-full">
                           <div className="flex items-center justify-between px-3 py-1.5 border-b border-border/20 bg-muted/20">
                             <span className="text-[10px] font-mono text-muted-foreground truncate max-w-[200px]">{previewUrl}</span>
                             <div className="flex gap-1">
                               <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => setIframeKey(k => k + 1)}>
                                 <RefreshCw className="h-3 w-3" />
                               </Button>
                               <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => window.open(previewUrl, "_blank")}>
                                 <ExternalLink className="h-3 w-3" />
                               </Button>
                             </div>
                           </div>
                           <iframe 
                             key={iframeKey}
                             src={previewUrl}
                             className="flex-1 w-full border-none bg-white"
                             title="Preview"
                           />
                         </div>
                      )}
                   </div>
                 )}
                 
                 {rightPanelTab === "artifacts" && (
                   <ArtifactsPanel 
                     artifacts={artifacts} 
                     onPreview={(url) => {
                        setPreviewUrl(url);
                        setRightPanelTab("preview");
                     }}
                     onOpenFile={(path) => {
                        // Attempt to deduce a name from the path if simple
                        const name = path.split('/').pop() || path;
                        openFile(path, name);
                     }}
                   />
                 )}
               </div>
            </Panel>
          </>
        )}
      </PanelGroup>
    </div>
  );
}
