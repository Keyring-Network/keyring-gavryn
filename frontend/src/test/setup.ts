import '@testing-library/jest-dom';
import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';
import React from 'react';

afterEach(() => {
  cleanup();
});

// Mock react-resizable-panels to avoid JSDOM cursor/style errors
vi.mock("react-resizable-panels", () => {
  return {
    Group: ({ children }: { children: React.ReactNode }) => React.createElement("div", { "data-testid": "panel-group" }, children),
    Panel: ({ children }: { children: React.ReactNode }) => React.createElement("div", { "data-testid": "panel" }, children),
    Separator: () => React.createElement("div", { "data-testid": "panel-resize-handle" }),
    PanelGroup: ({ children }: { children: React.ReactNode }) => React.createElement("div", { "data-testid": "panel-group" }, children), // Keep for backward compat if needed
    PanelResizeHandle: () => React.createElement("div", { "data-testid": "panel-resize-handle" }), // Keep for backward compat
    useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
  };
});
