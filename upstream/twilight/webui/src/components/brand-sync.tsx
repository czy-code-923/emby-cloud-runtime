"use client";
import {useEffect} from "react";
import {useSystemStore} from "@/store/system";
import {sanitizeImageUrl} from "@/lib/safe-url";

export function BrandSync(){
 const {info,fetchInfo}=useSystemStore();
 useEffect(()=>{void fetchInfo()},[fetchInfo]);
 useEffect(()=>{
  const icon=sanitizeImageUrl(info?.icon);if(!icon)return;
  const nodes=Array.from(document.head.querySelectorAll<HTMLLinkElement>('link[rel="icon"],link[rel="shortcut icon"],link[rel="apple-touch-icon"]'));
  if(!nodes.length){const link=document.createElement("link");link.rel="icon";document.head.appendChild(link);nodes.push(link)}
  for(const link of nodes){link.href=icon;link.removeAttribute("type");link.removeAttribute("sizes")}
 },[info?.icon]);
 return null;
}
