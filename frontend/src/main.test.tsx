import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderApp } from './main';
import ReactDOM from 'react-dom/client';

// Mock App component
vi.mock('./App', () => ({
  default: () => <div data-testid="mock-app">Mock App</div>
}));

// Mock ReactDOM.createRoot
const renderMock = vi.fn();
const createRootMock = vi.fn(() => ({
  render: renderMock,
  unmount: vi.fn(),
}));

vi.spyOn(ReactDOM, 'createRoot').mockImplementation(createRootMock as any);

describe('main.tsx', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renderApp renders App component', () => {
    const container = document.createElement('div');
    renderApp(container);
    
    expect(createRootMock).toHaveBeenCalledWith(container);
    expect(renderMock).toHaveBeenCalled();
  });

  it('renderApp mounts to container', () => {
     const container = document.createElement('div');
     renderApp(container);
     expect(createRootMock).toHaveBeenCalledWith(container);
  });

  it('executes renderApp if root element exists', async () => {
    const root = document.createElement('div');
    root.id = 'root';
    document.body.appendChild(root);

    vi.resetModules();
    await import('./main');
    
    expect(createRootMock).toHaveBeenCalledWith(root);
    document.body.removeChild(root);
  });
});
