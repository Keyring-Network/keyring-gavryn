import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { BrowserPanel } from "./BrowserPanel";
import type { RunEvent } from "@/lib/events";

describe("BrowserPanel", () => {
  it("uses browser.navigation URL in the address bar", () => {
    const events: RunEvent[] = [
      {
        run_id: "r1",
        seq: 1,
        type: "browser.navigation",
        ts: "2026-02-07T00:00:00Z",
        source: "browser_worker",
        payload: { url: "https://example.com", status: "completed" },
      },
    ];

    render(<BrowserPanel events={events} />);
    expect(screen.getAllByText("https://example.com").length).toBeGreaterThan(0);
  });

  it("stops loading spinner when snapshot follows latest interaction", () => {
    const events: RunEvent[] = [
      {
        run_id: "r1",
        seq: 1,
        type: "browser.click",
        ts: "2026-02-07T00:00:00Z",
        source: "browser_worker",
        payload: { selector: "#go" },
      },
      {
        run_id: "r1",
        seq: 2,
        type: "browser.snapshot",
        ts: "2026-02-07T00:00:01Z",
        source: "browser_worker",
        payload: { uri: "https://example.com/snap.png" },
      },
    ];

    const { container } = render(<BrowserPanel events={events} latestSnapshotUri="https://example.com/snap.png" />);
    expect(container.querySelector(".animate-spin")).not.toBeInTheDocument();
  });

  it("keeps selected snapshot when new snapshots arrive", () => {
    const baseEvents: RunEvent[] = [
      {
        run_id: "r1",
        seq: 1,
        type: "browser.snapshot",
        ts: "2026-02-07T00:00:01Z",
        source: "browser_worker",
        payload: { uri: "https://example.com/first.png" },
      },
      {
        run_id: "r1",
        seq: 2,
        type: "browser.snapshot",
        ts: "2026-02-07T00:00:02Z",
        source: "browser_worker",
        payload: { uri: "https://example.com/second.png" },
      },
    ];

    const { rerender } = render(<BrowserPanel events={baseEvents} />);
    const firstThumb = screen.getByRole("button", { name: /snapshot 1/i });
    fireEvent.click(firstThumb);

    const mainImage = screen.getByAltText("Browser snapshot") as HTMLImageElement;
    expect(mainImage.src).toContain("first.png");

    rerender(
      <BrowserPanel
        events={[
          ...baseEvents,
          {
            run_id: "r1",
            seq: 3,
            type: "browser.snapshot",
            ts: "2026-02-07T00:00:03Z",
            source: "browser_worker",
            payload: { uri: "https://example.com/third.png" },
          },
        ]}
      />,
    );

    expect((screen.getByAltText("Browser snapshot") as HTMLImageElement).src).toContain("first.png");
  });

  it("keeps selected snapshot when the same artifact URI is re-emitted with new query params", () => {
    const baseEvents: RunEvent[] = [
      {
        run_id: "r1",
        seq: 1,
        type: "browser.snapshot",
        ts: "2026-02-07T00:00:01Z",
        source: "browser_worker",
        payload: { uri: "https://example.com/first.png?token=one" },
      },
      {
        run_id: "r1",
        seq: 2,
        type: "browser.snapshot",
        ts: "2026-02-07T00:00:02Z",
        source: "browser_worker",
        payload: { uri: "https://example.com/second.png?token=one" },
      },
    ];

    const { rerender } = render(<BrowserPanel events={baseEvents} />);
    fireEvent.click(screen.getByRole("button", { name: /snapshot 1/i }));
    expect((screen.getByAltText("Browser snapshot") as HTMLImageElement).src).toContain("first.png");

    rerender(
      <BrowserPanel
        events={[
          {
            ...baseEvents[0],
            seq: 3,
            payload: { uri: "https://example.com/first.png?token=two" },
          },
          {
            ...baseEvents[1],
            seq: 4,
            payload: { uri: "https://example.com/second.png?token=two" },
          },
        ]}
      />,
    );

    expect((screen.getByAltText("Browser snapshot") as HTMLImageElement).src).toContain("first.png");
  });
});
