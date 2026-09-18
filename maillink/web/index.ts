import { lazy } from "react";
import type { UIPlugin } from "@octarq/plugin-sdk";

export const maillinkPlugin: UIPlugin = {
  name: "maillink",
  routes: [{ path: "/maillink", Component: lazy(() => import("./Page")) }],
  i18n: {
    en: {
      pageTitle: "Mail Links",
      pageDesc: "Short links auto-created from your inbound email — and available to AI agents over MCP.",
      empty: "No links yet. They appear here when an email containing a URL arrives.",
    },
    zh: {
      pageTitle: "邮件短链",
      pageDesc: "从收到的邮件里自动生成的短链，同时通过 MCP 提供给 AI Agent。",
      empty: "还没有短链。当收到含 URL 的邮件时会出现在这里。",
    },
  },
};

export default maillinkPlugin;
