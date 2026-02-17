import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Messages } from "./Messages";

describe("Messages feed", () => {
  it("renders grouped tool summary and message bubbles", () => {
    render(
      <Messages
        messages={[
          { id: "u1", role: "user", content: "run this", seq: 1 },
          { id: "a1", role: "assistant", content: "done", seq: 6 },
        ]}
        events={[
          {
            run_id: "run-1",
            seq: 2,
            type: "tool.started",
            source: "tool_runner",
            payload: { tool_name: "editor.read", input: { path: "README.md" } },
          },
          {
            run_id: "run-1",
            seq: 3,
            type: "tool.completed",
            source: "tool_runner",
            payload: { tool_name: "editor.read", input: { path: "README.md" }, output: { path: "README.md" } },
          },
        ]}
        isThinking={false}
      />,
    );

    expect(screen.getByText("run this")).toBeInTheDocument();
    expect(screen.getByText(/agent activity/i)).toBeInTheDocument();
    expect(screen.getByText(/1 tool call/i)).toBeInTheDocument();
    expect(screen.getByText("done")).toBeInTheDocument();
  });

  it("shows jump to latest when user scrolls up and new events arrive", () => {
    const { container, rerender } = render(
      <Messages
        messages={[{ id: "u1", role: "user", content: "run this", seq: 1 }]}
        events={[]}
        isThinking={false}
      />,
    );

    const scroller = container.querySelector(".dw-messages") as HTMLDivElement;
    Object.defineProperty(scroller, "clientHeight", { configurable: true, value: 200 });
    Object.defineProperty(scroller, "scrollHeight", { configurable: true, value: 1200 });
    Object.defineProperty(scroller, "scrollTop", { configurable: true, writable: true, value: 0 });

    fireEvent.scroll(scroller);

    rerender(
      <Messages
        messages={[{ id: "u1", role: "user", content: "run this", seq: 1 }]}
        events={[
          {
            run_id: "run-1",
            seq: 2,
            type: "tool.started",
            source: "tool_runner",
            payload: { tool_invocation_id: "inv-2", tool_name: "editor.read", input: { path: "README.md" } },
          },
        ]}
        isThinking={false}
      />,
    );

    expect(screen.getByText(/jump to latest/i)).toBeInTheDocument();
  });
});
