"use client";

import {Clapperboard,Clock3} from "lucide-react";
import type {MiniViewingSummary} from "@/lib/api-types";
import {useI18n} from "@/lib/i18n";
import {viewingDurationParts,viewingTimestamp} from "@/lib/member-viewing";
import s from "./miniapp.module.css";

export function ViewingSummary({viewing}:{viewing?:MiniViewingSummary}) {
  const {t,locale}=useI18n();
  const duration=viewing?.available&&viewing.has_window_records?viewingDurationParts(viewing.seconds):null;
  const last=viewing?.available?viewingTimestamp(viewing.last_watched_at,viewing.window_end,locale):null;
  const poll=viewing?.available?viewingTimestamp(viewing.last_poll,viewing.window_end,locale):null;
  const elapsed=duration?duration.hours>0?t("viewing.durationHMS",duration):duration.minutes>0?t("viewing.durationMS",duration):t("viewing.durationS",duration):t(viewing?.available?"viewing.noWindowRecords":"viewing.unavailable");
  return <div className={s.viewingSummary}>
    <div className={s.viewingMetric}><Clapperboard size={17}/><span>{t("viewing.window")}<strong>{elapsed}</strong></span></div>
    <div className={s.viewingMetric}><Clock3 size={17}/><span>{t("viewing.last")}<strong>{last||t(viewing?.available?"viewing.noRecords":"viewing.unavailable")}</strong></span></div>
    <p className={s.viewingNote}>{t("viewing.timezone")}</p>
    {viewing?.quality==="stale"&&<p role="status" className={s.viewingNote}>{t("viewing.stale")}</p>}
    {viewing?.quality==="disabled"&&<p className={s.viewingNote}>{t("viewing.disabled")}</p>}
    {viewing?.quality==="waiting"&&<p className={s.viewingNote}>{t("viewing.waiting")}</p>}
    <details><summary>{t("viewing.source")}</summary><p>{t("viewing.rule")}</p>{poll&&<p>{t("viewing.sampledAt")} {poll}</p>}</details>
  </div>;
}
