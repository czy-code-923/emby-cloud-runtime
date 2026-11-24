const rawSiteName = process.env.NEXT_PUBLIC_SITE_NAME?.trim();
const rawSiteTitle = process.env.NEXT_PUBLIC_SITE_TITLE?.trim();
const rawSiteDescription = process.env.NEXT_PUBLIC_SITE_DESCRIPTION?.trim();
const rawSiteIcon = process.env.NEXT_PUBLIC_SITE_ICON?.trim();

export const SITE_NAME = rawSiteName || "见微 Emby";
export const SITE_TITLE = rawSiteTitle || `${SITE_NAME} · 管理中心`;
export const SITE_DESCRIPTION = rawSiteDescription || "于细微处，发现好故事。见微 Emby 私人媒体中心。";
export const SITE_ICON = rawSiteIcon || "/brand/jianwei-mark.svg";
