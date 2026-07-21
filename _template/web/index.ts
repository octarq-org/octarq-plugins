// The plugin's frontend entry — the JS half's UIPlugin, mirroring the Go half's
// plugin.Plugin in ../plugin.go. A host composes it at build time by listing
// this package in its plugin manifest (octarq's web/octarq.plugins.json), which
// generates a `registerUIPlugin(myPlugin)` call. Besides routes/menu/i18n a
// UIPlugin may also contribute dashboard `widgets` and new top-level sidebar
// `areas` — see the UIPlugin type in @octarq/plugin-sdk.
import { lazy } from "react";
import type { UIPlugin } from "@octarq/plugin-sdk";

export const myPlugin: UIPlugin = {
  name: "myplugin", // MUST match Plugin.Name() in ../plugin.go
  routes: [
    { path: "/myplugin", Component: lazy(() => import("./Page")) },
  ],
  menu: [
    // `category` names the sidebar GROUP the entry joins: it must equal the
    // group's label (here the "Workspace" group next to Overview). Keep it in
    // sync with plugin.go Menus().
    { id: "myplugin", label: "My Plugin", path: "/myplugin", icon: "🧩", category: "Workspace" },
  ],
  i18n: {
    // Keys merge under your `name` namespace, so `pageTitle` is read as
    // t("myplugin.pageTitle") — no collisions with core or other plugins.
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

// A plugin package default-exports its UIPlugin (or an array), so the host
// manifest can compose it with `import myPlugin from "<this-package>"`.
export default myPlugin;
