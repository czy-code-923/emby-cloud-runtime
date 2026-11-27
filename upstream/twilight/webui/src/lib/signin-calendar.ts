export function signinCalendar(history:{date:string;total:number}[],now=new Date()){
 const parts=new Intl.DateTimeFormat("en",{timeZone:"Asia/Shanghai",year:"numeric",month:"2-digit",day:"2-digit"}).formatToParts(now);
 const get=(type:string)=>Number(parts.find(p=>p.type===type)?.value);const today=Date.UTC(get("year"),get("month")-1,get("day"));const records=new Map(history.map(x=>[x.date,x]));
 return Array.from({length:30},(_,i)=>{const date=new Date(today-(29-i)*86400000).toISOString().slice(0,10);const record=records.get(date);return {date,day:Number(date.slice(8)),signed:!!record,total:record?.total||0,today:i===29}})
}
