import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react-swc";
import wails from "@wailsio/runtime/plugins/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

// https://vitejs.dev/config/
export default defineConfig({
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("/node_modules/react-dom/")) {
            return "react-dom";
          }
          if (
            id.includes("/node_modules/react/") ||
            id.includes("/node_modules/scheduler/")
          ) {
            return "react-core";
          }
          if (id.includes("/node_modules/@tanstack/")) {
            return "tanstack";
          }
          if (
            id.includes("/bindings/") ||
            id.includes("/node_modules/@wailsio/runtime/")
          ) {
            return "wails";
          }
          if (
            id.includes("/node_modules/@base-ui/") ||
            id.includes("/node_modules/@floating-ui/") ||
            id.includes("/node_modules/tabbable/")
          ) {
            return "base-ui";
          }
          if (
            id.includes("/node_modules/zustand/") ||
            id.includes("/node_modules/mutative/") ||
            id.includes("/node_modules/reselect/")
          ) {
            return "state";
          }
        },
      },
    },
  },
  plugins: [
    tanstackRouter({
      autoCodeSplitting: true,
      routeFileIgnorePattern: "(^|[\\\\/])page\\.(?:ts|tsx|js|jsx)$",
      target: "react",
    }),
    tailwindcss(),
    react(),
    wails("./bindings"),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
  },
});
