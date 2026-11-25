"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Download, RefreshCw, CheckCircle2, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api, type DeploymentUpdateState } from "@/lib/api";
import { useI18n, type MessageKey } from "@/lib/i18n";
import { useAsyncResource } from "@/hooks/use-async-resource";

const phaseKeys: Record<string, MessageKey> = {
  idle: "deploymentUpdate.phase.idle", queued: "deploymentUpdate.phase.queued",
  download: "deploymentUpdate.phase.download", backup: "deploymentUpdate.phase.backup",
  applying: "deploymentUpdate.phase.applying", checking: "deploymentUpdate.phase.checking",
  succeeded: "deploymentUpdate.phase.succeeded", failed: "deploymentUpdate.phase.failed",
  rollback: "deploymentUpdate.phase.rollback", rolled_back: "deploymentUpdate.phase.rolled_back",
  recovery_required: "deploymentUpdate.phase.recovery_required", interrupted: "deploymentUpdate.phase.interrupted",
};
const active = new Set(["queued", "download", "backup", "applying", "checking", "rollback"]);

export default function UpdatesPage() {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [githubToken, setGithubToken] = useState("");
  const [credentialPassword, setCredentialPassword] = useState("");
  const generation = useRef(0);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const load = useCallback(async (signal?: AbortSignal): Promise<DeploymentUpdateState> => {
    const started = generation.current;
    const response = await api.deploymentStatus(signal);
    if (started !== generation.current) throw new DOMException("Superseded", "AbortError");
    if (!response.data) throw new Error(t("deploymentUpdate.unavailable"));
    return response.data;
  }, [t]);
  const { data: state, error: readError, execute: refresh, setData: setState } = useAsyncResource(load, { throwOnError: false });
  const running = !!state && active.has(state.job.phase);
  useEffect(() => {
    if (!running || busy) return;
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") void refresh({ throwOnError: false });
    }, 4000);
    return () => clearInterval(timer);
  }, [running, busy, refresh]);
  const perform = async (operation: "check" | "apply") => {
    if (busy) return;
    generation.current += 1;
    setBusy(true); setError("");
    try {
      const response = operation === "apply"
        ? await api.deploymentApply(state?.latest?.tag || "", state?.installed_version || "", password)
        : await api.deploymentCheck();
      if (!alive.current) return;
      if (response.data) setState(response.data);
      if (operation === "apply") { setPassword(""); setConfirm(false); }
    } catch (failure) {
      if (alive.current) setError(failure instanceof Error ? failure.message : t("deploymentUpdate.failed"));
    } finally { if (alive.current) setBusy(false); }
  };
  const visibleError = error || (readError ? t(running ? "deploymentUpdate.reconnecting" : "deploymentUpdate.unavailable") : "");
  const saveCredentials = async (clear: boolean) => {
    if (busy || running || !state?.credentials) return;
    generation.current += 1;
    setBusy(true); setError("");
    try {
      const response = await api.deploymentCredentials(clear ? "" : githubToken, clear, state.credentials.revision, credentialPassword);
      if (alive.current && response.data) setState(response.data);
    } catch (failure) {
      if (alive.current) setError(failure instanceof Error ? failure.message : t("deploymentUpdate.failed"));
    } finally {
      if (alive.current) { setBusy(false); setGithubToken(""); setCredentialPassword(""); }
    }
  };
  return <div className="mx-auto max-w-4xl space-y-6 p-4 sm:p-6">
    <div><h1 className="text-2xl font-semibold">{t("deploymentUpdate.title")}</h1><p className="mt-2 text-sm text-muted-foreground">{t("deploymentUpdate.description")}</p></div>
    <section className="rounded-xl border bg-card p-5 space-y-4">
      <div className="grid min-w-0 gap-4 sm:grid-cols-2"><div><p className="text-sm text-muted-foreground">{t("deploymentUpdate.current")}</p><p className="mt-1 break-all text-xl font-semibold">{state?.installed_version || t("deploymentUpdate.loading")}</p></div><div><p className="text-sm text-muted-foreground">{t("deploymentUpdate.latest")}</p><p className="mt-1 break-all text-xl font-semibold">{state?.latest?.version || t("deploymentUpdate.unchecked")}</p></div></div>
      <div className="flex flex-wrap gap-3"><Button variant="outline" disabled={busy || running} onClick={() => perform("check")}><RefreshCw className="mr-2 h-4 w-4 shrink-0" />{t("deploymentUpdate.check")}</Button><Button disabled={busy || running || !state?.latest?.available} onClick={() => setConfirm(true)}><Download className="mr-2 h-4 w-4 shrink-0" />{t("deploymentUpdate.apply")}</Button></div>
      {state?.latest && !state.latest.available && <p className="flex items-center gap-2 text-sm"><CheckCircle2 className="h-4 w-4 shrink-0" />{t("deploymentUpdate.upToDate")}</p>}
      <p className="break-all text-xs text-muted-foreground">{t("deploymentUpdate.source")}: github.com/czy-code-923/emby-cloud-runtime</p>
    </section>
    <section className="rounded-xl border bg-card p-5 space-y-2" aria-live="polite"><h2 className="font-medium">{t("deploymentUpdate.status")}</h2><p>{t(phaseKeys[state?.job.phase || "idle"] || "deploymentUpdate.loading")}</p>{state?.job.target && <p className="text-sm">{t("deploymentUpdate.target")}: {state.job.target}</p>}{state?.job.error && <p className="max-h-[30dvh] overflow-auto overscroll-contain break-words text-sm text-destructive">{state.job.error}</p>}</section>
    {state?.credentials && <section className="rounded-xl border bg-card p-5 space-y-4">
      <h2 className="font-medium">{t("deploymentUpdate.privateTitle")}</h2>
      <p className="text-sm text-muted-foreground">{t("deploymentUpdate.privateHint")}</p>
      <p className="text-sm">{t(state.credentials.configured ? "deploymentUpdate.tokenConfigured" : "deploymentUpdate.tokenMissing")}</p>
      <label className="block text-sm" htmlFor="github-release-token">{t("deploymentUpdate.tokenLabel")}</label>
      <Input id="github-release-token" type="password" autoComplete="off" value={githubToken} onChange={event => setGithubToken(event.target.value)} />
      <label className="block text-sm" htmlFor="credential-password">{t("deploymentUpdate.password")}</label>
      <Input id="credential-password" type="password" autoComplete="current-password" value={credentialPassword} onChange={event => setCredentialPassword(event.target.value)} />
      <div className="flex flex-wrap gap-3">
        <Button disabled={busy || running || !githubToken || !credentialPassword} onClick={() => void saveCredentials(false)}>{t("deploymentUpdate.saveToken")}</Button>
        <Button variant="outline" disabled={busy || running || !state.credentials.configured || !credentialPassword} onClick={() => void saveCredentials(true)}>{t("deploymentUpdate.clearToken")}</Button>
      </div>
    </section>}
    {visibleError && <p role="alert" className="flex items-start gap-2 text-sm text-destructive"><AlertCircle className="h-4 w-4 shrink-0" />{visibleError}</p>}
    {confirm && <section className="rounded-xl border bg-card p-5 space-y-4"><h2 className="font-medium">{t("deploymentUpdate.confirmTitle", { version: state?.latest?.version || "" })}</h2><p className="text-sm text-muted-foreground">{t("deploymentUpdate.confirmHint")}</p><label className="block text-sm" htmlFor="upgrade-password">{t("deploymentUpdate.password")}</label><Input id="upgrade-password" type="password" autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} /><div className="flex flex-wrap gap-3"><Button disabled={busy || !password} onClick={() => perform("apply")}>{t("deploymentUpdate.confirm")}</Button><Button variant="outline" onClick={() => { setConfirm(false); setPassword(""); }}>{t("deploymentUpdate.cancel")}</Button></div></section>}
  </div>;
}
