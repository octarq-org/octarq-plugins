import { useCallback, useEffect, useState } from "react";
import {
  PageShell,
  PageCard,
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
  req,
  ApiError,
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
    req<MailLink[]>("GET", "/api/maillink/recent")
      .then((rows) => setLinks(rows))
      .catch((err: unknown) => {
        // 402 and 404 have their own screen; any other status is a failure.
        const code = err instanceof ApiError ? err.status : 0;
        if (code === 402 || code === 404) setStatus(code);
        else setError(code);
      });
  }, []);

  useEffect(load, [load]);

  if (status === 402 || status === 404) {
    return (
      <PageShell title={t("maillink.pageTitle", "Mail Links")}>
        <LockedFallback status={status} feature="Mail Links" description={t("maillink.pageDesc")} />
      </PageShell>
    );
  }

  return (
    <PageShell
      width="full"
      title={t("maillink.pageTitle", "Mail Links")}
      description={t("maillink.pageDesc")}
    >
      {error !== null ? (
        <TableError error={error} onRetry={load} />
      ) : links === null ? (
        <TableSkeleton columnsCount={4} rowsCount={5} />
      ) : links.length === 0 ? (
        <TableEmpty emptyText={t("maillink.empty")} />
      ) : (
        <PageCard className="mt-0 overflow-hidden p-0">
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
        </PageCard>
      )}
    </PageShell>
  );
}
