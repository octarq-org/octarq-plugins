import { useState, useEffect } from "react";
import { Modal, Button, Badge } from "@octarq/plugin-sdk";
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
    <Modal
      wide
      onClose={onClose}
      title={t("twofa.auditModalTitle", "2FA Security Audit Logs")}
    >
      <div className="space-y-4 text-sm">
        <p className="text-xs text-white/50">
          {t("twofa.auditDesc", "Immutable activity history tracking every code generation, secret reveal, export, and AI agent query.")}
        </p>

        {loading ? (
          <div className="py-8 text-center text-white/50">{t("twofa.loading", "Loading audit events...")}</div>
        ) : logs.length === 0 ? (
          <div className="py-8 text-center text-white/40">{t("twofa.noLogs", "No audit events recorded yet.")}</div>
        ) : (
          <div className="max-h-96 overflow-y-auto rounded border border-white/10">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="border-b border-white/10 text-[11px] text-white/40">
                  <th className="p-2 font-medium">{t("twofa.time", "Time")}</th>
                  <th className="p-2 font-medium">{t("twofa.action", "Action")}</th>
                  <th className="p-2 font-medium">{t("twofa.account", "Account")}</th>
                  <th className="p-2 font-medium">{t("twofa.actor", "Actor")}</th>
                  <th className="p-2 font-medium">{t("twofa.ip", "IP")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {logs.map((item) => (
                  <tr key={item.id} className="text-xs hover:bg-white/5">
                    <td className="p-2 text-white/60 font-mono text-[11px] whitespace-nowrap">
                      {formatDate(item.created_at)}
                    </td>
                    <td className="p-2">
                      <Badge tone={getBadgeTone(item.action)} className="text-[10px] px-1.5 py-0.5">
                        {item.action}
                      </Badge>
                    </td>
                    <td className="p-2 font-medium text-white">
                      {item.account_name || "—"}
                    </td>
                    <td className="p-2 text-white/60 font-mono text-[11px]">
                      {item.actor}
                    </td>
                    <td className="p-2 text-white/40 font-mono text-[11px]">
                      {item.ip}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="flex justify-end pt-2 border-t border-white/10">
          <Button onClick={onClose}>{t("twofa.close", "Close")}</Button>
        </div>
      </div>
    </Modal>
  );
}
