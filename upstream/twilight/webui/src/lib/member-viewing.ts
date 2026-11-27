export function viewingDurationParts(seconds:number) {
  if (!Number.isSafeInteger(seconds) || seconds < 0) return null;
  return {hours:Math.floor(seconds/3600),minutes:Math.floor(seconds%3600/60),seconds:seconds%60};
}

export function viewingTimestamp(value:number,asOf:number,locale="zh-Hans"):string|null {
  if (!Number.isSafeInteger(value) || value<=0 || !Number.isSafeInteger(asOf) || value>asOf) return null;
  const date=new Date(value*1000);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat(locale,{timeZone:"Asia/Shanghai",year:"numeric",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",second:"2-digit",hourCycle:"h23"}).format(date);
}
