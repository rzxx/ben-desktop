import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "@/app/styles/index.css";
import { applyThemeToDocument, getInitialThemeState } from "@/lib/theme/bootstrap";

const rootElement = document.getElementById("root");

if (!rootElement) {
  throw new Error("Root element not found");
}

applyThemeToDocument(getInitialThemeState());

if (!rootElement.innerHTML) {
  ReactDOM.createRoot(rootElement).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
}
