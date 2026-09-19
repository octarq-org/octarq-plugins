import { useEffect, useState, useMemo, useCallback } from "react";
import {
  ScreenWrap,
  PageHeader,
  Button,
  Input,
  Empty,
  Skeleton,
  LockedFallback,
  useTranslation,
  useToast,
  useConfirm,
} from "@octarq/plugin-sdk";
import { Plus, RefreshCw, ScrollText, ShieldCheck } from "lucide-react";
import type { AccountSummary } from "./types";
import { AccountCard } from "./components/AccountCard";
import { AddEditModal } from "./components/AddEditModal";
import { SecretQrModal } from "./components/SecretQrModal";
import { AuditLogsModal } from "./components/AuditLogsModal";
import { ImportExportModal } from "./components/ImportExportModal";

export default function TwoFAPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const confirm = useConfirm();
  const [accounts, setAccounts] = useState<AccountSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<number | null>(null);
  const [searchTerm, setSearchTerm] = useState("");
  const [selectedTag, setSelectedTag] = useState<string | null>(null);

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
    const ok = await confirm({
      message: t("twofa.confirmDelete", "Are you sure you want to delete this 2FA account?"),
      danger: true,
    });
    if (!ok) return;
    try {
      const res = await fetch(`/api/twofa/accounts/${id}`, {
        method: "DELETE",
        credentials: "same-origin",
      });
      if (res.ok) {
        fetchAccounts();
        toast.success(t("twofa.deleted", "Account deleted."));
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

  const isFiltering = Boolean(searchTerm || selectedTag);

  return (
    <ScreenWrap>
      {/* Header */}
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <PageHeader
          title={t("twofa.pageTitle", "2FA Vault")}
          description={t(
            "twofa.pageDesc",
            "Centralized TOTP two-factor authentication manager for team & infrastructure accounts."
          )}
        />

        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => setAuditLogsOpen(true)} title={t("twofa.auditLogs", "Audit Logs")}>
            <ScrollText className="h-3.5 w-3.5" /> {t("twofa.auditLogs", "Audit Logs")}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setImportExportOpen(true)} title={t("twofa.importExport", "Import / Export")}>
            <RefreshCw className="h-3.5 w-3.5" /> {t("twofa.importExport", "Import / Export")}
          </Button>
          <Button
            size="sm"
            onClick={() => {
              setEditingAccount(null);
              setAddEditOpen(true);
            }}
          >
            <Plus className="h-3.5 w-3.5" /> {t("twofa.addAccount", "Add Account")}
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
            <span className="mr-1 text-muted-foreground">{t("twofa.filterTag", "Tag:")}</span>
            <Button
              variant={selectedTag === null ? "secondary" : "subtle"}
              size="sm"
              onClick={() => setSelectedTag(null)}
            >
              {t("twofa.all", "All")}
            </Button>
            {allTags.map((tg) => (
              <Button
                key={tg}
                variant={selectedTag === tg ? "secondary" : "subtle"}
                size="sm"
                onClick={() => setSelectedTag(selectedTag === tg ? null : tg)}
              >
                {tg}
              </Button>
            ))}
          </div>
        )}
      </div>

      {/* Account Cards Grid */}
      {loading ? (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-44 rounded-xl" />
          ))}
        </div>
      ) : filteredAccounts.length === 0 ? (
        <Empty
          reason={isFiltering ? t("twofa.noMatches", "No accounts matching search criteria") : t("twofa.emptyTitle", "No 2FA accounts registered yet")}
          detail={
            isFiltering
              ? t("twofa.tryDifferentSearch", "Try searching with a different term or clearing active tag filters.")
              : t("twofa.emptyDesc", "Add your first TOTP credential to start managing shared two-factor authentication tokens.")
          }
          action={
            !isFiltering ? (
              <Button
                size="sm"
                onClick={() => {
                  setEditingAccount(null);
                  setAddEditOpen(true);
                }}
              >
                <Plus className="h-3.5 w-3.5" /> {t("twofa.addFirstAccount", "Add First Account")}
              </Button>
            ) : undefined
          }
        >
          <ShieldCheck className="h-8 w-8 text-foreground/30" />
        </Empty>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filteredAccounts.map((acc) => (
            <AccountCard
              key={acc.id}
              account={acc}
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

      {auditLogsOpen && <AuditLogsModal onClose={() => setAuditLogsOpen(false)} t={t} />}

      {importExportOpen && (
        <ImportExportModal onClose={() => setImportExportOpen(false)} onImported={fetchAccounts} t={t} />
      )}
    </ScreenWrap>
  );
}
