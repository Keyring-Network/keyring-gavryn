export interface WorkspaceFileNode {
  name: string;
  path: string;
  type: "file" | "directory";
  children?: WorkspaceFileNode[];
}

export interface PreviewStrategy {
  command: string;
  args: string[];
  cwd: string;
  label: string;
}

export interface PreviewAttemptFailure {
  strategy: PreviewStrategy;
  error: string;
  logs?: string;
}

interface PromptArgs {
  runId: string;
  attempts: PreviewAttemptFailure[];
  terminalOutput: string;
  activeFile?: string | null;
}

const MAX_PREVIEW_STRATEGIES = 10;

function normalizePath(value: string): string {
  const trimmed = String(value || "").trim().replace(/\\/g, "/").replace(/^\.?\//, "");
  if (!trimmed) return ".";
  return trimmed.replace(/\/+/g, "/").replace(/\/$/, "") || ".";
}

function dirname(pathValue: string): string {
  const normalized = normalizePath(pathValue);
  if (normalized === "." || !normalized.includes("/")) return ".";
  const idx = normalized.lastIndexOf("/");
  if (idx <= 0) return ".";
  return normalized.slice(0, idx);
}

function joinPath(base: string, leaf: string): string {
  const normalizedBase = normalizePath(base);
  const normalizedLeaf = normalizePath(leaf);
  if (normalizedBase === ".") return normalizedLeaf;
  if (normalizedLeaf === ".") return normalizedBase;
  return `${normalizedBase}/${normalizedLeaf}`;
}

function depth(pathValue: string): number {
  const normalized = normalizePath(pathValue);
  if (normalized === ".") return 0;
  return normalized.split("/").length;
}

function collectWorkspacePaths(nodes: WorkspaceFileNode[]) {
  const files = new Set<string>();
  const directories = new Set<string>(["."]);
  const walk = (items: WorkspaceFileNode[]) => {
    for (const item of items) {
      const itemPath = normalizePath(item.path);
      if (item.type === "directory") {
        directories.add(itemPath);
      } else {
        files.add(itemPath);
        directories.add(dirname(itemPath));
      }
      if (item.children && item.children.length > 0) {
        walk(item.children);
      }
    }
  };
  walk(nodes);
  return { files, directories };
}

function detectPackageManager(root: string, files: Set<string>): "npm" | "pnpm" | "yarn" | "bun" {
  if (files.has(joinPath(root, "pnpm-lock.yaml"))) return "pnpm";
  if (files.has(joinPath(root, "yarn.lock"))) return "yarn";
  if (files.has(joinPath(root, "bun.lockb")) || files.has(joinPath(root, "bun.lock"))) return "bun";
  return "npm";
}

function packageManagerStrategies(pm: "npm" | "pnpm" | "yarn" | "bun"): Array<{ command: string; args: string[]; label: string }> {
  switch (pm) {
    case "pnpm":
      return [
        { command: "pnpm", args: ["dev"], label: "pnpm dev" },
        { command: "pnpm", args: ["start"], label: "pnpm start" },
        { command: "pnpm", args: ["preview"], label: "pnpm preview" },
      ];
    case "yarn":
      return [
        { command: "yarn", args: ["dev"], label: "yarn dev" },
        { command: "yarn", args: ["start"], label: "yarn start" },
        { command: "yarn", args: ["preview"], label: "yarn preview" },
      ];
    case "bun":
      return [
        { command: "bun", args: ["run", "dev"], label: "bun run dev" },
        { command: "bun", args: ["run", "start"], label: "bun run start" },
        { command: "bun", args: ["run", "preview"], label: "bun run preview" },
      ];
    default:
      return [
        { command: "npm", args: ["run", "dev"], label: "npm run dev" },
        { command: "npm", args: ["run", "start"], label: "npm run start" },
        { command: "npm", args: ["run", "preview"], label: "npm run preview" },
      ];
  }
}

export function buildPreviewStrategies(nodes: WorkspaceFileNode[]): PreviewStrategy[] {
  const { files, directories } = collectWorkspacePaths(nodes);
  const out: PreviewStrategy[] = [];
  const seen = new Set<string>();

  const push = (command: string, args: string[], cwd: string, label: string) => {
    const normalizedCwd = normalizePath(cwd);
    const key = `${normalizedCwd}|${command}|${args.join(" ")}`;
    if (seen.has(key)) return;
    seen.add(key);
    out.push({ command, args, cwd: normalizedCwd, label });
  };

  const packageRoots = Array.from(files)
    .filter((pathValue) => pathValue.endsWith("package.json"))
    .map((pathValue) => dirname(pathValue))
    .sort((a, b) => depth(a) - depth(b));

  for (const root of packageRoots) {
    const pm = detectPackageManager(root, files);
    for (const strategy of packageManagerStrategies(pm)) {
      push(strategy.command, strategy.args, root, `${strategy.label} (${root})`);
    }
    if (pm !== "npm") {
      for (const fallback of packageManagerStrategies("npm")) {
        push(fallback.command, fallback.args, root, `${fallback.label} (${root})`);
      }
    }
    push("npx", ["vite", "--host"], root, `npx vite --host (${root})`);
  }

  const staticRoots = Array.from(directories)
    .filter((dir) => files.has(joinPath(dir, "index.html")) && !files.has(joinPath(dir, "package.json")))
    .sort((a, b) => depth(a) - depth(b));
  for (const root of staticRoots) {
    push("python3", ["-m", "http.server", "4173"], root, `python3 -m http.server 4173 (${root})`);
    push("python", ["-m", "http.server", "4173"], root, `python -m http.server 4173 (${root})`);
  }

  if (out.length === 0) {
    push("npm", ["run", "dev"], ".", "npm run dev (.)");
    push("pnpm", ["dev"], ".", "pnpm dev (.)");
    push("yarn", ["dev"], ".", "yarn dev (.)");
    push("python3", ["-m", "http.server", "4173"], ".", "python3 -m http.server 4173 (.)");
  }

  return out.slice(0, MAX_PREVIEW_STRATEGIES);
}

export function buildFixNowPrompt(args: PromptArgs): string {
  const attemptLines = args.attempts.map((attempt, index) => {
    const details = attempt.logs ? `\n    logs: ${attempt.logs}` : "";
    return `${index + 1}. ${attempt.strategy.label}\n   error: ${attempt.error}${details}`;
  });

  const sections = [
    "Fix the local preview setup now.",
    "",
    `Run ID: ${args.runId}`,
    args.activeFile ? `Active file: ${args.activeFile}` : "",
    "",
    "What failed:",
    ...attemptLines,
    "",
    "Latest terminal output:",
    args.terminalOutput ? args.terminalOutput : "(no terminal output captured)",
    "",
    "Please inspect the workspace, fix the root cause, start the correct dev server, and return the preview URL.",
  ].filter(Boolean);

  return sections.join("\n").trim();
}
