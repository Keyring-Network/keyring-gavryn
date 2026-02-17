import { useEffect, useMemo, useRef, useState, type DragEvent, type KeyboardEvent } from "react";
import {
  AlertTriangle,
  ArrowUp,
  CalendarClock,
  Check,
  CircleDot,
  LayoutGrid,
  Library,
  Loader2,
  ListTodo,
  Monitor,
  PencilLine,
  Plus,
  Search,
  SlidersHorizontal,
  Sparkles,
  Square,
  Tag,
  Trash2,
  X,
  ChevronLeft,
  ChevronRight,
  FileCode,
  Globe,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { dedupeEvents, derivePanels, normalizeEvent, normalizeEventType, type RunEvent } from "@/lib/events";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "http://localhost:8080";

export type ChatMessage = {
  id: string;
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  seq: number;
  streaming?: boolean;
  type?: "text" | "file_change" | "tool_output";
  metadata?: any;
};

export type TaskStatus = "idle" | "running" | "completed" | "partial" | "cancelled" | "failed";

export type Task = {
  id: string;
  title: string;
  projectId: string;
  runId: string;
  status: TaskStatus;
  updatedAt: string;
  messageCount: number;
  tags: string[];
};

export type RunSummary = {
  id: string;
  status: string;
  title: string;
  created_at: string;
  updated_at: string;
  message_count: number;
};

export type Project = {
  id: string;
  name: string;
  tone: "slate" | "emerald" | "amber" | "sky" | "rose";
};

export type LibraryItem = {
  id: string;
  label: string;
  type: string;
  uri: string;
  contentType?: string;
  createdAt: string;
  runId: string;
  taskId?: string;
  taskTitle?: string;
};

export type Skill = {
  id: string;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
};

export type SkillFile = {
  path: string;
  content_base64: string;
  content_type: string;
  size_bytes: number;
  updated_at: string;
};

export type ContextNode = {
  id: string;
  parent_id?: string;
  name: string;
  node_type: "folder" | "file";
  content_type?: string;
  size_bytes?: number;
  created_at: string;
  updated_at: string;
};

type LLMSettingsState = {
  configured: boolean;
  mode: string;
  provider: string;
  model: string;
  base_url: string;
  codex_auth_path?: string;
  codex_home?: string;
  has_api_key: boolean;
  api_key_hint?: string;
};

type LLMSettingsForm = {
  mode: string;
  provider: string;
  model: string;
  base_url: string;
  api_key: string;
  codex_auth_path: string;
  codex_home: string;
};

type MemorySettingsState = {
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
};

type PersonalitySettingsState = {
  content: string;
  source?: string;
  created_at?: string;
  updated_at?: string;
};

type PanelType = "overview" | "browser" | "files" | "editor";

export const statusLabels: Record<TaskStatus, string> = {
  idle: "Idle",
  running: "Running",
  completed: "Completed",
  partial: "Partial",
  cancelled: "Cancelled",
  failed: "Failed",
};

export const statusBadge: Record<TaskStatus, string> = {
  idle: "border-border/60 text-muted-foreground",
  running: "border-emerald-400/40 text-emerald-200",
  completed: "border-emerald-400/40 text-emerald-200",
  partial: "border-amber-400/40 text-amber-200",
  cancelled: "border-amber-400/40 text-amber-200",
  failed: "border-rose-400/40 text-rose-200",
};

const defaultPersonality = `You are Gavryn, the local-first control plane assistant.

Behavior guidelines:
- Speak as Gavryn. Never identify as OpenCode or other system identities.
- Use the current date/time provided by the system prompt as authoritative.
- Use available tools when needed; if web-enabled tools are available, use them for current information instead of claiming you cannot browse.
- Be concise, precise, and action-oriented.
- For complex tasks, propose a short plan, then execute.
- Use Markdown with clear headings and bullet points.
- Ask clarifying questions when requirements are ambiguous.
- Respect project conventions and avoid unnecessary refactors.`;

const normalizeStatus = (value: string | undefined): TaskStatus => {
  if (
    value === "running" ||
    value === "completed" ||
    value === "partial" ||
    value === "cancelled" ||
    value === "failed" ||
    value === "idle"
  ) {
    return value;
  }
  return "idle";
};

function inferPreferredBrowserFromUserAgent(userAgent: string): string {
  const ua = String(userAgent || "").toLowerCase();
  if (!ua) return "";
  if (ua.includes("brave")) return "brave";
  if (ua.includes("edg/")) return "edge";
  if (ua.includes("opr/") || ua.includes("opera")) return "opera";
  if (ua.includes("vivaldi")) return "vivaldi";
  if (ua.includes("arc/") || ua.includes(" arc")) return "arc";
  if (ua.includes("chromium")) return "chromium";
  if (ua.includes("chrome/")) return "chrome";
  return "";
}

function TaskStatusGlyph({ status }: { status: TaskStatus }) {
  if (status === "running") {
    return <Loader2 className="h-3.5 w-3.5 text-orange-400 drop-shadow-[0_0_4px_rgba(251,146,60,0.45)] animate-spin" aria-label="Running" />;
  }
  if (status === "failed") {
    return <AlertTriangle className="h-3.5 w-3.5 text-rose-400" aria-label="Failed" />;
  }
  if (status === "completed") {
    return <Check className="h-3.5 w-3.5 text-emerald-400" aria-label="Completed" />;
  }
  if (status === "partial") {
    return <AlertTriangle className="h-3.5 w-3.5 text-amber-400" aria-label="Partial" />;
  }
  if (status === "cancelled") {
    return <Square className="h-3.5 w-3.5 text-amber-300" aria-label="Cancelled" />;
  }
  return <CircleDot className="h-3.5 w-3.5 text-muted-foreground" aria-label="Idle" />;
}

export const projectTones: Record<Project["tone"], { dot: string; badge: string }> = {
  slate: {
    dot: "bg-slate-300",
    badge: "border-slate-400/40 bg-slate-500/10 text-slate-200",
  },
  emerald: {
    dot: "bg-emerald-300",
    badge: "border-emerald-400/40 bg-emerald-500/10 text-emerald-200",
  },
  amber: {
    dot: "bg-amber-300",
    badge: "border-amber-400/40 bg-amber-500/10 text-amber-200",
  },
  sky: {
    dot: "bg-sky-300",
    badge: "border-sky-400/40 bg-sky-500/10 text-sky-200",
  },
  rose: {
    dot: "bg-rose-300",
    badge: "border-rose-400/40 bg-rose-500/10 text-rose-200",
  },
};

export const tagColors = [
  "bg-red-500/10 text-red-400 border-red-500/20",
  "bg-orange-500/10 text-orange-400 border-orange-500/20",
  "bg-amber-500/10 text-amber-400 border-amber-500/20",
  "bg-yellow-500/10 text-yellow-400 border-yellow-500/20",
  "bg-lime-500/10 text-lime-400 border-lime-500/20",
  "bg-green-500/10 text-green-400 border-green-500/20",
  "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  "bg-teal-500/10 text-teal-400 border-teal-500/20",
  "bg-cyan-500/10 text-cyan-400 border-cyan-500/20",
  "bg-sky-500/10 text-sky-400 border-sky-500/20",
  "bg-blue-500/10 text-blue-400 border-blue-500/20",
  "bg-indigo-500/10 text-indigo-400 border-indigo-500/20",
  "bg-violet-500/10 text-violet-400 border-violet-500/20",
  "bg-purple-500/10 text-purple-400 border-purple-500/20",
  "bg-fuchsia-500/10 text-fuchsia-400 border-fuchsia-500/20",
  "bg-pink-500/10 text-pink-400 border-pink-500/20",
  "bg-rose-500/10 text-rose-400 border-rose-500/20",
];

export const getTagColorIndex = (tag: string) => {
  let hash = 0;
  for (let i = 0; i < tag.length; i++) {
    hash = tag.charCodeAt(i) + ((hash << 5) - hash);
  }
  const index = Math.abs(hash) % tagColors.length;
  return index;
};

export const normalizeTagName = (value: string) => value.trim().toLowerCase();

export const normalizeSearch = (value: string) => value.trim().toLowerCase();

export const isSubsequence = (needle: string, haystack: string) => {
  let i = 0;
  let j = 0;
  while (i < needle.length && j < haystack.length) {
    if (needle[i] === haystack[j]) {
      i += 1;
    }
    j += 1;
  }
  return i === needle.length;
};

export const matchesQuery = (query: string, value: string) => {
  if (!query) return true;
  const target = normalizeSearch(value);
  if (!target) return false;
  return target.includes(query) || isSubsequence(query, target);
};

export const toneCycle: Project["tone"][] = ["amber", "emerald", "sky", "rose", "slate"];

export const quickActions = [
  "Plan project milestones",
  "Draft product requirements",
  "Research market signals",
  "Outline launch checklist",
];

export const createId = () => {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `task-${Date.now()}`;
};

import { BrowserPanel } from "@/components/BrowserPanel";
import { AutomationsPanel } from "@/components/AutomationsPanel";
import { EditorPanel } from "@/components/EditorPanel";
import { TaskFilesPanel } from "@/components/TaskFilesPanel";
import { Messages } from "@/features/messages/components/Messages";

export const formatTime = (value: string) =>
  new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

export const formatDate = (value: string) =>
  new Date(value).toLocaleDateString([], { month: "short", day: "numeric" });

export const titleFromArtifact = (type?: string, contentType?: string) => {
  if (contentType?.includes("pdf")) return "PDF document";
  if (contentType?.includes("presentation") || contentType?.includes("powerpoint")) return "Presentation";
  if (contentType?.startsWith("image/")) return "Image";
  if (!type) return "Artifact";
  const normalized = type.replace(/[_-]/g, " ");
  return normalized.charAt(0).toUpperCase() + normalized.slice(1);
};

function artifactFilenameFromUri(uri: string): string {
  try {
    const parsed = new URL(uri);
    const segments = parsed.pathname.split("/").filter(Boolean);
    const candidate = decodeURIComponent(segments[segments.length - 1] || "");
    return candidate || "";
  } catch {
    const cleaned = uri.split("?")[0].split("#")[0];
    const segments = cleaned.split("/").filter(Boolean);
    return decodeURIComponent(segments[segments.length - 1] || "");
  }
}

function documentExtension(value: string): string {
  const normalized = String(value || "").trim().toLowerCase();
  if (!normalized) return "";
  const withoutQuery = normalized.split("?")[0].split("#")[0];
  const lastSlash = withoutQuery.lastIndexOf("/");
  const basename = lastSlash >= 0 ? withoutQuery.slice(lastSlash + 1) : withoutQuery;
  const lastDot = basename.lastIndexOf(".");
  if (lastDot <= 0 || lastDot === basename.length - 1) return "";
  return basename.slice(lastDot + 1);
}

function isLibraryDocumentItem(item: LibraryItem): boolean {
  const contentType = String(item.contentType || "").toLowerCase();
  if (contentType.includes("text/markdown")) return true;
  if (contentType.includes("wordprocessingml.document")) return true;
  const labelExt = documentExtension(item.label);
  if (labelExt === "md" || labelExt === "docx") return true;
  const uriExt = documentExtension(item.uri);
  return uriExt === "md" || uriExt === "docx";
}

export const mergeLibraryItems = (existing: LibraryItem[], incoming: LibraryItem[]) => {
  const byKey = new Map<string, LibraryItem>();
  for (const item of existing) {
    const key = item.id || item.uri;
    if (!byKey.has(key)) {
      byKey.set(key, item);
    }
  }
  for (const item of incoming) {
    const key = item.id || item.uri;
    if (!byKey.has(key)) {
      byKey.set(key, item);
    }
  }
  return Array.from(byKey.values());
};

export default function App() {
  const [runId, setRunId] = useState<string | null>(null);
  const [status, setStatus] = useState<TaskStatus>("idle");
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [eventsByRun, setEventsByRun] = useState<Record<string, RunEvent[]>>({});
  const [messagesByRun, setMessagesByRun] = useState<Record<string, ChatMessage[]>>({});
  const [input, setInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [activeTaskId, setActiveTaskId] = useState<string | null>(null);
  const [activeView, setActiveView] = useState<string>("all");
  const [automationUnreadCount, setAutomationUnreadCount] = useState(0);
  const [selectedProjectId, setSelectedProjectId] = useState("");
  const [projectFormOpen, setProjectFormOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const [libraryItems, setLibraryItems] = useState<LibraryItem[]>([]);
  const [activePanel, setActivePanel] = useState<PanelType | null>(null);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [skillsLoading, setSkillsLoading] = useState(false);
  const [skillsError, setSkillsError] = useState<string | null>(null);
  const [activeSkillId, setActiveSkillId] = useState<string | null>(null);
  const [skillFiles, setSkillFiles] = useState<SkillFile[]>([]);
  const [newSkillName, setNewSkillName] = useState("");
  const [newSkillDescription, setNewSkillDescription] = useState("");
  const [newSkillContent, setNewSkillContent] = useState("");
  const [editSkillName, setEditSkillName] = useState("");
  const [editSkillDescription, setEditSkillDescription] = useState("");
  const [editSkillContent, setEditSkillContent] = useState("");
  const [showSkillsInfo, setShowSkillsInfo] = useState(false);
  const [skillDropActive, setSkillDropActive] = useState(false);
  const [contextNodes, setContextNodes] = useState<ContextNode[]>([]);
  const [contextLoading, setContextLoading] = useState(false);
  const [contextError, setContextError] = useState<string | null>(null);
  const [selectedContextId, setSelectedContextId] = useState<string | null>(null);
  const [newFolderName, setNewFolderName] = useState("");
  const [draftTags, setDraftTags] = useState<string[]>([]);
  const [composerTagInput, setComposerTagInput] = useState("");
  const [tagEditorTaskId, setTagEditorTaskId] = useState<string | null>(null);
  const [tagEditorValue, setTagEditorValue] = useState("");
  const [tagColorOverrides, setTagColorOverrides] = useState<Record<string, number>>({});
  const [llmSettings, setLlmSettings] = useState<LLMSettingsState | null>(null);
  const [memorySettings, setMemorySettings] = useState<MemorySettingsState | null>(null);
  const [personalitySettings, setPersonalitySettings] = useState<PersonalitySettingsState | null>(null);
  const [personalityDraft, setPersonalityDraft] = useState(defaultPersonality);
  const [personalityBusy, setPersonalityBusy] = useState(false);
  const [personalityStatus, setPersonalityStatus] = useState<string | null>(null);
  const [personalityError, setPersonalityError] = useState<string | null>(null);
  const [memoryPromptDismissed, setMemoryPromptDismissed] = useState(false);
  const [llmForm, setLlmForm] = useState<LLMSettingsForm>({
    mode: "remote",
    provider: "codex",
    model: "gpt-5.2-codex",
    base_url: "",
    api_key: "",
    codex_auth_path: "",
    codex_home: "",
  });
  const [llmStatus, setLlmStatus] = useState<string | null>(null);
  const [llmError, setLlmError] = useState<string | null>(null);
  const [llmBusy, setLlmBusy] = useState(false);
  const [llmAction, setLlmAction] = useState<"test" | "save" | null>(null);
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [composerModel, setComposerModel] = useState("");
  const [composerModelProvider, setComposerModelProvider] = useState("");
  const [composerModelSearchQuery, setComposerModelSearchQuery] = useState("");
  const [composerModelPickerOpen, setComposerModelPickerOpen] = useState(false);
  const [composerModelPickerLoading, setComposerModelPickerLoading] = useState(false);
  const [composerModelPickerError, setComposerModelPickerError] = useState<string | null>(null);
  const [wizardStep, setWizardStep] = useState(0);
  const [showSetupWizard, setShowSetupWizard] = useState(false);
  const [modelSearchQuery, setModelSearchQuery] = useState("");
  const [isModelDropdownOpen, setIsModelDropdownOpen] = useState(false);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);
  const [controlsExpanded, setControlsExpanded] = useState(false);
  const [userTabBrowserEnabled, setUserTabBrowserEnabled] = useState(false);
  const [messageBottomInset, setMessageBottomInset] = useState(160);
  const modelDropdownRef = useRef<HTMLDivElement>(null);

  const seenSeq = useRef<Set<number>>(new Set());
  const eventSourceRef = useRef<EventSource | null>(null);
  const tasksRef = useRef<Task[]>([]);
  const skillsFolderInputRef = useRef<HTMLInputElement | null>(null);
  const composerContainerRef = useRef<HTMLDivElement | null>(null);

  const panels = useMemo(() => derivePanels(events), [events]);
  const activeTask = tasks.find((task) => task.id === activeTaskId) || null;
  const activeSkill = skills.find((skill) => skill.id === activeSkillId) || null;
  const isLibraryView = activeView === "library";
  const isAutomationsView = activeView === "automations";
  const isSettingsView = activeView === "settings";
  const isSkillsView = activeView === "skills";
  const isContextView = activeView === "context";
  const showSideRail =
    Boolean(activeTask) &&
    !isLibraryView &&
    !isAutomationsView &&
    !isSettingsView &&
    !isSkillsView &&
    !isContextView;
  const showPanel = showSideRail && activePanel !== null;
  const hasLLMConfig = llmSettings?.configured ?? false;
  const wizardSteps = [
    {
      label: "Welcome",
      title: "Welcome to Gavryn",
      description: "A local-first control desk for AI-driven work. Let’s get you connected in a few minutes.",
    },
    {
      label: "How it works",
      title: "How Gavryn helps",
      description: "A quick tour so you know what you’re setting up and why it matters.",
    },
    {
      label: "Mode",
      title: "Choose connection mode",
      description: "Pick how Gavryn talks to your AI provider.",
    },
    {
      label: "Provider",
      title: "Select your provider",
      description: "Choose the service that will run your agents.",
    },
    {
      label: "Auth",
      title: "Add credentials",
      description: "Securely connect your account so tasks can run.",
    },
    {
      label: "Model",
      title: "Pick a model",
      description: "Choose the default model. You can switch per message inside a task.",
    },
    {
      label: "Personality",
      title: "Set assistant personality",
      description: "Review or customize the system instructions Gavryn uses.",
    },
    {
      label: "Test & Save",
      title: "Verify and finish",
      description: "Test the connection, then save your settings.",
    },
  ];
  const wizardProgress = ((wizardStep + 1) / wizardSteps.length) * 100;
  const providerBaseUrls: Record<string, string> = {
    openai: "https://api.openai.com/v1",
    openrouter: "https://openrouter.ai/api/v1",
    "opencode-zen": "https://opencode.ai/zen/v1",
    "kimi-for-coding": "https://api.kimi.com/coding/v1",
    "moonshot-ai": "https://api.moonshot.ai/v1",
  };
  const displayModelName = (provider: string, model: string) => {
    if (provider === "opencode-zen") {
      return model.replace(/^opencode\//, "");
    }
    return model;
  };
  const normalizeModelName = (provider: string, model: string) => {
    if (!model) return model;
    if (provider === "opencode-zen" && !model.startsWith("opencode/")) {
      return `opencode/${model}`;
    }
    return model;
  };
  const needsAPIKey = ["openai", "openrouter", "opencode-zen", "kimi-for-coding", "moonshot-ai"].includes(llmForm.provider);

  const projectById = useMemo(() => {
    return new Map(projects.map((project) => [project.id, project]));
  }, [projects]);

  const contextById = useMemo(() => {
    return new Map(contextNodes.map((node) => [node.id, node]));
  }, [contextNodes]);

  const selectedContext = selectedContextId ? contextById.get(selectedContextId) : null;
  const selectedContextParentId = selectedContext
    ? selectedContext.node_type === "folder"
      ? selectedContext.id
      : selectedContext.parent_id || ""
    : "";

  useEffect(() => {
    const node = composerContainerRef.current;
    if (!node) return;

    const updateInset = () => {
      const next = Math.max(24, Math.round(node.getBoundingClientRect().height) + 12);
      setMessageBottomInset((prev) => (Math.abs(prev - next) > 2 ? next : prev));
    };

    updateInset();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => updateInset());
    observer.observe(node);
    return () => observer.disconnect();
  }, [activeTaskId, activePanel, status, Boolean(error), userTabBrowserEnabled]);

  const filteredTasks = useMemo(() => {
    let scoped = tasks;
    if (
      activeView !== "all" &&
      activeView !== "library" &&
      activeView !== "automations" &&
      activeView !== "settings" &&
      activeView !== "skills" &&
      activeView !== "context"
    ) {
      scoped = scoped.filter((task) => task.projectId === activeView);
    }
    const query = normalizeSearch(searchQuery);
    if (!query) return scoped;
    const tokens = query.split(/\s+/).filter(Boolean);
    if (tokens.length === 0) return scoped;
    return scoped.filter((task) => {
      const projectName = task.projectId ? projectById.get(task.projectId)?.name || "" : "";
      const haystack = [task.title, projectName, ...task.tags].join(" ");
      return tokens.every((token) => matchesQuery(token, haystack));
    });
  }, [tasks, activeView, searchQuery, projectById]);

  const projectCounts = useMemo(() => {
    return tasks.reduce<Record<string, number>>((acc, task) => {
      if (!task.projectId) return acc;
      acc[task.projectId] = (acc[task.projectId] || 0) + 1;
      return acc;
    }, {});
  }, [tasks]);

  const tagClass = (tag: string) => {
    const normalized = normalizeTagName(tag);
    const override = tagColorOverrides[normalized];
    const index = override ?? getTagColorIndex(tag);
    return tagColors[index];
  };

  const sortedLibraryDocuments = useMemo(() => {
    return libraryItems.filter(isLibraryDocumentItem).sort(
      (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
    );
  }, [libraryItems]);

  const activeTaskLibraryItems = useMemo(() => {
    const currentRunId = activeTask?.runId;
    if (!currentRunId) return [];
    return libraryItems.filter((item) => item.runId === currentRunId);
  }, [libraryItems, activeTask?.runId]);

  useEffect(() => {
    tasksRef.current = tasks;
  }, [tasks]);

  useEffect(() => {
    if (!activeTask) {
      setActivePanel(null);
    } else if (activePanel === null) {
      setActivePanel("overview");
    }
  }, [activeTask, activePanel]);

  // Ensure activePanel is valid based on derived panels (optional, but good for UX)
  useEffect(() => {
    if (activePanel === "browser" && !panels.showBrowser && !panels.latestBrowserUri) {
      // Keep user on browser if they selected it, or maybe switch back? 
      // For now, let's allow manual switching freely.
    }
  }, [activePanel, panels]);

  useEffect(() => {
    loadRuns();
    loadLLMSettings();
    loadMemorySettings();
    loadPersonalitySettings();
    loadAutomationsUnreadCount();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const interval = window.setInterval(() => {
      void loadAutomationsUnreadCount();
    }, 60000);
    return () => window.clearInterval(interval);
  }, []);

  useEffect(() => {
    if (llmSettings !== null && !llmSettings.configured) {
      setActiveView("settings");
    }
  }, [llmSettings]);

  useEffect(() => {
    if (isSettingsView && !hasLLMConfig) {
      setWizardStep(0);
      setShowSetupWizard(true);
    }
  }, [isSettingsView, hasLLMConfig]);

  useEffect(() => {
    if (!isSettingsView) {
      setShowSetupWizard(false);
    }
  }, [isSettingsView]);

  useEffect(() => {
    if (!isSkillsView) return;
    loadSkills();
    if (skillsFolderInputRef.current) {
      skillsFolderInputRef.current.setAttribute("webkitdirectory", "");
      skillsFolderInputRef.current.setAttribute("directory", "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isSkillsView]);

  useEffect(() => {
    if (!activeSkillId) return;
    const skill = skills.find((item) => item.id === activeSkillId);
    if (!skill) {
      setActiveSkillId(null);
      setSkillFiles([]);
      return;
    }
    setEditSkillName(skill.name);
    setEditSkillDescription(skill.description);
    loadSkillFiles(skill.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSkillId, skills]);

  useEffect(() => {
    if (!isContextView) return;
    loadContextNodes();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isContextView]);

  // Click outside handler to close model dropdown
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (modelDropdownRef.current && !modelDropdownRef.current.contains(event.target as Node)) {
        setIsModelDropdownOpen(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  useEffect(() => {
    if (!composerModelPickerOpen) return;
    const handleEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        setComposerModelPickerOpen(false);
      }
    };
    document.addEventListener("keydown", handleEscape);
    return () => document.removeEventListener("keydown", handleEscape);
  }, [composerModelPickerOpen]);

  useEffect(() => {
    if (!composerModelPickerOpen) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, [composerModelPickerOpen]);

  useEffect(() => {
    if (!runId) {
      setConnected(false);
      return;
    }
    const source = new EventSource(`${API_BASE_URL}/runs/${runId}/events`);
    eventSourceRef.current = source;

    const onOpen = () => setConnected(true);
    const onError = () => setConnected(false);

    const onEvent = (event: MessageEvent<string>) => {
      try {
        const payload = normalizeEvent(JSON.parse(event.data) as RunEvent);
        if (seenSeq.current.has(payload.seq)) {
          return;
        }
        seenSeq.current.add(payload.seq);
        setEvents((prev) => {
          const nextEvents = dedupeEvents(prev, [payload]);
          setEventsByRun((current) => ({ ...current, [payload.run_id]: nextEvents }));
          return nextEvents;
        });
        setMessages((prev) => {
          const nextMessages = applyEvent(prev, payload);
          setMessagesByRun((current) => ({ ...current, [payload.run_id]: nextMessages }));
          return nextMessages;
        });
        const taskForRun = tasksRef.current.find((task) => task.runId === payload.run_id);
        const nextItems = collectLibraryItems(payload, taskForRun);
        if (nextItems.length > 0) {
          setLibraryItems((prev) => mergeLibraryItems(prev, nextItems));
        }
      } catch (err) {
        console.error("Failed to parse event", err);
      }
    };

    source.addEventListener("run_event", onEvent as EventListener);
    source.addEventListener("open", onOpen);
    source.addEventListener("error", onError);

    return () => {
      source.close();
    };
  }, [runId]);

  useEffect(() => {
    if (!activeTaskId) return;
    const latest = events[events.length - 1];
    if (!latest) return;
    const latestType = normalizeEventType(latest.type);
    let nextStatus: TaskStatus | null = null;
    if (latestType === "run.started") nextStatus = "running";
    if (latestType === "run.completed") nextStatus = "completed";
    if (latestType === "run.partial") nextStatus = "partial";
    if (latestType === "run.cancelled") nextStatus = "cancelled";
    if (latestType === "run.failed") nextStatus = "failed";
    if (!nextStatus) return;
    setStatus(nextStatus);
    setTasks((prev) =>
      prev.map((task) =>
        task.id === activeTaskId
          ? { ...task, status: nextStatus, updatedAt: new Date().toISOString() }
          : task
      )
    );
  }, [events, activeTaskId]);

  async function loadLLMSettings() {
    try {
      const response = await fetch(`${API_BASE_URL}/settings/llm`);
      if (!response.ok) {
        return;
      }
      const data = (await response.json()) as LLMSettingsState;
      setLlmSettings(data);
      const provider = data.provider || "codex";
      const model = data.model || "gpt-5.2-codex";
      setLlmForm({
        mode: data.mode || "remote",
        provider,
        model: displayModelName(provider, model),
        base_url: data.base_url || "",
        api_key: "",
        codex_auth_path: data.codex_auth_path || "",
        codex_home: data.codex_home || "",
      });
      if (!composerModel) {
        setComposerModel(displayModelName(provider, model));
      }
    } catch (err) {
      console.error("Failed to load LLM settings", err);
    }
  }

  async function loadRuns() {
    try {
      const response = await fetch(`${API_BASE_URL}/runs`);
      if (!response.ok) {
        return;
      }
      const data = (await response.json()) as { runs: RunSummary[] };
      const runs = data.runs || [];
      const restored = runs.map((run) => ({
        id: run.id,
        title: run.title || "Untitled task",
        projectId: "",
        runId: run.id,
        status: normalizeStatus(run.status),
        updatedAt: run.updated_at || run.created_at,
        messageCount: run.message_count ?? 0,
        tags: [],
      }));
      setTasks(restored);
    } catch (err) {
      console.error("Failed to load runs", err);
    }
  }

  async function loadMemorySettings() {
    try {
      const response = await fetch(`${API_BASE_URL}/settings/memory`);
      if (!response.ok) {
        return;
      }
      const data = (await response.json()) as MemorySettingsState;
      setMemorySettings(data);
    } catch (err) {
      console.error("Failed to load memory settings", err);
    }
  }

  async function loadPersonalitySettings() {
    try {
      const response = await fetch(`${API_BASE_URL}/settings/personality`);
      if (!response.ok) {
        return;
      }
      const data = (await response.json()) as PersonalitySettingsState;
      setPersonalitySettings(data);
      if (data.content && data.content.trim()) {
        setPersonalityDraft(data.content);
      } else {
        setPersonalityDraft(defaultPersonality);
      }
    } catch (err) {
      console.error("Failed to load personality settings", err);
    }
  }

  async function loadAutomationsUnreadCount() {
    try {
      await fetch(`${API_BASE_URL}/automations/process-due`, { method: "POST" });
      const response = await fetch(`${API_BASE_URL}/automations`);
      if (!response.ok) {
        return;
      }
      const payload = (await response.json()) as { unread_count?: number };
      setAutomationUnreadCount(
        typeof payload.unread_count === "number" && payload.unread_count > 0
          ? payload.unread_count
          : 0
      );
    } catch {
      // Ignore transient fetch failures for badge refresh.
    }
  }

  async function savePersonalitySettings(content: string) {
    setPersonalityBusy(true);
    setPersonalityStatus(null);
    setPersonalityError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/settings/personality`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to save personality settings");
      }
      const data = (await response.json()) as PersonalitySettingsState;
      setPersonalitySettings(data);
      if (data.content && data.content.trim()) {
        setPersonalityDraft(data.content);
      }
      setPersonalityStatus("Personality saved.");
    } catch (err) {
      setPersonalityError(err instanceof Error ? err.message : "Failed to save personality settings");
    } finally {
      setPersonalityBusy(false);
    }
  }

  async function updateMemorySettings(enabled: boolean) {
    try {
      const response = await fetch(`${API_BASE_URL}/settings/memory`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled }),
      });
      if (!response.ok) {
        return;
      }
      const data = (await response.json()) as MemorySettingsState;
      setMemorySettings(data);
    } catch (err) {
      console.error("Failed to update memory settings", err);
    }
  }

  async function fetchModelOptions(form: LLMSettingsForm) {
    setModelsLoading(true);
    setLlmError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/settings/llm/models`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          provider: form.provider,
          base_url: form.base_url,
          api_key: form.api_key,
          model: normalizeModelName(form.provider, form.model),
        }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to load models");
      }
      const data = (await response.json()) as { models: string[] };
      const models = data.models || [];
      setModelOptions(models.map((model) => displayModelName(form.provider, model)));
      if (data.models?.length && !form.model) {
        const nextModel = displayModelName(form.provider, data.models[0]);
        setLlmForm((prev) => ({ ...prev, model: nextModel }));
        if (!composerModel) {
          setComposerModel(nextModel);
        }
      }
    } catch (err) {
      setLlmError(err instanceof Error ? err.message : "Failed to load models");
      setModelOptions([]);
    } finally {
      setModelsLoading(false);
    }
  }

  async function loadComposerModelOptions(force = false) {
    const provider = activeComposerProvider;
    if (!force && composerModelProvider === provider && modelOptions.length > 0) {
      return;
    }

    setComposerModelProvider(provider);
    setComposerModelPickerLoading(true);
    setComposerModelPickerError(null);

    try {
      const requestBody: Record<string, string> = {};
      if (!(llmSettings?.configured && llmSettings.provider)) {
        requestBody.provider = provider;
        if (llmForm.base_url) {
          requestBody.base_url = llmForm.base_url;
        }
        if (llmForm.api_key) {
          requestBody.api_key = llmForm.api_key;
        }
      }

      const response = await fetch(`${API_BASE_URL}/settings/llm/models`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(requestBody),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to load models");
      }
      const data = (await response.json()) as { models: string[] };
      const models = (data.models || []).map((model) => displayModelName(provider, model));
      setModelOptions(models);
      if (models.length === 0) {
        setComposerModelPickerError(`No models returned for provider "${provider}".`);
      } else if (!composerModel) {
        setComposerModel(models[0]);
      }
    } catch (err) {
      setComposerModelPickerError(err instanceof Error ? err.message : "Failed to load models");
    } finally {
      setComposerModelPickerLoading(false);
    }
  }

  function openComposerModelPicker() {
    if (setupRequired) return;
    setComposerModelPickerOpen(true);
    setComposerModelSearchQuery("");
    void loadComposerModelOptions(true);
  }

  async function testLLMSettings() {
    setLlmAction("test");
    setLlmBusy(true);
    setLlmError(null);
    setLlmStatus(null);
    try {
      const response = await fetch(`${API_BASE_URL}/settings/llm/test`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...llmForm,
          model: normalizeModelName(llmForm.provider, llmForm.model),
        }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Test failed");
      }
      const data = (await response.json()) as { status: string };
      setLlmStatus(data.status || "Connected");
    } catch (err) {
      setLlmError(err instanceof Error ? err.message : "Test failed");
    } finally {
      setLlmBusy(false);
      setLlmAction(null);
    }
  }

  async function saveLLMSettings() {
    setLlmAction("save");
    setLlmBusy(true);
    setLlmError(null);
    setLlmStatus(null);
    try {
      const response = await fetch(`${API_BASE_URL}/settings/llm`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...llmForm,
          model: normalizeModelName(llmForm.provider, llmForm.model),
        }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Save failed");
      }
      const data = (await response.json()) as LLMSettingsState;
      setLlmSettings(data);
      setLlmStatus("Saved");
      if (llmForm.model) {
        setComposerModel(llmForm.model);
      }
      await fetchModelOptions({ ...llmForm, api_key: "" });
    } catch (err) {
      setLlmError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setLlmBusy(false);
      setLlmAction(null);
    }
  }


  function readFileAsBase64(file: File) {
    return new Promise<SkillFile>((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(new Error("Failed to read file"));
      reader.onload = () => {
        const buffer = reader.result instanceof ArrayBuffer ? reader.result : new ArrayBuffer(0);
        const bytes = new Uint8Array(buffer);
        let binary = "";
        bytes.forEach((byte) => {
          binary += String.fromCharCode(byte);
        });
        resolve({
          path: (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name,
          content_base64: btoa(binary),
          content_type: file.type || "",
          size_bytes: file.size,
          updated_at: new Date().toISOString(),
        });
      };
      reader.readAsArrayBuffer(file);
    });
  }

  function readContextFileAsBase64(file: File) {
    return new Promise<{ name: string; content_base64: string; content_type: string }>((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(new Error("Failed to read file"));
      reader.onload = () => {
        const buffer = reader.result instanceof ArrayBuffer ? reader.result : new ArrayBuffer(0);
        const bytes = new Uint8Array(buffer);
        let binary = "";
        bytes.forEach((byte) => {
          binary += String.fromCharCode(byte);
        });
        resolve({
          name: file.name,
          content_base64: btoa(binary),
          content_type: file.type || "",
        });
      };
      reader.readAsArrayBuffer(file);
    });
  }

  async function loadContextNodes() {
    setContextLoading(true);
    setContextError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/context`);
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to load context");
      }
      const data = (await response.json()) as { nodes: ContextNode[] };
      setContextNodes(data.nodes || []);
    } catch (err) {
      setContextError(err instanceof Error ? err.message : "Failed to load context");
    } finally {
      setContextLoading(false);
    }
  }

  async function createContextFolder() {
    const name = newFolderName.trim();
    if (!name) {
      setContextError("Folder name is required");
      return;
    }
    setContextError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/context/folders`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name,
          parent_id: selectedContextParentId,
        }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to create folder");
      }
      setNewFolderName("");
      await loadContextNodes();
    } catch (err) {
      setContextError(err instanceof Error ? err.message : "Failed to create folder");
    }
  }

  async function uploadContextFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    setContextError(null);
    try {
      const payloads = await Promise.all(Array.from(files).map(readContextFileAsBase64));
      for (const payload of payloads) {
        const response = await fetch(`${API_BASE_URL}/context/files`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            name: payload.name,
            parent_id: selectedContextParentId,
            content_base64: payload.content_base64,
            content_type: payload.content_type,
          }),
        });
        if (!response.ok) {
          const details = await response.text();
          throw new Error(details || "Failed to upload file");
        }
      }
      await loadContextNodes();
    } catch (err) {
      setContextError(err instanceof Error ? err.message : "Failed to upload file");
    }
  }

  async function removeContextNode(nodeId: string) {
    setContextError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/context/${nodeId}`, {
        method: "DELETE",
      });
      if (!response.ok && response.status !== 204) {
        const details = await response.text();
        throw new Error(details || "Failed to delete context item");
      }
      if (selectedContextId === nodeId) {
        setSelectedContextId(null);
      }
      await loadContextNodes();
    } catch (err) {
      setContextError(err instanceof Error ? err.message : "Failed to delete context item");
    }
  }

  async function loadSkills() {
    setSkillsLoading(true);
    setSkillsError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/skills`);
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to load skills");
      }
      const data = (await response.json()) as { skills: Skill[] };
      setSkills(data.skills || []);
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : "Failed to load skills");
    } finally {
      setSkillsLoading(false);
    }
  }

  async function loadSkillFiles(skillId: string) {
    setSkillsError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/skills/${skillId}/files`);
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to load skill files");
      }
      const data = (await response.json()) as { files: SkillFile[] };
      setSkillFiles(data.files || []);
      const markdown = data.files.find((file) => file.path.toLowerCase().endsWith("skill.md"));
      setEditSkillContent(markdown ? decodeBase64(markdown.content_base64) : "");
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : "Failed to load skill files");
    }
  }

  async function createSkill() {
    const name = newSkillName.trim();
    if (!name) {
      setSkillsError("Skill name is required");
      return;
    }
    setSkillsError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/skills`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name,
          description: newSkillDescription.trim(),
          files: [
            {
              path: "SKILL.md",
              content_base64: encodeBase64(newSkillContent || ""),
              content_type: "text/markdown",
            },
          ],
        }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to create skill");
      }
      const created = (await response.json()) as Skill;
      setNewSkillName("");
      setNewSkillDescription("");
      setNewSkillContent("");
      await loadSkills();
      setActiveSkillId(created.id);
      await loadSkillFiles(created.id);
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : "Failed to create skill");
    }
  }

  async function saveSkillMetadata() {
    if (!activeSkill) return;
    setSkillsError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/skills/${activeSkill.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: editSkillName.trim(),
          description: editSkillDescription.trim(),
        }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to update skill");
      }
      const updated = (await response.json()) as Skill;
      await loadSkills();
      setActiveSkillId(updated.id);
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : "Failed to update skill");
    }
  }

  async function saveSkillContent() {
    if (!activeSkill) return;
    setSkillsError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/skills/${activeSkill.id}/files`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          files: [
            {
              path: "SKILL.md",
              content_base64: encodeBase64(editSkillContent || ""),
              content_type: "text/markdown",
            },
          ],
        }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to update skill content");
      }
      await loadSkillFiles(activeSkill.id);
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : "Failed to update skill content");
    }
  }

  async function uploadSkillFiles(files: FileList | null) {
    if (!activeSkill || !files || files.length === 0) return;
    setSkillsError(null);
    try {
      const payload = await Promise.all(Array.from(files).map(readFileAsBase64));
      const response = await fetch(`${API_BASE_URL}/skills/${activeSkill.id}/files`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ files: payload }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to upload files");
      }
      await loadSkillFiles(activeSkill.id);
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : "Failed to upload files");
    }
  }

  function handleSkillDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setSkillDropActive(false);
    uploadSkillFiles(event.dataTransfer.files);
  }

  function renderContextTree(parentId: string | null, depth: number) {
    const children = contextNodes
      .filter((node) => (parentId ? node.parent_id === parentId : !node.parent_id))
      .sort((a, b) => a.name.localeCompare(b.name));
    return children.map((node) => (
      <div key={node.id}>
        <div
          style={{ paddingLeft: `${depth * 16}px` }}
          className={`flex items-center justify-between rounded-xl border px-3 py-2 text-sm transition ${
            selectedContextId === node.id
              ? "border-accent/60 bg-accent/10"
              : "border-border/60 bg-card/40 hover:border-border"
          }`}
        >
          <button
            type="button"
            onClick={() => setSelectedContextId(node.id)}
            className="flex flex-1 items-center gap-2 text-left"
          >
            <span className="text-[10px] uppercase text-muted-foreground">{node.node_type}</span>
            <span className="text-foreground">{node.name}</span>
          </button>
          <button
            type="button"
            onClick={() => removeContextNode(node.id)}
            className="rounded-full border border-border/60 p-1 text-muted-foreground hover:text-foreground"
          >
            <Trash2 className="h-3 w-3" />
          </button>
        </div>
        {node.node_type === "folder" ? renderContextTree(node.id, depth + 1) : null}
      </div>
    ));
  }

  async function removeSkill() {
    if (!activeSkill) return;
    setSkillsError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/skills/${activeSkill.id}`, {
        method: "DELETE",
      });
      if (!response.ok && response.status !== 204) {
        const details = await response.text();
        throw new Error(details || "Failed to delete skill");
      }
      setActiveSkillId(null);
      setSkillFiles([]);
      setEditSkillName("");
      setEditSkillDescription("");
      setEditSkillContent("");
      await loadSkills();
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : "Failed to delete skill");
    }
  }

  async function createRun() {
    setError(null);
    const response = await fetch(`${API_BASE_URL}/runs`, { method: "POST" });
    if (!response.ok) {
      const details = await response.text();
      throw new Error(details || "Failed to create run");
    }
    const data = await response.json();
    setRunId(data.run_id);
    setStatus("running");
    setEvents([]);
    setMessages([]);
    setEventsByRun((prev) => ({ ...prev, [data.run_id]: [] }));
    setMessagesByRun((prev) => ({ ...prev, [data.run_id]: [] }));
    setConnected(false);
    seenSeq.current = new Set();
    return data.run_id as string;
  }

  async function sendMessage(contentOverride?: string) {
    if (!hasLLMConfig) {
      setError("Complete LLM setup in Settings before sending messages.");
      setActiveView("settings");
      return;
    }
    const content = (contentOverride ?? input).trim();
    if (!content) return;
    setError(null);
    const prevStatus = status;
    let optimisticStatusApplied = false;
    let previousTaskStatus: TaskStatus | null = null;
    try {
      let currentRunId = runId;
      let taskId = activeTaskId;
      if (!currentRunId) {
        currentRunId = await createRun();
        const newTask: Task = {
          id: createId(),
          title: content,
          projectId: selectedProjectId,
          runId: currentRunId,
          status: "running",
          updatedAt: new Date().toISOString(),
          messageCount: 1,
          tags: draftTags,
        };
        setTasks((prev) => [newTask, ...prev]);
        setActiveTaskId(newTask.id);
        setDraftTags([]);
        setComposerTagInput("");
        taskId = newTask.id;
      } else if (taskId) {
        const activeTaskSnapshot = tasksRef.current.find((task) => task.id === taskId);
        previousTaskStatus = activeTaskSnapshot?.status || null;
        setStatus("running");
        setConnected(false);
        optimisticStatusApplied = true;
        setTasks((prev) =>
          prev.map((task) =>
            task.id === taskId
              ? { ...task, status: "running", updatedAt: new Date().toISOString() }
              : task
          )
        );
      }

      const metadata: Record<string, string> = {};
      if (composerModel) {
        const provider = llmSettings?.provider || llmForm.provider;
        metadata.llm_model = normalizeModelName(provider, composerModel);
      }
      if (userTabBrowserEnabled) {
        metadata.browser_mode = "user_tab";
        metadata.browser_control_mode = "user_tab";
        metadata.browser_interaction = "enabled";
        const userAgent = typeof navigator !== "undefined" ? String(navigator.userAgent || "") : "";
        if (userAgent) {
          metadata.browser_user_agent = userAgent;
          const preferred = inferPreferredBrowserFromUserAgent(userAgent);
          if (preferred) {
            metadata.browser_preferred_browser = preferred;
          }
        }
      }
      const response = await fetch(`${API_BASE_URL}/runs/${currentRunId}/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: "user", content, metadata }),
      });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to send message");
      }
      if (!contentOverride) {
        setInput("");
      }
      if (taskId) {
        setTasks((prev) =>
          prev.map((task) =>
            task.id === taskId
              ? {
                  ...task,
                  title: task.title || content,
                  messageCount: task.messageCount + 1,
                  updatedAt: new Date().toISOString(),
                }
              : task
          )
        );
      }
    } catch (err) {
      if (optimisticStatusApplied) {
        setStatus(prevStatus);
        if (activeTaskId && previousTaskStatus) {
          setTasks((prev) =>
            prev.map((task) =>
              task.id === activeTaskId
                ? { ...task, status: previousTaskStatus, updatedAt: new Date().toISOString() }
                : task
            )
          );
        }
      }
      setError(err instanceof Error ? err.message : "Failed to send message");
    }
  }

  function handleEditorFixNow(prompt: string) {
    setActivePanel("overview");
    void sendMessage(prompt);
  }

  async function cancelRun() {
    if (!runId) return;
    setError(null);
    const prevStatus = status;
    setStatus("cancelled");
    setConnected(false);
    if (activeTaskId) {
      setTasks((prev) =>
        prev.map((task) =>
          task.id === activeTaskId
            ? { ...task, status: "cancelled", updatedAt: new Date().toISOString() }
            : task
        )
      );
    }
    try {
      const response = await fetch(`${API_BASE_URL}/runs/${runId}/cancel`, { method: "POST" });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to stop run");
      }
    } catch (err) {
      setStatus(prevStatus);
      setError(err instanceof Error ? err.message : "Failed to stop run");
    }
  }

  function startNewTask() {
    setRunId(null);
    setStatus("idle");
    setEvents([]);
    setMessages([]);
    setInput("");
    setSearchQuery("");
    setConnected(false);
    setError(null);
    setMemoryPromptDismissed(false);
    seenSeq.current = new Set();
    setActiveTaskId(null);
    setActivePanel(null);
    setDraftTags([]);
    setComposerTagInput("");
    setTagEditorTaskId(null);
    setTagEditorValue("");
    if (activeView === "library" || activeView === "settings") {
      setActiveView("all");
    }
  }

  function selectTask(task: Task) {
    if (activeView === "library" || activeView === "settings") {
      setActiveView("all");
    }
    setError(null);
    setMemoryPromptDismissed(false);
    setComposerTagInput("");
    setTagEditorTaskId(null);
    setTagEditorValue("");
    const cachedEvents = eventsByRun[task.runId] || [];
    const cachedMessages = messagesByRun[task.runId] || [];
    if (task.runId === runId) {
      setActiveTaskId(task.id);
      setSelectedProjectId(task.projectId || "");
      setEvents(cachedEvents);
      setMessages(cachedMessages);
      seenSeq.current = new Set(cachedEvents.map((event) => event.seq));
      return;
    }
    setActiveTaskId(task.id);
    setSelectedProjectId(task.projectId || "");
    setRunId(task.runId);
    setStatus(task.status);
    setEvents(cachedEvents);
    setMessages(cachedMessages);
    setConnected(false);
    setActivePanel(null);
    seenSeq.current = new Set(cachedEvents.map((event) => event.seq));
  }

  async function deleteTask(taskId: string) {
    const task = tasksRef.current.find((item) => item.id === taskId);
    if (!task) return;
    if (task.id === activeTaskId && task.status === "running" && runId) {
      await cancelRun();
    }
    try {
      const response = await fetch(`${API_BASE_URL}/runs/${task.runId}`, { method: "DELETE" });
      if (!response.ok) {
        const details = await response.text();
        throw new Error(details || "Failed to delete task");
      }
    } catch (err) {
      console.error("Failed to delete task", err);
      return;
    }
    if (task.id === activeTaskId) {
      startNewTask();
    }
    setTasks((prev) => prev.filter((item) => item.id !== taskId));
    setLibraryItems((prev) => prev.filter((item) => item.taskId !== taskId));
    setEventsByRun((prev) => {
      const next = { ...prev };
      delete next[task.runId];
      return next;
    });
    setMessagesByRun((prev) => {
      const next = { ...prev };
      delete next[task.runId];
      return next;
    });
  }

  function updateTaskProject(taskId: string, projectId: string) {
    setTasks((prev) =>
      prev.map((task) => (task.id === taskId ? { ...task, projectId } : task))
    );
  }

  function addTagToTask(taskId: string, rawName: string) {
    const segments = rawName
      .split(",")
      .map((segment) => segment.trim())
      .filter(Boolean);
    if (segments.length === 0) return;
    setTasks((prev) =>
      prev.map((task) => {
        if (task.id !== taskId) return task;
        const existing = task.tags.map((tag) => normalizeTagName(tag));
        const additions = segments.filter(
          (segment) => !existing.includes(normalizeTagName(segment))
        );
        if (additions.length === 0) return task;
        return { ...task, tags: [...task.tags, ...additions] };
      })
    );
  }

  function removeTagFromTask(taskId: string, tagName: string) {
    const normalized = normalizeTagName(tagName);
    setTasks((prev) =>
      prev.map((task) => {
        if (task.id !== taskId) return task;
        const nextTags = task.tags.filter((tag) => normalizeTagName(tag) !== normalized);
        return { ...task, tags: nextTags };
      })
    );
  }

  function addTagToDraft(rawName: string) {
    const segments = rawName
      .split(",")
      .map((segment) => segment.trim())
      .filter(Boolean);
    if (segments.length === 0) return;
    setDraftTags((prev) => {
      const existing = prev.map((tag) => normalizeTagName(tag));
      const additions = segments.filter(
        (segment) => !existing.includes(normalizeTagName(segment))
      );
      if (additions.length === 0) return prev;
      return [...prev, ...additions];
    });
  }

  function removeTagFromDraft(tagName: string) {
    const normalized = normalizeTagName(tagName);
    setDraftTags((prev) => prev.filter((tag) => normalizeTagName(tag) !== normalized));
  }

  function handleComposerTagAdd() {
    const name = composerTagInput.trim();
    if (!name) return;
    if (activeTask) {
      addTagToTask(activeTask.id, name);
    } else {
      addTagToDraft(name);
    }
    setComposerTagInput("");
  }

  function handleTagEditorAdd(taskId: string) {
    const name = tagEditorValue.trim();
    if (!name) return;
    addTagToTask(taskId, name);
    setTagEditorValue("");
    setTagEditorTaskId(null);
  }

  function cycleTagColor(tagName: string) {
    const normalized = normalizeTagName(tagName);
    setTagColorOverrides((prev) => {
      const current = prev[normalized] ?? getTagColorIndex(tagName);
      const next = (current + 1) % tagColors.length;
      return { ...prev, [normalized]: next };
    });
  }

  function createProject() {
    const name = newProjectName.trim();
    if (!name) return;
    const id = createId();
    const tone = toneCycle[projects.length % toneCycle.length];
    const project: Project = { id, name, tone };
    setProjects((prev) => [...prev, project]);
    setNewProjectName("");
    setProjectFormOpen(false);
    setActiveView(id);
    setSelectedProjectId(id);
  }

  function deleteProject(projectId: string) {
    setProjects((prev) => prev.filter((project) => project.id !== projectId));
    setTasks((prev) =>
      prev.map((task) => (task.projectId === projectId ? { ...task, projectId: "" } : task))
    );
    if (selectedProjectId === projectId) {
      setSelectedProjectId("");
    }
    if (activeView === projectId) {
      setActiveView("all");
    }
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      const currentlyRunning = status === "running" || activeTask?.status === "running";
      if (currentlyRunning) {
        void cancelRun();
      } else {
        void sendMessage();
      }
    }
  }

  const lastMessage = messages[messages.length - 1];
  const runInProgress = status === "running" || activeTask?.status === "running";
  const awaitingResponse =
    runInProgress && (!lastMessage || lastMessage.role === "user" || lastMessage.streaming);
  const setupRequired = !hasLLMConfig;
  const showMemoryPrompt = Boolean(activeTask) && memorySettings && !memorySettings.enabled && !memoryPromptDismissed;
  const showWizard = !hasLLMConfig || showSetupWizard;
  const activeComposerProvider = llmSettings?.provider || llmForm.provider || "codex";
  const activeComposerModel =
    composerModel ||
    displayModelName(
      activeComposerProvider,
      llmSettings?.model || llmForm.model || "gpt-5.2-codex"
    );
  const filteredComposerModelOptions = useMemo(() => {
    const query = composerModelSearchQuery.trim().toLowerCase();
    if (!query) {
      return modelOptions;
    }
    return modelOptions.filter((model) => model.toLowerCase().includes(query));
  }, [composerModelSearchQuery, modelOptions]);

  const composer = (
    <div className="rounded-2xl border border-border/60 bg-background p-4">
      <textarea
        value={input}
        onChange={(event) => setInput(event.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="Assign a task or ask anything"
        disabled={setupRequired}
        className="min-h-[120px] w-full resize-none bg-transparent text-sm leading-relaxed outline-none placeholder:text-muted-foreground"
      />
      <div className="mt-4 flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
          <div className="flex items-center gap-2 rounded-full border border-border/60 px-3 py-1">
            <Tag className="h-3 w-3" />
            <select
              value={selectedProjectId}
              onChange={(event) => setSelectedProjectId(event.target.value)}
              className="bg-transparent text-xs font-medium outline-none"
            >
              <option value="">Unassigned</option>
              {projects.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {(activeTask ? activeTask.tags : draftTags).map((tag) => (
              <span
                key={`${activeTask?.id || "draft"}-${tag}`}
                className={`flex items-center gap-1 rounded-full border px-2 py-1 text-xs ${tagClass(tag)}`}
              >
                <button
                  type="button"
                  onClick={() => cycleTagColor(tag)}
                  className="rounded-full border border-current/30 p-0.5"
                >
                  <span className="h-1.5 w-1.5 rounded-full bg-current" />
                </button>
                {tag}
                <button
                  type="button"
                  onClick={() =>
                    activeTask ? removeTagFromTask(activeTask.id, tag) : removeTagFromDraft(tag)
                  }
                  className="ml-1 rounded-full border border-current/30 p-0.5"
                >
                  <X className="h-2.5 w-2.5" />
                </button>
              </span>
            ))}
            <div className="flex items-center gap-1 rounded-full border border-border/60 bg-card/60 px-2 py-1">
              <Tag className="h-3 w-3" />
              <input
                value={composerTagInput}
                onChange={(event) => setComposerTagInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    handleComposerTagAdd();
                  }
                }}
                placeholder="Add tag"
                className="w-20 bg-transparent text-xs outline-none placeholder:text-muted-foreground"
              />
              <button
                type="button"
                onClick={handleComposerTagAdd}
                className="rounded-full border border-border/60 p-0.5 text-muted-foreground hover:text-foreground"
              >
                <Plus className="h-3 w-3" />
              </button>
            </div>
            <button
              type="button"
              onClick={openComposerModelPicker}
              disabled={setupRequired}
              className="flex items-center gap-1 rounded-full border border-border/60 bg-card/60 px-2 py-1 text-xs transition hover:border-border hover:bg-card disabled:cursor-not-allowed disabled:opacity-60"
              aria-haspopup="dialog"
              aria-expanded={composerModelPickerOpen}
              aria-label="Choose model for next message"
              title="Choose model for next message"
            >
              <SlidersHorizontal className="h-3 w-3" />
              <span className="max-w-[11rem] truncate">{activeComposerModel || "Model"}</span>
            </button>
          </div>
          <span>Shift + Enter for a new line</span>
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={
              userTabBrowserEnabled
                ? "h-10 gap-1.5 border-emerald-400/50 bg-emerald-500/15 px-3 text-emerald-100 hover:bg-emerald-500/25 hover:text-emerald-50"
                : "h-10 w-10 p-0"
            }
            onClick={() => setUserTabBrowserEnabled((prev) => !prev)}
            aria-pressed={userTabBrowserEnabled}
            aria-label={userTabBrowserEnabled ? "Disable user tab browser mode" : "Enable user tab browser mode"}
            title={
              userTabBrowserEnabled
                ? "User tab browser mode enabled"
                : "Enable user tab browser mode"
            }
          >
            {userTabBrowserEnabled ? <span className="text-[10px] uppercase tracking-wide">Enabled</span> : null}
            <Globe className="h-4 w-4" />
          </Button>
          <Button
            variant={runInProgress ? "outline" : "default"}
            size="sm"
            className="h-10 w-10 p-0"
            onClick={() => {
              if (runInProgress) {
                void cancelRun();
                return;
              }
              void sendMessage();
            }}
            disabled={setupRequired || (runInProgress && !runId)}
            aria-label={runInProgress ? "Stop run" : "Send message"}
            title={runInProgress ? "Stop run" : "Send message"}
          >
            {runInProgress ? <Square className="h-4 w-4" /> : <ArrowUp className="h-4 w-4" />}
          </Button>
        </div>
      </div>
      {setupRequired && (
        <div className="mt-4 rounded-2xl border border-amber-400/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-100">
          LLM setup is required before you can send messages. Visit Settings to finish configuration.
        </div>
      )}
      {error && (
        <div className="mt-4 rounded-2xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-100">
          {error}
        </div>
      )}
      {composerModelPickerOpen && (
        <div
          className="fixed inset-0 z-[90] flex items-center justify-center bg-background/75 p-4 backdrop-blur-sm"
          onClick={() => setComposerModelPickerOpen(false)}
        >
          <div
            className="w-full max-w-xl rounded-2xl border border-border bg-card shadow-xl"
            onClick={(event) => event.stopPropagation()}
            role="dialog"
            aria-modal="true"
            aria-label="Model picker"
          >
            <div className="flex items-start justify-between gap-3 border-b border-border/70 px-4 py-3">
              <div>
                <p className="text-sm font-semibold text-foreground">Choose model</p>
                <p className="text-xs text-muted-foreground">
                  Provider: <span className="font-medium text-foreground">{activeComposerProvider}</span>. Applies to your next message.
                </p>
              </div>
              <button
                type="button"
                onClick={() => setComposerModelPickerOpen(false)}
                className="rounded-md border border-border/70 p-1 text-muted-foreground transition hover:text-foreground"
                aria-label="Close model picker"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="space-y-3 p-4">
              <div className="flex items-center gap-2">
                <input
                  value={composerModelSearchQuery}
                  onChange={(event) => setComposerModelSearchQuery(event.target.value)}
                  placeholder="Search models..."
                  className="h-9 w-full rounded-lg border border-border/70 bg-background px-3 text-sm text-foreground outline-none ring-0 placeholder:text-muted-foreground"
                  autoFocus
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void loadComposerModelOptions(true)}
                  disabled={composerModelPickerLoading}
                >
                  Refresh
                </Button>
              </div>
              {composerModelPickerLoading ? (
                <div className="flex items-center gap-2 rounded-xl border border-border/60 bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Loading models...
                </div>
              ) : null}
              {composerModelPickerError ? (
                <div className="rounded-xl border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-sm text-rose-100">
                  {composerModelPickerError}
                </div>
              ) : null}
              <div className="max-h-80 space-y-1 overflow-y-auto rounded-xl border border-border/60 bg-background/70 p-2">
                {filteredComposerModelOptions.length === 0 && !composerModelPickerLoading ? (
                  <div className="px-2 py-4 text-center text-sm text-muted-foreground">
                    No models found{composerModelSearchQuery ? ` for "${composerModelSearchQuery}"` : ""}.
                  </div>
                ) : (
                  filteredComposerModelOptions.map((model) => {
                    const selected = model === activeComposerModel;
                    return (
                      <button
                        key={model}
                        type="button"
                        onClick={() => {
                          setComposerModel(model);
                          setComposerModelPickerOpen(false);
                        }}
                        className={`flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition ${
                          selected
                            ? "bg-accent/20 text-foreground"
                            : "text-muted-foreground hover:bg-accent/10 hover:text-foreground"
                        }`}
                      >
                        <span className="truncate">{model}</span>
                        {selected ? <Check className="h-4 w-4 text-emerald-400" /> : null}
                      </button>
                    );
                  })
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );

  if (isSettingsView) {
    if (showWizard) {
      return (
        <div className="min-h-screen">
            <div className="mx-auto flex min-h-screen max-w-[960px] items-center px-6 py-10">
            <div className="relative w-full">
              <div className="pointer-events-none absolute -left-10 -top-16 h-40 w-40 rounded-full bg-accent/20 blur-3xl animate-[float_14s_ease-in-out_infinite]" />
              <div className="pointer-events-none absolute -right-16 top-8 h-56 w-56 rounded-full bg-amber-400/10 blur-3xl animate-[float_18s_ease-in-out_infinite]" />
              <div className="pointer-events-none absolute -bottom-16 left-12 h-48 w-48 rounded-full bg-amber-500/10 blur-3xl animate-[float_16s_ease-in-out_infinite]" />
              <Card className="relative overflow-hidden">
                <CardHeader>
                  <div className="mb-2 flex justify-end">
                    <Button variant="ghost" size="sm" onClick={() => setActiveView("all")}>
                      <ChevronLeft className="h-4 w-4" />
                      Back to tasks
                    </Button>
                  </div>
                  <div className="flex flex-wrap items-center gap-4">
                    <div className="flex h-20 w-20 items-center justify-center rounded-[28px] bg-card/70">
                      <img src="/gavryn-logo.png" alt="Gavryn" className="h-16 w-16" />
                    </div>
                    <div className="text-xs uppercase tracking-[0.22em] text-muted-foreground">Setup wizard</div>
                    <Sparkles className="h-4 w-4 text-amber-300/80 animate-[glow_2.6s_ease-in-out_infinite]" />
                  </div>
                  <CardTitle>{wizardSteps[wizardStep].title}</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Step {wizardStep + 1} of {wizardSteps.length} — {wizardSteps[wizardStep].label}
                  </p>
                  <p className="text-sm text-muted-foreground">{wizardSteps[wizardStep].description}</p>
                  <div className="mt-2 h-2 w-full overflow-hidden rounded-full border border-border/60 bg-card/40">
                    <div
                      className="h-full rounded-full bg-gradient-to-r from-amber-300 via-amber-400 to-amber-500 transition-[width] duration-500"
                      style={{ width: `${wizardProgress}%` }}
                    />
                  </div>
                </CardHeader>
                <CardContent className="space-y-5">
                {wizardStep === 0 && (
                  <div className="grid gap-6 md:grid-cols-[minmax(0,1fr)_220px]">
                    <div className="space-y-5">
                      <div className="flex flex-col gap-4 rounded-2xl border border-border/60 bg-muted/40 p-5 sm:flex-row sm:items-center">
                        <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-accent/20 text-accent shadow-glow">
                          <Sparkles className="h-5 w-5" />
                        </div>
                        <div>
                          <div className="text-sm font-semibold text-foreground">Your AI control desk</div>
                          <div className="text-sm text-muted-foreground">
                            Gavryn orchestrates agents that research, code, and ship work on your behalf.
                          </div>
                        </div>
                      </div>

                      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                        <div className="rounded-2xl border border-border/60 bg-card/50 p-4">
                          <div className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Tasks</div>
                          <div className="mt-2 text-sm text-muted-foreground">
                            Launch agents, monitor runs, and keep everything organized.
                          </div>
                        </div>
                        <div className="rounded-2xl border border-border/60 bg-card/50 p-4">
                          <div className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Skills</div>
                          <div className="mt-2 text-sm text-muted-foreground">
                            Reusable instructions so agents follow your playbook.
                          </div>
                        </div>
                        <div className="rounded-2xl border border-border/60 bg-card/50 p-4">
                          <div className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Memory</div>
                          <div className="mt-2 text-sm text-muted-foreground">
                            Save what works so agents remember outcomes, preferences, and repeats.
                          </div>
                        </div>
                        <div className="rounded-2xl border border-border/60 bg-card/50 p-4">
                          <div className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Context</div>
                          <div className="mt-2 text-sm text-muted-foreground">
                            Feed brand guides, content libraries, and past posts so output stays on-brand.
                          </div>
                        </div>
                      </div>

                      <div className="text-sm text-muted-foreground">
                        We’ll connect your provider, verify access, and set a default model.
                      </div>
                    </div>
                    <div className="flex items-center justify-center">
                      <img
                        src="/gavryn-mascot.png"
                        alt="Gavryn mascot"
                        className="w-full max-w-[220px] drop-shadow-[0_18px_40px_rgba(0,0,0,0.35)] animate-[float_8s_ease-in-out_infinite]"
                      />
                    </div>
                  </div>
                )}

                {wizardStep === 1 && (
                  <div className="space-y-4">
                    <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                      You’ll pick a provider, add credentials, and select a model. It takes about 2 minutes.
                    </div>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="rounded-2xl border border-border/60 bg-card/50 p-4">
                        <div className="text-xs uppercase tracking-[0.18em] text-muted-foreground">What you need</div>
                        <ul className="mt-2 space-y-2 text-sm text-muted-foreground">
                          <li>API key (OpenAI/OpenRouter/OpenCode Zen) or Codex CLI auth.</li>
                          <li>Preferred model name for new tasks.</li>
                        </ul>
                      </div>
                      <div className="rounded-2xl border border-border/60 bg-card/50 p-4">
                        <div className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Why it matters</div>
                        <div className="mt-2 text-sm text-muted-foreground">
                          Gavryn uses this connection to run agents, stream progress, and save artifacts.
                        </div>
                      </div>
                    </div>
                  </div>
                )}

                {wizardStep === 2 && (
                  <div className="space-y-3">
                    <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Choose mode</div>
                    <select
                      value={llmForm.mode}
                      onChange={(event) => setLlmForm((prev) => ({ ...prev, mode: event.target.value }))}
                      className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                    >
                      <option value="remote">Remote (recommended)</option>
                      <option value="local" disabled>
                        Local (coming soon)
                      </option>
                    </select>
                    <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                      Remote uses your provider credentials. Local mode will be added later.
                    </div>
                  </div>
                )}

                {wizardStep === 3 && (
                  <div className="space-y-3">
                    <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Provider</div>
                    <select
                      value={llmForm.provider}
                      onChange={(event) => {
                        const nextProvider = event.target.value;
                        const nextBaseUrl = providerBaseUrls[nextProvider];
                        setLlmForm((prev) => ({
                          ...prev,
                          provider: nextProvider,
                          api_key: "",
                          base_url: nextBaseUrl !== undefined ? nextBaseUrl : prev.base_url,
                        }));
                        setModelOptions([]);
                      }}
                      className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                    >
                      <option value="codex">OpenAI Codex (CLI auth)</option>
                      <option value="openai">OpenAI API</option>
                      <option value="openrouter">OpenRouter</option>
                      <option value="opencode-zen">OpenCode Zen</option>
                      <option value="kimi-for-coding">Kimi for Coding</option>
                      <option value="moonshot-ai">Moonshot AI</option>
                    </select>
                    {llmForm.provider === "codex" && (
                      <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                        Codex uses your local `codex login` auth file.
                      </div>
                    )}
                  </div>
                )}

                {wizardStep === 4 && (
                  <div className="space-y-3">
                    {llmForm.provider === "codex" ? (
                      <>
                        <div>
                          <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Codex auth path</div>
                          <input
                            value={llmForm.codex_auth_path}
                            onChange={(event) =>
                              setLlmForm((prev) => ({ ...prev, codex_auth_path: event.target.value }))
                            }
                            placeholder="~/.codex/auth.json"
                            className="mt-2 w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                          />
                        </div>
                        <div>
                          <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Codex home</div>
                          <input
                            value={llmForm.codex_home}
                            onChange={(event) => setLlmForm((prev) => ({ ...prev, codex_home: event.target.value }))}
                            placeholder="~/.codex"
                            className="mt-2 w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                          />
                        </div>
                        <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                          Run <span className="font-semibold text-foreground">codex login</span> to create the auth file.
                        </div>
                      </>
                    ) : (
                      <>
                        <div>
                          <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Base URL</div>
                          <input
                            value={llmForm.base_url}
                            onChange={(event) => setLlmForm((prev) => ({ ...prev, base_url: event.target.value }))}
                            placeholder={providerBaseUrls[llmForm.provider] ?? "https://api.openai.com/v1"}
                            className="mt-2 w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                          />
                        </div>
                        <div>
                          <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">API key</div>
                          <input
                            type="password"
                            value={llmForm.api_key}
                            onChange={(event) => setLlmForm((prev) => ({ ...prev, api_key: event.target.value }))}
                            placeholder={llmSettings?.has_api_key ? "Stored" : "sk-..."}
                            className="mt-2 w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                          />
                          {llmSettings?.api_key_hint && (
                            <div className="mt-1 text-xs text-muted-foreground">
                              Stored key ends with {llmSettings.api_key_hint}
                            </div>
                          )}
                        </div>
                      </>
                    )}
                  </div>
                )}

                {wizardStep === 5 && (
                  <div className="space-y-3">
                    <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Model</div>
                    
                    {/* Searchable Model Dropdown */}
                    <div className="relative" ref={modelDropdownRef}>
                      <div className="relative">
                        <input
                          value={modelSearchQuery || llmForm.model}
                          onChange={(event) => {
                            const value = event.target.value;
                            setModelSearchQuery(value);
                            setLlmForm((prev) => ({ ...prev, model: value }));
                            if (!isModelDropdownOpen) {
                              setIsModelDropdownOpen(true);
                            }
                          }}
                          onFocus={() => setIsModelDropdownOpen(true)}
                          placeholder="Search or select a model..."
                          className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 pr-10 text-sm outline-none"
                        />
                        {modelsLoading && (
                          <div className="absolute right-3 top-1/2 -translate-y-1/2">
                            <div className="h-4 w-4 animate-spin rounded-full border-2 border-border border-t-foreground" />
                          </div>
                        )}
                      </div>
                      
                      {isModelDropdownOpen && (
                        <div 
                          className="absolute z-50 mt-1 w-full rounded-xl border border-border/60 bg-card shadow-lg"
                          style={{ maxHeight: '400px', overflowY: 'auto', overflowX: 'hidden' }}
                        >
                          {/* Provider groups */}
                          {(() => {
                            const query = modelSearchQuery.toLowerCase();
                            const filtered = modelOptions.filter((m) => m.toLowerCase().includes(query));
                            
                            if (filtered.length === 0 && modelOptions.length > 0) {
                              return (
                                <div className="px-3 py-2 text-sm text-muted-foreground">
                                  No results for "{modelSearchQuery}"
                                </div>
                              );
                            }
                            
                            if (filtered.length === 0) {
                              return (
                                <div className="px-3 py-2 text-sm text-muted-foreground">
                                  No models loaded. Click "Load models" below.
                                </div>
                              );
                            }
                            
                            // Group models by provider (expanded for all 30+ models)
                            const groups: Record<string, string[]> = {
                              OpenAI: [],
                              Anthropic: [],
                              Google: [],
                              GLM: [],
                              Kimi: [],
                              Other: [],
                            };
                            
                            filtered.forEach((model) => {
                              const lowerModel = model.toLowerCase();
                              if (lowerModel.startsWith('gpt-')) {
                                groups.OpenAI.push(model);
                              } else if (lowerModel.startsWith('claude-')) {
                                groups.Anthropic.push(model);
                              } else if (lowerModel.startsWith('gemini-')) {
                                groups.Google.push(model);
                              } else if (lowerModel.startsWith('glm-')) {
                                groups.GLM.push(model);
                              } else if (lowerModel.startsWith('kimi-')) {
                                groups.Kimi.push(model);
                              } else {
                                groups.Other.push(model);
                              }
                            });
                            
                            return Object.entries(groups).map(([provider, models]) => {
                              if (models.length === 0) return null;
                              return (
                                <div key={provider}>
                                  <div className="bg-muted/80 px-3 py-1 text-xs font-medium uppercase tracking-wider text-muted-foreground backdrop-blur-sm">
                                    {provider}
                                  </div>
                                  {models.map((model) => (
                                    <button
                                      key={model}
                                      type="button"
                                      onClick={() => {
                                        setLlmForm((prev) => ({ ...prev, model }));
                                        setModelSearchQuery("");
                                        setIsModelDropdownOpen(false);
                                      }}
                                      className={`w-full px-3 py-2 text-left text-sm transition hover:bg-accent/10 ${
                                        llmForm.model === model ? 'bg-accent/20 text-foreground' : 'text-muted-foreground'
                                      }`}
                                    >
                                      {model}
                                    </button>
                                  ))}
                                </div>
                              );
                            });
                          })()}
                        </div>
                      )}
                    </div>
                    
                    <div className="flex flex-wrap items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setModelSearchQuery("");
                          fetchModelOptions(llmForm);
                        }}
                        disabled={modelsLoading}
                      >
                        {modelsLoading ? (
                          <>
                            <div className="mr-2 h-3 w-3 animate-spin rounded-full border-2 border-border border-t-foreground" />
                            Loading...
                          </>
                        ) : (
                          <>
                            <Search className="mr-2 h-3 w-3" />
                            Load models
                          </>
                        )}
                      </Button>
                      <span className="text-xs text-muted-foreground">
                        {modelOptions.length > 0 ? `${modelOptions.length} models available` : "Fetch available models from provider"}
                      </span>
                    </div>
                    
                    {llmForm.model && (
                      <div className="flex items-center gap-2">
                        <span className="text-xs uppercase tracking-wider text-muted-foreground">Selected:</span>
                        <span className="rounded-full bg-accent/20 px-3 py-1 text-sm text-foreground">{llmForm.model}</span>
                      </div>
                    )}
                    
                    <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                      This sets the default for new tasks. You can switch models per message in the task composer.
                    </div>
                  </div>
                )}

                {wizardStep === 6 && (
                  <div className="space-y-3">
                    <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Personality</div>
                    <textarea
                      value={personalityDraft}
                      onChange={(event) => setPersonalityDraft(event.target.value)}
                      rows={10}
                      className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                    />
                    <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                      This text is injected into every system prompt. Saved text overrides PERSONALITY.md or defaults.
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <Button size="sm" onClick={() => savePersonalitySettings(personalityDraft)} disabled={personalityBusy}>
                        {personalityBusy ? (
                          <>
                            <div className="mr-2 h-3 w-3 animate-spin rounded-full border-2 border-border border-t-foreground" />
                            Saving...
                          </>
                        ) : (
                          "Save personality"
                        )}
                      </Button>
                      {personalitySettings?.source && (
                        <span className="text-xs text-muted-foreground">Source: {personalitySettings.source}</span>
                      )}
                    </div>
                    {personalityStatus && (
                      <div className="rounded-2xl border border-emerald-500/40 bg-emerald-500/10 px-4 py-2 text-sm text-emerald-100">
                        {personalityStatus}
                      </div>
                    )}
                    {personalityError && (
                      <div className="rounded-2xl border border-rose-500/40 bg-rose-500/10 px-4 py-2 text-sm text-rose-100">
                        {personalityError}
                      </div>
                    )}
                  </div>
                )}

                {wizardStep === 7 && (
                  <div className="space-y-3">
                    <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                      Test your credentials, then save settings.
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <Button variant="outline" size="sm" onClick={testLLMSettings} disabled={llmBusy}>
                        {llmBusy && llmAction === "test" ? (
                          <>
                            <div className="mr-2 h-3 w-3 animate-spin rounded-full border-2 border-border border-t-foreground" />
                            Testing...
                          </>
                        ) : (
                          "Test connection"
                        )}
                      </Button>
                      <Button size="sm" onClick={saveLLMSettings} disabled={llmBusy}>
                        {llmBusy && llmAction === "save" ? (
                          <>
                            <div className="mr-2 h-3 w-3 animate-spin rounded-full border-2 border-border border-t-foreground" />
                            Saving...
                          </>
                        ) : (
                          "Save settings"
                        )}
                      </Button>
                    </div>
                    {llmStatus && (
                      <div className="rounded-2xl border border-emerald-500/40 bg-emerald-500/10 px-4 py-2 text-sm text-emerald-100">
                        {llmStatus}
                      </div>
                    )}
                    {llmError && (
                      <div className="rounded-2xl border border-rose-500/40 bg-rose-500/10 px-4 py-2 text-sm text-rose-100">
                        {llmError}
                      </div>
                    )}
                  </div>
                )}

                <div className="flex items-center justify-between pt-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setWizardStep((prev) => Math.max(prev - 1, 0))}
                    disabled={wizardStep === 0}
                  >
                    Back
                  </Button>
                  <div className="text-xs text-muted-foreground">
                    {wizardStep + 1} / {wizardSteps.length}
                  </div>
                  <Button
                    size="sm"
                    onClick={() => {
                      if (wizardStep === wizardSteps.length - 1) {
                        setActiveView("all");
                        return;
                      }
                      setWizardStep((prev) => Math.min(prev + 1, wizardSteps.length - 1));
                    }}
                    disabled={
                      (wizardStep === wizardSteps.length - 1 && !hasLLMConfig) ||
                      (wizardStep === 4 &&
                        needsAPIKey &&
                        !llmForm.api_key &&
                        !llmSettings?.has_api_key)
                    }
                  >
                    Next
                  </Button>
                </div>
              </CardContent>
              </Card>
            </div>
          </div>
        </div>
      );
    }
    return (
      <div className="min-h-screen">
        <div className="mx-auto flex min-h-screen max-w-[960px] items-center px-6 py-10">
          <div className="w-full space-y-6">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Settings</div>
                <div className="text-2xl font-semibold text-foreground">Control desk settings</div>
              </div>
              <Button variant="outline" size="sm" onClick={() => setActiveView("all")}>
                <ChevronLeft className="h-4 w-4" />
                Back to tasks
              </Button>
            </div>
            <Card>
              <CardHeader>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <CardTitle>Connection</CardTitle>
                    <p className="text-sm text-muted-foreground">Manage your provider defaults and credentials.</p>
                  </div>
                  <Button
                    size="sm"
                    onClick={() => {
                      setWizardStep(0);
                      setShowSetupWizard(true);
                    }}
                  >
                    Run setup wizard
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="grid gap-4 md:grid-cols-2">
                <div>
                  <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Mode</div>
                  <div className="mt-1 text-sm font-semibold text-foreground">
                    {llmSettings?.mode || "remote"}
                  </div>
                </div>
                <div>
                  <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Provider</div>
                  <div className="mt-1 text-sm font-semibold text-foreground">
                    {llmSettings?.provider || "codex"}
                  </div>
                </div>
                <div>
                  <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Model</div>
                  <div className="mt-1 text-sm font-semibold text-foreground">
                    {llmSettings
                      ? displayModelName(llmSettings.provider, llmSettings.model)
                      : "gpt-5.2-codex"}
                  </div>
                </div>
                <div>
                  <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Base URL</div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    {llmSettings?.base_url || providerBaseUrls[llmSettings?.provider || "openai"]}
                  </div>
                </div>
                <div>
                  <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">API key</div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    {llmSettings?.has_api_key
                      ? `Stored${llmSettings.api_key_hint ? ` (ends with ${llmSettings.api_key_hint})` : ""}`
                      : "Not set"}
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <CardTitle>Memory</CardTitle>
                    <p className="text-sm text-muted-foreground">Store high-value context from your runs.</p>
                  </div>
                  {memorySettings && (
                    <Button
                      size="sm"
                      variant={memorySettings.enabled ? "outline" : "default"}
                      onClick={() => updateMemorySettings(!memorySettings.enabled)}
                    >
                      {memorySettings.enabled ? "Disable" : "Enable"}
                    </Button>
                  )}
                </div>
              </CardHeader>
              <CardContent>
                {memorySettings ? (
                  <div className="text-sm text-muted-foreground">
                    Memory is currently {memorySettings.enabled ? "enabled" : "disabled"}.
                  </div>
                ) : (
                  <div className="text-sm text-muted-foreground">Loading memory settings...</div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <CardTitle>Personality</CardTitle>
                    <p className="text-sm text-muted-foreground">Customize the system instructions injected into chats.</p>
                  </div>
                  <Button size="sm" onClick={() => savePersonalitySettings(personalityDraft)} disabled={personalityBusy}>
                    {personalityBusy ? "Saving..." : "Save"}
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <textarea
                  value={personalityDraft}
                  onChange={(event) => setPersonalityDraft(event.target.value)}
                  rows={10}
                  className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                />
                <div className="text-xs text-muted-foreground">
                  Source: {personalitySettings?.source || "default"}
                </div>
                {personalityStatus && (
                  <div className="rounded-2xl border border-emerald-500/40 bg-emerald-500/10 px-4 py-2 text-sm text-emerald-100">
                    {personalityStatus}
                  </div>
                )}
                {personalityError && (
                  <div className="rounded-2xl border border-rose-500/40 bg-rose-500/10 px-4 py-2 text-sm text-rose-100">
                    {personalityError}
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-background">
      <aside
        className={`flex h-full flex-shrink-0 flex-col border-b border-border/60 bg-card/50 py-6 transition-all duration-300 ease-in-out lg:border-b-0 lg:border-r ${
          isSidebarCollapsed ? "w-[80px] px-2" : "w-full lg:w-80 px-6"
        }`}
      >
        <div className={`flex items-center gap-3 ${isSidebarCollapsed ? "flex-col justify-center" : ""}`}>
          <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-accent/20 text-accent shadow-glow">
            <Sparkles className="h-5 w-5" />
          </div>
          {!isSidebarCollapsed && (
            <div className="flex-1 overflow-hidden">
              <div className="text-sm uppercase tracking-[0.18em] text-muted-foreground truncate">Gavryn</div>
              <div className="font-display text-xl truncate">Control Desk</div>
            </div>
          )}
          <button
            onClick={() => setIsSidebarCollapsed(!isSidebarCollapsed)}
            className={`hidden lg:flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors ${
              isSidebarCollapsed ? "mt-2" : ""
            }`}
          >
            {isSidebarCollapsed ? <ChevronRight className="h-5 w-5" /> : <ChevronLeft className="h-5 w-5" />}
          </button>
        </div>

          <div className="mt-6 space-y-3">
            <Button
              className={`w-full ${isSidebarCollapsed ? "justify-center px-0" : "justify-between"}`}
              size="lg"
              onClick={startNewTask}
              title="New task"
            >
              {isSidebarCollapsed ? (
                <Plus className="h-5 w-5" />
              ) : (
                <>
                  <span className="flex items-center gap-2">
                    <Plus className="h-4 w-4" />
                    New task
                  </span>
                  <Sparkles className="h-4 w-4 text-accent-foreground/70" />
                </>
              )}
            </Button>
            {!isSidebarCollapsed && (
              <div className="flex items-center gap-2 rounded-2xl border border-border/60 bg-background/60 px-3 py-2">
                <Search className="h-4 w-4 text-muted-foreground" />
                <input
                  value={searchQuery}
                  onChange={(event) => setSearchQuery(event.target.value)}
                  placeholder="Search tasks or tags"
                  className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
                />
              </div>
            )}
          </div>

          <nav className="mt-6 space-y-1 text-sm">
            <button
              type="button"
              onClick={() => setActiveView("all")}
              className={`flex w-full items-center ${isSidebarCollapsed ? "justify-center px-0" : "justify-between px-3"} rounded-xl py-2 transition ${
                activeView === "all"
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60"
              }`}
              title="All tasks"
            >
              <span className="flex items-center gap-2">
                <ListTodo className="h-4 w-4" />
                {!isSidebarCollapsed && "All tasks"}
              </span>
              {!isSidebarCollapsed && <span className="text-xs text-muted-foreground">{tasks.length}</span>}
            </button>
            <button
              type="button"
              onClick={() => setActiveView("library")}
              className={`flex w-full items-center ${isSidebarCollapsed ? "justify-center px-0" : "justify-between px-3"} rounded-xl py-2 transition ${
                activeView === "library"
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60"
              }`}
              title="Library"
            >
              <span className="flex items-center gap-2">
                <Library className="h-4 w-4" />
                {!isSidebarCollapsed && "Library"}
              </span>
              {!isSidebarCollapsed && <span className="text-xs text-muted-foreground">{sortedLibraryDocuments.length}</span>}
            </button>
            <button
              type="button"
              onClick={() => setActiveView("automations")}
              className={`flex w-full items-center ${isSidebarCollapsed ? "justify-center px-0" : "justify-between px-3"} rounded-xl py-2 transition ${
                isAutomationsView
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60"
              }`}
              title="Automations"
            >
              <span className="flex items-center gap-2">
                <CalendarClock className="h-4 w-4" />
                {!isSidebarCollapsed && "Automations"}
              </span>
              {!isSidebarCollapsed && automationUnreadCount > 0 ? (
                <span className="rounded-full bg-rose-500/15 px-2 py-0.5 text-[10px] font-semibold text-rose-300">
                  {automationUnreadCount}
                </span>
              ) : null}
            </button>
            <button
              type="button"
              onClick={() => setActiveView("skills")}
              className={`flex w-full items-center ${isSidebarCollapsed ? "justify-center px-0" : "justify-between px-3"} rounded-xl py-2 transition ${
                activeView === "skills"
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60"
              }`}
              title="Skills"
            >
              <span className="flex items-center gap-2">
                <LayoutGrid className="h-4 w-4" />
                {!isSidebarCollapsed && "Skills"}
              </span>
              {!isSidebarCollapsed && <span className="text-xs text-muted-foreground">{skills.length}</span>}
            </button>
            <button
              type="button"
              onClick={() => setActiveView("context")}
              className={`flex w-full items-center ${isSidebarCollapsed ? "justify-center px-0" : "justify-between px-3"} rounded-xl py-2 transition ${
                activeView === "context"
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60"
              }`}
              title="Context"
            >
              <span className="flex items-center gap-2">
                <Monitor className="h-4 w-4" />
                {!isSidebarCollapsed && "Context"}
              </span>
              {!isSidebarCollapsed && <span className="text-xs text-muted-foreground">{contextNodes.length}</span>}
            </button>
            <button
              type="button"
              onClick={() => setActiveView("settings")}
              className={`flex w-full items-center ${isSidebarCollapsed ? "justify-center px-0" : "justify-between px-3"} rounded-xl py-2 transition ${
                activeView === "settings"
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60"
              }`}
              title="Settings"
            >
              <span className="flex items-center gap-2">
                <SlidersHorizontal className="h-4 w-4" />
                {!isSidebarCollapsed && "Settings"}
              </span>
            </button>
          </nav>

          <div className="mt-6">
            {!isSidebarCollapsed ? (
              <>
                <div className="flex items-center justify-between text-xs uppercase tracking-[0.2em] text-muted-foreground">
                  <span>Projects</span>
              <button
                type="button"
                className="rounded-full border border-border/60 px-2 py-1"
                onClick={() => setProjectFormOpen((prev) => !prev)}
              >
                <Plus className="h-3 w-3" />
              </button>
            </div>
            {projectFormOpen && (
              <div className="mt-3 rounded-2xl border border-border/60 bg-card/60 p-3">
                <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">
                  New project
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <input
                    value={newProjectName}
                    onChange={(event) => setNewProjectName(event.target.value)}
                    placeholder="Project name"
                    className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                  />
                  <Button size="sm" onClick={createProject}>
                    Add
                  </Button>
                </div>
              </div>
            )}
            <div className="mt-3 space-y-1">
              {projects.length === 0 ? (
                <div className="rounded-2xl border border-dashed border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                  Create a project to group your tasks.
                </div>
              ) : (
                projects.map((project) => (
                  <div
                    key={project.id}
                    className={`flex items-center justify-between rounded-xl px-3 py-2 text-sm transition ${
                      activeView === project.id
                        ? "bg-muted text-foreground"
                        : "text-muted-foreground hover:bg-muted/60"
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => {
                        setActiveView(project.id);
                        setSelectedProjectId(project.id);
                      }}
                      className="flex flex-1 items-center gap-2 text-left"
                    >
                      <span className={`h-2 w-2 rounded-full ${projectTones[project.tone].dot}`} />
                      {project.name}
                    </button>
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">
                        {projectCounts[project.id] || 0}
                      </span>
                      <button
                        type="button"
                        onClick={() => deleteProject(project.id)}
                        className="rounded-full border border-border/60 p-1 text-muted-foreground hover:text-foreground"
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
              </>
            ) : (
              <div className="flex flex-col items-center gap-3">
                <div className="h-px w-8 bg-border/60" />
                <button
                  type="button"
                  className="rounded-full border border-border/60 p-1.5 text-muted-foreground hover:text-foreground"
                  onClick={() => setProjectFormOpen(!projectFormOpen)}
                  title="New project"
                >
                  <Plus className="h-4 w-4" />
                </button>
                {projects.map((project) => (
                  <div
                    key={project.id}
                    className={`h-2.5 w-2.5 rounded-full ring-2 ring-transparent transition-all hover:ring-border ${projectTones[project.tone].dot} cursor-pointer`}
                    title={project.name}
                    onClick={() => {
                      setActiveView(project.id);
                      setSelectedProjectId(project.id);
                    }}
                  />
                ))}
              </div>
            )}
          </div>

          {!isSidebarCollapsed && (
            <div className="mt-6">
              <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Tasks</div>
            <div className="mt-3 space-y-2">
              {filteredTasks.length === 0 ? (
                <div className="rounded-2xl border border-dashed border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                  No tasks yet. Start a run to create your first task.
                </div>
              ) : (
                filteredTasks.map((task) => {
                  const isActive = task.id === activeTaskId;
                  return (
                    <div
                      key={task.id}
                      className={`rounded-xl border px-3 py-2 transition ${
                        isActive
                          ? "border-accent/60 bg-accent/10"
                          : "border-border/60 bg-card/40 hover:border-border"
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <button
                          type="button"
                          onClick={() => selectTask(task)}
                          className="flex min-w-0 flex-1 items-center gap-2 text-left"
                        >
                          <TaskStatusGlyph status={task.status} />
                          <span className="truncate text-sm text-foreground">{task.title}</span>
                        </button>
                        <div className="flex items-center gap-1">
                          <button
                            type="button"
                            onClick={() => {
                              setTagEditorTaskId(task.id);
                              setTagEditorValue("");
                            }}
                            className="rounded-full border border-border/60 p-1 text-muted-foreground hover:text-foreground"
                          >
                            <Tag className="h-3 w-3" />
                          </button>
                          <button
                            type="button"
                            onClick={() => deleteTask(task.id)}
                            className="rounded-full border border-border/60 p-1 text-muted-foreground hover:text-foreground"
                          >
                            <Trash2 className="h-3 w-3" />
                          </button>
                        </div>
                      </div>
                      {tagEditorTaskId === task.id && (
                        <div className="mt-2 flex flex-wrap gap-2">
                          {task.tags.map((tag) => (
                            <span
                              key={`${task.id}-chip-${tag}`}
                              className={`flex items-center gap-1 rounded-full border px-2 py-1 text-[11px] ${tagClass(tag)}`}
                            >
                              <button
                                type="button"
                                onClick={() => cycleTagColor(tag)}
                                className="rounded-full border border-current/30 p-0.5"
                              >
                                <span className="h-1.5 w-1.5 rounded-full bg-current" />
                              </button>
                              {tag}
                              <button
                                type="button"
                                onClick={() => removeTagFromTask(task.id, tag)}
                                className="rounded-full border border-current/30 p-0.5"
                              >
                                <X className="h-2.5 w-2.5" />
                              </button>
                            </span>
                          ))}
                          <div className="flex items-center gap-1 rounded-full border border-border/60 bg-card/60 px-2 py-1">
                            <input
                              value={tagEditorValue}
                              onChange={(event) => setTagEditorValue(event.target.value)}
                              onKeyDown={(event) => {
                                if (event.key === "Enter") {
                                  event.preventDefault();
                                  handleTagEditorAdd(task.id);
                                }
                              }}
                              placeholder="Tag"
                              className="w-16 bg-transparent text-[11px] outline-none placeholder:text-muted-foreground"
                            />
                            <button
                              type="button"
                              onClick={() => handleTagEditorAdd(task.id)}
                              className="rounded-full border border-border/60 p-0.5 text-muted-foreground hover:text-foreground"
                            >
                              <Plus className="h-3 w-3" />
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })
              )}
            </div>
          </div>
          )}
        </aside>

        <main className="flex-1 min-w-0 overflow-hidden">
            <div className="mx-auto flex h-full max-w-[1080px] flex-col px-6 py-6 lg:px-10">
            <header className="flex flex-wrap items-center justify-between gap-4 mb-6">
              <div className="flex flex-col gap-1 min-w-0 flex-1">
                {(isLibraryView || isAutomationsView || isSettingsView || isSkillsView || isContextView || !activeTask) ? (
                  <>
                    <span className="text-xs uppercase tracking-[0.2em] text-muted-foreground truncate">
                      {isLibraryView
                        ? "Library"
                        : isAutomationsView
                          ? "Automations"
                        : isSettingsView
                          ? showWizard
                            ? "Setup"
                            : "Settings"
                          : isSkillsView
                            ? "Skills"
                            : isContextView
                              ? "Context"
                              : "Control Desk"}
                    </span>
                    <h1 className="font-display text-2xl truncate">
                      {isLibraryView
                        ? "Artifacts library"
                        : isAutomationsView
                          ? "Automations"
                        : isSettingsView
                          ? showWizard
                            ? "LLM setup"
                            : "Settings"
                          : isSkillsView
                            ? "Skill library"
                            : isContextView
                              ? "Context library"
                              : "What can I do for you?"}
                    </h1>
                  </>
                ) : null}
              </div>
              
              {!isLibraryView && !isAutomationsView && !isSettingsView && !isSkillsView && !isContextView && activeTask && (
                <div className="flex items-center gap-2 rounded-lg border border-border/60 bg-muted/40 p-1">
                  <Button
                    variant={activePanel === "overview" ? "secondary" : "ghost"}
                    size="sm"
                    onClick={() => setActivePanel("overview")}
                    className="h-8 gap-1.5 px-3 text-xs font-medium"
                  >
                    <LayoutGrid className="h-3.5 w-3.5" />
                    Overview
                  </Button>
                  <div className="h-4 w-px bg-border/60" />
                  <Button
                    variant={activePanel === "browser" ? "secondary" : "ghost"}
                    size="sm"
                    onClick={() => setActivePanel("browser")}
                    className="h-8 gap-1.5 px-3 text-xs font-medium"
                  >
                    <Monitor className="h-3.5 w-3.5" />
                    Browser
                  </Button>
                  <Button
                    variant={activePanel === "files" ? "secondary" : "ghost"}
                    size="sm"
                    onClick={() => setActivePanel("files")}
                    className="h-8 gap-1.5 px-3 text-xs font-medium"
                  >
                    <FileCode className="h-3.5 w-3.5" />
                    Files
                  </Button>
                  <Button
                    variant={activePanel === "editor" ? "secondary" : "ghost"}
                    size="sm"
                    onClick={() => setActivePanel("editor")}
                    className="h-8 gap-1.5 px-3 text-xs font-medium"
                  >
                    <PencilLine className="h-3.5 w-3.5" />
                    Editor
                  </Button>
                </div>
              )}
            </header>

            <div className="flex-1 min-h-0 overflow-hidden">
          {isLibraryView ? (
            <section className="h-full overflow-auto rounded-3xl border border-border/60 bg-card/60 p-6">
              <div className="flex items-center justify-between">
                <div className="text-sm text-muted-foreground">
                  Artifacts created by agents across your tasks.
                </div>
                <div className="text-xs text-muted-foreground">
                  {sortedLibraryDocuments.length} items
                </div>
              </div>
              <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                {sortedLibraryDocuments.length === 0 ? (
                  <div className="col-span-full rounded-2xl border border-dashed border-border/60 bg-muted/40 p-6 text-sm text-muted-foreground">
                    No documents yet. Markdown and Word documents from tasks will appear here.
                  </div>
                ) : (
                  sortedLibraryDocuments.map((item) => {
                    const task = item.taskId ? tasks.find((task) => task.id === item.taskId) : undefined;
                    const project = task?.projectId ? projectById.get(task.projectId) : undefined;
                    return (
                      <div key={item.id || item.uri} className="overflow-hidden rounded-2xl border border-border/60 bg-card/70">
                        {item.contentType?.startsWith("image/") ? (
                          <img src={item.uri} alt={item.label} className="h-40 w-full object-cover" />
                        ) : (
                          <div className="flex h-40 items-center justify-center bg-muted/40 text-sm text-muted-foreground">
                            {item.label}
                          </div>
                        )}
                        <div className="space-y-2 p-4">
                          <div className="flex items-center justify-between">
                            <div className="text-sm font-semibold text-foreground">{item.label}</div>
                            <span className="text-xs text-muted-foreground">{formatDate(item.createdAt)}</span>
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {item.taskTitle || task?.title || "Untitled task"}
                          </div>
                          <div className="flex items-center justify-between text-xs text-muted-foreground">
                            <span>{project?.name || "Unassigned"}</span>
                            <a
                              href={item.uri}
                              target="_blank"
                              rel="noreferrer"
                              className="text-xs font-semibold text-foreground hover:underline"
                            >
                              Open
                            </a>
                          </div>
                        </div>
                      </div>
                    );
                  })
                )}
              </div>
            </section>
          ) : isAutomationsView ? (
            <AutomationsPanel
              defaultModel={activeComposerModel}
              modelOptions={modelOptions}
              onUnreadCountChange={setAutomationUnreadCount}
            />
          ) : isSkillsView ? (
            <section className="h-full overflow-auto grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
              <Card>
                <CardHeader className="flex flex-row items-center justify-between">
                  <div>
                    <CardTitle>Skill library</CardTitle>
                    <p className="text-sm text-muted-foreground">
                      Manage reusable skill instructions and reference files.
                    </p>
                  </div>
                  <Button variant="outline" size="sm" onClick={() => setShowSkillsInfo((prev) => !prev)}>
                    {showSkillsInfo ? "Hide" : "Info"}
                  </Button>
                </CardHeader>
                <CardContent className="space-y-6">
                  {showSkillsInfo && (
                    <div className="rounded-2xl border border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                      Skills live at <span className="font-semibold text-foreground">~/.config/opencode/skills/&lt;skill-name&gt;</span>.
                      Each skill requires a <span className="font-semibold text-foreground">SKILL.md</span>. Optional folders like
                      <span className="font-semibold text-foreground"> references/</span> and <span className="font-semibold text-foreground">scripts/</span>
                      are supported.
                    </div>
                  )}

                  <div className="space-y-3">
                    <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Create skill</div>
                    <input
                      value={newSkillName}
                      onChange={(event) => setNewSkillName(event.target.value)}
                      placeholder="skill-name"
                      className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                    />
                    <input
                      value={newSkillDescription}
                      onChange={(event) => setNewSkillDescription(event.target.value)}
                      placeholder="Short description"
                      className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                    />
                    <textarea
                      value={newSkillContent}
                      onChange={(event) => setNewSkillContent(event.target.value)}
                      placeholder="SKILL.md content"
                      className="min-h-[120px] w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                    />
                    <Button onClick={createSkill}>Create skill</Button>
                  </div>

                  <div className="space-y-3">
                    <div className="flex items-center justify-between text-xs uppercase tracking-[0.2em] text-muted-foreground">
                      <span>Skills</span>
                      <span>{skills.length}</span>
                    </div>
                    {skillsLoading ? (
                      <div className="text-sm text-muted-foreground">Loading skills...</div>
                    ) : skills.length === 0 ? (
                      <div className="rounded-2xl border border-dashed border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                        No skills yet. Create your first skill to get started.
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {skills.map((skill) => (
                          <button
                            key={skill.id}
                            type="button"
                            onClick={() => setActiveSkillId(skill.id)}
                            className={`flex w-full items-center justify-between rounded-xl border px-3 py-2 text-left text-sm transition ${
                              activeSkillId === skill.id
                                ? "border-accent/60 bg-accent/10"
                                : "border-border/60 bg-card/40 hover:border-border"
                            }`}
                          >
                            <div>
                              <div className="font-semibold text-foreground">{skill.name}</div>
                              <div className="text-xs text-muted-foreground">{skill.description || "No description"}</div>
                            </div>
                            <span className="text-xs text-muted-foreground">
                              {formatDate(skill.updatedAt)}
                            </span>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>

                  {skillsError && (
                    <div className="rounded-2xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-100">
                      {skillsError}
                    </div>
                  )}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>Active skill</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  {!activeSkill ? (
                    <div className="rounded-2xl border border-dashed border-border/60 bg-muted/40 p-6 text-sm text-muted-foreground">
                      Select a skill to view or edit its files.
                    </div>
                  ) : (
                    <>
                      <div className="space-y-2">
                        <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Metadata</div>
                        <input
                          value={editSkillName}
                          onChange={(event) => setEditSkillName(event.target.value)}
                          className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                        />
                        <input
                          value={editSkillDescription}
                          onChange={(event) => setEditSkillDescription(event.target.value)}
                          className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                        />
                        <Button variant="outline" onClick={saveSkillMetadata}>
                          Save metadata
                        </Button>
                      </div>

                      <div className="space-y-2">
                        <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">SKILL.md</div>
                        <textarea
                          value={editSkillContent}
                          onChange={(event) => setEditSkillContent(event.target.value)}
                          className="min-h-[200px] w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                        />
                        <Button onClick={saveSkillContent}>Save SKILL.md</Button>
                      </div>

                      <div className="space-y-2">
                        <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Upload files</div>
                        <div
                          onDragOver={(event) => {
                            event.preventDefault();
                            setSkillDropActive(true);
                          }}
                          onDragLeave={() => setSkillDropActive(false)}
                          onDrop={handleSkillDrop}
                          className={`rounded-2xl border border-dashed px-4 py-4 text-sm transition ${
                            skillDropActive
                              ? "border-accent/70 bg-accent/10 text-foreground"
                              : "border-border/60 bg-muted/40 text-muted-foreground"
                          }`}
                        >
                          Drop files or folders here
                          <input
                            type="file"
                            multiple
                            onChange={(event) => {
                              uploadSkillFiles(event.target.files);
                              event.currentTarget.value = "";
                            }}
                            className="mt-3 w-full text-sm text-muted-foreground"
                          />
                          <input
                            ref={skillsFolderInputRef}
                            type="file"
                            multiple
                            onChange={(event) => {
                              uploadSkillFiles(event.target.files);
                              event.currentTarget.value = "";
                            }}
                            className="mt-2 w-full text-sm text-muted-foreground"
                          />
                        </div>
                      </div>

                      <div className="space-y-2">
                        <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Files</div>
                        {skillFiles.length === 0 ? (
                          <div className="text-sm text-muted-foreground">No files uploaded yet.</div>
                        ) : (
                          <div className="space-y-2">
                            {skillFiles.map((file) => (
                              <div
                                key={file.path}
                                className="flex items-center justify-between rounded-xl border border-border/60 bg-card/40 px-3 py-2 text-sm"
                              >
                                <div>
                                  <div className="font-semibold text-foreground">{file.path}</div>
                                  <div className="text-xs text-muted-foreground">
                                    {Math.round(file.size_bytes / 1024)} KB
                                  </div>
                                </div>
                                <span className="text-xs text-muted-foreground">{formatDate(file.updated_at)}</span>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>

                      <Button variant="destructive" onClick={removeSkill}>
                        Delete skill
                      </Button>
                    </>
                  )}
                </CardContent>
              </Card>
            </section>
          ) : isContextView ? (
            <section className="h-full overflow-auto grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
              <Card>
                <CardHeader>
                  <CardTitle>Context tree</CardTitle>
                  <p className="text-sm text-muted-foreground">
                    Organize files and folders that inform your tasks.
                  </p>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-2">
                    <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Add folder</div>
                    <div className="flex items-center gap-2">
                      <input
                        value={newFolderName}
                        onChange={(event) => setNewFolderName(event.target.value)}
                        placeholder="Folder name"
                        className="w-full rounded-xl border border-border/60 bg-background/60 px-3 py-2 text-sm outline-none"
                      />
                      <Button size="sm" onClick={createContextFolder}>
                        Add
                      </Button>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      New folders are created under the selected folder.
                    </div>
                  </div>

                  <div className="space-y-2">
                    <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Upload files</div>
                    <input
                      type="file"
                      multiple
                      onChange={(event) => {
                        uploadContextFiles(event.target.files);
                        event.currentTarget.value = "";
                      }}
                      className="w-full text-sm text-muted-foreground"
                    />
                    <div className="text-xs text-muted-foreground">
                      Files are uploaded into the selected folder.
                    </div>
                  </div>

                  <div className="space-y-2">
                    <div className="flex items-center justify-between text-xs uppercase tracking-[0.2em] text-muted-foreground">
                      <span>Nodes</span>
                      <span>{contextNodes.length}</span>
                    </div>
                    {contextLoading ? (
                      <div className="text-sm text-muted-foreground">Loading context...</div>
                    ) : contextNodes.length === 0 ? (
                      <div className="rounded-2xl border border-dashed border-border/60 bg-muted/40 p-4 text-sm text-muted-foreground">
                        No context yet. Add folders or upload files.
                      </div>
                    ) : (
                      <div className="space-y-2">{renderContextTree(null, 0)}</div>
                    )}
                  </div>

                  {contextError && (
                    <div className="rounded-2xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-100">
                      {contextError}
                    </div>
                  )}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>Details</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  {!selectedContext ? (
                    <div className="rounded-2xl border border-dashed border-border/60 bg-muted/40 p-6 text-sm text-muted-foreground">
                      Select a folder or file to see details.
                    </div>
                  ) : (
                    <div className="space-y-2">
                      <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Name</div>
                      <div className="text-sm font-semibold text-foreground">{selectedContext.name}</div>
                      <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Type</div>
                      <div className="text-sm text-muted-foreground">{selectedContext.node_type}</div>
                      {selectedContext.content_type && (
                        <>
                          <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Content type</div>
                          <div className="text-sm text-muted-foreground">{selectedContext.content_type}</div>
                        </>
                      )}
                      {selectedContext.size_bytes ? (
                        <>
                          <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Size</div>
                          <div className="text-sm text-muted-foreground">
                            {Math.round(selectedContext.size_bytes / 1024)} KB
                          </div>
                        </>
                      ) : null}
                      <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Updated</div>
                      <div className="text-sm text-muted-foreground">{formatDate(selectedContext.updated_at)}</div>
                    </div>
                  )}
                </CardContent>
              </Card>
            </section>
          ) : (
            <div className="h-full flex flex-col min-h-0">
              {activeTask ? (
                <>
                  {showMemoryPrompt && (
                    <Card className="mb-4 shrink-0">
                      <CardHeader>
                        <CardTitle>Enable memory</CardTitle>
                        <p className="text-sm text-muted-foreground">
                          Save important project details so future tasks can reuse them.
                        </p>
                      </CardHeader>
                      <CardContent className="flex flex-wrap items-center gap-2">
                        <Button
                          size="sm"
                          onClick={() => {
                            updateMemorySettings(true);
                            setMemoryPromptDismissed(true);
                          }}
                        >
                          Enable memory
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setMemoryPromptDismissed(true)}
                        >
                          Not now
                        </Button>
                      </CardContent>
                    </Card>
                  )}

                  {activePanel === "overview" && (
                    <Card className="flex min-h-0 flex-1 flex-col">
                      <CardContent className="flex min-h-0 flex-1 flex-col overflow-hidden p-0">
                        <div className="flex min-h-0 flex-1 overflow-hidden">
                          <Messages
                            messages={messages}
                            events={eventsByRun[activeTask?.runId || ""] || []}
                            isThinking={awaitingResponse}
                            bottomInset={messageBottomInset}
                          />
                        </div>
                        <div
                          ref={composerContainerRef}
                          className="shrink-0 space-y-3 border-t border-border/60 bg-card p-4 pt-4"
                        >
                          {composer}
                        </div>
                      </CardContent>
                    </Card>
                  )}

                  {activePanel === "browser" && (
                    <div className="h-full min-h-0 rounded-2xl border border-border/60 bg-card overflow-hidden">
                      <BrowserPanel
                        events={eventsByRun[activeTask?.runId || ""] || []}
                        latestSnapshotUri={panels.latestBrowserUri}
                      />
                    </div>
                  )}

                  {activePanel === "files" && (
                    <TaskFilesPanel items={activeTaskLibraryItems} />
                  )}

                  {activePanel === "editor" && (
                    <div className="h-full min-h-0 rounded-2xl border border-border/60 bg-card overflow-hidden">
                      <EditorPanel
                        runId={activeTask?.runId || ""}
                        artifacts={activeTaskLibraryItems}
                        onFixNow={handleEditorFixNow}
                      />
                    </div>
                  )}
                </>
              ) : (
                <section className="rounded-3xl border border-border/60 bg-card/60 p-6 md:p-8">
                  <div>{composer}</div>

                  <div className="mt-6 flex flex-wrap gap-2">
                    {quickActions.map((action) => (
                      <button
                        key={action}
                        type="button"
                        className="rounded-full border border-border/60 bg-card/60 px-4 py-2 text-xs text-muted-foreground transition hover:border-border"
                        onClick={() => setInput(action)}
                      >
                        {action}
                      </button>
                    ))}
                  </div>
                </section>
              )}
            </div>
          )}
          </div>
        </div>
      </main>
    </div>
  );
}

export function collectLibraryItems(event: RunEvent, task?: Task) {
  const type = normalizeEventType(event.type);
  const items: LibraryItem[] = [];
  const runId = event.run_id;
  const taskId = task?.id;
  const taskTitle = task?.title;
  const createdAt = event.ts || event.timestamp || new Date().toISOString();

  if (type === "browser.snapshot") {
    if (event.payload?.transient) {
      return items;
    }
    const uri = event.payload?.uri;
    if (typeof uri === "string") {
      items.push({
        id: String(event.payload?.artifact_id || `browser-${event.seq}`),
        label: "Browser snapshot",
        type: "browser.snapshot",
        uri,
        contentType: event.payload?.content_type,
        createdAt,
        runId,
        taskId,
        taskTitle,
      });
    }
  }

  if (type === "tool.completed") {
    const artifacts = event.payload?.artifacts;
    if (Array.isArray(artifacts)) {
      for (const artifact of artifacts) {
        const uri = artifact?.uri;
        if (typeof uri !== "string") continue;
        const contentType = artifact?.content_type;
        const type = artifact?.type || "artifact";
        const explicitLabel =
          (typeof artifact?.label === "string" && artifact.label.trim()) ||
          (typeof artifact?.name === "string" && artifact.name.trim()) ||
          (typeof artifact?.file_name === "string" && artifact.file_name.trim()) ||
          "";
        const uriFilename = artifactFilenameFromUri(uri);
        items.push({
          id: String(artifact?.artifact_id || `${event.seq}-${type}`),
          label: explicitLabel || uriFilename || titleFromArtifact(type, contentType),
          type,
          uri,
          contentType,
          createdAt,
          runId,
          taskId,
          taskTitle,
        });
      }
    }
  }

  return items;
}

function stringifyDisplayValue(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (value === undefined || value === null) {
    return "";
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function looksLikeProtocolMessage(content: string) {
  const normalized = content.trim().toLowerCase();
  if (!normalized) return false;
  if (normalized.startsWith("tool result:")) return true;
  if (normalized.startsWith("```tool")) return true;
  if (normalized.startsWith("```json") && normalized.includes("tool_calls")) return true;
  if (normalized.includes("\"tool_calls\"") && normalized.includes("\"tool_name\"")) return true;
  if (normalized.startsWith("event:") || normalized.startsWith("data:{")) return true;
  return false;
}

export function applyEvent(prev: ChatMessage[], event: RunEvent): ChatMessage[] {
  const type = normalizeEventType(event.type);
  if (type === "message.added") {
    const role = (event.payload?.role || "assistant") as ChatMessage["role"];
    const rawContent = event.payload?.content;
    let content = "";
    if (typeof rawContent === "string") {
      content = rawContent.trim();
    } else if (rawContent !== undefined && rawContent !== null) {
      content = stringifyDisplayValue(rawContent).trim();
    }
    if (!content || role === "system" || (role !== "user" && role !== "assistant")) {
      return prev;
    }
    if (role === "assistant" && looksLikeProtocolMessage(content)) {
      return prev;
    }
    const id = String(event.payload?.message_id || `${event.seq}`);
    return [...prev, { id, role, content, seq: event.seq }];
  }

  if (type === "tool.completed") {
    // Tool events are rendered from typed run events in the activity feed.
    // Avoid synthesizing extra chat messages that duplicate the same action.
    return prev;
  }

  if (type === "model.token") {
    const token = String(event.payload?.text || "");
    if (!token) return prev;
    const next = [...prev];
    const last = next[next.length - 1];
    if (last && last.role === "assistant" && last.streaming) {
      last.content += token;
      return next;
    }
    const newMessage: ChatMessage = {
      id: `stream-${event.seq}`,
      role: "assistant",
      content: token,
      seq: event.seq,
      streaming: true,
    };
    next.push(newMessage);
    return next;
  }

  if (type === "model.summary") {
    const summary = String(event.payload?.text || "");
    
    // Find the streaming message
    const streamingIndex = prev.findIndex((m) => m.streaming);
    
    // model.summary is rendered as a reasoning row; do not add a separate chat bubble.
    if (streamingIndex === -1) {
      return prev;
    }

    const streamingMsg = prev[streamingIndex];
    
    // Check if the streaming message looks like a tool call
    const isToolCall = 
      streamingMsg.content.includes('"tool_use"') || 
      streamingMsg.content.includes("tool_use") ||
      streamingMsg.content.includes("```json");

    const next = [...prev];

    if (isToolCall) {
      // Finalize tool-call stream so the typed feed can consume tool events
      next[streamingIndex] = { ...streamingMsg, streaming: false };
    } else {
      // Prefer streamed assistant text; only use summary as fallback when stream is empty.
      const fallbackContent = summary.trim() && !streamingMsg.content.trim() ? summary : streamingMsg.content;
      next[streamingIndex] = { ...streamingMsg, content: fallbackContent, streaming: false };
    }

    return next;
  }

  if (type === "run.failed") {
    const detailRaw = event.payload?.error;
    const detailText =
      typeof detailRaw === "string" ? detailRaw.trim() : stringifyDisplayValue(detailRaw).trim();
    const detail = detailText === "{}" ? "" : detailText;
    const content = detail
      ? `Run failed: ${detail}`
      : "Run failed. Check worker logs for details.";
    const newMessage: ChatMessage = {
      id: `error-${event.seq}`,
      role: "assistant",
      content,
      seq: event.seq,
    };
    return [...prev, newMessage];
  }

  if (type === "run.partial") {
    return prev;
  }

  if (type === "run.phase.changed") {
    return prev;
  }

  if (type === "step.planned") {
    return prev;
  }

  if (type === "workspace.changed") {
    return prev;
  }

  return prev;
}

export function encodeBase64(value: string) {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte);
  });
  return btoa(binary);
}

export function decodeBase64(value: string) {
  const binary = atob(value || "");
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}
