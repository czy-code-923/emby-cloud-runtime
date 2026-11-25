"use client";
import {useCallback,useEffect,useRef,useState,type FormEvent} from "react";
import {ShoppingBag,ArrowUpRight,RefreshCw} from "lucide-react";
import {api} from "@/lib/api";
import {useAsyncResource} from "@/hooks/use-async-resource";
import {useI18n} from "@/lib/i18n";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {OrderStatus,money} from "@/components/commerce-order";
import type {ShopOrder} from "@/lib/commerce-types";
import s from "./shop.module.css";

export default function ShopPage(){
 const {t}=useI18n();const [logged,setLogged]=useState(false),[code,setCode]=useState(""),[busy,setBusy]=useState(false),[notice,setNotice]=useState(""),[page,setPage]=useState(1);
 useEffect(()=>{const controller=new AbortController();void api.shopOrders(1,controller.signal).then(r=>{if(r.success)setLogged(true)}).catch(()=>{});return()=>controller.abort()},[]);
 const keys=useRef(new Map<string,string>());
 const loadCatalog=useCallback(async(signal?:AbortSignal)=>{const r=await api.shopCatalog(signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[]);
 const catalog=useAsyncResource(loadCatalog,{throwOnError:false});
 const loadOrders=useCallback(async(signal?:AbortSignal)=>{const r=await api.shopOrders(page,signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[page]);
 const orders=useAsyncResource(loadOrders,{immediate:logged,throwOnError:false});
 async function run(task:()=>Promise<{success:boolean;message?:string}>){if(busy)return;setBusy(true);setNotice("");try{const r=await task();if(!r.success)throw new Error(r.message);setNotice(r.message||t("commerce.saved"));await orders.execute({throwOnError:false})}catch(e){setNotice(e instanceof Error?e.message:t("commerce.failed"))}finally{setBusy(false)}}
 async function login(e:FormEvent){e.preventDefault();if(busy)return;setBusy(true);setNotice("");try{const r=await api.shopLogin(code);if(!r.success)throw new Error(r.message);setCode("");setLogged(true);setPage(1)}catch(e){setNotice(e instanceof Error?e.message:t("commerce.failed"))}finally{setBusy(false)}}
 const purchase=(id:string)=>run(async()=>{let key=keys.current.get(id);if(!key){key=crypto.randomUUID();keys.current.set(id,key)}const r=await api.shopCreate(id,catalog.data!.revision,key);if(r.success)keys.current.delete(id);return r});
 return <main className={s.page}><div className={s.wrap}>
  <header className={s.header}><div><p className={s.sub}>JIANWEI · MEMBERSHIP</p><h1>{t("commerce.title")}</h1></div><ShoppingBag size={30} strokeWidth={1.4}/></header>
  <p className={s.sub}>{t("commerce.intro")}</p>
  {(notice||catalog.error)&&<p role="status" className={s.notice}>{notice||catalog.error}</p>}
  {!logged?<form className={s.glass} onSubmit={login}><h2 className="font-medium">{t("commerce.login")}</h2><p className={s.sub}>{t("commerce.loginHelp")}</p><label>{t("commerce.loginCode")}<Input value={code} onChange={e=>setCode(e.target.value)} autoComplete="off" maxLength={20} required/></label><Button disabled={busy||code.trim().length!==20}>{t("commerce.login")}</Button></form>:<div className={s.row}><span>{orders.data?.username}</span><Button variant="ghost" disabled={busy} onClick={()=>void run(async()=>{const r=await api.shopLogout();if(r.success){setLogged(false);orders.setData(undefined)}return r})}>{t("commerce.logout")}</Button></div>}
  <section className={s.grid}>{catalog.data?.products.map(p=><article key={p.id} className={s.glass}><h2 className="font-medium">{p.name}</h2><p className={s.price}>{money(p.price_cents)}</p><p className={s.sub}>{p.kind===1?t("commerce.registration"):t("commerce.renewal")} · {p.days} {t("mini.days")}</p><Button disabled={!logged||busy} onClick={()=>void purchase(p.id)}>{t("commerce.order")}<ArrowUpRight size={16}/></Button></article>)}</section>
  {catalog.data&&!catalog.data.enabled&&<p className={s.notice}>{t("commerce.closed")}</p>}
  {logged&&<section className={s.glass}><div className={s.row}><h2 className="font-medium">{t("commerce.orders")}</h2><Button variant="ghost" disabled={busy||orders.isLoading} onClick={()=>void orders.execute({throwOnError:false})}><RefreshCw size={16}/>{t("commerce.refresh")}</Button></div>{orders.error&&<p role="alert">{orders.error}</p>}{orders.data?.items.map(o=><OrderCard key={o.id} order={o} busy={busy} run={run}/>)}{!orders.isLoading&&!orders.data?.items.length&&<p className={s.sub}>{t("commerce.empty")}</p>}<div className={s.row}><Button variant="outline" disabled={page<=1||busy} onClick={()=>setPage(page-1)}>{t("commerce.previous")}</Button><span>{page}</span><Button variant="outline" disabled={!orders.data?.has_next||busy} onClick={()=>setPage(page+1)}>{t("commerce.next")}</Button></div></section>}
  <footer className={s.foot}>{t("commerce.footer")}</footer>
 </div></main>
}
function OrderCard({order:o,busy,run}:{order:ShopOrder;busy:boolean;run:(task:()=>Promise<{success:boolean;message?:string}>)=>Promise<void>}){
 const {t}=useI18n();const [note,setNote]=useState("");
 return <article className={s.order}><div className={s.row}><span className="font-medium">{o.product.name} · {money(o.product.price_cents)}</span><OrderStatus order={o}/></div><p className={s.sub}>#{o.id}</p>
 {o.status==="pending"&&<><p className={s.sub}>{t("commerce.deadline")} {new Date(o.pay_before*1000).toLocaleString()}</p><p className={s.pre}>{o.instructions}</p><p>{o.channel} · {o.receiving_account}</p><p className={s.sub}>{t("commerce.paymentHelp")}</p><Input aria-label={t("commerce.note")} value={note} onChange={e=>setNote(e.target.value)} maxLength={500} placeholder={t("commerce.note")}/><div className={s.row}><Button disabled={busy} onClick={()=>void run(()=>api.shopPayment(o.id,note))}>{t("commerce.paid")}</Button><Button variant="outline" disabled={busy} onClick={()=>void run(()=>api.shopPayment(o.id,"",true))}>{t("commerce.cancel")}</Button></div></>}
 {o.admin_note&&<p className={s.pre}>{o.admin_note}</p>}{o.code&&<><code className={s.code}>{o.code}</code><p className={s.sub}>{t("commerce.redeemHelp")} · {new Date((o.code_expires_at||0)*1000).toLocaleDateString()}</p></>}
 </article>
}
