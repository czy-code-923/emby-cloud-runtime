"use client";

import { useCallback, useEffect, useState } from "react";
import { Download, RefreshCw, CheckCircle2, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiRequest } from "@/lib/api-request";

type UpdateState = {
  installed_version: string | null;
  source_repo: string;
  latest?: { tag: string; version: string; available: boolean; url: string } | null;
  job: { phase: string; target?: string; message?: string; error?: string; updated_at?: number };
};
const names: Record<string, string> = {
  idle: "尚未执行升级", queued: "升级已排队", download: "正在下载并校验发布包",
  backup: "正在备份并验证数据库", applying: "正在应用程序与镜像", checking: "正在检查服务健康",
  succeeded: "升级完成", failed: "升级失败", rollback: "正在恢复上一版本程序",
  rolled_back: "已恢复上一版本程序", recovery_required: "需要人工检查恢复", interrupted: "上次升级被中断",
};
const active = new Set(["queued", "download", "backup", "applying", "checking", "rollback"]);
const headers = { "X-Twilight-Client": "webui", "X-Twilight-Intent": "deploy-release" };

export default function UpdatesPage() {
  const [state, setState] = useState<UpdateState | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const refresh = useCallback(async () => {
    const response = await apiRequest<UpdateState>("/admin/deployment/status");
    if (response.data) setState(response.data);
  }, []);
  useEffect(() => { refresh().catch(() => setError("暂时无法连接升级执行器")); }, [refresh]);
  useEffect(() => {
    if (!state || !active.has(state.job.phase)) return;
    const timer = setInterval(() => {
      refresh().then(() => setError("")).catch(() => setError("升级期间服务正在重启，页面会自动重新连接"));
    }, 4000);
    return () => clearInterval(timer);
  }, [state, refresh]);
  const perform = async (operation: "check" | "apply") => {
    setBusy(true); setError("");
    try {
      const response = await apiRequest<UpdateState>(`/admin/deployment/${operation}`, {
        method: "POST", headers,
        body: JSON.stringify(operation === "apply" ? { tag: state?.latest?.tag, expected_version: state?.installed_version, password } : {}),
      });
      if (response.data) setState(response.data);
      if (operation === "apply") { setPassword(""); setConfirm(false); }
    } catch (failure) { setError(failure instanceof Error ? failure.message : "操作失败，请稍后重试"); }
    finally { setBusy(false); }
  };
  const running = !!state && active.has(state.job.phase);
  return <div className="mx-auto max-w-4xl space-y-6 p-4 sm:p-6">
    <div><h1 className="text-2xl font-semibold">版本与升级</h1><p className="mt-2 text-sm text-muted-foreground">检查官方仓库已发布的正式版本，并保留现有账号、配置与媒体数据。</p></div>
    <section className="rounded-xl border bg-card p-5 space-y-4">
      <div className="grid gap-4 sm:grid-cols-2"><div><p className="text-sm text-muted-foreground">当前版本</p><p className="mt-1 text-xl font-semibold">{state?.installed_version || "读取中"}</p></div><div><p className="text-sm text-muted-foreground">最新正式版本</p><p className="mt-1 text-xl font-semibold">{state?.latest?.version || "尚未检查"}</p></div></div>
      <div className="flex flex-wrap gap-3"><Button variant="outline" disabled={busy || running} onClick={() => perform("check")}><RefreshCw className="mr-2 h-4 w-4" />检查最新版本</Button><Button disabled={busy || running || !state?.latest?.available} onClick={() => setConfirm(true)}><Download className="mr-2 h-4 w-4" />一键拉取并升级</Button></div>
      {state?.latest && !state.latest.available && <p className="flex items-center gap-2 text-sm"><CheckCircle2 className="h-4 w-4" />当前已是最新正式版本</p>}
      <p className="break-all text-xs text-muted-foreground">程序源：github.com/czy-code-923/emby-cloud-runtime</p>
    </section>
    <section className="rounded-xl border bg-card p-5 space-y-2" aria-live="polite"><h2 className="font-medium">升级状态</h2><p>{names[state?.job.phase || "idle"] || state?.job.phase}</p>{state?.job.target && <p className="text-sm">目标版本：{state.job.target}</p>}{state?.job.message && <p className="text-sm text-muted-foreground">{state.job.message}</p>}{state?.job.error && <p className="text-sm text-destructive">{state.job.error}</p>}</section>
    {error && <p role="alert" className="flex items-start gap-2 text-sm text-destructive"><AlertCircle className="h-4 w-4 shrink-0" />{error}</p>}
    {confirm && <section className="rounded-xl border bg-card p-5 space-y-4"><h2 className="font-medium">升级到 {state?.latest?.version}</h2><p className="text-sm text-muted-foreground">升级前自动创建加密备份，期间会短暂停止播放与管理服务。失败时恢复上一版本程序与镜像，数据库不会被旧快照覆盖。</p><label className="block text-sm" htmlFor="upgrade-password">管理员当前密码</label><Input id="upgrade-password" type="password" autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} /><div className="flex gap-3"><Button disabled={busy || !password} onClick={() => perform("apply")}>确认升级</Button><Button variant="outline" onClick={() => { setConfirm(false); setPassword(""); }}>取消</Button></div></section>}
  </div>;
}
