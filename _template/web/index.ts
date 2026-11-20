import { lazy } from "react";
import type { UIPlugin } from "@octarq/plugin-sdk";

export const myPlugin: UIPlugin = {
  name: "myplugin",
  routes: [
    { path: "/myplugin", Component: lazy(() => import("./Page")) },
  ],
  i18n: {
    en: {
      pageTitle: "My Plugin",
      pageDesc: "A starter full-stack Octarq plugin.",
      feature: "My Plugin",
      description: "A starter Octarq plugin.",
      loading: "Loading…",
    },
    zh: {
      pageTitle: "我的插件",
      pageDesc: "一个全栈 Octarq 插件起点。",
      feature: "我的插件",
      description: "一个 Octarq 插件起点。",
      loading: "加载中…",
    },
  },
};

export default myPlugin;
