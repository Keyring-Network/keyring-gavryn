import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AutomationsPanel } from "./AutomationsPanel";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "http://localhost:8080";

describe("AutomationsPanel", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("loads automations and inbox with unread count callback", async () => {
    const onUnreadCountChange = vi.fn();
    const automation = {
      id: "auto-1",
      name: "Daily RWA Brief",
      prompt: "Summarize top RWA stories.",
      model: "gpt-5.2-codex",
      days: ["mon", "tue", "wed", "thu", "fri"],
      time: "09:00",
      timezone: "UTC",
      enabled: true,
      next_run_at: "2026-02-10T09:00:00Z",
      last_run_at: "2026-02-09T09:00:00Z",
      in_progress: false,
      unread_count: 1,
      created_at: "2026-02-09T09:00:00Z",
      updated_at: "2026-02-09T09:00:00Z",
    };

    vi.spyOn(global, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === `${API_BASE_URL}/automations`) {
        return new Response(JSON.stringify({ automations: [automation], unread_count: 1 }), { status: 200 });
      }
      if (url === `${API_BASE_URL}/automations/auto-1/inbox`) {
        return new Response(
          JSON.stringify({
            automation,
            unread_count: 1,
            inbox: [
              {
                id: "entry-1",
                automation_id: "auto-1",
                status: "completed",
                trigger: "schedule",
                started_at: "2026-02-09T09:00:00Z",
                completed_at: "2026-02-09T09:02:00Z",
                final_response: "Automation summary output",
                unread: true,
              },
            ],
          }),
          { status: 200 }
        );
      }
      return new Response("{}", { status: 200 });
    });

    render(
      <AutomationsPanel
        defaultModel="gpt-5.2-codex"
        modelOptions={["gpt-5.2-codex", "gpt-4.1"]}
        onUnreadCountChange={onUnreadCountChange}
      />
    );

    expect((await screen.findAllByText("Daily RWA Brief")).length).toBeGreaterThan(0);
    expect(await screen.findByText(/Automation summary output/)).toBeInTheDocument();
    await waitFor(() => {
      expect(onUnreadCountChange).toHaveBeenCalledWith(1);
    });
  });

  it("creates a new automation", async () => {
    const user = userEvent.setup();
    const automations: Array<Record<string, unknown>> = [];

    vi.spyOn(global, "fetch").mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method || "GET";

      if (url === `${API_BASE_URL}/automations` && method === "GET") {
        return new Response(JSON.stringify({ automations, unread_count: 0 }), { status: 200 });
      }

      if (url === `${API_BASE_URL}/automations` && method === "POST") {
        const payload = JSON.parse(String(init?.body || "{}")) as Record<string, unknown>;
        const created = {
          id: "auto-created",
          created_at: "2026-02-09T09:00:00Z",
          updated_at: "2026-02-09T09:00:00Z",
          ...payload,
        };
        automations.push(created);
        return new Response(JSON.stringify(created), { status: 201 });
      }

      if (url === `${API_BASE_URL}/automations/auto-created/inbox`) {
        return new Response(
          JSON.stringify({
            automation: automations[0] || {},
            unread_count: 0,
            inbox: [],
          }),
          { status: 200 }
        );
      }

      return new Response("{}", { status: 200 });
    });

    render(
      <AutomationsPanel
        defaultModel="gpt-5.2-codex"
        modelOptions={["gpt-5.2-codex"]}
      />
    );

    await user.type(screen.getByPlaceholderText("Daily DeFi brief"), "Morning research");
    await user.type(
      screen.getByPlaceholderText("Browse the web and send me a daily RWA market briefing with sources."),
      "Summarize top DeFi and RWA news."
    );
    await user.click(screen.getByRole("button", { name: "Create automation" }));

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalledWith(
        `${API_BASE_URL}/automations`,
        expect.objectContaining({ method: "POST" })
      );
    });
    expect((await screen.findAllByText("Morning research")).length).toBeGreaterThan(0);
  });
});
