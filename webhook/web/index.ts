import { lazy } from "react";
import type { UIPlugin } from "@octarq/plugin-sdk";

// The frontend half — a settings page for the Webhook connector. `name` must
// match the Go Plugin.Name(). See the backend in ../plugin.go.
export const webhookPlugin: UIPlugin = {
  name: "webhook",
  routes: [
    { path: "/webhook", Component: lazy(() => import("./Page")) },
  ],
  i18n: {
    en: {
      pageTitle: "Webhook",
      pageDesc: "Forward inbound email to a webhook URL, and let AI agents ping external endpoints via MCP.",
      url: "Webhook URL",
      save: "Save",
      test: "Send test",
      saved: "Saved.",
      testSent: "Test message sent.",
    },
    zh: {
      pageTitle: "Webhook",
      pageDesc: "把收到的邮件转发到 Webhook URL，并让 AI Agent 通过 MCP 触发 HTTP 请求。",
      url: "Webhook 地址",
      save: "保存",
      test: "发送测试",
      saved: "已保存。",
      testSent: "测试消息已发送。",
    },
  },
};

export default webhookPlugin;
