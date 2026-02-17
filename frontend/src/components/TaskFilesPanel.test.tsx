import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";

import { TaskFilesPanel } from "./TaskFilesPanel";
import type { LibraryItem } from "@/App";

const items: LibraryItem[] = [
  {
    id: "a-docx",
    label: "RWA-summary.docx",
    type: "artifact",
    uri: "http://localhost:8080/files/rwa-summary.docx",
    contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    createdAt: "2026-02-09T10:00:00Z",
    runId: "run-1",
  },
  {
    id: "a-code",
    label: "report.ts",
    type: "code",
    uri: "http://localhost:8080/files/report.ts",
    contentType: "text/typescript",
    createdAt: "2026-02-09T09:00:00Z",
    runId: "run-1",
  },
  {
    id: "a-snap",
    label: "Browser snapshot",
    type: "browser.snapshot",
    uri: "http://localhost:8080/files/snapshot.png",
    contentType: "image/png",
    createdAt: "2026-02-09T08:00:00Z",
    runId: "run-1",
  },
];

describe("TaskFilesPanel", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("shows task files and supports category filtering", async () => {
    const user = userEvent.setup();
    render(<TaskFilesPanel items={items} />);

    expect(screen.getByText("Task files")).toBeInTheDocument();
    expect(screen.getByText("RWA-summary.docx")).toBeInTheDocument();
    expect(screen.getByText("report.ts")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Docs" }));
    expect(screen.getByText("RWA-summary.docx")).toBeInTheDocument();
    expect(screen.queryByText("report.ts")).not.toBeInTheDocument();
  });

  it("supports search and opens files externally", async () => {
    const openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
    const user = userEvent.setup();

    render(<TaskFilesPanel items={items} />);

    await user.type(screen.getByPlaceholderText("Search files..."), "summary");
    expect(screen.getByText("RWA-summary.docx")).toBeInTheDocument();
    expect(screen.queryByText("report.ts")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Open/i }));
    expect(openSpy).toHaveBeenCalledWith(
      "http://localhost:8080/files/rwa-summary.docx",
      "_blank",
      "noopener,noreferrer"
    );
  });
});
