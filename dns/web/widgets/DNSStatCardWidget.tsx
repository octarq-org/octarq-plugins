import { StatCard, useOverviewData, useTranslation } from "@octarq/plugin-sdk";
import { useNavigate } from "react-router-dom";
import { Globe } from "lucide-react";

export default function DNSStatCardWidget() {
  const o = useOverviewData();
  const nav = useNavigate();
  const { t } = useTranslation();

  if (!o || o.domains === undefined) return null;

  return (
    <StatCard
      label={t("overview.domains")}
      value={(o.domains ?? 0).toLocaleString()}
      delta={t("overview.domainsDelta", { link: o.linkDomains ?? 0, mail: o.mailDomains ?? 0 })}
      icon={<Globe className="h-4 w-4" />}
      onClick={() => nav("/domains")}
      index={3}
    />
  );
}
