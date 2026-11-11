import { defineConfig } from "vitest/config";

// Component tests live next to the plugin UI they cover. The plugins and the
// SDK each declare React as a peer, so a workspace install can hoist two
// copies; dedupe pins every React consumer to this workspace's single copy and
// the inline setting keeps Vitest from loading those packages through Node,
// which would bypass the dedupe.
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
      // Vitest would otherwise load node_modules through Node, bypassing the
      // dedupe above.
      deps: { inline: true },
    },
  },
});
