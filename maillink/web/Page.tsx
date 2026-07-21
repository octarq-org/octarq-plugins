import { useEffect, useState } from "react";
import {
  ScreenWrap,
  PageHeader,
  GlassCard,
  Empty,
  LockedFallback,
  useTranslation,
} from "@octarq/plugin-sdk";

interface MailLink {
  ID: number;
  Slug: string;
  Target: string;
  Subject: string;
  From: string;
}

export default function MailLinksPage() {
  const { t } = useTranslation();
  const [links, setLinks] = useState<MailLink[]>([]);
  const [status, setStatus] = useState<number | null>(null);

  useEffect(() => {
    fetch("/api/maillink/recent", { credentials: "same-origin" })
      .then((res) => {
        if (!res.ok) {
          setStatus(res.status);
          return null;
        }
        return res.json() as Promise<MailLink[]>;
      })
      .then((rows) => rows && setLinks(rows))
      .catch(() => setStatus(0));
  }, []);

  if (status === 402 || status === 404) {
    return (
      <ScreenWrap>
        <LockedFallback status={status} feature="Mail Links" description={t("maillink.pageDesc")} />
      </ScreenWrap>
    );
  }

  return (
    <ScreenWrap>
      <PageHeader title={t("maillink.pageTitle", "Mail Links")} description={t("maillink.pageDesc")} />
      {links.length === 0 ? (
        <Empty>{t("maillink.empty")}</Empty>
      ) : (
        <div className="space-y-2">
          {links.map((l) => (
            <GlassCard key={l.ID} className="flex items-center justify-between gap-4 p-4">
              <div className="min-w-0">
                <p className="truncate text-sm text-white/80">/{l.Slug}</p>
                <p className="truncate text-xs text-white/40">{l.Target}</p>
              </div>
              <div className="shrink-0 text-right text-xs text-white/40">
                <p className="truncate">{l.Subject || "(no subject)"}</p>
                <p className="truncate">{l.From}</p>
              </div>
            </GlassCard>
          ))}
        </div>
      )}
    </ScreenWrap>
  );
}
