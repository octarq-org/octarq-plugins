import { StatCard, useOverviewData, useTranslation } from "@octarq/plugin-sdk";
import { useNavigate } from "react-router-dom";
import { Mail } from "lucide-react";

export default function MailStatCardWidget() {
  const o = useOverviewData();
  const nav = useNavigate();
  const { t } = useTranslation();

  if (!o || o.mailboxes === undefined) return null;

  return (
    <StatCard
      label={t("overview.mailboxes")}
      value={(o.mailboxes ?? 0).toLocaleString()}
      delta={t("overview.unread", { count: o.unread ?? 0 })}
      icon={<Mail className="h-4 w-4" />}
      onClick={() => nav("/mail")}
      index={2}
    />
  );
}
