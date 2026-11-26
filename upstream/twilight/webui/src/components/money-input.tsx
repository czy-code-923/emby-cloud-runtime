"use client";
import {useState} from "react";
import {Input} from "@/components/ui/input";
import {centsToYuan,yuanToCents} from "@/lib/money";
import {useI18n} from "@/lib/i18n";
export function MoneyInput({value,onChange,minimum=0}:{value:number;onChange:(cents:number)=>void;minimum?:number}) {
 const {t}=useI18n();const [draft,setDraft]=useState(()=>centsToYuan(value));
 const parsed=yuanToCents(draft),invalid=parsed===null||parsed<minimum;
 return <><Input type="text" inputMode="decimal" pattern="[0-9]{1,6}(\.[0-9]{1,2})?" maxLength={9} required value={draft} placeholder="19.99" aria-invalid={invalid} onChange={e=>{const text=e.target.value,cents=yuanToCents(text);setDraft(text);onChange(cents??-1);e.target.setCustomValidity(cents===null||cents<minimum?t("v6.moneyInvalid"):"")}} onBlur={e=>e.target.setCustomValidity(invalid?t("v6.moneyInvalid"):"")}/>{invalid&&<small className="text-destructive">{t("v6.moneyInvalid")}</small>}</>;
}
