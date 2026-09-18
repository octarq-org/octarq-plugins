import { useState, useEffect } from "react";
import {
  Modal,
  Button,
  Badge,
  Table,
  THead,
  TBody,
  TR,
  TH,
  TD,
  TableSkeleton,
  TableEmpty,
} from "@octarq/plugin-sdk";
import type { AuditLog } from "../types";

interface Props {
  onClose: () => void;
  t: (key: string, fallback?: string) => string;
}

export function AuditLogsModal({ onClose, t }: Props) {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setLoading(true);
    fetch("/api/twofa/logs", { credentials: "same-origin" })
      .then((res) => (res.ok ? (res.json() as Promise<AuditLog[]>) : []))
      .then((data) => {
        setLogs(data);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  }, []);

  const getBadgeTone = (action: string) => {
    switch (action) {
      case "view_secret":
      case "export_accounts":
        return "red" as const;
      case "create_account":
      case "update_account":
        return "cyan" as const;
      case "generate_code":
        return "green" as const;
      default:
        return "neutral" as const;
    }
  };

  const formatDate = (iso: string) => {
    try {
      const d = new Date(iso);
      return d.toLocaleString();
    } catch {
      return iso;
    }
  };

  return (
    <Modal wide onClose={onClose} title={t("twofa.auditModalTitle", "2FA Security Audit Logs")}>
      <div className="space-y-4 text-sm">
        <p className="text-xs text-muted-foreground">
          {t(
            "twofa.auditDesc",
            "Immutable activity history tracking every code generation, secret reveal, export, and AI agent query.",
          )}
        </p>

        {loading ? (
          <TableSkeleton columnsCount={5} rowsCount={4} />
        ) : logs.length === 0 ? (
          <TableEmpty emptyText={t("twofa.noLogs", "No audit events recorded yet.")} />
        ) : (
          <div className="max-h-96 overflow-y-auto rounded border border-border">
            <Table>
              <THead>
                <TR>
                  <TH>{t("twofa.time", "Time")}</TH>
                  <TH>{t("twofa.action", "Action")}</TH>
                  <TH>{t("twofa.account", "Account")}</TH>
                  <TH>{t("twofa.actor", "Actor")}</TH>
                  <TH>{t("twofa.ip", "IP")}</TH>
                </TR>
              </THead>
              <TBody>
                {logs.map((item) => (
                  <TR key={item.id}>
                    <TD className="whitespace-nowrap font-mono text-[11px]">
                      {formatDate(item.created_at)}
                    </TD>
                    <TD>
                      <Badge tone={getBadgeTone(item.action)} className="px-1.5 py-0.5 text-[10px]">
                        {item.action}
                      </Badge>
                    </TD>
                    <TD className="font-medium text-foreground">{item.account_name || "—"}</TD>
                    <TD className="font-mono text-[11px]">{item.actor}</TD>
                    <TD className="font-mono text-[11px]">{item.ip}</TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          </div>
        )}

        <div className="flex justify-end border-t border-border pt-2">
          <Button onClick={onClose}>{t("twofa.close", "Close")}</Button>
        </div>
      </div>
    </Modal>
  );
}
