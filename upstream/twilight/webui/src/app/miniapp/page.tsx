"use client";
import {PointsShop,ShopLoginCode,LiveService,MemberOrders} from "./points-shop";
import Script from "next/script";
import Image from "next/image";
import { useCallback, useEffect, useState, useRef, type ReactNode, type FormEvent } from "react";
import { Film, Sparkles, Ticket, Smartphone, Compass, UserRound, RefreshCw, ShoppingBag, Check, Users, Clapperboard, ArrowUpRight, ShieldCheck } from "lucide-react";
import { api } from "@/lib/api";
import type { MiniAccount } from "@/lib/api-types";
import { useAsyncResource } from "@/hooks/use-async-resource";
import { useI18n, type MessageKey } from "@/lib/i18n";
import styles from "./miniapp.module.css";
import {Discovery,type DiscoverySection} from "./discovery";
import {sanitizeImageUrl} from "@/lib/safe-url";
import {RetentionPanel} from "./retention";
import {MemberSecurity} from "./member-security";
import {MemberRequests} from "./requests";
import {MemberDevices} from "./member-devices";

declare global { interface Window { Telegram?: {WebApp?: {initData:string;ready():void;expand():void;openLink?(url:string):void}} } }
type Tab = "activity"|"discover"|"home"|"benefits"|"requests"|"account";
type ReadSection = "lines"|"devices"|"requests"|"invite";
type PanelData = {lines?:{name:string;url:string}[];whitelist_lines?:{name:string;url:string}[];items?:{id:number;title:string;status:string;admin_note?:string}[];codes?:{code:string;active:boolean;days:number}[];can_invite?:boolean;reason?:string;total?:number};
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
 if(section==="lines")return <div className={styles.list}>{[...(data.lines||[]),...(data.whitelist_lines||[])].map((line,i)=><div key={i}><Compass size={18}/><span>{line.name}<small>{line.url}</small><small>{t("ops.port")} {new URL(line.url).port||(line.url.startsWith("https:")?"443":"80")}</small></span><button type="button" onClick={()=>void copy(line.url)}>{copied===line.url?<Check size={18}/>:t("mini.copy")}</button></div>)}{!data.lines?.length&&!data.whitelist_lines?.length&&<p className={styles.muted}>{t("mini.emptyLines")}</p>}</div>;
 if(section==="invite")return <div className={styles.list}><p className={styles.muted}>{data.can_invite?t("mini.inviteReady"):data.reason||t("mini.inviteUnavailable")}</p>{data.codes?.map(code=><div key={code.code}><Ticket size={18}/><span>{code.code}<small>{code.days} {t("mini.days")} · {code.active?t("mini.usable"):t("mini.unusable")}</small></span><button onClick={()=>void copy(code.code)}>{copied===code.code?<Check size={18}/>:t("mini.copy")}</button></div>)}</div>;
 return <div className={styles.list}>{data.items?.length?data.items.map(item=><div key={item.id}><Clapperboard size={18}/><span>{item.title}<small>{requestStatus(item.status,t)}</small>{item.admin_note&&<small>{item.admin_note}</small>}</span></div>):<p className={styles.muted}>{t("mini.emptyRequests")}</p>}</div>;
}
export default function MiniAppPage() {
 const {t}=useI18n();
 const [sdk,setSdk]=useState(false),[tab,setTab]=useState<Tab>("home"),[busy,setBusy]=useState(false),[notice,setNotice]=useState(""),[revision,setRevision]=useState(0),[confirmRenew,setConfirmRenew]=useState(false);
 const [discovery,setDiscovery]=useState<DiscoverySection>("announcements");
 function openDiscovery(section:DiscoverySection){setDiscovery(section);setTab("discover")}
 const [retryUntil,setRetryUntil]=useState(0),[tick,setTick]=useState(Date.now());
 useEffect(()=>{if(!retryUntil)return;const timer=setInterval(()=>setTick(Date.now()),1000);return()=>clearInterval(timer)},[retryUntil]);
 const retrySeconds=Math.max(0,Math.ceil((retryUntil-tick)/1000));
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
  if(operation.current||retrySeconds)return false;operation.current=true;setBusy(true);setNotice("");
  try{const r=await api.miniAction(name,body);if(!r.success){const delay=Number((r.data as {retry_after_seconds?:number})?.retry_after_seconds||0);if(delay>0){setRetryUntil(Date.now()+delay*1000);setTick(Date.now())}throw new Error(r.message)}setNotice(r.message||t("mini.saved"));setRevision(v=>v+1);await execute();return true}catch(e){setNotice(e instanceof Error?e.message:t("mini.failed"));return false}finally{operation.current=false;setBusy(false);setConfirmRenew(false)}
 }
 function form(e:FormEvent<HTMLFormElement>,name:"redeem"|"register"|"requests"|"security"|"password"|"device-release") {e.preventDefault();const el=e.currentTarget;const body=Object.fromEntries(new FormData(el).entries());if(name==="register"&&body.emby_password!==body.confirm_password){setNotice(t("v6.inputMismatch"));return}void action(name,body).then(success=>{if(success){el.reset();if(name==="register")setNotice(t("v6.backupPassword"))}})}
 async function sendPasswordEmail(){if(operation.current)return;operation.current=true;setBusy(true);try{const r=await api.miniAction<{verification_id:string}>("password-email");if(!r.success)throw new Error(r.message);setVerificationID(r.data?.verification_id||"");setNotice(r.message)}catch(e){setNotice(e instanceof Error?e.message:t("mini.failed"))}finally{operation.current=false;setBusy(false)}}
 function external(url:string){if(!url.startsWith("https://"))return;if(window.Telegram?.WebApp?.openLink)window.Telegram.WebApp.openLink(url);else window.open(url,"_blank","noopener,noreferrer")}
 const renew=data?.signin.summary.renewal;
 const tile=(icon:ReactNode,label:string,detail:string,click:()=>void,disabled=false)=><button type="button" className={styles.tile} disabled={disabled||busy} onClick={click}><span className={styles.tileIcon}>{icon}</span><span><strong>{label}</strong><small>{detail}</small></span></button>;
 return <main className={styles.shell}>
  <Script src="https://telegram.org/js/telegram-web-app.js" strategy="afterInteractive" onReady={()=>setSdk(true)} onError={()=>setSdk(true)}/>
  <div className={styles.ambient} aria-hidden="true"/>
  <div className={styles.content}>
   <header className={styles.header}><Image src={sanitizeImageUrl(data?.avatar||data?.icon)||"/brand/jianwei-mark.svg"} alt="" width={44} height={44} unoptimized onError={e=>{if(!e.currentTarget.src.endsWith("/brand/jianwei-mark.svg"))e.currentTarget.src="/brand/jianwei-mark.svg"}}/><div><h1>{t("mini.brand")}</h1><p>{data?`${data.emby_username||data.username} · ${data.role_label||t("mini.member")}`:t("jianwei.tagline")}</p></div><button className={styles.iconButton} aria-label={t("mini.refresh")} disabled={isLoading||busy} onClick={()=>{setRevision(v=>v+1);void execute()}}><RefreshCw size={19}/></button></header>
   {error?<Glass><p role="alert">{error}</p><button onClick={()=>void execute()}>{t("common.retry")}</button></Glass>:isLoading&&!data||!data?<Glass><p>{t("common.loading")}</p></Glass>:<>
    {data.banner&&<aside className={styles.ad}>{data.banner_image&&<Image src={sanitizeImageUrl(data.banner_image)||"/brand/jianwei-panel.png"} alt="" width={600} height={200} unoptimized className="mb-3 w-full rounded-2xl"/>}<small>{t("mini.ad")}</small><p>{data.banner}</p>{data.banner_url&&<button onClick={()=>external(data.banner_url)}>{t("mini.learnMore")} <ArrowUpRight size={14}/></button>}</aside>}
    {retrySeconds>0&&<p role="status" className={styles.notice}>{t("v6.retrySeconds",{seconds:retrySeconds})}</p>}
    {notice&&<div role="status" aria-live="polite" className={styles.notice}>{notice}</div>}
    {tab==="home"&&<>
     <Glass className={styles.hero}><div className={styles.eyebrow}><span className={`${styles.dot} ${data.media_enabled?styles.online:""}`}/>{data.media_enabled?t("mini.active"):data.has_emby?t("mini.suspended"):t("mini.pending")}</div><h2>{t("mini.welcome")}</h2><p>{t("mini.welcomeDetail")}</p><div className={styles.balance}><span>{data.points.toLocaleString()}<small>{data.signin.config.currency_name}</small></span><Sparkles size={35}/></div><div className={styles.expiry}><span>{t("mini.validUntil")}</span><strong>{expiryText(data,t)}</strong></div></Glass>
     <div className={styles.grid}>{tile(<Sparkles/>,t("mini.signin"),data.signin.summary.today_signed?t("mini.signed"):t("mini.signinRange",{min:data.signin.config.daily_min,max:data.signin.config.daily_max}),()=>void action("signin"),!data.signin_enabled||data.signin.summary.today_signed)}{tile(<Ticket/>,t("ops.activity"),t("ops.activityHint"),()=>setTab("activity"))}{tile(<Compass/>,t("mini.lines"),t("mini.linesHint"),()=>setTab("account"))}{tile(<Clapperboard/>,t("mini.requests"),t("mini.requestsHint"),()=>setTab("requests"),!data.requests_enabled)}</div>
     <div className={styles.grid}>{tile(<Film/>,t("mini.announcements"),t("mini.announcementsHint"),()=>openDiscovery("announcements"))}{tile(<Sparkles/>,t("mini.rank"),t("mini.rankHint"),()=>openDiscovery("rank"))}{tile(<Film/>,t("mini.favorites"),t("mini.favoritesHint"),()=>openDiscovery("favorites"))}{tile(<ShoppingBag/>,t("mini.shop"),t("mini.shopHint"),()=>data.shop_url?external(data.shop_url):setTab("benefits"),!data.shop_url)}</div>
     <LiveService/>
    </>}
    {tab==="discover"&&<Discovery key={discovery} initial={discovery}/>}
    {tab==="activity"&&<><Glass><h2>{t("v6.activityTitle")}</h2><p className={styles.muted}>{t("mini.signinRange",{min:data.signin.config.daily_min,max:data.signin.config.daily_max})}</p><button className={styles.primary} disabled={busy||!data.signin_enabled||data.signin.summary.today_signed} onClick={()=>void action("signin")}>{data.signin.summary.today_signed?t("mini.signed"):t("mini.signin")}</button><details><summary>{t("mini.recent")}</summary><div className={styles.history}>{data.signin.history.map((entry,i)=><div key={`${entry.date}-${i}`}><span>{entry.date}</span><strong>+{entry.total}</strong></div>)}</div></details></Glass><RetentionPanel revision={revision} onChange={()=>{setRevision(v=>v+1);void execute()}}/></>}
    {tab==="benefits"&&<><PointsShop revision={revision} onChange={()=>{setRevision(v=>v+1);void execute()}}/><ShopLoginCode shopURL={data.shop_url} open={external}/><MemberOrders revision={revision} onChange={()=>{setRevision(v=>v+1);void execute()}}/>
     <Glass><h2>{t("mini.benefits")}</h2><p className={styles.muted}>{t("mini.redeemDescription")}</p><form onSubmit={e=>form(e,"redeem")}><label>{t("mini.code")}<input name="code" maxLength={180} autoComplete="off" required placeholder={t("mini.codePlaceholder")}/></label><button className={styles.primary} disabled={busy||retrySeconds>0}>{t("mini.redeem")}</button></form></Glass>
     {renew?.enabled&&<Glass><h3>{t("mini.renew")}</h3><p>{t("mini.renewRule",{cost:renew.cost,days:renew.days})}</p><button className={styles.primary} disabled={busy||!data.has_emby||!renew.affordable} onClick={()=>confirmRenew?void action("renew"):setConfirmRenew(true)}>{confirmRenew?t("mini.confirmSpend",{cost:renew.cost}):t("mini.renew")}</button>{confirmRenew&&<button className={styles.subtle} onClick={()=>setConfirmRenew(false)}>{t("common.cancel")}</button>}</Glass>}
     {data.invite_enabled&&<Glass><h3><Users size={18}/>{t("mini.invite")}</h3><RemotePanel section="invite" revision={revision}/><button className={styles.secondary} disabled={busy} onClick={()=>void action("invite")}>{t("mini.createInvite")}</button></Glass>}
    </>}
    {tab==="requests"&&<MemberRequests enabled={data.requests_enabled}/>}
    {tab==="account"&&<>
     <Glass><h2><UserRound size={21}/>{t("mini.account")}</h2><div className={styles.detail}><span>{t("mini.embyUser")}</span><strong>{data.emby_username||t("mini.pending")}</strong></div><div className={styles.detail}><span>{t("mini.validUntil")}</span><strong>{expiryText(data,t)}</strong></div><p className={styles.muted}>{t("mini.accountHint")}</p></Glass>
     {!data.has_emby&&<Glass><h3>{t("mini.register")}</h3>{data.can_register?<form onSubmit={e=>form(e,"register")}><label>{t("mini.username")}<input name="emby_username" minLength={3} maxLength={32} autoComplete="username" required/></label><label>{t("mini.password")}<input name="emby_password" type="password" minLength={8} maxLength={128} autoComplete="new-password" required/></label><label>{t("v6.confirmPassword")}<input name="confirm_password" type="password" minLength={8} maxLength={128} autoComplete="new-password" required/></label><p className={styles.muted}>{t("mini.passwordHint")}</p><button className={styles.primary} disabled={busy}>{t("mini.register")}</button></form>:<><p>{t("mini.registrationRequired")}</p><button className={styles.secondary} onClick={()=>setTab("benefits")}>{t("mini.redeem")}</button></>}</Glass>}
     <Glass><h3><Compass size={18}/>{t("mini.lines")}</h3><RemotePanel section="lines" revision={revision}/></Glass>
     <Glass><h3>{t("mini.registeredDevices")}</h3><MemberDevices revision={revision} busy={busy} release={async body=>{await action("device-release",body)}}/></Glass>
     <MemberSecurity account={data} onChange={()=>void execute()}/>
     <Glass><h3><ShieldCheck size={18}/>{t("mini.nativeBot")}</h3><p className={styles.muted}>{t("mini.nativeBotHint")}</p><p className={styles.commands}>/panel · /announcements · /rank · /favorites · /library · /shopcode · /signin · /redeem · /register · /devices · /lines · /request · /invite · /help</p><details><summary>{t("mini.about")}</summary><p>{t("mini.aboutText")}</p></details></Glass>
    </>}
    <nav className={styles.nav} aria-label={t("mini.navigation")}>{([{key:"home",icon:<Film/>,label:t("mini.home")},{key:"discover",icon:<Compass/>,label:t("mini.discovery")},{key:"activity",icon:<Sparkles/>,label:t("v6.activityNav")},{key:"benefits",icon:<Ticket/>,label:t("mini.benefits")},{key:"requests",icon:<Clapperboard/>,label:t("mini.requests")},{key:"account",icon:<UserRound/>,label:t("mini.account")}] as const).map(item=><button key={item.key} aria-current={tab===item.key?"page":undefined} onClick={()=>{setTab(item.key);setNotice("");setConfirmRenew(false)}}>{item.icon}<span>{item.label}</span></button>)}</nav>
   </>}
  </div>
 </main>;
}
function expiryText(data:MiniAccount,t:(key:MessageKey)=>string){return !data.has_emby?t("mini.pending"):data.expired_at<0?t("mini.permanent"):data.expired_at?new Date(data.expired_at*1000).toLocaleDateString():t("mini.pending")}

function requestStatus(status:string,t:(key:MessageKey)=>string){
 const keys:Record<string,MessageKey>={unhandled:"mini.status.pending",accepted:"mini.status.approved",pending:"mini.status.pending",approved:"mini.status.approved",processing:"mini.status.processing",downloading:"mini.status.downloading",completed:"mini.status.completed",rejected:"mini.status.rejected",cancelled:"mini.status.cancelled",failed:"mini.status.failed"};
 const key=keys[status.toLowerCase()];return key?t(key):t("mini.status.processing");
}
