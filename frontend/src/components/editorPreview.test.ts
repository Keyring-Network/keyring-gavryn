import { describe, expect, it } from "vitest";

import { buildFixNowPrompt, buildPreviewStrategies, type WorkspaceFileNode } from "./editorPreview";

describe("buildPreviewStrategies", () => {
  it("prefers package-manager commands in package roots", () => {
    const files: WorkspaceFileNode[] = [
      { name: "frontend", path: "frontend", type: "directory", children: [
        { name: "package.json", path: "frontend/package.json", type: "file" },
        { name: "pnpm-lock.yaml", path: "frontend/pnpm-lock.yaml", type: "file" },
      ] },
    ];

    const strategies = buildPreviewStrategies(files);
    expect(strategies.length).toBeGreaterThan(0);
    expect(strategies[0].command).toBe("pnpm");
    expect(strategies[0].args).toEqual(["dev"]);
    expect(strategies[0].cwd).toBe("frontend");
  });

  it("adds static-site server fallback when only index.html exists", () => {
    const files: WorkspaceFileNode[] = [
      { name: "site", path: "site", type: "directory", children: [
        { name: "index.html", path: "site/index.html", type: "file" },
      ] },
    ];

    const strategies = buildPreviewStrategies(files);
    expect(strategies.some((entry) => entry.command === "python3" && entry.cwd === "site")).toBe(true);
  });
});

describe("buildFixNowPrompt", () => {
  it("captures preview attempts and terminal output", () => {
    const prompt = buildFixNowPrompt({
      runId: "run-123",
      activeFile: "frontend/src/App.tsx",
      attempts: [
        {
          strategy: { command: "npm", args: ["run", "dev"], cwd: "frontend", label: "npm run dev (frontend)" },
          error: "missing script: dev",
          logs: "npm ERR! missing script: dev",
        },
      ],
      terminalOutput: "npm ERR! missing script: dev",
    });

    expect(prompt).toContain("Run ID: run-123");
    expect(prompt).toContain("npm run dev (frontend)");
    expect(prompt).toContain("missing script: dev");
    expect(prompt).toContain("Latest terminal output:");
  });
});
