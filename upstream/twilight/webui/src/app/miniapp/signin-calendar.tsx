"use client";
import {signinCalendar} from "@/lib/signin-calendar";
import {useI18n} from "@/lib/i18n";
import s from "./miniapp.module.css";
export function SigninCalendar({history}:{history:{date:string;total:number}[]}){const {t}=useI18n();const days=signinCalendar(history);return <div className={s.calendarWrap}><h3>{t("v7.signin30")}</h3><div className={s.calendar} role="list">{days.map(d=><div role="listitem" key={d.date} className={`${s.calendarDay} ${d.signed?s.calendarSigned:""} ${d.today?s.calendarToday:""}`} title={`${d.date} · ${d.signed?"+"+d.total:t("v7.notSigned")}`} aria-label={`${d.date} · ${d.signed?t("mini.signed"):t("v7.notSigned")}`}><small>{d.day}</small><span>{d.signed?"✓":"·"}</span></div>)}</div><p className={s.muted}>{days.filter(x=>x.signed).length} / 30 · {t("v7.calendarHint")}</p></div>}
