import { useCallback, useEffect, useState } from "react";
import {
  ScreenWrap,
  PageHeader,
  Table,
  THead,
  TBody,
  TR,
  TH,
  TD,
  TableEmpty,
  TableError,
  TableSkeleton,
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
  const [links, setLinks] = useState<MailLink[] | null>(null);
  const [status, setStatus] = useState<number | null>(null);
  const [error, setError] = useState<number | null>(null);

  const load = useCallback(() => {
    setError(null);
    fetch("/api/maillink/recent", { credentials: "same-origin" })
      .then((res) => {
        if (!res.ok) {
          setStatus(res.status);
          return null;
        }
        return res.json() as Promise<MailLink[]>;
      })
      .then((rows) => rows && setLinks(rows))
      .catch(() => setError(0));
  }, []);

  useEffect(load, [load]);

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
      {error !== null ? (
        <TableError error={error} onRetry={load} />
      ) : links === null ? (
        <TableSkeleton columnsCount={4} rowsCount={5} />
      ) : links.length === 0 ? (
        <TableEmpty emptyText={t("maillink.empty")} />
      ) : (
        <Table>
          <THead>
            <TR>
              <TH>{t("maillink.colShort", "Short link")}</TH>
              <TH>{t("maillink.colTarget", "Target")}</TH>
              <TH>{t("maillink.colSubject", "Subject")}</TH>
              <TH>{t("maillink.colFrom", "From")}</TH>
            </TR>
          </THead>
          <TBody>
            {links.map((l) => (
              <TR key={l.ID}>
                <TD className="font-mono text-xs text-foreground">/{l.Slug}</TD>
                <TD className="max-w-[18rem] truncate text-xs text-muted-foreground">{l.Target}</TD>
                <TD className="max-w-[18rem] truncate text-xs">{l.Subject || "—"}</TD>
                <TD className="max-w-[14rem] truncate text-xs text-muted-foreground">{l.From}</TD>
              </TR>
            ))}
          </TBody>
        </Table>
      )}
    </ScreenWrap>
  );
}
