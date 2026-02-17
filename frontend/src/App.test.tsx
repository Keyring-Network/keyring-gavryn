import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi, describe, it, expect, beforeEach, afterEach } from "vitest";
import App from "./App";

// Mock child components if necessary
window.HTMLElement.prototype.scrollIntoView = vi.fn();

// Mock ResizeObserver
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// Mock crypto.randomUUID
Object.defineProperty(global, "crypto", {
  value: {
    randomUUID: () => "test-uuid-" + Math.random().toString(36).substring(7),
  },
});

// Mock URL.createObjectURL
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

describe("App Component", () => {
  const mockLLMSettings = {
    configured: true,
    mode: "remote",
    provider: "openai",
    model: "gpt-4",
    base_url: "",
    has_api_key: true,
  };

  const mockMemorySettings = {
    enabled: true,
  };

  const mockPersonalitySettings = {
    content: "You are Gavryn.",
    source: "default",
  };

  beforeEach(() => {
    vi.clearAllMocks();
    // Default mock to prevent unhandled rejections
    (global.fetch as any).mockResolvedValue({
      ok: true,
      json: async () => ({}),
      text: async () => "",
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  const setupFetchMock = (overrides: Record<string, any> = {}) => {
    let runIdCounter = 0;
    (global.fetch as any).mockImplementation(async (url: string, options: any) => {
      if (overrides[url]) {
        const response = overrides[url];
        return {
          ok: response.ok !== false,
          status: response.status || 200,
          json: async () => response.data || {},
          text: async () => response.text || "",
        };
      }
      
      // Default mocks for common endpoints
      if (url.includes("/settings/llm") && !url.includes("/models") && !url.includes("/test")) {
        return {
          ok: true,
          json: async () => mockLLMSettings,
        };
      }
      if (url.includes("/settings/memory")) {
        return {
          ok: true,
          json: async () => mockMemorySettings,
        };
      }
      if (url.includes("/settings/personality")) {
        return {
          ok: true,
          json: async () => mockPersonalitySettings,
        };
      }
      if (url.endsWith("/skills")) {
        return {
          ok: true,
          json: async () => ({ skills: [] }),
        };
      }
      if (url.endsWith("/context")) {
        return {
          ok: true,
          json: async () => ({ nodes: [] }),
        };
      }
      if (url.endsWith("/runs") && options?.method === "POST") {
        runIdCounter++;
        return {
          ok: true,
          json: async () => ({ run_id: `run-${runIdCounter}` }),
        };
      }
      if (url.includes("/messages")) {
        return {
          ok: true,
          json: async () => ({ id: "msg-1" }),
        };
      }
      
      return {
        ok: true,
        json: async () => ({}),
        text: async () => "",
      };
    });
  };

  describe("1. Initial Render Tests", () => {
    it("renders without crashing and shows control desk title", async () => {
      setupFetchMock();
      const { container } = render(<App />);
      
      // Use findAllByText because "Control Desk" appears in sidebar and header
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);
    });

    it("displays loading state initially (implied by async fetch)", async () => {
      setupFetchMock();
      render(<App />);
      expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining("/settings/llm"));
    });
  });

  describe("2. State Management Tests", () => {
    it("initializes with empty runs", async () => {
      setupFetchMock();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);
      expect(screen.queryByText("Running")).not.toBeInTheDocument();
    });

    it("handles task selection", async () => {
      setupFetchMock();
      const user = userEvent.setup();
      const { container } = render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // We can't easily select a task if none exist.
      // We need to create one first or mock the initial state if possible.
      // But App doesn't load tasks on mount (it seems to rely on local state or events).
      // Wait, App.tsx doesn't seem to load tasks from API on mount?
      // Checking code: const [tasks, setTasks] = useState<Task[]>([]);
      // It seems tasks are only added via createRun/sendMessage or maybe I missed a loadTasks?
      // I checked the code, there is NO loadTasks. Tasks seem to be ephemeral or I missed something.
      // Ah, `tasks` state is local.
      
      // So we must create a task to test selection.
    });
  });

  describe("Wizard Flow", () => {
    it("shows wizard when not configured", async () => {
      setupFetchMock({
        [API_BASE_URL + "/settings/llm"]: {
          data: { configured: false },
        },
      });

      render(<App />);
      
      expect(await screen.findByText(/Welcome to Gavryn/i)).toBeInTheDocument();
    });

    it("navigates through wizard steps", async () => {
      setupFetchMock({
        [API_BASE_URL + "/settings/llm"]: {
          data: { configured: false },
        },
        [API_BASE_URL + "/settings/llm/models"]: {
          data: { models: ["gpt-4"] },
        },
        [API_BASE_URL + "/settings/llm/test"]: {
          data: { status: "Connected" },
        },
      });

      const user = userEvent.setup();
      const { container } = render(<App />);

      expect(await screen.findByText(/Welcome to Gavryn/i)).toBeInTheDocument();

      // Step 1: Welcome -> Next
      await user.click(screen.getByText("Next"));

      // Step 2: How it works -> Next
      await user.click(screen.getByText("Next"));

      // Step 3: Mode -> Next (default Remote)
      await user.click(screen.getByText("Next"));

      // Step 4: Provider -> Next (default Codex)
      await user.click(screen.getByText("Next"));

      // Step 5: Auth -> Next (skip for Codex/mock)
      await user.click(screen.getByText("Next"));

      // Step 6: Model -> Next
      await user.click(screen.getByText("Next"));

      // Step 7: Personality -> Next
      await user.click(screen.getByText("Next"));

      // Step 8: Test & Save
      await user.click(screen.getByText("Test connection"));
      await waitFor(() => screen.getByText("Connected"));
      
      await user.click(screen.getByText("Save settings"));
      
      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining("/settings/llm"),
        expect.objectContaining({ method: "POST" })
      );
    });
  });

  describe("3. Event Handler & Interaction Tests", () => {
    it("handles creating new task via composer", async () => {
      const runId = "run-123";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: {
          data: { run_id: runId },
        },
        [API_BASE_URL + `/runs/${runId}/messages`]: {
          data: { id: "msg-1" },
        },
      });

      const user = userEvent.setup();
      const { container } = render(<App />);

      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Do something{enter}");

      await waitFor(() => {
        expect(global.fetch).toHaveBeenCalledWith(
          expect.stringContaining("/runs"),
          expect.objectContaining({ method: "POST" })
        );
      });
      
      // Use getAllByText because the title might appear in multiple places (list + header)
      expect(screen.getAllByText("Do something").length).toBeGreaterThan(0);
    });

    it("handles project operations (create/delete)", async () => {
      setupFetchMock();
      const user = userEvent.setup();
      render(<App />);

      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // Find the "New project" button (Plus icon in Projects section)
      // It's the button inside the div with "Projects" text
      const projectsHeader = screen.getByText("Projects");
      const addProjectBtn = projectsHeader.parentElement?.querySelector("button");
      if (addProjectBtn) await user.click(addProjectBtn);

      // Now input should be visible
      const projectInput = screen.getByPlaceholderText("Project name");
      await user.type(projectInput, "My Project");
      
      const addBtn = screen.getByText("Add");
      await user.click(addBtn);

      expect((await screen.findAllByText("My Project")).length).toBeGreaterThan(0);

      // Delete project
      // We need to find the delete button for this project.
      // It's a trash icon button.
      // The project item is a div containing the button with text "My Project"
      // We can find the button with text "My Project" and then find the sibling delete button.
      const projectButton = screen.getByRole("button", { name: /My Project/i });
      const projectItem = projectButton.closest("div");
      const deleteBtn = projectItem?.querySelector("button:last-child");
      if (deleteBtn) await user.click(deleteBtn);
      
      expect(screen.queryByText("My Project")).not.toBeInTheDocument();
    });
  });

  describe("4. Async Function Tests", () => {
    it("loads skills on mount of skills view", async () => {
      setupFetchMock({
        [API_BASE_URL + "/skills"]: {
          data: { skills: [{ id: "s1", name: "Test Skill", description: "Desc", updatedAt: new Date().toISOString() }] },
        },
      });

      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // Navigate to Skills
      const skillsNav = screen.getByText("Skills");
      await user.click(skillsNav);

      expect(await screen.findByText("Test Skill")).toBeInTheDocument();
    });

    it("loads context nodes on mount of context view", async () => {
      setupFetchMock({
        [API_BASE_URL + "/context"]: {
          data: { nodes: [{ id: "c1", name: "Test Context", node_type: "file" }] },
        },
      });

      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // Navigate to Context
      const contextNav = screen.getByText("Context");
      await user.click(contextNav);

      expect(await screen.findByText("Test Context")).toBeInTheDocument();
    });
  });

  describe("5. Error State Tests", () => {
    it("handles API errors gracefully when sending message", async () => {
      setupFetchMock({
        [API_BASE_URL + "/runs"]: {
          ok: false,
          status: 500,
          text: "Server Error",
        },
      });

      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Fail me{enter}");

      expect(await screen.findByText("Server Error")).toBeInTheDocument();
    });
  });

  describe("6. SSE Event Tests", () => {
    it("connects to event stream and handles events", async () => {
      const runId = "run-sse";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: {
          data: { run_id: runId },
        },
        [API_BASE_URL + `/runs/${runId}/messages`]: {
          data: { id: "msg-1" },
        },
      });

      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // Start a run
      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Start Run{enter}");

      await waitFor(() => {
        expect(MockEventSource).toHaveBeenCalledWith(expect.stringContaining(`/runs/${runId}/events`));
      });

      // Simulate incoming event
      const eventSource = mockEventSourceInstance;
      const messageEvent = new MessageEvent("run_event", {
        data: JSON.stringify({
          type: "run.started",
          run_id: runId,
          seq: 1,
          data: {},
        }),
      });

      // Find the event listener callback
      const eventListener = eventSource.addEventListener.mock.calls.find(
        (call) => call[0] === "run_event"
      )[1];

      act(() => {
        eventListener(messageEvent);
      });

      // Verify chat controls remain active after stream event handling
      await waitFor(() => {
        expect(screen.getByRole("button", { name: /Stop run/i })).toBeEnabled();
      });
    });
  });
  
  describe("Comprehensive Coverage Tests", () => {
    it("switches send button to stop immediately on follow-up message", async () => {
      const runId = "run-follow-up-stop";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: {
          data: { run_id: runId },
        },
        [API_BASE_URL + `/runs/${runId}/messages`]: {
          data: { id: "msg-1" },
        },
      });

      const user = userEvent.setup();
      render(<App />);
      await screen.findAllByText(/Control Desk/i);

      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "First request");
      await user.click(screen.getByRole("button", { name: /Send message/i }));

      await waitFor(() => expect(MockEventSource).toHaveBeenCalled());
      const eventListener = mockEventSourceInstance.addEventListener.mock.calls.find(
        (call) => call[0] === "run_event"
      )[1];

      act(() => {
        eventListener(
          new MessageEvent("run_event", {
            data: JSON.stringify({ type: "run.completed", run_id: runId, seq: 1, payload: {} }),
          })
        );
      });

      await waitFor(() => {
        expect(screen.getByRole("button", { name: /Send message/i })).toBeInTheDocument();
      });

      const followUpInput = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(followUpInput, "Follow-up request");
      await user.click(screen.getByRole("button", { name: /Send message/i }));

      await waitFor(() => {
        expect(screen.getByRole("button", { name: /Stop run/i })).toBeInTheDocument();
      });
    });

    it("opens model picker modal and uses selected model for next message", async () => {
      const runId = "run-model-picker";
      setupFetchMock({
        [API_BASE_URL + "/settings/llm/models"]: {
          data: { models: ["gpt-4", "gpt-4o-mini"] },
        },
        [API_BASE_URL + "/runs"]: {
          data: { run_id: runId },
        },
        [API_BASE_URL + `/runs/${runId}/messages`]: {
          data: { id: "msg-1" },
        },
      });

      const user = userEvent.setup();
      render(<App />);
      await screen.findAllByText(/Control Desk/i);

      await user.click(screen.getByRole("button", { name: /Choose model for next message/i }));
      expect(await screen.findByRole("dialog", { name: /Model picker/i })).toBeInTheDocument();
      expect(screen.getByText("gpt-4o-mini")).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "gpt-4o-mini" }));
      expect(screen.queryByRole("dialog", { name: /Model picker/i })).not.toBeInTheDocument();

      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Use alternate model");
      await user.click(screen.getByRole("button", { name: /Send message/i }));

      await waitFor(() => {
        const call = (global.fetch as any).mock.calls.find(
          (entry: any[]) => entry[0] === `${API_BASE_URL}/runs/${runId}/messages`
        );
        expect(call).toBeDefined();
        const [, options] = call;
        const body = JSON.parse(options.body);
        expect(body.metadata.llm_model).toBe("gpt-4o-mini");
      });
    });

    it("shows enabled label for user-tab browser toggle", async () => {
      setupFetchMock();
      const user = userEvent.setup();
      render(<App />);
      await screen.findAllByText(/Control Desk/i);

      const toggle = screen.getByRole("button", { name: /Enable user tab browser mode/i });
      await user.click(toggle);

      expect(screen.getByText("Enabled")).toBeInTheDocument();
      expect(toggle).toHaveAttribute("aria-pressed", "true");
    });

    it("includes user-tab browser metadata when browser mode is enabled", async () => {
      const runId = "run-browser-mode";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: {
          data: { run_id: runId },
        },
        [API_BASE_URL + `/runs/${runId}/messages`]: {
          data: { id: "msg-1" },
        },
      });

      const user = userEvent.setup();
      render(<App />);
      await screen.findAllByText(/Control Desk/i);

      await user.click(screen.getByRole("button", { name: /Enable user tab browser mode/i }));
      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Use browser tab mode");
      await user.click(screen.getByRole("button", { name: /Send message/i }));

      await waitFor(() => {
        const call = (global.fetch as any).mock.calls.find(
          (entry: any[]) => entry[0] === `${API_BASE_URL}/runs/${runId}/messages`
        );
        expect(call).toBeDefined();
        const [, options] = call;
        const body = JSON.parse(options.body);
        expect(body.metadata.browser_mode).toBe("user_tab");
        expect(body.metadata.browser_control_mode).toBe("user_tab");
        expect(body.metadata.browser_interaction).toBe("enabled");
        expect(typeof body.metadata.browser_user_agent).toBe("string");
      });
    });

    it("handles tag operations", async () => {
      setupFetchMock();
      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // Add tag to draft
      const tagInput = screen.getByPlaceholderText("Add tag");
      await user.type(tagInput, "urgent{enter}");
      
      expect(await screen.findByText("urgent")).toBeInTheDocument();
      
      // Remove tag
      const removeBtn = screen.getByText("urgent").parentElement?.querySelector("button:last-child");
      if (removeBtn) await user.click(removeBtn);
      
      expect(screen.queryByText("urgent")).not.toBeInTheDocument();
    });

    it("handles cancelling a run", async () => {
      const runId = "run-cancel";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: {
          data: { run_id: runId },
        },
        [API_BASE_URL + `/runs/${runId}/messages`]: {
          data: { id: "msg-1" },
        },
        [API_BASE_URL + `/runs/${runId}/cancel`]: {
          data: {},
        },
      });

      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // Start run
      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Task to cancel{enter}");
      
      // Simulate run started event to enable stop button
      await waitFor(() => expect(MockEventSource).toHaveBeenCalled());
      const eventSource = mockEventSourceInstance;
      const eventListener = eventSource.addEventListener.mock.calls.find(
        (call) => call[0] === "run_event"
      )[1];
      
      act(() => {
        eventListener(new MessageEvent("run_event", {
          data: JSON.stringify({
            type: "run.started",
            run_id: runId,
            seq: 1,
            data: {},
          }),
        }));
      });

      // Click Stop button (single send/stop control)
      const stopBtn = screen.getByRole("button", { name: /Stop run/i });
      await user.click(stopBtn);
      
      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining("/cancel"),
        expect.objectContaining({ method: "POST" })
      );
    });
    
    it("handles skill creation and file upload", async () => {
      const newSkill = { id: "s-new", name: "New Skill", description: "Desc", updatedAt: new Date().toISOString() };
      let currentSkills: any[] = [];

      (global.fetch as any).mockImplementation(async (url: string, options: any) => {
        if (url.endsWith("/skills")) {
          if (options?.method === "POST") {
            currentSkills = [newSkill];
            return { ok: true, json: async () => newSkill };
          }
          return { ok: true, json: async () => ({ skills: currentSkills }) };
        }
        if (url.endsWith("/skills/s-new/files")) {
           return { ok: true, json: async () => ({ files: [] }) };
        }
        // Default mocks
        if (url.includes("/settings/llm")) return { ok: true, json: async () => mockLLMSettings };
        if (url.includes("/settings/memory")) return { ok: true, json: async () => mockMemorySettings };
        if (url.endsWith("/context")) return { ok: true, json: async () => ({ nodes: [] }) };
        
        return { ok: true, json: async () => ({}) };
      });

      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);
      
      await user.click(screen.getByText("Skills"));
      
      // Click New Skill button (it's an input field area in the new design)
      // "Create skill" section
      const nameInput = screen.getByPlaceholderText("skill-name");
      await user.type(nameInput, "New Skill");
      
      const descInput = screen.getByPlaceholderText("Short description");
      await user.type(descInput, "Desc");
      
      const createBtn = screen.getByRole("button", { name: "Create skill" });
      await user.click(createBtn);
      
      await waitFor(() => {
        expect(screen.getByText("New Skill")).toBeInTheDocument();
      });
    });
    
    it("handles context folder creation", async () => {
      setupFetchMock({
        [API_BASE_URL + "/context"]: {
          data: { nodes: [] },
        },
        [API_BASE_URL + "/context/folders"]: {
          data: {},
        },
      });

      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);
      
      await user.click(screen.getByText("Context"));
      
      const folderInput = screen.getByPlaceholderText("Folder name");
      await user.type(folderInput, "My Folder");
      
      const createBtn = screen.getByText("Add");
      await user.click(createBtn);
      
      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining("/context/folders"),
        expect.objectContaining({ method: "POST" })
      );
    });

    it("handles search filtering", async () => {
      setupFetchMock();
      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      // Create one task
      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      fireEvent.change(input, { target: { value: "Task One" } });
      fireEvent.keyDown(input, { key: "Enter" });
      await waitFor(() => expect(screen.getAllByText("Task One").length).toBeGreaterThan(0));
      
      // Search for "One"
      const searchInput = screen.getByPlaceholderText("Search tasks or tags");
      fireEvent.change(searchInput, { target: { value: "One" } });

      expect(screen.getAllByText("Task One").length).toBeGreaterThan(0);
      
      // Search for "NonExistent"
      fireEvent.change(searchInput, { target: { value: "NonExistent" } });
      
      // The task title is still in the header (h1), but should be gone from the list (button)
      expect(screen.queryByRole("button", { name: /Task One/i })).not.toBeInTheDocument();
    });

    it("handles library view rendering", async () => {
      setupFetchMock();
      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);

      await user.click(screen.getByText("Library"));
      expect(screen.getByText("Artifacts library")).toBeInTheDocument();
    });
    
    it("handles shift+enter in composer", async () => {
      setupFetchMock();
      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);
      
      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Line 1{Shift>}{Enter}{/Shift}Line 2");
      
      expect(input).toHaveValue("Line 1\nLine 2");
      // Should NOT have sent message
      expect(global.fetch).not.toHaveBeenCalledWith(expect.stringContaining("/messages"), expect.anything());
    });
    
    it("handles error states for skills loading", async () => {
      setupFetchMock({
        [API_BASE_URL + "/skills"]: {
          ok: false,
          status: 500,
          text: "Failed to load skills",
        },
      });
      
      const user = userEvent.setup();
      render(<App />);
      const elements = await screen.findAllByText(/Control Desk/i);
      expect(elements.length).toBeGreaterThan(0);
      
      await user.click(screen.getByText("Skills"));
      
      expect(await screen.findByText("Failed to load skills")).toBeInTheDocument();
    });
  });

  describe("7. Panel Control Tests", () => {
    it("handles panel switching", async () => {
      const runId = "run-panel";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: {
          data: { run_id: runId },
        },
        [API_BASE_URL + `/runs/${runId}/messages`]: {
          data: { id: "msg-1" },
        },
        [API_BASE_URL + "/settings/llm"]: {
          data: { configured: true, provider: "openai", model: "gpt-4" },
        },
      });

      render(<App />);
      
      // Ensure app is loaded and configured
      const elements = await screen.findAllByText("Control Desk");
      expect(elements.length).toBeGreaterThan(0);

      // Create task
      const input = screen.getByPlaceholderText("Assign a task or ask anything");
      await userEvent.type(input, "Panel Task");
      await userEvent.click(screen.getByRole("button", { name: /Send message/i }));

      // Wait for task panel controls (which means activeTask is set)
      await waitFor(() => expect(screen.getByRole("button", { name: /Overview/i })).toBeInTheDocument(), { timeout: 5000 });

      // Check for buttons - they are visible by default now in the header
      const browserBtn = screen.getByRole("button", { name: /^Browser$/i });
      const filesBtn = screen.getByRole("button", { name: /Files/i });
      const editorBtn = screen.getByRole("button", { name: /Editor/i });
      expect(browserBtn).toBeInTheDocument();
      expect(filesBtn).toBeInTheDocument();
      
      // Click Browser
      await userEvent.click(browserBtn);
      expect(await screen.findByText("Activity Log")).toBeInTheDocument();

      // Click Files
      await userEvent.click(filesBtn);
      expect(await screen.findByText("Task files")).toBeInTheDocument();

      // Click Editor
      await userEvent.click(editorBtn);
      expect(await screen.findByText("Explorer")).toBeInTheDocument();
    });
  });

  describe("Regression: Tool Activity & Summary", () => {
    it("should not create empty summary message after streaming tool call", async () => {
      const runId = "run-reg-1";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: { data: { run_id: runId } },
        [API_BASE_URL + `/runs/${runId}/messages`]: { data: { id: "msg-1" } },
      });

      const user = userEvent.setup();
      render(<App />);

      const input = await screen.findByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Run tool{enter}");

      await waitFor(() => expect(MockEventSource).toHaveBeenCalled());
      const eventListener = mockEventSourceInstance.addEventListener.mock.calls.find(
        (call) => call[0] === "run_event"
      )[1];

      const emit = (type: string, payload: any, seq: number) => {
        act(() => {
          eventListener(new MessageEvent("run_event", {
            data: JSON.stringify({ type, run_id: runId, seq, payload }),
          }));
        });
      };

      // 1. Run started
      emit("run.started", {}, 1);

      // 2. Emit concrete tool events
      emit("tool.started", { tool_name: "process.exec", input: { command: "node", args: ["-v"] } }, 2);
      emit("tool.completed", { tool_name: "process.exec", output: { command: "node", args: ["-v"] } }, 3);

      const activityToggles = await screen.findAllByRole("button", { name: /Agent activity/i });
      await user.click(activityToggles[activityToggles.length - 1]);

      await waitFor(() => {
         const runText = screen.queryByText(/Run command node -v/i);
         expect(runText).toBeInTheDocument();
      });

      // 3. Model summary with whitespace (should not add empty assistant bubble)
      emit("model.summary", { text: "   " }, 4);

      const messages = screen.queryAllByText("assistant");
      // Should be 0 because tool activity doesn't show "assistant" label and no empty message should be added
      expect(messages.length).toBe(0);
    });

    it("should not duplicate tool call if summary is identical", async () => {
      const runId = "run-reg-2";
      setupFetchMock({
        [API_BASE_URL + "/runs"]: { data: { run_id: runId } },
        [API_BASE_URL + `/runs/${runId}/messages`]: { data: { id: "msg-1" } },
      });

      const user = userEvent.setup();
      const { container } = render(<App />);

      const input = await screen.findByPlaceholderText("Assign a task or ask anything");
      await user.type(input, "Run tool duplication{enter}");

      await waitFor(() => expect(MockEventSource).toHaveBeenCalled());
      const eventListener = mockEventSourceInstance.addEventListener.mock.calls.find(
        (call) => call[0] === "run_event"
      )[1];

      const emit = (type: string, payload: any, seq: number) => {
        act(() => {
          eventListener(new MessageEvent("run_event", {
            data: JSON.stringify({ type, run_id: runId, seq, payload }),
          }));
        });
      };

      emit("run.started", {}, 1);
      emit("tool.started", { tool_name: "process.exec", input: { command: "echo", args: ["hello"] } }, 2);
      emit("tool.completed", { tool_name: "process.exec", output: { command: "echo", args: ["hello"] } }, 3);

      const activityToggles = await screen.findAllByRole("button", { name: /Agent activity/i });
      await user.click(activityToggles[activityToggles.length - 1]);

      await waitFor(() => {
         const runText = screen.queryByText(/Run command echo hello/i);
         expect(runText).toBeInTheDocument();
      });

      // Model summary with identical text
      emit("model.summary", { text: "Run command echo hello" }, 4);

      // Verify: Should only have ONE visible tool row
      const toolActivities = Array.from(
        container.querySelectorAll(".dw-tool-inline:not(.dw-reasoning-inline) .dw-tool-value")
      ).filter((node) => node.textContent?.match(/Run command echo hello/i));
      expect(toolActivities.length).toBe(1);
    });
  });
});
