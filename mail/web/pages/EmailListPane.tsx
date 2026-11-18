import { PageCard, Empty, Button, Input, Skeleton, useTranslation } from "@octarq/plugin-sdk";
import { Search, X, Mail as MailIcon, Inbox } from "lucide-react";
import { Email, Mailbox } from "../api";
import { MailFolder } from "./types";
import { EmailListItem } from "./EmailListItem";

interface EmailListPaneProps {
  emails: Email[];
  loading: boolean;
  folder: MailFolder;
  q: string;
  onSearchChange: (q: string) => void;
  openEmailId?: number;
  onOpenEmail: (e: Email) => void;
  onScroll: (e: React.UIEvent<HTMLDivElement>) => void;
  boxes: Mailbox[];
  activeMailboxId?: number;
  onNewBox: () => void;
}

export function EmailListPane({
  emails,
  loading,
  folder,
  q,
  onSearchChange,
  openEmailId,
  onOpenEmail,
  onScroll,
  boxes,
  activeMailboxId,
  onNewBox,
}: EmailListPaneProps) {
  const { t } = useTranslation();

  return (
    <div className="flex h-full min-h-0 w-full flex-col">
      <div className="relative mb-3">
        <Search className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          className="!pl-10"
          aria-label={t("mail.searchPlaceholder")}
          placeholder={t("mail.searchPlaceholder")}
          value={q}
          onChange={(e) => onSearchChange(e.target.value)}
        />
        {q && (
          <button
            type="button"
            onClick={() => onSearchChange("")}
            aria-label={t("mail.clearSearch")}
            className="absolute right-3 top-1/2 -translate-y-1/2 cursor-pointer rounded-md p-0.5 text-muted-foreground transition-colors duration-[var(--dur-fast)] hover:bg-surface-hover hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      <PageCard className="mt-0 flex-1 overflow-hidden p-0">
        {emails.length === 0 && loading ? (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("mail.loading")}
            className="divide-y divide-border"
          >
            {Array.from({ length: 7 }).map((_, i) => (
              <div key={i} className="flex flex-col gap-1.5 p-3.5">
                <div className="flex items-center justify-between gap-2">
                  <Skeleton className="h-3.5 w-36" />
                  <Skeleton className="h-3 w-12" />
                </div>
                <Skeleton className="h-3.5 w-4/5" />
                <Skeleton className="h-3 w-3/5" />
              </div>
            ))}
          </div>
        ) : (
          <div className="max-h-[650px] divide-y divide-border overflow-y-auto" onScroll={onScroll}>
            {emails.length === 0 ? (
              q ? (
                <div className="flex flex-col items-center gap-2.5 px-6 py-10 text-center">
                  <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                    <Search className="h-4 w-4" />
                  </div>
                  <p className="text-[13px] text-muted-foreground">
                    {t("mail.emptyFilteredReason")} <span className="font-mono text-foreground">{`“${q}”`}</span>
                  </p>
                  <Button variant="ghost" size="sm" onClick={() => onSearchChange("")}>
                    {t("mail.clearSearch")}
                  </Button>
                </div>
              ) : boxes.length === 0 ? (
                <Empty
                  reason={t("mail.emptyNoBoxesReason")}
                  detail={t("mail.emptyNoBoxesDetail")}
                  action={
                    <Button variant="primary" size="sm" className="mt-1" onClick={onNewBox}>
                      {t("mail.newMailbox")}
                    </Button>
                  }
                >
                  <MailIcon />
                </Empty>
              ) : (
                <Empty
                  reason={
                    <>
                      {t("mail.emptyInboxReasonPre")}{" "}
                      <span className="font-mono">
                        {`“${boxes.find((b) => b.id === activeMailboxId)?.address ?? boxes[0]?.address}”`}
                      </span>
                    </>
                  }
                  detail={t("mail.emptyInboxDetail")}
                >
                  <Inbox />
                </Empty>
              )
            ) : (
              <>
                {emails.map((e) => (
                  <EmailListItem
                    key={e.id}
                    email={e}
                    folder={folder}
                    isSelected={openEmailId === e.id}
                    onSelect={() => onOpenEmail(e)}
                  />
                ))}
                {loading && (
                  <div
                    role="status"
                    aria-busy="true"
                    aria-label={t("mail.loading")}
                    className="flex flex-col gap-1.5 p-3.5"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <Skeleton className="h-3.5 w-32" />
                      <Skeleton className="h-3 w-12" />
                    </div>
                    <Skeleton className="h-3.5 w-3/4" />
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </PageCard>
    </div>
  );
}
