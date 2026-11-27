import {sanitizeImageUrl} from "./safe-url";

// Uploaded QR images are served by the same backend through both entrances.
// Use the local entrance while previewing to keep CSP restricted to self.
export function commerceImageUrl(raw?:string):string {
 const value=raw||"";
 try {
  const u=new URL(value,"https://local.invalid");
  if(/^\/api\/v2\/commerce\/assets\/[a-f0-9]+\.(png|jpg)$/.test(u.pathname)&&!u.search&&!u.hash)return u.pathname;
 }catch{}
 return sanitizeImageUrl(value)||"";
}
