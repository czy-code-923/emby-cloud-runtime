"use client";
import {useCallback,useState} from "react";
import {api} from "@/lib/api";
import {useAsyncResource} from "@/hooks/use-async-resource";
import {useI18n} from "@/lib/i18n";
import styles from "./miniapp.module.css";
type Registry={items:{id:string;name:string}[];max_devices:number;max_streams:number;enabled:boolean};
export function MemberDevices({revision,busy,release}:{revision:number;busy:boolean;release:(body:Record<string,unknown>)=>Promise<void>}){
 const {t}=useI18n();const [selected,setSelected]=useState("");
 const load=useCallback(async(signal?:AbortSignal)=>{void revision;const r=await api.miniRead<Registry>("device-registry",signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[revision]);
 const {data,error,isLoading}=useAsyncResource(load,{throwOnError:false});
 if(error)return <p role="alert">{error}</p>;
 if(isLoading||!data)return <p>{t("common.loading")}</p>;
 return <><p className={styles.muted}>{t("mini.maxDevices")}: {data.max_devices||t("mini.unlimited")} · {t("mini.maxStreams")}: {data.max_streams||t("mini.unlimited")}</p><div className={styles.list}>{data.items.map(item=><div key={item.id}><span>{item.name||t("mini.deviceID")}<small>{item.id}</small></span><button disabled={busy} onClick={()=>setSelected(item.id)}>{t("mini.releaseDevice")}</button></div>)}</div>{selected&&<form onSubmit={e=>{e.preventDefault();const body=Object.fromEntries(new FormData(e.currentTarget).entries());e.currentTarget.reset();setSelected("");void release({...body,device_id:selected})}}><p>{t("mini.releaseHint")}</p><label>{t("mini.security")}<input name="security_code" type="password" inputMode="numeric" pattern="[0-9]{6,12}" autoComplete="off" required/></label><button className={styles.secondary} disabled={busy}>{t("mini.releaseDevice")}</button><button type="button" className={styles.subtle} onClick={()=>setSelected("")}>{t("common.cancel")}</button></form>}</>;
}
