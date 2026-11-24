"use client";
import {useCallback,useEffect,useState} from "react";
import {api} from "@/lib/api";
import {useAsyncResource} from "@/hooks/use-async-resource";
import {useI18n} from "@/lib/i18n";
import {useToast} from "@/hooks/use-toast";
import {useConfirm} from "@/components/ui/confirm-dialog";
import {Button} from "@/components/ui/button";
import {Card,CardContent} from "@/components/ui/card";
import {Input} from "@/components/ui/input";
import {Label} from "@/components/ui/label";
import {Switch} from "@/components/ui/switch";

export function CertificateManager(){
 const {t}=useI18n();const {toast}=useToast();const {confirm}=useConfirm();
 const [profile,setProfile]=useState<"account"|"shop">("account");
 const [email,setEmail]=useState<string|null>(null);const [token,setToken]=useState("");const [password,setPassword]=useState("");
 const [accepted,setAccepted]=useState(false);const [staging,setStaging]=useState(false);const [busy,setBusy]=useState(false);
 const load=useCallback(async(signal?:AbortSignal)=>{const res=await api.networkStatus(signal);if(!res.success||!res.data)throw new Error(res.message);return res.data;},[]);
 const {data,error,isLoading,execute}=useAsyncResource(load,{immediate:true,throwOnError:false});
 const running=data?.job.phase==="queued"||data?.job.phase==="running";
 useEffect(()=>{if(!running)return;const timer=setInterval(()=>{if(document.visibilityState==="visible")void execute();},4000);return()=>clearInterval(timer);},[running,execute]);
 const current=data?.profiles[profile];const origin=data?.desired_origins[profile]||"";
 const changeProfile=(value:"account"|"shop")=>{setProfile(value);setEmail(null);setToken("");setPassword("");setAccepted(false);setStaging(false);};
 const action=async(operation:"configure"|"issue"|"renew"|"publish")=>{
  if(busy||running||!password)return;
  if(operation==="publish"&&!await confirm({title:t("network.publishTitle"),description:t("network.publishConfirm"),tone:"warning",confirmLabel:t("network.publish")}))return;
  setBusy(true);
  try{
   const res=await api.networkAction(operation,{profile,expected_revision:current?.desired.revision||0,password,...(operation==="configure"?{email:email??current?.desired.email??"",cloudflare_token:token,accept_terms:accepted,staging}:{})});
   if(!res.success)throw new Error(res.message);
   if(operation==="configure")setToken("");
   toast({title:t("network.submitted"),variant:"success"});await execute();
  }catch(err){toast({title:t("common.operationFailed"),description:err instanceof Error?err.message:undefined,variant:"destructive"});}
  finally{setPassword("");setBusy(false);}
 };
 return <Card><CardContent className="space-y-5 p-5">
  <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-xl font-semibold">{t("network.title")}</h2><p className="mt-1 text-sm text-muted-foreground">{t("network.description")}</p></div><Button variant="outline" onClick={()=>void execute()} disabled={busy||isLoading}>{t("common.refresh")}</Button></div>
  <div className="flex flex-wrap gap-2"><Button variant={profile==="account"?"default":"outline"} onClick={()=>changeProfile("account")} disabled={busy||running}>{t("network.account")}</Button><Button variant={profile==="shop"?"default":"outline"} onClick={()=>changeProfile("shop")} disabled={busy||running}>{t("network.shop")}</Button></div>
  {error?<p role="alert" className="text-sm text-destructive">{error}</p>:null}
  <dl className="grid gap-4 rounded-xl border bg-muted/20 p-4 text-sm sm:grid-cols-2"><div><dt className="text-muted-foreground">{t("network.desired")}</dt><dd className="mt-1 break-all font-medium">{origin||t("network.notConfigured")}</dd></div><div><dt className="text-muted-foreground">{t("network.applied")}</dt><dd className="mt-1 break-all font-medium">{current?.applied.origin||t("network.notPublished")}</dd></div><div><dt className="text-muted-foreground">{t("network.certificate")}</dt><dd className="mt-1">{current?.certificate?`${current.certificate.staging?t("network.testCertificate"):t("network.production")} · ${new Date(current.certificate.expires_at*1000).toLocaleString()}`:t("network.noCertificate")}</dd></div><div><dt className="text-muted-foreground">{t("network.renewal")}</dt><dd className="mt-1">{data?.automatic_renewal.enabled&&data.automatic_renewal.active?t("network.renewalDescription"):t("network.renewalInactive")}</dd></div></dl>
  <div className="grid gap-4 sm:grid-cols-2"><div className="space-y-2"><Label htmlFor="certificate-email">{t("network.email")}</Label><Input id="certificate-email" type="email" value={email??current?.desired.email??""} onChange={e=>setEmail(e.target.value)} disabled={busy||running}/></div><div className="space-y-2"><Label htmlFor="cloudflare-token">{t("network.token")}</Label><Input id="cloudflare-token" type="password" autoComplete="off" maxLength={200} value={token} placeholder={current?.credential_saved?t("network.keepSecret"):t("network.enterSecret")} onChange={e=>setToken(e.target.value)} disabled={busy||running}/></div></div>
  <p className="text-sm text-muted-foreground">{t("network.scopedToken")}</p>
  <div className="flex items-center justify-between gap-4"><Label htmlFor="certificate-staging">{t("network.staging")}</Label><Switch id="certificate-staging" checked={staging} onCheckedChange={setStaging} disabled={busy||running}/></div>
  <label className="flex items-start gap-3 text-sm"><input type="checkbox" className="mt-1 h-4 w-4 shrink-0" checked={accepted} onChange={e=>setAccepted(e.target.checked)} disabled={busy||running}/><span>{t("network.acceptTerms")} <a href="https://letsencrypt.org/repository/" target="_blank" rel="noreferrer" className="underline">{t("network.terms")}</a></span></label>
  <div className="space-y-2"><Label htmlFor="certificate-password">{t("network.currentPassword")}</Label><Input id="certificate-password" type="password" autoComplete="current-password" value={password} onChange={e=>setPassword(e.target.value)} disabled={busy||running}/></div>
  <div className="grid grid-cols-2 gap-3 lg:grid-cols-4"><Button variant="outline" disabled={busy||running||!origin||!accepted||!password} onClick={()=>void action("configure")}>{t("network.save")}</Button><Button disabled={busy||running||!current?.configured||!password} onClick={()=>void action("issue")}>{t("network.issue")}</Button><Button variant="outline" disabled={busy||running||!current?.certificate||!password} onClick={()=>void action("renew")}>{t("network.renew")}</Button><Button variant="outline" disabled={busy||running||!current?.certificate||current.certificate.staging||!password} onClick={()=>void action("publish")}>{t("network.publish")}</Button></div>
  {data&&data.job.phase!=="idle"?<p role="status" className={data?.job.phase==="failed"?"rounded-lg bg-destructive/10 p-3 text-sm text-destructive":"rounded-lg bg-muted p-3 text-sm"}>{running?t("network.running"):data?.job.phase==="succeeded"?t("network.succeeded"):t("network.failed")}</p>:null}
 </CardContent></Card>;
}
