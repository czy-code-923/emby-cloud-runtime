"use client";

import Link from "next/link";
import { AdminConfigSections } from "@/components/admin/config-section-editor";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/lib/i18n";

export default function EntrySettingsPage() {
 const { t } = useI18n();
 return <div className="space-y-6">
  <div><h1 className="text-2xl font-bold">{t("entrySettings.title")}</h1><p className="mt-2 text-sm text-muted-foreground">{t("entrySettings.description")}</p></div>
  <Card><CardContent className="space-y-3 p-4 text-sm">
   <h2 className="font-semibold">{t("entrySettings.manualTitle")}</h2>
   <p>{t("entrySettings.manualSteps")}</p>
   <p className="text-muted-foreground">{t("entrySettings.isolation")}</p>
  </CardContent></Card>
  <AdminConfigSections sectionKeys={["Global"]} sectionFieldKeys={{Global:["public_base_url"]}} title={t("entrySettings.siteTitle")} description={t("entrySettings.siteDescription")} notice={t("entrySettings.savedOnly")} />
  <AdminConfigSections sectionKeys={["Emby"]} sectionFieldKeys={{Emby:["emby_public_url","emby_url_list","emby_whitelist_url","emby_url_list_for_whitelist"]}} title={t("entrySettings.playTitle")} description={t("entrySettings.playDescription")} />
  <div className="flex flex-wrap gap-3"><Button asChild variant="outline"><Link href="/admin/telegram">{t("navigation.telegramAdmin")}</Link></Button><Button asChild variant="outline"><Link href="/admin/config">{t("navigation.configAdmin")}</Link></Button></div>
 </div>;
}
