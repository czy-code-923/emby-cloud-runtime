"use client";
import {useCallback,useState} from "react";
import {api} from "@/lib/api";
import {useAsyncResource} from "@/hooks/use-async-resource";
import {useI18n} from "@/lib/i18n";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import type {RetentionPolicy,RetentionView} from "@/lib/retention-types";
type Overview={policy:RetentionPolicy;members:RetentionView[]};
export default function RetentionAdmin(){
 const {t}=useI18n();const [password,setPassword]=useState(""),[busy,setBusy]=useState(false),[notice,setNotice]=useState("");
 const load=useCallback(async(signal?:AbortSignal)=>{const r=await api.adminRetention<Overview>(signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[]);
 const state=useAsyncResource(load,{throwOnError:false});
 async function toggle(){if(!state.data||busy)return;setBusy(true);try{const r=await api.adminRetentionEnabled(!state.data.policy.enabled,state.data.policy.revision,password);setNotice(r.message);if(r.success){setPassword("");await state.execute()}}finally{setBusy(false)}}
 return <div className="space-y-6"><h1 className="text-2xl font-medium">{t("retention.title")}</h1><p className="text-muted-foreground">{t("retention.policy")}</p>{(notice||state.error)&&<p role="status">{notice||state.error}</p>}<section className="rounded-2xl border p-5 space-y-4"><p>{t("retention.enabled")} · {state.data?.policy.enabled?t("retention.active"):t("retention.inactive")}</p><label>{t("retention.password")}<Input type="password" value={password} onChange={e=>setPassword(e.target.value)} autoComplete="current-password"/></label><Button onClick={()=>void toggle()} disabled={busy||!password||!state.data}>{t("retention.save")}</Button></section><h2 className="text-lg font-medium">{t("retention.members")}</h2><div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">{state.data?.members.map(v=><article key={v.account.member.uid} className="rounded-2xl border p-4 space-y-2"><p>UID {v.account.member.uid} · {v.account.member.selected_mode}</p><p>{t("retention.balance")} {v.balance}</p><p>{t("retention.watch")} {Math.floor(v.watch_seconds/60)} / 30 {t("retention.minutes")}</p><p>{t("retention.end")} {new Date(v.window_end*1000).toLocaleString()}</p><p>{t("retention.sync")} · {v.account.applied_version>=(v.account.member.last_settlement?.new_end||0)?t("retention.applied"):t("retention.pending")}</p></article>)}</div></div>
}
