import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, errorText, unwrap, type Schemas } from "../../../api/client";
import { qk, useSettings } from "../../../api/hooks";
import { Button, Field, Select, Segmented, Switch } from "../../../components/ui";
import { t } from "../../../i18n";
import { useDraft } from "../../../lib/draft";
import { fieldErrors } from "../../../lib/fields";

export function AISettings({ s }: { s: Schemas["settings"] }) {
  const save = useSaveSettings();
  const qc = useQueryClient();
  const toast = useToast();
  const { can } = useGrants();
  const writable = can("settings", true);

  const [tab, setTab] = useState<"model" | "rules" | "bot">("model");

  const [modelForm, setModelForm] = useDraft({
    provider_type: s.ai?.provider_type ?? "",
    api_key: s.ai?.api_key ?? "",
    base_url: s.ai?.base_url ?? "",
    model_name: s.ai?.model_name ?? "",
    ollama_host: s.ai?.ollama_host ?? "",
  });

  const [rulesForm, setRulesForm] = useDraft({
    system_prompt: s.ai?.system_prompt ?? "",
    max_per_chat: String(s.ai?.max_per_chat ?? 50),
    max_per_day: String(s.ai?.max_per_day ?? 200),
  });

  const [botForm, setBotForm] = useDraft({
    bot_token: s.ai?.bot_token ?? "",
  });

  const submitModel = (e: FormEvent) => {
    e.preventDefault();
    const body: Record<string, any> = {};
    if (modelForm.provider_type !== s.ai?.provider_type) body.provider_type = modelForm.provider_type;
    if (modelForm.api_key !== s.ai?.api_key) body.api_key = modelForm.api_key;
    if (modelForm.base_url !== s.ai?.base_url) body.base_url = modelForm.base_url;
    if (modelForm.model_name !== s.ai?.model_name) body.model_name = modelForm.model_name;
    if (modelForm.ollama_host !== s.ai?.ollama_host) body.ollama_host = modelForm.ollama_host;
    save.mutate({ provider_type: modelForm.provider_type, api_key: modelForm.api_key, base_url: modelForm.base_url, model_name: modelForm.model_name, ollama_host: modelForm.ollama_host });
  };

  const submitRules = (e: FormEvent) => {
    e.preventDefault();
    const body: Record<string, any> = {};
    if (rulesForm.system_prompt !== s.ai?.system_prompt) body.system_prompt = rulesForm.system_prompt;
    const max_per_chat = Number(rulesForm.max_per_chat);
    const max_per_day = Number(rulesForm.max_per_day);
    if (!Number.isNaN(max_per_chat) && max_per_chat !== s.ai?.max_per_chat) body.max_per_chat = max_per_chat;
    if (!Number.isNaN(max_per_day) && max_per_day !== s.ai?.max_per_day) body.max_per_day = max_per_day;
    save.mutate(body);
  };

  const submitBot = (e: FormEvent) => {
    e.preventDefault();
    const body: Record<string, any> = {};
    if (botForm.bot_token !== s.ai?.bot_token) body.bot_token = botForm.bot_token;
    save.mutate(body);
  };

  return (
    <section className="card glass reveal" style={{ "--i": 6 } as React.CSSProperties}>
      <div className="card-head">
        <div className="flex items-center gap-2 mb-4">
          <h2 className="card-title">{t("settings.ai.title")}</h2>
          <span className="badge badge-secondary">{t("settings.ai.beta")}</span>
        </div>
        <div className="card-sub">{t("settings.ai.subtitle")}</div>
      </div>

      <div className="flex gap-1 border-b border-[var(--hairline)] mb-6">
        {[ "model", "rules", "bot" ] as const}.map((t) => (
          <button key={t} type="button" onClick={() => setTab(t)} className={`px-4 py-2 text-sm font-medium transition-colors ${tab === t ? "border-[var(--accent)] text-[var(--ink-900)]" : "text-[var(--ink-500)] hover:text-[var(--ink-700)]"}`}>
            {t(`settings.ai.tab.${t}`)}
          </button>
        ))}
      </div>

      <div className="mt-4">
        {tab === "model" && (
          <form onSubmit={submitModel} noValidate className="pt-5">
            <Field label={t("settings.ai.provider")} htmlFor="ai-provider">
              <select id="ai-provider" className="input max-w-[320px]" value={modelForm.provider_type} onChange={(e) => setModelForm((f) => ({ ...f, provider_type: e.target.value as any }))}>
                <option value="letsencrypt">Let's Encrypt</option>
                <option value="zerossl">ZeroSSL</option>
                <option value="google">Google</option>
              </select>
            </Field>
            <Field label={t("settings.ai.api_key")} htmlFor="ai-api-key" error={errors.api_key}>
              <input id="ai-api-key" className="input mono max-w-[320px]" type="password" value={modelForm.api_key} onChange={(e) => setModelForm((f) => ({ ...f, api_key: e.target.value }))} placeholder={t("settings.ai.api_key_placeholder")} aria-invalid={!!errors.api_key} />
            </Field>
            <Field label={t("settings.ai.base_url")} htmlFor="ai-base-url" error={false} hint={t("settings.ai.base_url_hint")}>
              <input id="ai-base-url" className="input mono max-w-[320px]" value={modelForm.base_url} onChange={(e) => setModelForm((f) => ({ ...f, base_url: e.target.value }))} placeholder="https://api.xkiro.com/v1" />
            </Field>
            <Field label={t("settings.ai.model_name")} htmlFor="ai-model-name" error={false} hint={t("settings.ai.model_name_hint")}>
              <input id="ai-model-name" className="input" value={modelForm.model_name} onChange={(e) => setModelForm((f) => ({ ...f, model_name: e.target.value }))} placeholder="gpt-4o-mini" />
            </Field>
            {modelForm.provider_type === "ollama" && (
              <Field label={t("settings.ai.ollama_host")} htmlFor="ai-ollama-host" error={false} hint={t("settings.ai.ollama_host_hint")}>
                <input id="ai-ollama-host" className="input mono max-w-[320px]" value={modelForm.ollama_host} onChange={(e) => setModelForm((f) => ({ ...f, ollama_host: e.target.value }))} placeholder="http://localhost:11434" />
              </Field>
            )}
          </div>
          <div className="mt-4 flex gap-2">
            <Button variant="primary" type="submit" form="ai-model-form" loading={save.isPending} disabled={!writable || editing}>
              {t("common.save")}
            </Button>
            <Button variant="ghost" size="sm" loading={false} onClick={() => modelTest.mutate({ provider_type: modelForm.provider_type, api_key: modelForm.api_key, base_url: modelForm.base_url, model_name: modelForm.model_name, ollama_host: modelForm.ollama_host })} disabled={!writable || editing}>
              {t("settings.ai.test_connection")}
            </Button>
          </div>
        </>
        )}
        {tab === "rules" && (
          <>
            <div className="mb-4">
              <Field label={t("settings.ai.system_prompt")} htmlFor="ai-system-prompt" error={false} hint={t("settings.ai.system_prompt_hint")}>
                <textarea id="ai-system-prompt" className="input min-h-[200px]" value={rulesForm.system_prompt} onChange={(e) => setRulesForm((f) => ({ ...f, system_prompt: e.target.value }))} disabled={!writable || editing} />
              </Field>
              <div className="grid gap-x-3 sm:grid-cols-2">
                <Field label={t("settings.ai.max_per_chat")} htmlFor="ai-max-chat" error={false} hint={t("settings.ai.max_per_chat_hint")}>
                  <input id="ai-max-chat" className="input max-w-[160px]" inputMode="numeric" value={rulesForm.max_per_chat} onChange={(e) => setRulesForm((f) => ({ ...f, max_per_chat: e.target.value }))} disabled={!writable || editing} />
                </Field>
                <Field label={t("settings.ai.max_per_day")} htmlFor="ai-max-day" error={false} hint={t("settings.ai.max_per_day_hint")}>
                  <input id="ai-max-day" className="input max-w-[160px]" inputMode="numeric" value={rulesForm.max_per_day} onChange={(e) => setRulesForm((f) => ({ ...f, max_per_day: e.target.value }))} placeholder="200" disabled={!writable || editing} />
                </Field>
              </div>
              <div className="mt-4 flex gap-2">
                <Button variant="primary" type="submit" form="rules-form" loading={save.isPending} disabled={!writable || editing}>
                  {t("common.save")}
                </Button>
                <Button variant="ghost" size="sm" loading={false} disabled={!writable || editing} onClick={() => setRulesForm({ system_prompt: s.ai?.system_prompt ?? "", max_per_chat: String(s.ai?.max_per_chat ?? 50), max_per_day: String(s.ai?.max_per_day ?? 200) })}>
                  {t("settings.ai.reset")}
                </Button>
              </div>
            </div>
          </>
        )}
        {tab === "bot" && (
          <>
            <div className="mb-4">
              <Field label={t("settings.ai.bot_token")} htmlFor="ai-bot-token" error={errors.bot_token} hint={t("settings.ai.bot_token_hint")}>
                <input id="ai-bot-token" className="input mono max-w-[320px]" type="password" value={botForm.bot_token} onChange={(e) => setBotForm((f) => ({ ...f, bot_token: e.target.value }))} aria-invalid={!!errors.bot_token} disabled={!writable || botEditing} />
              </Field>
              <div className="mt-4 flex gap-2">
                <Button variant="primary" type="submit" form="bot-form" loading={save.isPending} disabled={!writable || botEditing}>
                  {t("common.save")}
                </Button>
                <Button variant="ghost" size="sm" loading={false} disabled={!writable || botEditing} onClick={() => setBotForm({ bot_token: s.ai?.bot_token ?? "" })} disabled={!writable || botEditing}>
                  {t("settings.ai.reset")}
                </Button>
              </div>
              <p className="mt-2 text-xs text-[var(--ink-500)]">{t("settings.ai.bot_token_hint")}</p>
            </div>
            <div className="mt-4 border-t border-[var(--hairline)] pt-4">
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-[13px] font-medium">{t("settings.ai.enabled")}</div>
                  <div className="mt-1 text-xs text-[var(--ink-500)]">{t("settings.ai.enabled_hint")}</div>
                </div>
                <Switch checked={cfg.enabled} onChange={(v) => save.mutate({ enabled: v })} disabled={!writable} />
              </div>
            </div>
            {cfg.bot_token && (
              <div className="mt-4 flex items-center gap-2">
                <p className="text-xs text-[var(--ink-500)]">{t("settings.ai.bot_user")}: <span className="mono">{cfg.bot_username}</span></p>
              </div>
            )}
          </>
        )}
      </div>
    </section>
  );
}

export default AISettings;
