import { lazy } from "react";
import type { UIPlugin } from "@octarq/plugin-sdk";
import { maillinkI18n } from "./i18n";

export const maillinkPlugin: UIPlugin = {
  name: "maillink",
  routes: [{ path: "/maillink", Component: lazy(() => import("./Page")) }],
  i18n: maillinkI18n,
};

export default maillinkPlugin;
