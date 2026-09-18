import { lazy } from "react";
import type { UIPlugin } from "@octarq/plugin-sdk";

// The frontend half — a settings page for the Telegram connector. `name` must
// match the Go Plugin.Name(). See the backend in ../plugin.go.
export const telegramPlugin: UIPlugin = {
  name: "telegram",
  routes: [
    { path: "/telegram", Component: lazy(() => import("./Page")) },
  ],
  i18n: {
    en: {
      pageTitle: "Telegram",
      pageDesc: "Forward inbound email to a Telegram chat, and let AI agents ping you via MCP.",
      botToken: "Bot token",
      chatId: "Chat ID",
      save: "Save",
      test: "Send test",
      saved: "Saved.",
      testSent: "Test message sent.",
    },
    zh: {
      pageTitle: "Telegram",
      pageDesc: "把收到的邮件转发到 Telegram 群/私聊，并让 AI Agent 通过 MCP 给你发消息。",
      botToken: "Bot Token",
      chatId: "Chat ID",
      save: "保存",
      test: "发送测试",
      saved: "已保存。",
      testSent: "测试消息已发送。",
    },
  },
};

export default telegramPlugin;
