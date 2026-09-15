import { useEffect, useState, useMemo, useCallback } from "react";
import {
  ScreenWrap,
  PageHeader,
  Button,
  Input,
  LockedFallback,
  useTranslation,
} from "@octarq/plugin-sdk";
import type { AccountSummary } from "./types";
import { AccountCard } from "./components/AccountCard";
import { AddEditModal } from "./components/AddEditModal";
import { SecretQrModal } from "./components/SecretQrModal";
import { AuditLogsModal } from "./components/AuditLogsModal";
import { ImportExportModal } from "./components/ImportExportModal";

export default function TwoFAPage() {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<AccountSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<number | null>(null);
  const [searchTerm, setSearchTerm] = useState("");
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  const [toastMsg, setToastMsg] = useState("");

  // Modals state
  const [addEditOpen, setAddEditOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState<AccountSummary | null>(null);
  const [secretQrOpen, setSecretQrOpen] = useState(false);
  const [secretQrAccount, setSecretQrAccount] = useState<AccountSummary | null>(null);
  const [auditLogsOpen, setAuditLogsOpen] = useState(false);
  const [importExportOpen, setImportExportOpen] = useState(false);

  const fetchAccounts = useCallback(async () => {
    try {
      const res = await fetch("/api/twofa/accounts", { credentials: "same-origin" });
      if (!res.ok) {
        setStatus(res.status);
        setLoading(false);
        return;
      }
      const data = (await res.json()) as AccountSummary[];
      setAccounts(data);
      setLoading(false);
    } catch {
      setStatus(0);
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  // 1-second interval to tick countdown and refresh codes at boundary
  useEffect(() => {
    const timer = setInterval(() => {
      setAccounts((prev) => {
        let needsRefresh = false;
        const updated = prev.map((acc) => {
          const nextRem = acc.seconds_remaining - 1;
          if (nextRem <= 0) {
            needsRefresh = true;
          }
          return {
            ...acc,
            seconds_remaining: nextRem <= 0 ? acc.period || 30 : nextRem,
          };
        });

        if (needsRefresh) {
          fetchAccounts();
        }

        return updated;
      });
    }, 1000);

    return () => clearInterval(timer);
  }, [fetchAccounts]);

  const showToast = (msg: string) => {
    setToastMsg(msg);
    setTimeout(() => setToastMsg(""), 2000);
  };

  const handleTogglePin = async (id: number) => {
    try {
      const res = await fetch(`/api/twofa/accounts/${id}/pin`, {
        method: "POST",
        credentials: "same-origin",
      });
      if (res.ok) {
        fetchAccounts();
      }
    } catch {
      // ignore
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm(t("twofa.confirmDelete", "Are you sure you want to delete this 2FA account?"))) {
      return;
    }
    try {
      const res = await fetch(`/api/twofa/accounts/${id}`, {
        method: "DELETE",
        credentials: "same-origin",
      });
      if (res.ok) {
        fetchAccounts();
        showToast(t("twofa.deleted", "Account deleted."));
      }
    } catch {
      // ignore
    }
  };

  // Collect all unique tags for filter pills
  const allTags = useMemo(() => {
    const tagSet = new Set<string>();
    accounts.forEach((acc) => {
      if (acc.tags) {
        acc.tags
          .split(",")
          .map((tg) => tg.trim())
          .filter(Boolean)
          .forEach((tg) => tagSet.add(tg));
      }
    });
    return Array.from(tagSet);
  }, [accounts]);

  // Filter accounts by search query and selected tag
  const filteredAccounts = useMemo(() => {
    return accounts.filter((acc) => {
      const query = searchTerm.toLowerCase();
      const matchSearch =
        !query ||
        acc.name.toLowerCase().includes(query) ||
        (acc.issuer && acc.issuer.toLowerCase().includes(query)) ||
        (acc.account && acc.account.toLowerCase().includes(query)) ||
        (acc.tags && acc.tags.toLowerCase().includes(query));

      const matchTag =
        !selectedTag ||
        (acc.tags &&
          acc.tags
            .split(",")
            .map((tg) => tg.trim())
            .includes(selectedTag));

      return matchSearch && matchTag;
    });
  }, [accounts, searchTerm, selectedTag]);

  // Locked/gated state fallback
  if (status === 402 || status === 404) {
    return (
      <ScreenWrap>
        <LockedFallback
          status={status}
          feature="2FA Vault"
          description={t("twofa.pageDesc", "Centralized TOTP two-factor authentication manager for team & infrastructure accounts.")}
        />
      </ScreenWrap>
    );
  }

  return (
    <ScreenWrap>
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between mb-6">
        <PageHeader
          title={t("twofa.pageTitle", "2FA Vault")}
          description={t(
            "twofa.pageDesc",
            "Centralized TOTP two-factor authentication manager for team & infrastructure accounts."
          )}
        />

        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            onClick={() => setAuditLogsOpen(true)}
            title={t("twofa.auditLogs", "Audit Logs")}
            className="text-xs h-9 px-3"
          >
            📋 {t("twofa.auditLogs", "Audit Logs")}
          </Button>
          <Button
            variant="ghost"
            onClick={() => setImportExportOpen(true)}
            title={t("twofa.importExport", "Import / Export")}
            className="text-xs h-9 px-3"
          >
            🔄 {t("twofa.importExport", "Import / Export")}
          </Button>
          <Button
            onClick={() => {
              setEditingAccount(null);
              setAddEditOpen(true);
            }}
            className="text-xs h-9 px-3"
          >
            + {t("twofa.addAccount", "Add Account")}
          </Button>
        </div>
      </div>

      {/* Filter and Search Bar */}
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="max-w-md flex-1">
          <Input
            placeholder={t("twofa.searchPlaceholder", "Search by name, issuer, account, or tags...")}
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
          />
        </div>

        {allTags.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 text-xs">
            <span className="text-white/40 mr-1">{t("twofa.filterTag", "Tag:")}</span>
            <button
              onClick={() => setSelectedTag(null)}
              className={`rounded px-2 py-0.5 transition-colors ${
                selectedTag === null
                  ? "bg-emerald-500 text-white font-medium"
                  : "bg-white/5 text-white/60 hover:bg-white/10"
              }`}
            >
              {t("twofa.all", "All")}
            </button>
            {allTags.map((tg) => (
              <button
                key={tg}
                onClick={() => setSelectedTag(selectedTag === tg ? null : tg)}
                className={`rounded px-2 py-0.5 transition-colors ${
                  selectedTag === tg
                    ? "bg-emerald-500 text-white font-medium"
                    : "bg-white/5 text-white/60 hover:bg-white/10"
                }`}
              >
                {tg}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Toast Alert Feedback */}
      {toastMsg && (
        <div className="fixed bottom-6 right-6 z-50 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white shadow-lg animate-bounce">
          {toastMsg}
        </div>
      )}

      {/* Account Cards Grid */}
      {loading ? (
        <div className="py-16 text-center text-white/40">
          {t("twofa.loadingAccounts", "Loading 2FA vault accounts...")}
        </div>
      ) : filteredAccounts.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-white/10 p-12 text-center">
          <span className="text-4xl mb-3">🔐</span>
          <h3 className="text-base font-semibold text-white">
            {searchTerm || selectedTag
              ? t("twofa.noMatches", "No accounts matching search criteria")
              : t("twofa.emptyTitle", "No 2FA accounts registered yet")}
          </h3>
          <p className="mt-1 text-xs text-white/50 max-w-sm">
            {searchTerm || selectedTag
              ? t("twofa.tryDifferentSearch", "Try searching with a different term or clearing active tag filters.")
              : t("twofa.emptyDesc", "Add your first TOTP credential to start managing shared two-factor authentication tokens.")}
          </p>
          {!searchTerm && !selectedTag && (
            <Button
              className="mt-4 text-xs h-9 px-3"
              onClick={() => {
                setEditingAccount(null);
                setAddEditOpen(true);
              }}
            >
              + {t("twofa.addFirstAccount", "Add First Account")}
            </Button>
          )}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filteredAccounts.map((acc) => (
            <AccountCard
              key={acc.id}
              account={acc}
              onCopy={() => showToast(t("twofa.copiedToast", "Code copied to clipboard!"))}
              onTogglePin={handleTogglePin}
              onViewSecret={(a) => {
                setSecretQrAccount(a);
                setSecretQrOpen(true);
              }}
              onEdit={(a) => {
                setEditingAccount(a);
                setAddEditOpen(true);
              }}
              onDelete={handleDelete}
              t={t}
            />
          ))}
        </div>
      )}

      {/* Modals */}
      {addEditOpen && (
        <AddEditModal
          onClose={() => setAddEditOpen(false)}
          account={editingAccount}
          onSaved={fetchAccounts}
          t={t}
        />
      )}

      {secretQrOpen && (
        <SecretQrModal
          onClose={() => {
            setSecretQrOpen(false);
            setSecretQrAccount(null);
          }}
          account={secretQrAccount}
          t={t}
        />
      )}

      {auditLogsOpen && (
        <AuditLogsModal
          onClose={() => setAuditLogsOpen(false)}
          t={t}
        />
      )}

      {importExportOpen && (
        <ImportExportModal
          onClose={() => setImportExportOpen(false)}
          onImported={fetchAccounts}
          t={t}
        />
      )}
    </ScreenWrap>
  );
}
