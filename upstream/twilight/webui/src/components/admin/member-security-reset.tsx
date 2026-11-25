"use client";
import {useState} from "react";
import {api} from "@/lib/api";
import {useI18n} from "@/lib/i18n";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
export function MemberSecurityReset({uid}:{uid:number}){
 const {t}=useI18n();const [password,setPassword]=useState(""),[confirmed,setConfirmed]=useState(false),[busy,setBusy]=useState(false),[notice,setNotice]=useState("");
 async function reset(devices:boolean){if(!confirmed||!password||busy)return;setBusy(true);try{const r=await api.resetMemberSecurity(uid,password,devices);setNotice(r.message)}catch(e){setNotice(e instanceof Error?e.message:t("mini.failed"))}finally{setPassword("");setConfirmed(false);setBusy(false)}}
 return <details className="rounded-lg border p-3 text-sm font-normal"><summary>{t("mini.adminReset")}</summary><p className="my-3 text-muted-foreground">{t("mini.resetHint")}</p><Input type="password" autoComplete="current-password" aria-label={t("mini.adminPassword")} placeholder={t("mini.adminPassword")} value={password} onChange={e=>setPassword(e.target.value)}/><label className="my-3 flex gap-2"><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>{t("mini.confirmReset")}</label><div className="flex flex-wrap gap-2"><Button type="button" size="sm" variant="outline" disabled={busy||!confirmed||!password} onClick={()=>void reset(false)}>{t("mini.resetSecurity")}</Button><Button type="button" size="sm" variant="outline" disabled={busy||!confirmed||!password} onClick={()=>void reset(true)}>{t("mini.resetDevices")}</Button></div>{notice&&<p className="mt-2" role="status">{notice}</p>}</details>;
}
