import { render, screen, fireEvent, waitFor, act, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi, describe, it, expect, beforeEach } from "vitest";
import App from "./App";

// Mock globals
window.HTMLElement.prototype.scrollIntoView = vi.fn();
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};
Object.defineProperty(global, "crypto", {
  value: { randomUUID: () => "test-uuid-" + Math.random().toString(36).substring(7) },
});
global.URL.createObjectURL = vi.fn(() => "blob:test");

// Mock FileReader
class MockFileReader {
  result: ArrayBuffer | null = null;
  onload: () => void = () => {};
  onerror: () => void = () => {};
  readAsArrayBuffer(blob: Blob) {
    setTimeout(() => {
      this.result = new TextEncoder().encode("test content").buffer;
      this.onload();
    }, 0);
  }
}
(global as any).FileReader = MockFileReader;

// Mock EventSource
const mockEventSourceInstance = {
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  close: vi.fn(),
};
const MockEventSource = vi.fn(() => mockEventSourceInstance);
global.EventSource = MockEventSource as any;

// Mock fetch
global.fetch = vi.fn();

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "http://localhost:8080";

describe("App Coverage Boost", () => {
  const mockLLMSettings = {
    configured: true,
    mode: "remote",
    provider: "openai",
    model: "gpt-4",
    base_url: "",
    has_api_key: true,
  };

  beforeEach(() => {
    vi.clearAllMocks();
    (global.fetch as any).mockResolvedValue({
      ok: true,
      json: async () => ({}),
      text: async () => "",
    });
  });

  const setupFetchMock = (overrides: Record<string, any> = {}) => {
    (global.fetch as any).mockImplementation(async (url: string, options: any) => {
      if (overrides[url]) {
        const response = overrides[url];
        if (typeof response === 'function') return response(url, options);
        return {
          ok: response.ok !== false,
          status: response.status || 200,
          json: async () => response.data || {},
          text: async () => response.text || "",
        };
      }
      
      if (url.includes("/settings/llm")) return { ok: true, json: async () => mockLLMSettings };
      if (url.includes("/settings/memory")) return { ok: true, json: async () => ({ enabled: true }) };
      if (url.includes("/settings/personality")) {
        return { ok: true, json: async () => ({ content: "You are Gavryn.", source: "default" }) };
      }
      if (url.endsWith("/skills")) return { ok: true, json: async () => ({ skills: [] }) };
      if (url.endsWith("/context")) return { ok: true, json: async () => ({ nodes: [] }) };
      if (url.endsWith("/runs") && options?.method === "POST") return { ok: true, json: async () => ({ run_id: "run-1" }) };
      
      return { ok: true, json: async () => ({}) };
    });
  };

  it("handles subsequence search", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);

    const input = screen.getByPlaceholderText("Assign a task or ask anything");
    await user.type(input, "Task One{enter}");
    await waitFor(() => expect(screen.getAllByText("Task One").length).toBeGreaterThan(0));

    const searchInput = screen.getByPlaceholderText("Search tasks or tags");
    // "tkone" is a subsequence of "Task One"
    await user.type(searchInput, "tkone");
    
    expect(screen.getAllByText("Task One").length).toBeGreaterThan(0);
  });

  it("handles tag splitting and cycling", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);

    // Add tags to draft
    const tagInput = screen.getByPlaceholderText("Add tag");
    await user.type(tagInput, "tag1, tag2{enter}");
    
    expect(screen.getByText("tag1")).toBeInTheDocument();
    expect(screen.getByText("tag2")).toBeInTheDocument();
    
    // Cycle color
    const tag1 = screen.getByText("tag1");
    const tagSpan = tag1.closest("span");
    const colorBtn = tagSpan?.querySelector("button:first-child"); // The color dot button
    if (colorBtn) await user.click(colorBtn);
    
    // Remove tag
    const removeBtn = tagSpan?.querySelector("button:last-child"); // The X button
    if (removeBtn) await user.click(removeBtn);
    expect(screen.queryByText("tag1")).not.toBeInTheDocument();
  });

  it("handles project creation and deletion", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);

    // Create project
    const projectsHeader = screen.getByText("Projects");
    const addProjectBtn = projectsHeader.parentElement?.querySelector("button");
    if (addProjectBtn) await user.click(addProjectBtn);

    const projectInput = screen.getByPlaceholderText("Project name");
    await user.type(projectInput, "Proj A");
    const addBtn = screen.getByText("Add");
    await user.click(addBtn);
    
    // Verify project created - use findAllByText since it appears in multiple places
    const projAElements = await screen.findAllByText("Proj A");
    expect(projAElements.length).toBeGreaterThan(0);
    
    // Assign task to project via composer
    const composer = screen.getByPlaceholderText("Assign a task or ask anything").closest("div");
    if (!composer) throw new Error("Composer not found");
    
    // Find the project select by finding one of its options
    const unassignedOption = within(composer).getByText("Unassigned");
    const select = unassignedOption.closest("select");
    if (!select) throw new Error("Project select not found");
    
    const projectOption = within(select).getByText("Proj A");
    await user.selectOptions(select, [projectOption.getAttribute("value") || ""]);
    
    // Delete project - find the project button in the list (ignore select option)
    const projectBtn = screen.getAllByText("Proj A").find(el => el.tagName === "BUTTON");
    if (!projectBtn) throw new Error("Project button not found");
    
    const projectItem = projectBtn.closest("div.rounded-xl");
    if (!projectItem) throw new Error("Project item container not found");
    
    const deleteBtn = projectItem.querySelector("button:last-child");
    if (!deleteBtn) throw new Error("Delete project button not found");
    
    await user.click(deleteBtn);
    await waitFor(() => expect(screen.queryByText("Proj A")).not.toBeInTheDocument());
  });

  it("handles context tree recursion and deletion", async () => {
    setupFetchMock({
      [API_BASE_URL + "/context"]: {
        data: {
          nodes: [
            { id: "f1", name: "Folder 1", node_type: "folder" },
            { id: "f2", name: "Folder 2", node_type: "folder", parent_id: "f1" },
            { id: "file1", name: "File 1", node_type: "file", parent_id: "f2" },
          ],
        },
      },
      [API_BASE_URL + "/context/file1"]: { method: "DELETE", ok: true },
    });

    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    await user.click(screen.getByText("Context"));
    
    expect(await screen.findByText("Folder 1")).toBeInTheDocument();
    expect(screen.getByText("Folder 2")).toBeInTheDocument();
    expect(screen.getByText("File 1")).toBeInTheDocument();
    
    // Delete file
    const fileNode = screen.getByText("File 1").closest("div");
    const deleteBtn = fileNode?.querySelector("button:last-child");
    if (deleteBtn) await user.click(deleteBtn);
    
    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining("/context/file1"), expect.objectContaining({ method: "DELETE" }));
  });

  it("handles library artifact types", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    const eventSource = mockEventSourceInstance;
    const input = screen.getByPlaceholderText("Assign a task or ask anything");
    await user.type(input, "Run{enter}");
    await waitFor(() => expect(MockEventSource).toHaveBeenCalled());
    
    const eventListener = eventSource.addEventListener.mock.calls.find(call => call[0] === "run_event")[1];
    
    act(() => {
      eventListener(new MessageEvent("run_event", {
        data: JSON.stringify({
          type: "tool.completed",
          run_id: "run-1",
          seq: 1,
          payload: {
            artifacts: [
              { artifact_id: "a1", type: "image", uri: "img.png", content_type: "image/png" },
              { artifact_id: "a2", type: "file", uri: "notes.md", content_type: "text/markdown" },
              { artifact_id: "a3", type: "code", uri: "code.ts" },
              {
                artifact_id: "a4",
                type: "file",
                uri: "report.docx",
                content_type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
              },
            ]
          },
        }),
      }));
    });
    
    await user.click(screen.getByText("Library"));
    
    expect((await screen.findAllByText("notes.md")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("report.docx").length).toBeGreaterThan(0);
    expect(screen.queryByText("img.png")).not.toBeInTheDocument();
    expect(screen.queryByText("code.ts")).not.toBeInTheDocument();
  });

  it("handles streaming events and failures", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    const input = screen.getByPlaceholderText("Assign a task or ask anything");
    await user.type(input, "Stream{enter}");
    const eventListener = mockEventSourceInstance.addEventListener.mock.calls.find(call => call[0] === "run_event")[1];
    
    // Token stream
    act(() => {
      eventListener(new MessageEvent("run_event", {
        data: JSON.stringify({ type: "model.token", run_id: "run-1", seq: 1, payload: { text: "Hello" } }),
      }));
    });
    act(() => {
      eventListener(new MessageEvent("run_event", {
        data: JSON.stringify({ type: "model.token", run_id: "run-1", seq: 2, payload: { text: " World" } }),
      }));
    });
    
    expect(await screen.findByText("Hello World")).toBeInTheDocument();
    
    // Model summary (thought chain)
    act(() => {
      eventListener(new MessageEvent("run_event", {
        data: JSON.stringify({ type: "model.summary", run_id: "run-1", seq: 3, payload: { text: "Thinking process" } }),
      }));
    });

    const activityToggles = await screen.findAllByRole("button", { name: /Agent activity/i });
    await user.click(activityToggles[activityToggles.length - 1]);

    const reasoningRows = await screen.findAllByText("Thinking process");
    expect(reasoningRows.length).toBeGreaterThan(0);
    
    // Run failed
    act(() => {
      eventListener(new MessageEvent("run_event", {
        data: JSON.stringify({ type: "run.failed", run_id: "run-1", seq: 4, payload: {} }),
      }));
    });
    
    expect(await screen.findByText(/Run failed/)).toBeInTheDocument();
  });

  it("handles memory prompts", async () => {
    // Mock memory disabled
    setupFetchMock({
      [API_BASE_URL + "/settings/memory"]: { ok: true, json: async () => ({ enabled: false }) },
    });
    
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    // Create task
    const input = screen.getByPlaceholderText("Assign a task or ask anything");
    await user.type(input, "Task{enter}");
    
    // Should see memory prompt title
    const promptTitle = await screen.findByRole("heading", { name: "Enable memory" });
    expect(promptTitle).toBeInTheDocument();
    
    // Click Enable button
    const enableBtn = screen.getByRole("button", { name: "Enable memory" });
    await user.click(enableBtn);
    
    // Should dismiss prompt
    await waitFor(() => expect(screen.queryByText("Enable memory")).not.toBeInTheDocument());
  });

  it("handles error states for all async actions", async () => {
    setupFetchMock({
      [API_BASE_URL + "/skills"]: { ok: false, status: 500, text: "Skills Error" },
      [API_BASE_URL + "/context"]: { ok: false, status: 500, text: "Context Error" },
      [API_BASE_URL + "/settings/memory"]: { ok: false, status: 500 },
    });

    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    await user.click(screen.getByText("Skills"));
    expect(await screen.findByText("Skills Error")).toBeInTheDocument();
    
    await user.click(screen.getByText("Context"));
    expect(await screen.findByText("Context Error")).toBeInTheDocument();
  });

  it("handles panel switching with content", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);

    const input = screen.getByPlaceholderText("Assign a task or ask anything");
    await user.type(input, "Task with panels{enter}");
    await waitFor(() => expect(screen.getAllByText("Task with panels").length).toBeGreaterThan(0));

    // Emit browser snapshot event
    const eventListener = mockEventSourceInstance.addEventListener.mock.calls.find(call => call[0] === "run_event")[1];
    act(() => {
      eventListener(new MessageEvent("run_event", {
        data: JSON.stringify({
          type: "browser.snapshot",
          run_id: "run-1",
          seq: 1,
          payload: { uri: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==" },
        }),
      }));
    });

    // Browser panel
    // Expand controls first
    const overviewBtn = screen.getByRole("button", { name: /Overview/i });
    await user.click(overviewBtn);
    
    const browserBtn = screen.getByRole("button", { name: /^Browser$/i });
    await user.click(browserBtn);
    expect(screen.getAllByText("Browser").length).toBeGreaterThan(0);
    // Should show image now, not "No output"
    expect(screen.queryByText("No browser output yet.")).not.toBeInTheDocument();
    expect(screen.getByAltText("Browser snapshot")).toBeInTheDocument();
    
    // Editor panel
    const filesBtn = screen.getByRole("button", { name: /Files/i });
    await user.click(filesBtn);
    expect(screen.getByText("Task files")).toBeInTheDocument();

    // Editor panel
    const editorBtn = screen.getByRole("button", { name: /Editor/i });
    await user.click(editorBtn);
    expect(screen.getByText("Explorer")).toBeInTheDocument();
    
    // Close panel not supported in new UI via button (always open if active task)
    // We can test switching back to overview
    await user.click(overviewBtn);
    
    expect(screen.getAllByText("Task with panels").length).toBeGreaterThan(0);
  });

  it("handles settings interactions with Codex", async () => {
    setupFetchMock({
      [API_BASE_URL + "/settings/llm/test"]: { data: { status: "Connected" } },
      [API_BASE_URL + "/settings/llm"]: { method: "POST", data: { configured: true } },
      [API_BASE_URL + "/settings/llm/models"]: { data: { models: ["gpt-4"] } },
    });
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);

    await user.click(screen.getByText("Settings"));
    await user.click(screen.getByText("Run setup wizard"));
    
    // Navigate to Provider step
    expect(screen.getByText("Welcome to Gavryn")).toBeInTheDocument();
    await user.click(screen.getByText("Next"));
    await user.click(screen.getByText("Next"));
    await user.click(screen.getByText("Next"));
    
    // Select Codex
    const providerSelect = document.querySelector("select");
    if (!providerSelect) throw new Error("Provider select not found");
    await user.selectOptions(providerSelect, "codex");
    
    // Check explanation text
    expect(screen.getByText(/Codex uses your local/)).toBeInTheDocument();
    
    // Next
    await user.click(screen.getByText("Next"));
    
    // Auth inputs for Codex
    const inputs = screen.getAllByRole("textbox");
    // Should have auth path and home path
    expect(inputs.length).toBeGreaterThanOrEqual(2);
    await user.type(inputs[0], "/path/to/auth");
    await user.type(inputs[1], "/path/to/home");
    
    await user.click(screen.getByText("Next"));
    
    // Model
    await user.click(screen.getByText("Next"));

    // Personality
    await user.click(screen.getByText("Next"));
    
    // Test & Save
    await user.click(screen.getByText("Test connection"));
    await waitFor(() => expect(screen.getByText("Connected")).toBeInTheDocument());
    
    await user.click(screen.getByText("Save settings"));
    await waitFor(() => expect(screen.getByText("Saved")).toBeInTheDocument());
  });

  it("handles skill management", async () => {
    const skillId = "s1";
    setupFetchMock({
      [API_BASE_URL + "/skills"]: { data: { skills: [{ id: skillId, name: "Skill 1", description: "Desc", updatedAt: new Date().toISOString() }] } },
      [API_BASE_URL + `/skills/${skillId}/files`]: { data: { files: [{ path: "SKILL.md", content_base64: "SGVsbG8=", content_type: "text/markdown", size_bytes: 5, updated_at: new Date().toISOString() }] } },
      [API_BASE_URL + `/skills/${skillId}`]: { method: "PUT", data: { id: skillId, name: "Skill 1 Updated" } },
      [API_BASE_URL + `/skills/${skillId}`]: { method: "DELETE", status: 204 },
    });
    
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    await user.click(screen.getByText("Skills"));
    await waitFor(() => expect(screen.getByText("Skill 1")).toBeInTheDocument());
    
    const skillText = screen.getByText("Skill 1");
    const skillBtn = skillText.closest("button");
    if (!skillBtn) throw new Error("Skill button not found");
    
    await user.click(skillBtn);
    
    await waitFor(() => expect(screen.getByText("Metadata")).toBeInTheDocument());
    
    const textboxes = screen.getAllByRole("textbox");
    const contentInput = textboxes[textboxes.length - 1];
    
    if ((contentInput as HTMLTextAreaElement).value !== "Hello") {
      fireEvent.change(contentInput, { target: { value: "Hello" } });
    }
    await user.type(contentInput, " World");
    
    const saveBtn = screen.getByText("Save SKILL.md");
    await user.click(saveBtn);
    
    await user.click(screen.getByText("Delete skill"));
    await waitFor(() => expect(screen.queryByDisplayValue("Skill 1 Updated")).not.toBeInTheDocument());
  });

  it("handles task management", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    // Create task 1
    const input = screen.getByPlaceholderText("Assign a task or ask anything");
    await user.type(input, "Task 1{enter}");
    
    await waitFor(() => expect(screen.getAllByText("Task 1").length).toBeGreaterThan(0));
    
    // Click New task
    await user.click(screen.getByText("New task"));
    
    // Re-query input
    const freshInput = screen.getByPlaceholderText("Assign a task or ask anything");
    
    // Create task 2
    await user.type(freshInput, "Task 2{enter}");
    
    await waitFor(() => expect(screen.getAllByText("Task 2").length).toBeGreaterThan(0));
    
    const aside = document.querySelector("aside");
    if (!aside) throw new Error("Aside not found");
    
    const task1Title = within(aside).getByText("Task 1");
    const taskItem = task1Title.closest("div.rounded-xl");
    if (!taskItem) throw new Error("Task item not found");
    
    const deleteBtn = taskItem.querySelector("button:last-child");
    if (!deleteBtn) throw new Error("Delete button not found");
    
    await user.click(deleteBtn);
    
    await waitFor(() => {
      expect(screen.queryByText("Task 1")).not.toBeInTheDocument();
    });
  });

  it("handles task creation validation", async () => {
    setupFetchMock();
    const user = userEvent.setup();
    render(<App />);
    const elements = await screen.findAllByText(/Control Desk/i);
    expect(elements.length).toBeGreaterThan(0);
    
    const input = screen.getByPlaceholderText("Assign a task or ask anything");
    await user.type(input, "Task 1{enter}");
    
    expect(screen.queryByText(/Complete LLM setup/)).not.toBeInTheDocument();
    
    await waitFor(() => expect(screen.getAllByText("Task 1").length).toBeGreaterThan(0));
  });

});
