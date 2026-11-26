import { useMemo, useState } from "react";
import {
  Button,
  Field,
  FormError,
  Input,
  Modal,
  PageCard,
  PageRow,
  Select,
  Switch,
  Textarea,
  useToast,
} from "@octarq/plugin-sdk";
import type { TFunc } from "@octarq/plugin-sdk";
import { ApiError, apiSend } from "../api";
import type {
  CredentialWriteResult,
  DetectResult,
  SchemaInfo,
  Tracker,
  TrackerWriteBody,
} from "../types";
import { clockToMinutes, minutesToClock } from "../format";

export interface TrackerFormProps {
  schemas: SchemaInfo[];
  tracker?: Tracker;
  onClose: () => void;
  onSaved: () => void;
  t: TFunc;
}

interface FormState {
  name: string;
  schema: string;
  baseUrl: string;
  apiBaseUrl: string;
  username: string;
  loginSecret: string;
  cookie: string;
  apiKey: string;
  userAgent: string;
  timezone: string;
  signinEnabled: boolean;
  statsHours: string;
  timeout: string;
  windowStart: string;
  windowEnd: string;
}

function stateFrom(tracker: Tracker | undefined): FormState {
  const fallbackSchema = tracker?.schema ?? "";
  return {
    name: tracker?.name ?? "",
    schema: fallbackSchema,
    baseUrl: tracker?.base_url ?? "",
    apiBaseUrl: tracker?.api_base_url ?? "",
    username: tracker?.username ?? "",
    loginSecret: "",
    cookie: "",
    apiKey: "",
    userAgent: tracker?.user_agent ?? "",
    timezone: tracker?.timezone ?? "",
    signinEnabled: tracker?.signin_enabled ?? true,
    statsHours: String(tracker?.stats_interval_hours ?? 6),
    timeout: String(tracker?.timeout_seconds ?? 20),
    windowStart: tracker && tracker.signin_window_start_min ? minutesToClock(tracker.signin_window_start_min) : "",
    windowEnd: tracker && tracker.signin_window_end_min ? minutesToClock(tracker.signin_window_end_min) : "",
  };
}

export function TrackerForm({ schemas, tracker, onClose, onSaved, t }: TrackerFormProps) {
  const toast = useToast();
  const [form, setForm] = useState<FormState>(() => stateFrom(tracker));
  const [advanced, setAdvanced] = useState(false);
  const [saving, setSaving] = useState(false);
  const [detecting, setDetecting] = useState(false);
  const [detectNote, setDetectNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const selected = useMemo(
    () => schemas.find((s) => s.schema === form.schema),
    [schemas, form.schema],
  );
  const caps = selected?.capabilities;

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  const detect = async () => {
    if (!form.baseUrl.trim()) return;
    setDetecting(true);
    setDetectNote(null);
    try {
      const result = await apiSend<DetectResult>("POST", "/api/pt/detect", { url: form.baseUrl.trim() });
      if (!result.reachable) setDetectNote(t("detectUnreachable", "The site did not answer."));
      else if (result.challenge) setDetectNote(t("detectChallenge", "The site shows an anti-bot challenge."));
      else if (result.has_captcha) setDetectNote(t("detectCaptcha", "The login page has a captcha."));
      else if (result.suggested_schema && result.suggested_schema !== form.schema) {
        const label = schemas.find((s) => s.schema === result.suggested_schema)?.display_name;
        setDetectNote(t("detectSuggested", "This address looks like {{schema}}.", { schema: label ?? result.suggested_schema }));
      } else if (result.suggested_schema && result.suggested_schema === form.schema) {
        setDetectNote(t("detectMatch", "The page matches the selected site type."));
      } else if (result.has_login_form) {
        setDetectNote(t("detectUnknown", "The plugin does not recognize this site. Choose the site type that matches it."));
      } else setDetectNote(t("detectNoForm", "The page has no login form."));
    } catch {
      setDetectNote(t("detectUnreachable", "The site did not answer."));
    } finally {
      setDetecting(false);
    }
  };

  const submit = async () => {
    setError(null);
    if (!form.name.trim()) {
      setError(t("fieldName", "Name"));
      return;
    }
    if (!form.baseUrl.trim()) {
      setError(t("fieldBaseURL", "Site address"));
      return;
    }
    if (!form.schema) {
      setError(t("fieldSchemaRequired", "Choose the site type."));
      return;
    }

    const body: TrackerWriteBody = {
      name: form.name.trim(),
      schema: form.schema,
      base_url: form.baseUrl.trim(),
      api_base_url: form.apiBaseUrl.trim() || undefined,
      username: form.username.trim() || undefined,
      user_agent: form.userAgent.trim() || undefined,
      timezone: form.timezone.trim() || undefined,
      signin_enabled: form.signinEnabled,
      stats_interval_hours: Number(form.statsHours) || undefined,
      timeout_seconds: Number(form.timeout) || undefined,
      signin_window_start_min: form.windowStart ? clockToMinutes(form.windowStart) : 0,
      signin_window_end_min: form.windowEnd ? clockToMinutes(form.windowEnd) : 0,
    };

    setSaving(true);
    try {
      if (tracker) {
        await apiSend<Tracker>("PUT", `/api/pt/trackers/${tracker.id}`, body);
        if (form.loginSecret || form.cookie || form.apiKey) {
          const result = await apiSend<CredentialWriteResult>(
            "PUT",
            `/api/pt/trackers/${tracker.id}/credentials`,
            {
              login_secret: form.loginSecret || undefined,
              cookie: form.cookie || undefined,
              site_api_token: form.apiKey || undefined,
            },
          );
          if (!result.stored) {
            setError(result.message || t("errorGeneric", "The action failed. Try again."));
            setSaving(false);
            return;
          }
        }
      } else {
        await apiSend<Tracker>("POST", "/api/pt/trackers", {
          ...body,
          login_secret: form.loginSecret || undefined,
          cookie: form.cookie || undefined,
          site_api_token: form.apiKey || undefined,
        });
      }
      toast.success(t("save", "Save"));
      onSaved();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("errorGeneric", "The action failed. Try again."));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal title={tracker ? t("editTitle", "Edit site") : t("createTitle", "Add site")} onClose={onClose} wide>
      <div className="max-h-[70vh] overflow-y-auto pr-1">
        <Field label={t("fieldName", "Name")}>
          <Input value={form.name} onChange={(e) => set("name", e.target.value)} maxLength={120} />
        </Field>

        <Field label={t("fieldSchema", "Site type")}>
          <Select
            value={form.schema}
            onValueChange={(v) => set("schema", v)}
            options={schemas.map((s) => ({ value: s.schema, label: s.display_name }))}
            placeholder={t("fieldSchemaPlaceholder", "Choose the site type")}
            aria-label={t("fieldSchema", "Site type")}
          />
        </Field>

        <Field label={t("fieldBaseURL", "Site address")} hint={selected?.domains?.join(", ")}>
          <div className="flex gap-2">
            <Input
              value={form.baseUrl}
              onChange={(e) => set("baseUrl", e.target.value)}
              placeholder="https://tracker.example.com"
            />
            <Button type="button" variant="outline" onClick={detect} disabled={detecting}>
              {detecting ? t("detecting", "Checking…") : t("detectButton", "Check address")}
            </Button>
          </div>
        </Field>
        {detectNote ? (
          <p className="-mt-2 mb-4 text-[12.5px] leading-relaxed text-muted-foreground">{detectNote}</p>
        ) : null}

        {caps?.api_key_auth ? (
          <Field label={t("fieldAPIBaseURL", "API address")}>
            <Input
              value={form.apiBaseUrl}
              onChange={(e) => set("apiBaseUrl", e.target.value)}
              placeholder="https://api.tracker.example.com"
            />
          </Field>
        ) : null}

        {caps?.login ? (
          <Field label={t("fieldUsername", "Account name")}>
            <Input value={form.username} onChange={(e) => set("username", e.target.value)} />
          </Field>
        ) : null}

        {caps?.login ? (
          <Field label={t("fieldLoginSecret", "Account secret")} hint={tracker ? t("secretKeepHint", "Leave empty to keep the saved value.") : undefined}>
            <Input
              type="password"
              value={form.loginSecret}
              onChange={(e) => set("loginSecret", e.target.value)}
              autoComplete="new-password"
            />
          </Field>
        ) : null}

        {caps?.api_key_auth ? (
          <Field label={t("fieldAPIKey", "API key")} hint={tracker ? t("secretKeepHint", "Leave empty to keep the saved value.") : t("apiKeyHint", "The plugin reads data with this key.")}>
            <Input
              type="password"
              value={form.apiKey}
              onChange={(e) => set("apiKey", e.target.value)}
              autoComplete="off"
            />
          </Field>
        ) : null}

        {caps?.cookie_auth ? (
          <Field label={t("fieldCookie", "Cookie")} hint={t("cookieHint", "Paste the site cookies from a signed-in browser.")}>
            <Textarea
              value={form.cookie}
              onChange={(e) => set("cookie", e.target.value)}
              rows={3}
              spellCheck={false}
            />
          </Field>
        ) : null}

        <PageRow
          title={t("fieldSigninEnabled", "Sign in daily")}
          control={
            <Switch
              checked={form.signinEnabled}
              onCheckedChange={(v) => set("signinEnabled", v)}
              aria-label={t("fieldSigninEnabled", "Sign in daily")}
            />
          }
          className="mb-4 rounded-lg border border-border px-3"
        />

        <Field label={t("fieldStatsHours", "Refresh every (hours)")}>
          <Input
            type="number"
            min={1}
            max={168}
            value={form.statsHours}
            onChange={(e) => set("statsHours", e.target.value)}
          />
        </Field>

        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="mb-3"
          onClick={() => setAdvanced((v) => !v)}
        >
          {t("advancedTitle", "Advanced settings")}
        </Button>

        {advanced ? (
          <PageCard className="mt-0 mb-4 p-3 sm:p-3">
            <Field label={t("fieldTimezone", "Time zone")}>
              <Input
                value={form.timezone}
                onChange={(e) => set("timezone", e.target.value)}
                placeholder="Asia/Shanghai"
              />
            </Field>
            <Field label={t("fieldWindowStart", "Sign-in window starts")} hint={t("windowHint", "Leave both empty to sign in at any time.")}>
              <Input
                type="time"
                value={form.windowStart}
                onChange={(e) => set("windowStart", e.target.value)}
              />
            </Field>
            <Field label={t("fieldWindowEnd", "Sign-in window ends")}>
              <Input type="time" value={form.windowEnd} onChange={(e) => set("windowEnd", e.target.value)} />
            </Field>
            <Field label={t("fieldTimeout", "Timeout (seconds)")}>
              <Input
                type="number"
                min={5}
                max={120}
                value={form.timeout}
                onChange={(e) => set("timeout", e.target.value)}
              />
            </Field>
            <Field label={t("fieldUserAgent", "Browser agent")}>
              <Input value={form.userAgent} onChange={(e) => set("userAgent", e.target.value)} />
            </Field>
          </PageCard>
        ) : null}

        {error ? <FormError err={error} className="mt-3" /> : null}
      </div>

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose} disabled={saving}>
          {t("cancel", "Cancel")}
        </Button>
        <Button onClick={submit} disabled={saving}>
          {saving ? t("saving", "Saving…") : t("save", "Save")}
        </Button>
      </div>
    </Modal>
  );
}
