// Parse decimal CNY without floating-point multiplication or rounding.
export function yuanToCents(value: string): number | null {
  const match = /^(\d{1,6})(?:\.(\d{1,2}))?$/.exec(value.trim());
  if (!match) return null;
  const cents = Number(match[1]) * 100 + Number((match[2] || "").padEnd(2, "0"));
  return Number.isSafeInteger(cents) && cents <= 10_000_000 ? cents : null;
}
export function centsToYuan(cents: number): string {
  return Number.isSafeInteger(cents) && cents >= 0 ? `${Math.floor(cents / 100)}.${String(cents % 100).padStart(2, "0")}` : "";
}
