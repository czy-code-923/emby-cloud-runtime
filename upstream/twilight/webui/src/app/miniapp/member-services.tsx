"use client";
import {useCallback,useState} from "react";
import {api} from "@/lib/api";
import {useI18n} from "@/lib/i18n";
import {useAsyncResource} from "@/hooks/use-async-resource";
import type {MyServices} from "@/lib/member-services";
import s from "./miniapp.module.css";
export function MemberServices({revision}:{revision:number}){const {t}=useI18n();const [notice,setNotice]=useState("");const load=useCallback(async(signal?:AbortSignal)=>{void revision;const r=await api.myMemberServices<MyServices>(signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[revision]);const state=useAsyncResource(load,{throwOnError:false});if(!state.data?.items.length&&!state.error)return null;
 return <section className={s.glass}><h3>{t("v9.services")}</h3><p className={s.muted}>{t("v9.memberServicesHelp")}</p>{(notice||state.error)&&<p role="status">{notice||state.error}</p>}{state.data?.items.map(v=><details className={s.glass} key={v.id}><summary>{v.name} · {t(v.access?"v9.accessGranted":"v9.accessRequired")}</summary><p className={s.muted}>{v.included?t("v9.included"):v.until>0?new Date(v.until*1000).toLocaleString():t("v9.useServiceCode")}</p>{v.access&&v.lines.map(line=><button key={line} className={s.secondary} onClick={()=>void navigator.clipboard.writeText(line).then(()=>setNotice(t("common.copied"))).catch(()=>setNotice(t("ops.copyHelp")))}>{t("mini.copy")} · {line}</button>)}</details>)}</section>
}
