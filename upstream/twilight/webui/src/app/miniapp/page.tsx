"use client";
import Script from "next/script";
import Image from "next/image";
import { useCallback, useState, useRef, type ReactNode, type FormEvent } from "react";
import { Film, Sparkles, Ticket, Smartphone, Compass, UserRound, RefreshCw, ShoppingBag, Check, Users, Clapperboard, ArrowUpRight, ShieldCheck } from "lucide-react";
import { api } from "@/lib/api";
import type { MiniAccount } from "@/lib/api-types";
import { useAsyncResource } from "@/hooks/use-async-resource";
import { useI18n, type MessageKey } from "@/lib/i18n";
import styles from "./miniapp.module.css";
import {Discovery,type DiscoverySection} from "./discovery";
import {sanitizeImageUrl} from "@/lib/safe-url";
import {MemberDevices} from "./member-devices";

declare global { interface Window { Telegram?: {WebApp?: {initData:string;ready():void;expand():void;openLink?(url:string):void}} } }
type Tab = "discover"|"home"|"benefits"|"requests"|"account";
type ReadSection = "lines"|"devices"|"requests"|"invite";
type PanelData = {lines?:{name:string;url:string}[];whitelist_lines?:{name:string;url:string}[];items?:{id:number;title:string;status:string}[];codes?:{code:string;active:boolean;days:number}[];can_invite?:boolean;reason?:string;total?:number};
type Device = {session_id:string;device_name:string;client:string;now_playing?:{name:string}|null};
function Glass({children,className=""}:{children:ReactNode;className?:string}) {return <section className={`${styles.glass} ${className}`}>{children}</section>}
function RemotePanel({section,revision}:{section:ReadSection;revision:number}) {
 const {t}=useI18n();
 const load=useCallback(async(signal?:AbortSignal)=>{void revision;const r=await api.miniRead<PanelData|Device[]>(section,signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[section,revision]);
 const {data,error,isLoading,execute}=useAsyncResource(load,{throwOnError:false});
 const [copied,setCopied]=useState("");
 async function copy(value:string){try{await navigator.clipboard.writeText(value);setCopied(value)}catch{setCopied("")}}
 if(error)return <p role="alert">{error} <button onClick={()=>void execute()}>{t("common.retry")}</button></p>;
 if(isLoading||!data)return <p className={styles.muted}>{t("common.loading")}</p>;
 if(Array.isArray(data))return <div className={styles.list}>{data.length?data.map(item=><div key={item.session_id}><Smartphone size={18}/><span>{item.device_name}<small>{item.client}{item.now_playing?` · ${item.now_playing.name}`:""}</small></span></div>):<p className={styles.muted}>{t("mini.emptyDevices")}</p>}</div>;
 if(section==="lines")return <div className={styles.list}>{[...(data.lines||[]),...(data.whitelist_lines||[])].map((line,i)=><div key={i}><Compass size={18}/><span>{line.name}<small>{t("mini.linePrivate")}</small></span><button type="button" onClick={()=>void copy(line.url)}>{copied===line.url?<Check size={18}/>:t("mini.copy")}</button></div>)}{!data.lines?.length&&!data.whitelist_lines?.length&&<p className={styles.muted}>{t("mini.emptyLines")}</p>}</div>;
 if(section==="invite")return <div className={styles.list}><p className={styles.muted}>{data.can_invite?t("mini.inviteReady"):data.reason||t("mini.inviteUnavailable")}</p>{data.codes?.map(code=><div key={code.code}><Ticket size={18}/><span>{code.code}<small>{code.days} {t("mini.days")} · {code.active?t("mini.usable"):t("mini.unusable")}</small></span><button onClick={()=>void copy(code.code)}>{copied===code.code?<Check size={18}/>:t("mini.copy")}</button></div>)}</div>;
 return <div className={styles.list}>{data.items?.length?data.items.map(item=><div key={item.id}><Clapperboard size={18}/><span>{item.title}<small>{requestStatus(item.status,t)}</small></span></div>):<p className={styles.muted}>{t("mini.emptyRequests")}</p>}</div>;
}
export default function MiniAppPage() {
 const {t}=useI18n();
 const [sdk,setSdk]=useState(false),[tab,setTab]=useState<Tab>("home"),[busy,setBusy]=useState(false),[notice,setNotice]=useState(""),[revision,setRevision]=useState(0),[confirmRenew,setConfirmRenew]=useState(false);
 const [discovery,setDiscovery]=useState<DiscoverySection>("announcements");
 function openDiscovery(section:DiscoverySection){setDiscovery(section);setTab("discover")}
 const [verificationID,setVerificationID]=useState("");
 const requestId=useRef(""),authenticated=useRef(false),operation=useRef(false);
 const load=useCallback(async(signal?:AbortSignal)=>{
  const app=window.Telegram?.WebApp;
  if(!app?.initData)throw new Error(t("jianwei.openFromTelegram"));
  app.ready();app.expand();
  if(!authenticated.current){
   if(!requestId.current)requestId.current=Array.from(crypto.getRandomValues(new Uint8Array(32)),v=>v.toString(16).padStart(2,"0")).join("");
   const session=await api.miniSession(app.initData,requestId.current,signal);if(!session.success)throw new Error(session.message);authenticated.current=true;
  }
  const response=await api.miniAccount(signal);if(!response.success||!response.data)throw new Error(response.message);return response.data;
 },[t]);
 const {data,error,isLoading,execute}=useAsyncResource(load,{immediate:sdk,throwOnError:false});
 async function action(name:Parameters<typeof api.miniAction>[0],body:Record<string,unknown>={}){
  if(operation.current)return;operation.current=true;setBusy(true);setNotice("");
  try{const r=await api.miniAction(name,body);if(!r.success)throw new Error(r.message);setNotice(r.message||t("mini.saved"));setRevision(v=>v+1);await execute()}catch(e){setNotice(e instanceof Error?e.message:t("mini.failed"))}finally{operation.current=false;setBusy(false);setConfirmRenew(false)}
 }
 function form(e:FormEvent<HTMLFormElement>,name:"redeem"|"register"|"requests"|"security"|"password"|"device-release") {e.preventDefault();const el=e.currentTarget;const body=Object.fromEntries(new FormData(el).entries());el.reset();void action(name,body)}
 async function sendPasswordEmail(){if(operation.current)return;operation.current=true;setBusy(true);try{const r=await api.miniAction<{verification_id:string}>("password-email");if(!r.success)throw new Error(r.message);setVerificationID(r.data?.verification_id||"");setNotice(r.message)}catch(e){setNotice(e instanceof Error?e.message:t("mini.failed"))}finally{operation.current=false;setBusy(false)}}
 function external(url:string){if(!url.startsWith("https://"))return;if(window.Telegram?.WebApp?.openLink)window.Telegram.WebApp.openLink(url);else window.open(url,"_blank","noopener,noreferrer")}
 const renew=data?.signin.summary.renewal;
 const tile=(icon:ReactNode,label:string,detail:string,click:()=>void,disabled=false)=><button type="button" className={styles.tile} disabled={disabled||busy} onClick={click}><span className={styles.tileIcon}>{icon}</span><span><strong>{label}</strong><small>{detail}</small></span></button>;
 return <main className={styles.shell}>
  <Script src="https://telegram.org/js/telegram-web-app.js" strategy="afterInteractive" onReady={()=>setSdk(true)} onError={()=>setSdk(true)}/>
  <div className={styles.ambient} aria-hidden="true"/>
  <div className={styles.content}>
   <header className={styles.header}><Image src={sanitizeImageUrl(data?.icon)||"/brand/jianwei-mark.svg"} alt="" width={44} height={44} unoptimized/><div><h1>{t("mini.brand")}</h1><p>{data?`${data.username} · ${t("mini.member")} ${data.uid}`:t("jianwei.tagline")}</p></div><button className={styles.iconButton} aria-label={t("mini.refresh")} disabled={isLoading||busy} onClick={()=>{setRevision(v=>v+1);void execute()}}><RefreshCw size={19}/></button></header>
   {error?<Glass><p role="alert">{error}</p><button onClick={()=>void execute()}>{t("common.retry")}</button></Glass>:isLoading&&!data||!data?<Glass><p>{t("common.loading")}</p></Glass>:<>
    {data.banner&&<aside className={styles.ad}><small>{t("mini.ad")}</small><p>{data.banner}</p>{data.banner_url&&<button onClick={()=>external(data.banner_url)}>{t("mini.learnMore")} <ArrowUpRight size={14}/></button>}</aside>}
    {notice&&<div role="status" aria-live="polite" className={styles.notice}>{notice}</div>}
    {tab==="home"&&<>
     <Glass className={styles.hero}><div className={styles.eyebrow}><span className={`${styles.dot} ${data.media_enabled?styles.online:""}`}/>{data.media_enabled?t("mini.active"):data.has_emby?t("mini.suspended"):t("mini.pending")}</div><h2>{t("mini.welcome")}</h2><p>{t("mini.welcomeDetail")}</p><div className={styles.balance}><span>{data.points.toLocaleString()}<small>{data.signin.config.currency_name}</small></span><Sparkles size={35}/></div><div className={styles.expiry}><span>{t("mini.validUntil")}</span><strong>{expiryText(data,t)}</strong></div></Glass>
     <div className={styles.grid}>{tile(<Sparkles/>,t("mini.signin"),data.signin.summary.today_signed?t("mini.signed"):t("mini.signinRange",{min:data.signin.config.daily_min,max:data.signin.config.daily_max}),()=>void action("signin"),!data.signin_enabled||data.signin.summary.today_signed)}{tile(<Ticket/>,t("mini.redeem"),t("mini.redeemHint"),()=>setTab("benefits"))}{tile(<Compass/>,t("mini.lines"),t("mini.linesHint"),()=>setTab("account"))}{tile(<Clapperboard/>,t("mini.requests"),t("mini.requestsHint"),()=>setTab("requests"),!data.requests_enabled)}</div>
     <div className={styles.grid}>{tile(<Film/>,t("mini.announcements"),t("mini.announcementsHint"),()=>openDiscovery("announcements"))}{tile(<Sparkles/>,t("mini.rank"),t("mini.rankHint"),()=>openDiscovery("rank"))}{tile(<Film/>,t("mini.favorites"),t("mini.favoritesHint"),()=>openDiscovery("favorites"))}{tile(<ShoppingBag/>,t("mini.shop"),t("mini.shopHint"),()=>data.shop_url?external(data.shop_url):setTab("benefits"),!data.shop_url)}</div>
     <Glass><h3>{t("mini.recent")}</h3>{data.signin.history.length?<div className={styles.history}>{data.signin.history.slice(0,5).map((entry,i)=><div key={`${entry.date}-${i}`}><span>{t("mini.signin")}<small>{entry.date}</small></span><strong>+{entry.total}</strong></div>)}</div>:<p className={styles.muted}>{t("mini.emptyHistory")}</p>}</Glass>
    </>}
    {tab==="discover"&&<Discovery key={discovery} initial={discovery}/>}
    {tab==="benefits"&&<>
     <Glass><h2>{t("mini.benefits")}</h2><p className={styles.muted}>{t("mini.redeemDescription")}</p><form onSubmit={e=>form(e,"redeem")}><label>{t("mini.code")}<input name="code" maxLength={180} autoComplete="off" required placeholder={t("mini.codePlaceholder")}/></label><button className={styles.primary} disabled={busy}>{t("mini.redeem")}</button></form></Glass>
     {renew?.enabled&&<Glass><h3>{t("mini.renew")}</h3><p>{t("mini.renewRule",{cost:renew.cost,days:renew.days})}</p><button className={styles.primary} disabled={busy||!data.has_emby||!renew.affordable} onClick={()=>confirmRenew?void action("renew"):setConfirmRenew(true)}>{confirmRenew?t("mini.confirmSpend",{cost:renew.cost}):t("mini.renew")}</button>{confirmRenew&&<button className={styles.subtle} onClick={()=>setConfirmRenew(false)}>{t("common.cancel")}</button>}</Glass>}
     {data.invite_enabled&&<Glass><h3><Users size={18}/>{t("mini.invite")}</h3><RemotePanel section="invite" revision={revision}/><button className={styles.secondary} disabled={busy} onClick={()=>void action("invite")}>{t("mini.createInvite")}</button></Glass>}
     {data.shop_url&&<Glass><h3><ShoppingBag size={18}/>{t("mini.shop")}</h3><p className={styles.muted}>{t("mini.shopHint")}</p><button className={styles.secondary} onClick={()=>external(data.shop_url)}>{t("mini.openShop")} <ArrowUpRight size={17}/></button></Glass>}
    </>}
    {tab==="requests"&&<Glass><h2>{t("mini.requests")}</h2>{data.requests_enabled?<><p className={styles.muted}>{t("mini.requestHelp")}</p><form onSubmit={e=>form(e,"requests")}><label>{t("mini.title")}<input name="title" maxLength={120} required/></label><label>{t("mini.note")}<textarea name="note" rows={3} maxLength={1000}/></label><button className={styles.primary} disabled={busy}>{t("mini.submitRequest")}</button></form><h3>{t("mini.myRequests")}</h3><RemotePanel section="requests" revision={revision}/></>:<p>{t("mini.requestsDisabled")}</p>}</Glass>}
    {tab==="account"&&<>
     <Glass><h2><UserRound size={21}/>{t("mini.account")}</h2><div className={styles.detail}><span>{t("mini.embyUser")}</span><strong>{data.emby_username||t("mini.pending")}</strong></div><div className={styles.detail}><span>{t("mini.validUntil")}</span><strong>{expiryText(data,t)}</strong></div><p className={styles.muted}>{t("mini.accountHint")}</p></Glass>
     {!data.has_emby&&<Glass><h3>{t("mini.register")}</h3>{data.can_register?<form onSubmit={e=>form(e,"register")}><label>{t("mini.username")}<input name="emby_username" minLength={3} maxLength={32} autoComplete="username" required/></label><label>{t("mini.password")}<input name="emby_password" type="password" minLength={8} maxLength={128} autoComplete="new-password" required/></label><p className={styles.muted}>{t("mini.passwordHint")}</p><button className={styles.primary} disabled={busy}>{t("mini.register")}</button></form>:<><p>{t("mini.registrationRequired")}</p><button className={styles.secondary} onClick={()=>setTab("benefits")}>{t("mini.redeem")}</button></>}</Glass>}
     <Glass><h3><Compass size={18}/>{t("mini.lines")}</h3><RemotePanel section="lines" revision={revision}/></Glass>
     <Glass><h3><Smartphone size={18}/>{t("mini.devices")}</h3><RemotePanel section="devices" revision={revision}/></Glass>
     <Glass><h3>{t("mini.registeredDevices")}</h3><MemberDevices revision={revision} busy={busy} release={body=>action("device-release",body)}/></Glass>
     <Glass><h3><ShieldCheck size={18}/>{t("mini.security")}</h3><p className={styles.muted}>{t("mini.securityHint")}</p><form onSubmit={e=>form(e,"security")}>{data.security_code_set&&<label>{t("mini.oldCode")}<input name="old_code" type="password" inputMode="numeric" pattern="[0-9]{6,12}" autoComplete="off" required/></label>}<label>{t("mini.newCode")}<input name="new_code" type="password" inputMode="numeric" pattern="[0-9]{6,12}" autoComplete="new-password" required/></label><button className={styles.secondary} disabled={busy}>{t("mini.saveSecurity")}</button></form></Glass>
     {data.has_emby&&data.security_code_set&&<Glass><h3>{t("mini.changePassword")}</h3><form onSubmit={e=>form(e,"password")}><label>{t("mini.security")}<input name="security_code" type="password" inputMode="numeric" autoComplete="off" required/></label><label>{t("mini.password")}<input name="new_password" type="password" minLength={8} maxLength={128} autoComplete="new-password" required/></label><input name="verification_id" type="hidden" value={verificationID}/>{data.password_email_required&&<><button type="button" className={styles.secondary} disabled={busy} onClick={()=>void sendPasswordEmail()}>{t("mini.sendEmailCode")}</button><label>{t("mini.emailCode")}<input name="email_code" autoComplete="one-time-code" required/></label></>}<p className={styles.muted}>{t("mini.passwordHint")}</p><button className={styles.secondary} disabled={busy}>{t("mini.changePassword")}</button></form></Glass>}
     <Glass><h3><ShieldCheck size={18}/>{t("mini.nativeBot")}</h3><p className={styles.muted}>{t("mini.nativeBotHint")}</p><p className={styles.commands}>/panel · /announcements · /rank · /favorites · /library · /shopcode · /signin · /redeem · /register · /devices · /lines · /request · /invite · /help</p><details><summary>{t("mini.about")}</summary><p>{t("mini.aboutText")}</p><a href="https://github.com/czy-code-923/emby-cloud-runtime" target="_blank" rel="noopener noreferrer">{t("mini.sourceLicense")}</a></details></Glass>
    </>}
    <nav className={styles.nav} aria-label={t("mini.navigation")}>{([{key:"home",icon:<Film/>,label:t("mini.home")},{key:"discover",icon:<Compass/>,label:t("mini.discovery")},{key:"benefits",icon:<Ticket/>,label:t("mini.benefits")},{key:"requests",icon:<Clapperboard/>,label:t("mini.requests")},{key:"account",icon:<UserRound/>,label:t("mini.account")}] as const).map(item=><button key={item.key} aria-current={tab===item.key?"page":undefined} onClick={()=>{setTab(item.key);setNotice("");setConfirmRenew(false)}}>{item.icon}<span>{item.label}</span></button>)}</nav>
   </>}
  </div>
 </main>;
}
function expiryText(data:MiniAccount,t:(key:MessageKey)=>string){return !data.has_emby?t("mini.pending"):data.expired_at<0?t("mini.permanent"):data.expired_at?new Date(data.expired_at*1000).toLocaleDateString():t("mini.pending")}

function requestStatus(status:string,t:(key:MessageKey)=>string){
 const keys:Record<string,MessageKey>={pending:"mini.status.pending",approved:"mini.status.approved",processing:"mini.status.processing",downloading:"mini.status.downloading",completed:"mini.status.completed",rejected:"mini.status.rejected",cancelled:"mini.status.cancelled",failed:"mini.status.failed"};
 return keys[status]?t(keys[status]):status;
}
