"use client";
import {useCallback,useState} from "react";
import {ShieldCheck} from "lucide-react";
import {api} from "@/lib/api";
import {useAsyncResource} from "@/hooks/use-async-resource";
import {useI18n} from "@/lib/i18n";
import type {RetentionView,RetentionPreview} from "@/lib/retention-types";
import s from "./miniapp.module.css";
const date=(v:number)=>v>0?new Date(v*1000).toLocaleString():"—";
export function RetentionPanel({revision,onChange}:{revision:number;onChange:()=>void}){
 const {t}=useI18n();const [busy,setBusy]=useState(false),[message,setMessage]=useState(""),[quote,setQuote]=useState<RetentionPreview>(),[consent,setConsent]=useState(false),[risk,setRisk]=useState(false);
 const load=useCallback(async(signal?:AbortSignal)=>{void revision;const r=await api.miniRetention<RetentionView>(signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[revision]);
 const state=useAsyncResource(load,{throwOnError:false});const v=state.data,m=v?.account.member;
 async function run(task:()=>Promise<void>){if(busy)return;setBusy(true);setMessage("");try{await task()}catch(e){setMessage(e instanceof Error?e.message:t("commerce.failed"))}finally{setBusy(false)}}
 const preview=(mode:string)=>run(async()=>{const r=await api.miniRetentionPreview<RetentionPreview>(mode);if(!r.success||!r.data)throw new Error(r.message);setQuote(r.data);setConsent(false);setRisk(false)});
 const confirm=()=>run(async()=>{if(!quote)return;const r=await api.miniRetentionConfirm({token:quote.token,cost_cap:quote.quote.cost,authorize_a:consent,acknowledge_risk:risk});if(!r.success)throw new Error(r.message);setQuote(undefined);setMessage(r.message);onChange()});
 const withdraw=()=>run(async()=>{const r=await api.miniRetentionWithdraw();if(!r.success)throw new Error(r.message);setQuote(undefined);setMessage(r.message);onChange()});
 return <section className={s.glass}><h3><ShieldCheck size={19}/>{t("retention.title")}</h3><p className={s.muted}>{t("retention.rules")}</p>{(message||state.error)&&<p role="status">{message||state.error}</p>}{v&&m&&<>
 {!v.policy.enabled&&<p>{t("retention.disabled")}</p>}
 <div className={s.list}><div><span>{t("retention.mode")}<small>{m.selected_mode||t("retention.none")} · {m.participation_enabled?t("retention.active"):t("retention.inactive")}</small></span></div><div><span>{t("retention.window")}<small>{date(v.window_start)} → {date(v.window_end)}</small></span></div><div><span>{t("retention.watch")}<small>{Math.floor(v.watch_seconds/60)} / {v.policy.watch_seconds/60} {t("retention.minutes")}</small></span><span>{t("retention.days")}<small>{v.signin_days}</small></span></div><div><span>{t("retention.balance")}<small>{v.balance} / {v.policy.a_cost}</small></span><span>{t("retention.switch")}<small>{date(m.next_switch_at)}</small></span></div><div><span>{t("retention.lastPoll")}<small>{v.last_poll?date(v.last_poll):t("retention.waiting")}</small></span></div></div>
 {v.selectable?<div className={s.grid}><button className={s.secondary} disabled={busy} onClick={()=>void preview("A")}>{t("retention.a")}</button><button className={s.secondary} disabled={busy} onClick={()=>void preview("B")}>{t("retention.b")}</button></div>:<p className={s.muted}>{t("retention.unavailable")}</p>}
 {quote&&<div className={s.glass}><h3>{t("retention.preview")} · {quote.quote.target_mode}</h3><p>{t("retention.shortfall")} {quote.quote.points_shortfall} {t("retention.points")} / {Math.ceil(quote.quote.watch_shortfall/60)} {t("retention.minutes")}</p>{quote.quote.target_mode==="A"&&<label><input type="checkbox" checked={consent} onChange={e=>setConsent(e.target.checked)}/>{t("retention.consent")}</label>}{quote.quote.risk&&<label><input type="checkbox" checked={risk} onChange={e=>setRisk(e.target.checked)}/>{t("retention.risk")}</label>}<button className={s.primary} disabled={busy||(quote.quote.target_mode==="A"&&!consent)||(quote.quote.risk&&!risk)} onClick={()=>void confirm()}>{t("retention.confirm")}</button></div>}
 {m.participation_enabled&&<button className={s.secondary} disabled={busy} onClick={()=>void withdraw()}>{t("retention.withdraw")}</button>}
 {v.ledger.length>0&&<details><summary>{t("retention.history")}</summary>{v.ledger.map(x=><p key={x.window_id}>{x.mode} · {x.spent_points} {t("retention.points")} · {date(x.new_end)}</p>)}</details>}
 </>}</section>
}
