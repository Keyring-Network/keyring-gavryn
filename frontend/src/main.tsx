import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

export function renderApp(container: HTMLElement) {
  ReactDOM.createRoot(container).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>
  );
}

const rootElement = document.getElementById("root");
if (rootElement) {
  renderApp(rootElement);
}
