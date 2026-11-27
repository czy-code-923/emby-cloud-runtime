"use client";
import Image from "next/image";
import {useEffect,useState} from "react";
import {Input} from "@/components/ui/input";
import {Button} from "@/components/ui/button";
import {commerceImageUrl} from "@/lib/commerce-images";
import {useI18n} from "@/lib/i18n";
export function CommerceImagePicker({url,file,onChange,proof=false}:{url:string;file?:File;onChange:(file:File|undefined)=>void;proof?:boolean}) {
 const {t}=useI18n();const [preview,setPreview]=useState(""),[error,setError]=useState("");
 useEffect(()=>{if(!file){setPreview("");return}const src=URL.createObjectURL(file);setPreview(src);return()=>URL.revokeObjectURL(src)},[file]);
 const src=preview||commerceImageUrl(url);
 return <div className="space-y-2"><label className="block text-sm">{t(proof?"v8.proofUpload":"ops.uploadQR")}<Input type="file" accept="image/png,image/jpeg" onChange={e=>{const image=e.target.files?.[0];e.target.value="";if(!image)return;if(!["image/png","image/jpeg"].includes(image.type)||image.size>2*1024*1024){setError(t("v6.imageInvalid"));return}setError("");onChange(image)}}/></label><p className="text-xs text-muted-foreground">{t(proof?"v8.proofHelp":"v8.imageHelp")}</p>{error&&<p role="alert" className="text-sm text-destructive">{error}</p>}{src&&<Image src={src} width={200} height={200} alt={t(proof?"v8.proofUpload":"v6.qrPreview")} unoptimized className="h-auto max-h-64 w-auto max-w-full rounded-xl border object-contain"/>}{file&&<div className="flex flex-wrap items-center gap-2 text-sm"><span>{file.name} · {t("v6.pendingUpload")}</span><Button type="button" size="sm" variant="outline" onClick={()=>onChange(undefined)}>{t("common.cancel")}</Button></div>}</div>;
}
