import { lazy } from "react";
import type { UIPlugin } from "@octarq/plugin-sdk";
import { ptI18n } from "./i18n";

const pt: UIPlugin = {
  name: "pt",
  routes: [{ path: "/personal/pt", Component: lazy(() => import("./Page")) }],
  i18n: ptI18n,
};

export default pt;
