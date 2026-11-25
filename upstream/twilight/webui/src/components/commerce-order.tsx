"use client";
import {useI18n,type MessageKey} from "@/lib/i18n";
import type {ShopOrder} from "@/lib/commerce-types";
export const money=(cents:number)=>new Intl.NumberFormat("zh-CN",{style:"currency",currency:"CNY"}).format(cents/100);
export function OrderStatus({order}:{order:ShopOrder}){
 const {t}=useI18n();
 const known=new Set(["pending","review","hold","paid_hold","issued","cancelled","closed_unpaid","refund_pending","refunded"]);
 const key=order.redeemed?"redeemed":known.has(order.status)?order.status:"unknown";
 return <span className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium">{t(`commerce.status.${key}` as MessageKey)}</span>;
}
