import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import eslintConfigPrettier from "eslint-config-prettier";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";
import pluginRouter from "@tanstack/eslint-plugin-router";

export default defineConfig(
  globalIgnores(["bindings", "dist", "node_modules"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [
      js.configs.recommended,
      ...tseslint.configs.recommended,
      reactHooks.configs.flat["recommended-latest"],
      reactRefresh.configs.vite,
      eslintConfigPrettier,
      ...pluginRouter.configs["flat/recommended"],
    ],
    languageOptions: {
      ecmaVersion: "latest",
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/lib/api/**/*"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/bindings/**", "**/bindings/**"],
              message:
                "Import generated Wails bindings only from src/lib/api/*.",
            },
          ],
        },
      ],
    },
  },
  {
    files: [
      "src/app/**/*.{ts,tsx}",
      "src/components/**/*.{ts,tsx}",
      "src/hooks/**/*.{ts,tsx}",
      "src/stores/**/*.{ts,tsx}",
      "src/lib/**/*.{ts,tsx}",
    ],
    ignores: ["src/routes/**/*", "src/lib/api/**/*"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/routes/**"],
              message: "Do not import from route modules outside src/routes.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/lib/**/*.{ts,tsx}"],
    ignores: ["src/lib/api/**/*"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["react", "@/hooks/**", "@/components/**", "@/app/**"],
              message:
                "Keep src/lib framework-agnostic and outside React/UI layers.",
            },
          ],
        },
      ],
      "no-restricted-syntax": [
        "error",
        {
          selector: "FunctionDeclaration[id.name=/^use[A-Z]/]",
          message: "React hooks must live under src/hooks.",
        },
        {
          selector: "VariableDeclarator[id.name=/^use[A-Z]/]",
          message: "React hooks must live under src/hooks.",
        },
      ],
    },
  },
  {
    files: ["src/routes/**/page.tsx"],
    rules: {
      "no-restricted-syntax": [
        "error",
        {
          selector: "FunctionDeclaration[id.name=/^[A-Z]/][id.name!=/Page$/]",
          message:
            "Move local components out of page.tsx into src/components/*.",
        },
      ],
    },
  },
);
