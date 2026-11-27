"use client";
import {useI18n,type MessageKey} from "@/lib/i18n";
import type {ShopOrder} from "@/lib/commerce-types";
import Image from "next/image";
import {commerceImageUrl} from "@/lib/commerce-images";
export const money=(cents:number)=>new Intl.NumberFormat("zh-CN",{style:"currency",currency:"CNY"}).format(cents/100);
export function OrderStatus({order}:{order:ShopOrder}){
 const {t}=useI18n();
 const known=new Set(["pending","review","hold","paid_hold","issued","cancelled","closed_unpaid","refund_pending","refunded"]);
 const key=order.redeemed&&order.status==="issued"?"redeemed":known.has(order.status)?order.status:"unknown";
 return <span className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium">{t(`commerce.status.${key}` as MessageKey)}</span>;
}
export function OrderPaymentDetails({order}:{order:ShopOrder}){
 const {t}=useI18n();const qr=commerceImageUrl(order.payment_qr);
 return <div className="space-y-3 rounded-xl border bg-muted/30 p-4 text-sm"><h3 className="font-medium">{t("v6.paymentSnapshot")}</h3><p>{order.channel} · {order.receiving_account}</p><p className="whitespace-pre-wrap">{order.instructions}</p>{qr&&<Image src={qr} alt={t("v6.qrPreview")} width={200} height={200} unoptimized className="h-auto max-h-60 w-auto max-w-full rounded-xl object-contain"/>}{order.payment_proof_url&&<div><p>{t("v8.paymentProof")}</p><Image src={order.payment_proof_url} alt={t("v8.paymentProof")} width={400} height={600} unoptimized className="mt-2 h-auto max-h-96 w-auto max-w-full rounded-xl object-contain"/></div>}{(order.received_cents||0)>0&&<p>{t(order.receipt_kind==="manual_order_confirmation"?"v8.confirmedAmount":"v6.receivedYuan")} {money(order.received_cents||0)} · {order.paid_at?new Date(order.paid_at*1000).toLocaleString():""}</p>}</div>;
}
