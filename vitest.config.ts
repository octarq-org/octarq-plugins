import { defineConfig } from "vitest/config";

const SHARED_PACKAGES = [
  "react",
  "react-dom",
  "@base-ui/react",
  "class-variance-authority",
  "clsx",
  "framer-motion",
  "sonner",
  "tailwind-merge",
];

export default defineConfig({
  esbuild: { jsx: "automatic" },
  ssr: {
    noExternal: [/@octarq\/plugin-sdk/],
  },
  resolve: {
    dedupe: SHARED_PACKAGES,
  },
  test: {
    environment: "jsdom",
    include: ["*/web/**/*.test.{ts,tsx}"],
    server: {
      deps: { inline: true },
    },
  },
});
