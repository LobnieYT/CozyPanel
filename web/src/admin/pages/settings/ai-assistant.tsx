import { useState, type FormEvent } from "react";
import { basePath, csrf } from "../../../api/client";
import { useGrants } from "../../../api/hooks";
import { Button, Field } from "../../../components/ui";
import { useToast } from "../../../components/toast";
import { Switch } from "../../../components/switch";
import { t } from "../../../i18n";
import { useDraft } from "../../../lib/draft";

export function AISettings() {
  const { can } = useGrants();
  const writable = can("settings", true);
  const toast = useToast();

  const [tab, setTab] = useState<"model" | "rules" | "bot">("model");
  const [editing, setEditing] = useState(false);
  const [botEditing, setBotEditing] = useState(false);
  const [testStatus, setTestStatus] = useState<"idle" | "ok" | "error">("idle");
  const [testMessage, setTestMessage] = useState("");

  const modelForm = useDraft({
    provider_type: "",
    api_key: "",
    base_url: "",
    model_name: "",
    ollama_host: "",
  });
  const rulesForm = useDraft({
    system_prompt: "",
    max_per_chat: "50",
    max_per_day: "200",
  });
  const botForm = useDraft({
    bot_token: "",
  });

  const apiPut = async (body: unknown) => {
    const res = await fetch(basePath + "/api/v1/ai-assistant/config", {
      method: "PUT",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...(csrf() ? { "X-CSRF-Token": csrf() } : {}),
      },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error(await res.text());
  };

  const apiPost = async (body: unknown) => {
    const res = await fetch(basePath + "/api/v1/ai-assistant/test", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...(csrf() ? { "X-CSRF-Token": csrf() } : {}),
      },
      body: JSON.stringify(body),
    });
    return res;
  };

  const submitModel = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await apiPut({
        provider_type: modelForm.draft.provider_type,
        api_key: modelForm.draft.api_key,
        base_url: modelForm.draft.base_url,
        model_name: modelForm.draft.model_name,
        ollama_host: modelForm.draft.ollama_host,
      });
      modelForm.reset();
      setEditing(false);
      toast.ok(t("settings.saved"));
    } catch {
      toast.error(t("errors.generic"));
    }
  };

  const submitRules = async (e: FormEvent) => {
    e.preventDefault();
    try {
      const max_per_chat = Number(rulesForm.draft.max_per_chat);
      const max_per_day = Number(rulesForm.draft.max_per_day);
      const body: Record<string, unknown> = { system_prompt: rulesForm.draft.system_prompt };
      if (!Number.isNaN(max_per_chat)) body.max_per_chat = max_per_chat;
      if (!Number.isNaN(max_per_day)) body.max_per_day = max_per_day;
      await apiPut(body);
      rulesForm.reset();
      setEditing(false);
      toast.ok(t("settings.saved"));
    } catch {
      toast.error(t("errors.generic"));
    }
  };

  const submitBot = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await apiPut({ bot_token: botForm.draft.bot_token });
      botForm.reset();
      setBotEditing(false);
      toast.ok(t("settings.saved"));
    } catch {
      toast.error(t("errors.generic"));
    }
  };

  const toggleEnabled = async (checked: boolean) => {
    try {
      await apiPut({ enabled: checked });
      toast.ok(t("settings.saved"));
    } catch {
      toast.error(t("errors.generic"));
    }
  };

  const testConnection = async () => {
    setTestStatus("idle");
    setTestMessage("");
    try {
      const res = await apiPost({
        provider_type: modelForm.draft.provider_type,
        api_key: modelForm.draft.api_key,
        base_url: modelForm.draft.base_url,
        model_name: modelForm.draft.model_name,
        ollama_host: modelForm.draft.ollama_host,
      });
      const body = (await res.json()) as { success?: boolean; message?: string };
      if (res.ok && body.success) {
        setTestStatus("ok");
        setTestMessage(body.message ?? t("settings.saved"));
      } else {
        setTestStatus("error");
        setTestMessage(body.message ?? t("errors.generic"));
      }
    } catch {
      setTestStatus("error");
      setTestMessage(t("errors.generic"));
    }
  };

  return (
    <section className="card glass reveal" style={{ "--i": 6 } as React.CSSProperties}>
      <div className="card-head">
        <div className="flex items-center gap-2 mb-4">
          <h2 className="card-title">{t("settings.ai-assistant-title")}</h2>
          <span className="badge badge-secondary">{t("common.beta")}</span>
        </div>
        <div className="card-sub">{t("settings.ai-assistant-subtitle")}</div>
      </div>

      <div className="flex gap-1 border-b border-[var(--hairline)] mb-6">
        {["model", "rules", "bot"].map((key) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key as "model" | "rules" | "bot")}
            className={`px-4 py-2 text-sm font-medium transition-colors ${tab === key ? "border-[var(--accent)] text-[var(--ink-900)]" : "text-[var(--ink-500)] hover:text-[var(--ink-700)]"}`}
          >
            {t(`settings.ai-tab-${key}` as `settings.ai-tab-${"model" | "rules" | "bot"}`)}
          </button>
        ))}
      </div>

      <div className="mt-4">
        {tab === "model" && (
          <form onSubmit={submitModel} noValidate className="pt-5">
            <Field label={t("settings.ai-provider")} htmlFor="ai-provider">
              <select
                id="ai-provider"
                className="input max-w-[320px]"
                value={modelForm.draft.provider_type}
                onChange={(e) => modelForm.setDraft((prev) => ({ ...prev, provider_type: e.target.value }))}
              >
                <option value="openai">OpenAI</option>
                <option value="ollama">Ollama</option>
              </select>
            </Field>
            <Field label={t("settings.ai-api-key")} htmlFor="ai-api-key">
              <input
                id="ai-api-key"
                className="input mono max-w-[320px]"
                type="password"
                value={modelForm.draft.api_key}
                onChange={(e) => modelForm.setDraft((prev) => ({ ...prev, api_key: e.target.value }))}
                placeholder={t("settings.ai-api-key-hint")}
              />
            </Field>
            <Field label={t("settings.ai-base-url")} htmlFor="ai-base-url" hint={t("settings.ai-base-url-hint")}>
              <input
                id="ai-base-url"
                className="input mono max-w-[320px]"
                value={modelForm.draft.base_url}
                onChange={(e) => modelForm.setDraft((prev) => ({ ...prev, base_url: e.target.value }))}
                placeholder="https://api.xkiro.com/v1"
              />
            </Field>
            <Field label={t("settings.ai-model-name")} htmlFor="ai-model-name" hint={t("settings.ai-model-name-hint")}>
              <input
                id="ai-model-name"
                className="input"
                value={modelForm.draft.model_name}
                onChange={(e) => modelForm.setDraft((prev) => ({ ...prev, model_name: e.target.value }))}
                placeholder="gpt-4o-mini"
              />
            </Field>
            {modelForm.draft.provider_type === "ollama" && (
              <Field label={t("settings.ai-ollama-host")} htmlFor="ai-ollama-host" hint={t("settings.ai-ollama-host-hint")}>
                <input
                  id="ai-ollama-host"
                  className="input mono max-w-[320px]"
                  value={modelForm.draft.ollama_host}
                  onChange={(e) => modelForm.setDraft((prev) => ({ ...prev, ollama_host: e.target.value }))}
                  placeholder="http://localhost:11434"
                />
              </Field>
            )}
            <div className="mt-4 flex gap-2">
              <Button variant="primary" type="submit" disabled={!writable || editing}>
                {t("common.save")}
              </Button>
              <Button variant="ghost" size="sm" onClick={testConnection} disabled={!writable || editing}>
                {t("settings.ai-tab-model")}
              </Button>
              {testStatus === "ok" && (
                <p className="mt-2 text-xs text-green-600">{testMessage}</p>
              )}
              {testStatus === "error" && (
                <p className="mt-2 text-xs text-red-600">{testMessage}</p>
              )}
            </div>
          </form>
        )}
        {tab === "rules" && (
          <form id="rules-form" onSubmit={submitRules} noValidate>
            <>
              <div className="mb-4">
              <Field label={t("settings.ai-system-prompt")} htmlFor="ai-system-prompt" hint={t("settings.ai-system-prompt-hint")}>
                <textarea
                  id="ai-system-prompt"
                  className="input min-h-[200px]"
                  value={rulesForm.draft.system_prompt}
                  onChange={(e) => rulesForm.setDraft((prev) => ({ ...prev, system_prompt: e.target.value }))}
                  disabled={!writable || editing}
                />
              </Field>
              <div className="grid gap-x-3 sm:grid-cols-2">
                <Field label={t("settings.ai-max-per-chat")} htmlFor="ai-max-chat" hint={t("settings.ai-max-per-chat-hint")}>
                  <input
                    id="ai-max-chat"
                    className="input max-w-[160px]"
                    inputMode="numeric"
                    value={rulesForm.draft.max_per_chat}
                    onChange={(e) => rulesForm.setDraft((prev) => ({ ...prev, max_per_chat: e.target.value }))}
                    disabled={!writable || editing}
                  />
                </Field>
                <Field label={t("settings.ai-max-per-day")} htmlFor="ai-max-day" hint={t("settings.ai-max-per-day-hint")}>
                  <input
                    id="ai-max-day"
                    className="input max-w-[160px]"
                    inputMode="numeric"
                    value={rulesForm.draft.max_per_day}
                    onChange={(e) => rulesForm.setDraft((prev) => ({ ...prev, max_per_day: e.target.value }))}
                    placeholder="200"
                    disabled={!writable || editing}
                  />
                </Field>
              </div>
              <div className="mt-4 flex gap-2">
                <Button variant="primary" type="submit" form="rules-form" disabled={!writable || editing}>
                  {t("common.save")}
                </Button>
                <Button variant="ghost" size="sm" disabled={!writable || editing} onClick={() => rulesForm.reset()}>
                  {t("common.cancel")}
                </Button>
              </div>
            </div>
            </>
          </form>
        )}
        {tab === "bot" && (
          <form id="bot-form" onSubmit={submitBot} noValidate>
            <>
              <div className="mb-4">
              <Field label={t("settings.ai-bot-token")} htmlFor="ai-bot-token" hint={t("settings.ai-bot-token-hint")}>
                <input
                  id="ai-bot-token"
                  className="input mono max-w-[320px]"
                  type="password"
                  value={botForm.draft.bot_token}
                  onChange={(e) => botForm.setDraft((prev) => ({ ...prev, bot_token: e.target.value }))}
                  disabled={!writable || botEditing}
                />
              </Field>
              <div className="mt-4 flex gap-2">
                <Button variant="primary" type="submit" form="bot-form" disabled={!writable || botEditing}>
                  {t("common.save")}
                </Button>
                <Button variant="ghost" size="sm" disabled={!writable || botEditing} onClick={() => botForm.reset()}>
                  {t("common.cancel")}
                </Button>
              </div>
              <p className="mt-2 text-xs text-[var(--ink-500)]">{t("settings.ai-bot-token-hint")}</p>
            </div>
            <div className="mt-4 border-t border-[var(--hairline)] pt-4">
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-[13px] font-medium">{t("settings.ai-bot-enabled")}</div>
                  <div className="mt-1 text-xs text-[var(--ink-500)]">{t("settings.ai-enabled-hint")}</div>
                </div>
                <Switch checked={false} onChange={toggleEnabled} label={t("settings.ai-bot-enabled")} disabled={!writable} />
              </div>
            </div>
            </>
          </form>
        )}
      </div>
    </section>
  );
}

export default AISettings;
