import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTypeScript from "eslint-config-next/typescript";

export default defineConfig([
  ...nextVitals,
  ...nextTypeScript,
  {
    linterOptions: {
      noInlineConfig: true,
      reportUnusedDisableDirectives: "error",
    },
  },
  {
    files: ["**/*.{ts,tsx,js,jsx,mjs}"],
    ignores: ["**/*.test.{ts,tsx,js,jsx,mjs}", "**/*.spec.{ts,tsx,js,jsx,mjs}", "e2e/**"],
    rules: {
      complexity: ["error", 15],
      curly: ["error", "all"],
      eqeqeq: ["error", "always"],
      "max-depth": ["error", 4],
      "max-lines": ["error", { max: 600, skipBlankLines: false, skipComments: false }],
      "max-lines-per-function": [
        "error",
        { max: 250, skipBlankLines: false, skipComments: false, IIFEs: true },
      ],
      "no-console": ["error", { allow: ["warn", "error"] }],
      "prefer-const": "error",
    },
  },
  {
    files: ["**/*.test.{ts,tsx,js,jsx,mjs}", "**/*.spec.{ts,tsx,js,jsx,mjs}", "e2e/**/*.{ts,tsx,js,jsx,mjs}"],
    rules: {
      complexity: ["error", 15],
      curly: ["error", "all"],
      eqeqeq: ["error", "always"],
      "max-depth": ["error", 4],
      "max-lines": ["error", { max: 800, skipBlankLines: false, skipComments: false }],
      "max-lines-per-function": [
        "error",
        { max: 350, skipBlankLines: false, skipComments: false, IIFEs: true },
      ],
      "no-console": ["error", { allow: ["warn", "error"] }],
      "prefer-const": "error",
    },
  },
  {
    files: ["**/*.ts", "**/*.tsx"],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "@typescript-eslint/consistent-type-imports": "error",
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "error",
    },
  },
  globalIgnores([".next/**", ".next-build/**", ".next-e2e/**", ".next-netplay-*/**", "lib/api/generated/**", "playwright-report/**", "test-results/**"]),
]);
