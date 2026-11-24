"use client";
import Script from "next/script";
import Image from "next/image";
import { useCallback, useState, useRef } from "react";
import { api } from "@/lib/api";
import { useAsyncResource } from "@/hooks/use-async-resource";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/lib/i18n";

declare global { interface Window { Telegram?: {WebApp?: {initData:string;ready():void;expand():void}} } }
export default function MiniAppPage() {
 const {t}=useI18n();
 const [sdk,setSdk]=useState(false);
 const requestId=useRef("");
 const load=useCallback(async(signal?:AbortSignal)=>{
  const app=window.Telegram?.WebApp;
  if(!app?.initData) throw new Error(t("jianwei.openFromTelegram"));
  app.ready(); app.expand();
  if(!requestId.current) requestId.current=Array.from(crypto.getRandomValues(new Uint8Array(32)),value=>value.toString(16).padStart(2,"0")).join("");
  const session=await api.miniSession(app.initData,requestId.current,signal);
  if(!session.success)throw new Error(session.message);
  const response=await api.miniAccount(signal);
  if(!response.success||!response.data)throw new Error(response.message);
  return response.data;
 },[t]);
 const {data,error,isLoading,execute}=useAsyncResource(load,{immediate:sdk,throwOnError:false});
 return <main className="mx-auto min-h-dvh max-w-xl space-y-5 px-5 pb-10 pt-7">
  <Script src="https://telegram.org/js/telegram-web-app.js" strategy="afterInteractive" onReady={()=>setSdk(true)} onError={()=>setSdk(true)}/>
  <header className="flex items-center gap-3"><Image src="/brand/jianwei-mark.svg" alt="" width={48} height={48}/><div><h1 className="text-xl font-semibold">{t("jianwei.memberCenter")}</h1><p className="text-sm text-muted-foreground">{t("jianwei.tagline")}</p></div></header>
  {error?<Card><CardContent className="space-y-3 p-5"><p role="alert">{error}</p><Button variant="outline" onClick={()=>void execute()}>{t("common.retry")}</Button></CardContent></Card>:isLoading||!data?<p>{t("common.loading")}</p>:<>
   <Card><CardContent className="space-y-4 p-5"><p className="font-semibold">{data.username} <span className="text-sm text-muted-foreground">· UID {data.uid}</span></p><div className="grid grid-cols-2 gap-4"><div><p className="text-sm text-muted-foreground">{t("jianwei.mediaStatus")}</p><p>{data.media_enabled?t("jianwei.enabled"):t("jianwei.notActivated")}</p></div><div><p className="text-sm text-muted-foreground">{t("jianwei.balance")}</p><p className="text-2xl font-semibold tabular-nums">{data.points}</p></div></div></CardContent></Card>
   <p className="text-sm text-muted-foreground">{t("jianwei.identityHint")}</p>
  </>}
 </main>;
}
