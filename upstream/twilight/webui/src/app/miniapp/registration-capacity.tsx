"use client";

import {Ticket,UsersRound} from "lucide-react";
import type {MiniRegistrationSummary} from "@/lib/api-types";
import {useI18n} from "@/lib/i18n";
import s from "./miniapp.module.css";

export function RegistrationCapacity({registration:v}:{registration?:MiniRegistrationSummary}) {
  const {t}=useI18n();
  if(!v)return null;
  const unlimited=v.unlimited;
  const left=unlimited?t("capacity.unlimited"):v.remaining===null?t("capacity.unavailable"):v.remaining.toLocaleString();
  return <section className={`${s.glass} ${s.capacity}`} aria-label={t("capacity.title")}>
    <div className={s.capacityHeading}><div><Ticket size={18}/><h2>{t("capacity.title")}</h2></div><span>Emby</span></div>
    <div className={s.capacityMetrics}>
      <div><span>{t("capacity.total")}</span><strong>{unlimited?t("capacity.unlimited"):v.limit.toLocaleString()}</strong></div>
      <div><span><UsersRound size={12}/>{t("capacity.registered")}</span><strong>{v.registered.toLocaleString()}</strong></div>
      <div className={s.capacityRemaining}><span>{t("capacity.remaining")}</span><strong>{left}</strong></div>
    </div>
    {!unlimited&&v.limit>0&&<progress className={s.capacityProgress} aria-label={t("capacity.occupied")} max={v.limit} value={Math.min(v.occupied,v.limit)}/>}
    <p className={s.capacityNote}>{v.reserved>0?t("capacity.reservedHint",{count:v.reserved}):t("capacity.hint")}</p>
  </section>;
}
