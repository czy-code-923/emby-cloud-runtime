"use client";
import {useCallback,useState,type FormEvent} from "react";
import Image from "next/image";
import {sanitizeMediaPosterUrl} from "@/lib/media-poster-url";
import {Heart,Film,Pin,Search,Trophy,Megaphone} from "lucide-react";
import {api} from "@/lib/api";
import {useAsyncResource} from "@/hooks/use-async-resource";
import {useI18n} from "@/lib/i18n";
import styles from "./miniapp.module.css";
export type DiscoverySection="announcements"|"rank"|"favorites"|"library";
interface DiscoveryItem{poster_url?:string;id?:string|number;title:string;content?:string;pinned?:boolean;created_at?:number;plays?:number;duration?:number;year?:number;type?:string;favorite?:boolean}
interface DiscoveryData{items:DiscoveryItem[];page?:number;has_next?:boolean;total?:number}
export function Discovery({initial="announcements"}:{initial?:DiscoverySection}){
 const {t}=useI18n();const [section,setSection]=useState<DiscoverySection>(initial),[page,setPage]=useState(1),[query,setQuery]=useState(""),[search,setSearch]=useState(""),[busy,setBusy]=useState(false),[notice,setNotice]=useState("");
 const load=useCallback(async(signal?:AbortSignal)=>{if(section==="library"&&search.length<2)return {items:[]} as DiscoveryData;const r=await api.miniBrowse<DiscoveryData>(section,page,search,signal);if(!r.success||!r.data)throw new Error(r.message);return r.data},[section,page,search]);
 const {data,error,isLoading,execute}=useAsyncResource(load,{throwOnError:false});
 function choose(next:DiscoverySection){setSection(next);setPage(1);setNotice("")}
 async function toggle(item:DiscoveryItem){if(busy||typeof item.id!=="string")return;setBusy(true);setNotice("");try{const r=await api.miniFavorite(item.id,!item.favorite);if(!r.success)throw new Error(r.message);setNotice(r.message);await execute({throwOnError:false})}catch(e){setNotice(e instanceof Error?e.message:t("mini.failed"))}finally{setBusy(false)}}
 function submit(e:FormEvent){e.preventDefault();setSearch(query.trim());setPage(1)}
 return <section className={styles.glass}><h2>{t("mini.discovery")}</h2><div className={styles.chips}>{([{key:"announcements",icon:<Megaphone size={16}/>,label:t("mini.announcements")},{key:"rank",icon:<Trophy size={16}/>,label:t("mini.rank")},{key:"favorites",icon:<Heart size={16}/>,label:t("mini.favorites")},{key:"library",icon:<Search size={16}/>,label:t("mini.library")}] as const).map(item=><button key={item.key} aria-pressed={section===item.key} disabled={busy} onClick={()=>choose(item.key)}>{item.icon}{item.label}</button>)}</div>
 {section==="rank"&&<p className={styles.muted}>{t("mini.rankHelp")}</p>}{section==="favorites"&&<p className={styles.muted}>{t("mini.favoritesHelp")}</p>}
 {section==="library"&&<form onSubmit={submit}><label>{t("mini.searchTitle")}<input value={query} onChange={e=>setQuery(e.target.value)} minLength={2} maxLength={120} required/></label><button className={styles.secondary} disabled={isLoading||busy}>{t("mini.search")}</button></form>}
 {(notice||error)&&<p role="status" className={styles.notice}>{notice||error}</p>}{isLoading?<p className={styles.muted}>{t("common.loading")}</p>:<div className={styles.discoveryList}>{data?.items.map((item,i)=><article key={String(item.id??i)} className={styles.discoveryItem}>{sanitizeMediaPosterUrl(item.poster_url)?<div className={styles.poster}><Image src={sanitizeMediaPosterUrl(item.poster_url)!} alt={item.title} width={120} height={180} unoptimized onError={e=>{e.currentTarget.hidden=true}}/></div>:section==="rank"?<span className={styles.rankNumber}>{String(i+1).padStart(2,"0")}</span>:<span className={styles.discoveryIcon}>{section==="announcements"?<Megaphone size={19}/>:<Film size={19}/>}</span>}<div><h3>{item.pinned&&<Pin size={13}/>} {item.title}</h3>{item.created_at&&<small>{new Date(item.created_at*1000).toLocaleDateString()}</small>}{item.content&&<p>{item.content}</p>}{item.plays!==undefined&&<small>{t("mini.rankCount",{count:item.plays,minutes:Math.round((item.duration||0)/60)})}</small>}{item.year!==undefined&&<small>{item.year||""} · {item.type==="Series"?t("mini.series"):t("mini.movie")}</small>}</div>{typeof item.favorite==="boolean"&&<button className={styles.iconButton} aria-label={item.favorite?t("mini.removeFavorite"):t("mini.addFavorite")} disabled={busy} onClick={()=>void toggle(item)}><Heart size={20} fill={item.favorite?"currentColor":"none"}/></button>}</article>)}</div>}
 {!isLoading&&!error&&!data?.items.length&&<p className={styles.muted}>{t("mini.discoveryEmpty")}</p>}{section!=="rank"&&<div className={styles.chips}><button disabled={page<=1||busy||isLoading} onClick={()=>setPage(page-1)}>{t("commerce.previous")}</button><span>{page}</span><button disabled={!data?.has_next||busy||isLoading} onClick={()=>setPage(page+1)}>{t("commerce.next")}</button></div>}
 </section>
}
