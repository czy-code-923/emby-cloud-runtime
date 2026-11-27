"use client";
import Image from "next/image";
import {useEffect,useState} from "react";
import {ArrowUpRight,Check,ShieldCheck,Sparkles} from "lucide-react";
import {useI18n} from "@/lib/i18n";
import type {ShopCatalog,ShopProduct,ShopOrders} from "@/lib/commerce-types";
import {Button} from "@/components/ui/button";
import {Dialog,DialogContent,DialogHeader,DialogTitle,DialogDescription,DialogFooter} from "@/components/ui/dialog";
import {money} from "@/components/commerce-order";
import s from "./shop.module.css";

export function ShopHero({catalog}:{catalog?:ShopCatalog}){
 const {t}=useI18n();
 return <header className={s.hero}><div className={s.heroTop}><Image src={catalog?.icon||"/brand/jianwei-mark.svg"} alt={catalog?.brand||"见微 Emby"} width={44} height={44} unoptimized className={s.brandIcon}/><span>JIANWEI · MEMBERSHIP</span></div><h1>{t("commerce.title")}</h1><p>{t("v8.shopHero")}</p><div className={s.heroBadges}><span><ShieldCheck size={15}/>{t("v8.heroVerify")}</span><span><Sparkles size={15}/>{t("v8.heroDelivery")}</span></div></header>;
}

export function MembershipCatalog({catalog,logged,busy,eligibility,onPurchase}:{catalog?:ShopCatalog;logged:boolean;busy:boolean;eligibility?:ShopOrders;onPurchase:(id:string,channel:string)=>Promise<void>}){
 const {t}=useI18n();const [kind,setKind]=useState(1),[product,setProduct]=useState<ShopProduct>(),[channel,setChannel]=useState("");
 useEffect(()=>{if(eligibility?.has_emby)setKind(2)},[eligibility?.has_emby]);
 const channels=catalog?.channels?.filter(c=>c.enabled)||[];
 const title=(type:number)=>t(type===4?"v10.serviceCategory":type===1?"v8.categoryRegister":type===2?"v8.categoryRenew":"v8.categoryWhite");
 const reason=(p:ShopProduct)=>p.recommendation||t(p.service_id?"v10.serviceReason":p.kind===1?"v8.reasonRegister":p.kind===2?"v8.reasonRenew":"v8.reasonWhite");
 const protectedPurchase=!!eligibility?.protected;
 const eligible=(p:ShopProduct)=>p.service_id?!!eligibility?.eligible_service_products?.includes(p.id):p.kind===1?!eligibility?.has_emby:p.kind===2?!!eligibility?.has_emby&&!protectedPurchase:!protectedPurchase;
 const blocked=(p:ShopProduct)=>p.service_id?t("v10.serviceIneligible"):p.kind===1?t("v8.alreadyRegistered"):protectedPurchase?t("v8.noPurchaseRequired"):t("v8.registerFirst");
 const inCategory=(p:ShopProduct,k:number)=>k===4?!!p.service_id:!p.service_id&&p.kind===k;
 const items=catalog?.products.filter(p=>inCategory(p,kind))||[];
 return <section className={s.catalog}>
  <div className={s.categoryTabs} aria-label={t("v8.productCategories")}>{[1,2,3,...(catalog?.products.some(p=>p.service_id)?[4]:[])].map(k=><button key={k} type="button" aria-pressed={kind===k} className={kind===k?s.categoryActive:""} onClick={()=>setKind(k)}>{title(k)}<small>{catalog?.products.filter(p=>inCategory(p,k)).length||0}</small></button>)}</div>
  <p className={s.sub}>{t(kind===4?"v10.serviceReason":kind===1?"v8.reasonRegister":kind===2?"v8.reasonRenew":"v8.reasonWhite")}</p>
  <div className={s.grid}>{items.map(p=><article className={s.product} key={p.id}>
   <div className={s.row}><span className={s.productKind}>{title(p.kind)}</span><span className={s.duration}>{p.days} {t("mini.days")}</span></div>
   <h2>{p.name}</h2><p className={s.price}>{money(p.price_cents)}</p>
   <p className={s.productReason}><Check size={16}/><span>{reason(p)}</span></p>
   <small className={s.sub}>{t(p.days<=31?"v8.reasonMonth":p.days<=100?"v8.reasonQuarter":"v8.reasonLong")}</small>
   {p.kind===2&&(eligibility?.whitelist_until||0)>Date.now()/1000&&<p className={s.sub}>{t("v8.whiteRenewalNote")}</p>}
   <Button disabled={!logged||busy} onClick={()=>{setProduct(p);setChannel(catalog?.default_channel||channels[0]?.id||"")}}>{!logged?t("v8.loginToBuy"):!eligible(p)?blocked(p):t("v8.chooseProduct")}<ArrowUpRight size={16}/></Button>
  </article>)}</div>
  {catalog&&items.length===0&&<p className={s.notice}>{t("v8.categoryEmpty")}</p>}
  <Dialog open={!!product} onOpenChange={open=>{if(!open)setProduct(undefined)}}><DialogContent className="max-h-[88dvh] overflow-y-auto rounded-3xl sm:max-w-lg"><DialogHeader><DialogTitle>{t("v8.checkoutTitle")}</DialogTitle><DialogDescription>{product?.name} · {money(product?.price_cents||0)}</DialogDescription></DialogHeader>
   <fieldset className={s.paymentChoices}><legend>{t("ops.paymentMethods")}</legend><p className="mb-3 text-sm text-muted-foreground">{t("v8.choosePaymentHelp")}</p>{channels.map(c=><label key={c.id} className={channel===c.id?s.paymentSelected:s.paymentChoice}><input type="radio" name="payment-channel" value={c.id} checked={channel===c.id} onChange={()=>setChannel(c.id)}/><span>{c.name}<small>{t(c.kind==="alipay_phrase"?"v8.phraseMethod":"v8.qrMethod")}</small></span></label>)}</fieldset>
   {product&&!eligible(product)&&<p role="status" className={s.notice}>{blocked(product)}</p>}
   <DialogFooter><Button variant="outline" onClick={()=>setProduct(undefined)}>{t("common.cancel")}</Button><Button disabled={busy||!product||!eligible(product)||!channels.some(c=>c.id===channel)} onClick={()=>{if(!product)return;const id=product.id;setProduct(undefined);void onPurchase(id,channel)}}>{t("commerce.order")}</Button></DialogFooter>
  </DialogContent></Dialog>
 </section>;
}
