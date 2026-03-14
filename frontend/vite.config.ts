import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react-swc";
import wails from "@wailsio/runtime/plugins/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    tanstackRouter({
      autoCodeSplitting: true,
      routeFileIgnorePattern:
        "(^|[\\\\/])(page\\.(?:ts|tsx|js|jsx)|components|hooks|catalog)([\\\\/]|$)",
      target: "react",
    }),
    tailwindcss(),
    react(),
    wails("./bindings"),
  ],
});
