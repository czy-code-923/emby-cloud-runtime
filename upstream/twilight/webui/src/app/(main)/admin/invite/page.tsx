"use client";

import { useCallback, useDeferredValue, useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  Ban,
  CheckSquare2,
  ChevronLeft,
  ChevronDown,
  ChevronRight,
  GitBranch,
  Loader2,
  RefreshCw,
  Search,
  ShieldCheck,
  Square,
  Trash2,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { AdminConfigSections } from "@/components/admin/config-section-editor";
import { AdminTable } from "@/components/admin/admin-table";
import { useAsyncResource } from "@/hooks/use-async-resource";
import { useToast } from "@/hooks/use-toast";
import { api, type InviteForest, type InviteAdminRow } from "@/lib/api";
import { useI18n, type MessageKey } from "@/lib/i18n";

interface TreeRow {
  node: InviteAdminRow;
  depth: number;
  root: number;
  childCount: number;
  descendants: number;
}

interface DepthPromptState {
  title: string;
  description: string;
  value: string;
  confirmLabel: string;
  min?: number;
  max?: number;
  resolve: (value: string | null) => void;
}

function roleLabelKey(role: number): MessageKey {
  if (role === 0) return "adminInvite.roleAdmin";
  if (role === 2) return "adminInvite.roleWhitelist";
  return "adminInvite.roleUser";
}

function formatUnix(seconds: number | null | undefined, locale: string, permanent: string): string {
  if (!seconds || seconds <= 0 || seconds >= 253402214400) return permanent;
  return new Date(seconds * 1000).toLocaleString(locale);
}

export default function AdminInviteTreePage() {
  const { toast } = useToast();
  const { confirm } = useConfirm();
  const { locale, t } = useI18n();
  const [forest, setForest] = useState<InviteForest | null>(null);
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const [rootFilter, setRootFilter] = useState<number | "all">("all");
  const [collapsed, setCollapsed] = useState<Set<number>>(new Set());
  const [selectedUid, setSelectedUid] = useState<number | null>(null);
  const [selectedUids, setSelectedUids] = useState<Set<number>>(new Set());
  const [page, setPage] = useState(1);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [depthPrompt, setDepthPrompt] = useState<DepthPromptState | null>(null);
  const params = useMemo(() => ({ page, search: deferredQuery.trim(), root: rootFilter, collapsed: [...collapsed], selected: selectedUid }), [page, deferredQuery, rootFilter, collapsed, selectedUid]);
  const load = useCallback(async (signal?: AbortSignal) => {
    const res = await api.adminGetInviteTree(signal, params);
    if (!res.success || !res.data) throw new Error(res.message || t("adminInvite.loadFailed"));
    if (!signal?.aborted) setForest(res.data);
    return res.data;
  }, [params, t]);
  const { isLoading: loading, error, execute: reload } = useAsyncResource(load, { immediate: true, throwOnError: false });
  const rootOptions = forest?.roots || [];
  const rows = useMemo<TreeRow[]>(() => (forest?.rows || []).map((row) => ({ node: row, depth: row.depth, root: row.root_uid, childCount: row.direct_children, descendants: row.descendants })), [forest]);
  const selected = (forest?.selected?.uid === selectedUid ? forest.selected : rows.find((row) => row.node.uid === selectedUid)?.node) || null;
  const visibleUids = useMemo(() => rows.map((row) => row.node.uid), [rows]);
  const allVisibleSelected = visibleUids.length > 0 && visibleUids.every((uid) => selectedUids.has(uid));
  const someVisibleSelected = visibleUids.some((uid) => selectedUids.has(uid));
  const selectedCount = selectedUids.size;
  useEffect(() => { setSelectedUids(new Set()); }, [params]);

  const requestDepth = useCallback((title: string, description: string, confirmLabel: string, min = 0) => {
    return new Promise<string | null>((resolve) => {
      setDepthPrompt({ title, description, value: "1", confirmLabel, min, resolve });
    });
  }, []);

  const requestRenewDays = useCallback(() => {
    return new Promise<string | null>((resolve) => {
      setDepthPrompt({
        title: t("adminInvite.quickRenewDaysTitle"),
        description: t("adminInvite.quickRenewDaysDescription"),
        value: "30",
        confirmLabel: t("adminInvite.continue"),
        min: -1,
        max: 36500,
        resolve,
      });
    });
  }, [t]);

  const closeDepthPrompt = (value: string | null) => {
    setDepthPrompt((current) => {
      if (current) current.resolve(value);
      return null;
    });
  };

  const handleDetach = async () => {
    if (!selected || busyAction) return;
    const ok = await confirm({
      title: t("adminInvite.detachTitle"),
      description: t("adminInvite.detachDescription"),
      tone: "warning",
      confirmLabel: t("adminInvite.detach"),
    });
    if (!ok) return;
    setBusyAction("detach");
    try {
      const res = await api.adminDetachInviteUser(selected.uid).catch((err) => ({
        success: false,
        message: err instanceof Error ? err.message : t("adminInvite.requestFailed"),
      }));
      if (res.success) {
        toast({ title: t("adminInvite.detached") });
        await reload();
      } else {
        toast({ title: t("common.operationFailed"), description: res.message, variant: "destructive" });
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleDetachAndDeleteEmby = async () => {
    if (!selected || busyAction) return;
    const ok = await confirm({
      title: t("adminInvite.detachDeleteEmbyTitle"),
      description: t("adminInvite.detachDeleteEmbyDescription", { username: selected.username, uid: selected.uid }),
      tone: "danger",
      confirmLabel: t("adminInvite.detachDeleteEmby"),
    });
    if (!ok) return;
    setBusyAction("detach-delete-emby");
    try {
      const res = await api.adminDetachInviteUserAndDeleteEmby(selected.uid).catch((err) => ({
        success: false,
        message: err instanceof Error ? err.message : t("adminInvite.requestFailed"),
      }));
      if (res.success) {
        toast({ title: t("adminInvite.detachedAndDeletedEmby") });
        setSelectedUid(null);
        await reload();
      } else {
        toast({ title: t("common.operationFailed"), description: res.message, variant: "destructive" });
      }
    } finally {
      setBusyAction(null);
    }
  };

  const toggleSelected = (uid: number, checked: boolean) => {
    setSelectedUids((prev) => {
      const next = new Set(prev);
      if (checked) next.add(uid);
      else next.delete(uid);
      return next;
    });
  };

  const toggleVisibleSelection = (checked: boolean) => {
    setSelectedUids((prev) => {
      const next = new Set(prev);
      for (const uid of visibleUids) {
        if (checked) next.add(uid);
        else next.delete(uid);
      }
      return next;
    });
  };

  const handleBulkDetach = async (deleteEmby: boolean, onlyEmbyDisabled = false) => {
    if (busyAction) return;
    const uids = [...selectedUids];
    if (uids.length === 0) {
      toast({ title: t("adminInvite.noBulkSelection"), variant: "destructive" });
      return;
    }
    const ok = await confirm({
      title: onlyEmbyDisabled
        ? t("adminInvite.bulkDetachDisabledEmbyTitle")
        : deleteEmby
          ? t("adminInvite.bulkDetachDeleteEmbyTitle")
          : t("adminInvite.bulkDetachTitle"),
      description: onlyEmbyDisabled
        ? t("adminInvite.bulkDetachDisabledEmbyDescription", { count: uids.length })
        : deleteEmby
          ? t("adminInvite.bulkDetachDeleteEmbyDescription", { count: uids.length })
          : t("adminInvite.bulkDetachDescription", { count: uids.length }),
      tone: deleteEmby ? "danger" : "warning",
      confirmLabel: deleteEmby ? t("adminInvite.detachDeleteEmby") : t("adminInvite.detach"),
    });
    if (!ok) return;
    setBusyAction("bulk-detach");
    try {
      const res = await api.adminBatchDetachInviteUsers(uids, { deleteEmby, onlyEmbyDisabled }).catch((err) => ({
        success: false,
        message: err instanceof Error ? err.message : t("adminInvite.requestFailed"),
        data: null,
      }));
      if (res.success && res.data) {
        toast({
          title: t("adminInvite.bulkComplete"),
          description: onlyEmbyDisabled
            ? t("adminInvite.bulkDisabledEmbyResult", {
                success: res.data.success,
                failed: res.data.failed,
                deleted: res.data.deleted_emby || 0,
                skipped: res.data.skipped_not_emby_disabled || 0,
              })
            : t("adminInvite.bulkResult", {
                success: res.data.success,
                failed: res.data.failed,
                deleted: res.data.deleted_emby || 0,
              }),
          variant: res.data.failed > 0 ? "default" : "success",
        });
        setSelectedUids(new Set());
        await reload();
      } else {
        toast({ title: t("common.operationFailed"), description: res.message, variant: "destructive" });
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleQuickMaintenance = async (
    payload: { scope: "selected" | "subtree" | "all"; uids?: number[]; root_uid?: number; depth?: number; include_root?: boolean },
    labelKey: MessageKey,
  ) => {
    if (busyAction) return;
    const rawDays = await requestRenewDays();
    if (rawDays === null) return;
    const renewDays = parseInt(rawDays, 10);
    if (!Number.isFinite(renewDays) || renewDays === 0 || renewDays < -1 || renewDays > 36500) {
      toast({ title: t("adminInvite.quickRenewDaysInvalid"), variant: "destructive" });
      return;
    }
    const ok = await confirm({
      title: t("adminInvite.quickConfirmTitle"),
      description: t(labelKey, {
        renewal: renewDays === -1 ? t("adminInvite.permanent") : t("adminInvite.daysValue", { days: renewDays }),
      }),
      tone: "danger",
      confirmLabel: t("adminInvite.quickConfirm"),
    });
    if (!ok) return;
    setBusyAction("quick-maintenance");
    try {
      const res = await api.adminInviteQuickMaintenance({ ...payload, detach: true, renew_days: renewDays }).catch((err) => ({
        success: false,
        message: err instanceof Error ? err.message : t("adminInvite.requestFailed"),
        data: null,
      }));
      if (res.success && res.data) {
        toast({
          title: t("adminInvite.quickComplete"),
          description: t("adminInvite.quickResult", {
            success: res.data.success,
            failed: res.data.failed,
            detached: res.data.detached,
            renewed: res.data.renewed,
            skipped: res.data.renew_skipped_disabled || 0,
          }),
          variant: res.data.failed > 0 ? "default" : "success",
        });
        setSelectedUids(new Set());
        setSelectedUid(null);
        await reload();
      } else {
        toast({ title: t("common.operationFailed"), description: res.message, variant: "destructive" });
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleSelectedQuickMaintenance = async () => {
    const uids = [...selectedUids];
    if (uids.length === 0) {
      toast({ title: t("adminInvite.noBulkSelection"), variant: "destructive" });
      return;
    }
    await handleQuickMaintenance({ scope: "selected", uids }, "adminInvite.quickSelectedDescription");
  };

  const handleSubtreeQuickMaintenance = async () => {
    if (!selected) return;
    await handleQuickMaintenance({ scope: "subtree", root_uid: selected.uid, depth: -1, include_root: false }, "adminInvite.quickSubtreeDescription");
  };

  const handleAllQuickMaintenance = async () => {
    await handleQuickMaintenance({ scope: "all" }, "adminInvite.quickAllDescription");
  };

  const handleCascadeToggle = async (enable: boolean) => {
    if (!selected || busyAction) return;
    const action = enable ? t("adminInvite.enable") : t("adminInvite.disable");
    const raw = await requestDepth(
      t("adminInvite.cascadeAction", { action }),
      t("adminInvite.depthDescription"),
      t("adminInvite.confirmAction", { action }),
    );
    if (raw === null) return;
    const depth = parseInt(raw, 10);
    if (!Number.isFinite(depth) || depth < -1) {
      toast({ title: t("adminInvite.depthInvalid"), variant: "destructive" });
      return;
    }
    const ok = await confirm({
      title: t("adminInvite.confirmCascade", { action }),
      description: depth === -1 ? t("adminInvite.wholeSubtree") : t("adminInvite.depthApply", { depth }),
      tone: enable ? "warning" : "danger",
      confirmLabel: t("adminInvite.confirmAction", { action }),
    });
    if (!ok) return;
    setBusyAction(enable ? "cascade-enable" : "cascade-disable");
    try {
      const res = await api.toggleUserActive(selected.uid, { enable, cascadeDepth: depth }).catch((err) => ({
        success: false,
        message: err instanceof Error ? err.message : t("adminInvite.requestFailed"),
        data: null,
      }));
      if (res.success) {
        toast({
          title: t("adminInvite.cascadeComplete"),
          description: t("adminInvite.cascadeResult", { affected: res.data?.affected?.length ?? 0, skipped: res.data?.skipped?.length ?? 0 }),
          variant: "success",
        });
        await reload();
      } else {
        toast({ title: t("common.operationFailed"), description: res.message, variant: "destructive" });
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleCascadeDelete = async () => {
    if (!selected || busyAction) return;
    const raw = await requestDepth(
      t("adminInvite.cascadeDelete"),
      t("adminInvite.depthDescription"),
      t("adminInvite.continueDelete"),
    );
    if (raw === null) return;
    const depth = parseInt(raw, 10);
    if (!Number.isFinite(depth) || depth < 0) {
      toast({ title: t("adminInvite.depthInvalid"), variant: "destructive" });
      return;
    }
    const ok = await confirm({
      title: t("adminInvite.confirmCascadeDelete"),
      description: depth === 0 ? t("adminInvite.deleteWholeSubtree") : t("adminInvite.deleteDepth", { depth }),
      tone: "danger",
      confirmLabel: t("common.delete"),
    });
    if (!ok) return;
    setBusyAction("cascade-delete");
    try {
      const res = await api.deleteUserScoped(selected.uid, { mode: "with_emby", cascadeDepth: depth }).catch((err) => ({
        success: false,
        message: err instanceof Error ? err.message : t("adminInvite.requestFailed"),
      }));
      if (res.success) {
        toast({ title: t("adminInvite.deleted"), variant: "success" });
        setSelectedUid(null);
        await reload();
      } else {
        toast({ title: t("common.operationFailed"), description: res.message, variant: "destructive" });
      }
    } finally {
      setBusyAction(null);
    }
  };

  const toggleCollapse = (uid: number) => {
    setPage(1);
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            <GitBranch className="h-5 w-5" />
            {t("adminInvite.title")}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {t("adminInvite.description")}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => void reload()} disabled={loading || Boolean(busyAction)}>
            {loading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RefreshCw className="mr-2 h-4 w-4" />}
            {t("common.refresh")}
          </Button>
          <Button variant="destructive" size="sm" onClick={() => void handleAllQuickMaintenance()} disabled={loading || (Boolean(busyAction) || loading) || !forest || forest.total_relations === 0}>
            <Ban className="mr-2 h-4 w-4" />
            {t("adminInvite.quickAll")}
          </Button>
        </div>
      </div>

      <Card>
        <CardContent className="flex flex-col gap-3 p-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="grid gap-2 sm:grid-cols-[minmax(220px,1fr)_220px]">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input disabled={Boolean(busyAction)} value={query} onChange={(event) => { setQuery(event.target.value); setPage(1); }} placeholder={t("adminInvite.searchPlaceholder")} className="pl-9" />
            </div>
            <select
              value={rootFilter}
              disabled={Boolean(busyAction)}
              aria-label={t("adminInvite.allRoots")}
              onChange={(event) => { setRootFilter(event.target.value === "all" ? "all" : Number(event.target.value)); setPage(1); }}
              className="h-10 rounded-md border bg-background px-3 text-sm"
            >
              <option value="all">{t("adminInvite.allRoots")}</option>
              {rootOptions.map(({ uid, username }) => (
                <option key={uid} value={uid}>
                  #{uid} {username}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
            <Badge variant="outline">{t("adminInvite.usersCount", { count: forest?.total_nodes ?? 0 })}</Badge>
            <Badge variant="outline">{t("adminInvite.relationsCount", { count: forest?.total_relations ?? 0 })}</Badge>
            <Badge variant="outline">{t("adminInvite.rootsCount", { count: forest?.roots.length ?? 0 })}</Badge>
          </div>
        </CardContent>
      </Card>

      {selectedCount > 0 && (
        <Card className="border-primary/30 bg-primary/5">
          <CardContent className="flex flex-col gap-3 p-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="flex min-w-0 items-center gap-2 text-sm font-medium">
              <CheckSquare2 className="h-4 w-4 text-primary" />
              <span>{t("adminInvite.selectedCount", { count: selectedCount })}</span>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={() => toggleVisibleSelection(true)} disabled={allVisibleSelected || visibleUids.length === 0}>
                <CheckSquare2 className="mr-2 h-4 w-4" />
                {t("adminInvite.selectVisible")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setSelectedUids(new Set())}>
                <Square className="mr-2 h-4 w-4" />
                {t("adminInvite.clearSelection")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => void handleBulkDetach(false)} disabled={(Boolean(busyAction) || loading)}>
                <Ban className="mr-2 h-4 w-4" />
                {t("adminInvite.bulkDetach")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => void handleSelectedQuickMaintenance()} disabled={(Boolean(busyAction) || loading)}>
                <ShieldCheck className="mr-2 h-4 w-4" />
                {t("adminInvite.quickSelected")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => void handleBulkDetach(true, true)} disabled={(Boolean(busyAction) || loading)}>
                <Trash2 className="mr-2 h-4 w-4" />
                {t("adminInvite.bulkDetachDisabledEmby")}
              </Button>
              <Button variant="destructive" size="sm" onClick={() => void handleBulkDetach(true)} disabled={(Boolean(busyAction) || loading)}>
                <Trash2 className="mr-2 h-4 w-4" />
                {t("adminInvite.bulkDetachDeleteEmby")}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {error ? (
        <Card className="border-destructive/40">
          <CardContent className="flex items-center gap-2 p-4 text-sm text-destructive">
            <AlertTriangle className="h-4 w-4" />
            {error}
          </CardContent>
        </Card>
      ) : loading && !forest ? (
        <div className="flex h-60 items-center justify-center">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : !forest || forest.total_nodes === 0 ? (
        <Card className="border-dashed">
          <CardContent className="space-y-2 p-10 text-center">
            <GitBranch className="mx-auto h-10 w-10 text-muted-foreground" />
            <p className="font-medium">{t("adminInvite.empty")}</p>
            <p className="text-xs text-muted-foreground">{t("adminInvite.emptyDescription")}</p>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="p-0">
            <AdminTable<TreeRow>
              rows={rows}
              rowKey={(row) => row.node.uid}
              containerClassName="max-h-[min(70dvh,900px)]"
              rowClassName={(row) => (selectedUid === row.node.uid ? "bg-primary/5" : "")}
              onRowContextMenu={(row, event) => {
                event.preventDefault();
                if (!busyAction && !loading) setSelectedUid(row.node.uid);
              }}
              empty={<div className="p-8 text-center text-sm text-muted-foreground">{t("adminInvite.noMatches")}</div>}
              columns={[
                {
                  key: "select",
                  hideOnCard: true,
                  headerClassName: "w-12",
                  header: (
                    <Checkbox
                      checked={allVisibleSelected ? true : someVisibleSelected ? "indeterminate" : false}
                      onCheckedChange={(checked) => toggleVisibleSelection(checked === true)}
                      aria-label={t("adminInvite.selectVisible")}
                    />
                  ),
                  cell: (row) => (
                    <Checkbox
                      checked={selectedUids.has(row.node.uid)}
                      onCheckedChange={(checked) => toggleSelected(row.node.uid, checked === true)}
                      aria-label={t("adminInvite.selectUser", { username: row.node.username })}
                    />
                  ),
                },
                {
                  key: "user",
                  header: t("adminInvite.user"),
                  cell: ({ node, depth, root, childCount }) => (
                    <div className="flex items-center gap-2" style={{ paddingLeft: Math.min(depth, 12) * 18 }}>
                      {childCount > 0 ? (
                        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => toggleCollapse(node.uid)} disabled={Boolean(busyAction) || loading} aria-label={t("adminInvite.details")}>
                          {collapsed.has(node.uid) ? <ChevronRight className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                        </Button>
                      ) : (
                        <span className="h-7 w-7" />
                      )}
                      <div className="min-w-0">
                        <button className="truncate text-left font-medium hover:underline" onClick={() => setSelectedUid(node.uid)}>
                          {node.username}
                        </button>
                        <p className="text-xs text-muted-foreground">
                          UID {node.uid} · L{depth + 1} · root {root}
                        </p>
                      </div>
                    </div>
                  ),
                },
                { key: "role", header: t("adminInvite.role"), cell: (row) => t(roleLabelKey(row.node.role)) },
                {
                  key: "status",
                  header: t("adminInvite.status"),
                  cell: (row) => (
                    <Badge variant={row.node.active ? "success" : "destructive"}>
                      {row.node.active ? t("adminInvite.enable") : t("adminInvite.disable")}
                    </Badge>
                  ),
                },
                {
                  key: "emby",
                  header: "Emby",
                  cell: (row) => (
                    <Badge variant={row.node.emby_disabled ? "destructive" : row.node.emby_bound ? "outline" : "secondary"}>
                      {row.node.emby_disabled
                        ? t("adminInvite.embyDisabled")
                        : row.node.emby_bound
                          ? t("adminInvite.bound")
                          : t("adminInvite.unbound")}
                    </Badge>
                  ),
                },
                { key: "telegram", header: "Telegram", cell: (row) => row.node.telegram_id || "-" },
                {
                  key: "children",
                  header: t("adminInvite.children"),
                  cell: (row) =>
                    t("adminInvite.childSummary", {
                      direct: row.childCount,
                      total: row.descendants,
                    }),
                },
                {
                  key: "actions",
                  header: t("adminInvite.actions"),
                  className: "text-right",
                  headerClassName: "text-right",
                  cell: (row) => (
                    <Button variant="outline" size="sm" onClick={() => setSelectedUid(row.node.uid)} disabled={(Boolean(busyAction) || loading)}>
                      {t("adminInvite.details")}
                    </Button>
                  ),
                },
              ]}
            />
            <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground">
              <span>{t("adminInvite.renderedRows", { shown: rows.length, total: forest.total_rows })} · {forest.page}/{forest.pages}</span>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" disabled={loading || Boolean(busyAction) || forest.page <= 1} onClick={() => setPage(forest.page - 1)} aria-label={t("common.previousPage")}><ChevronLeft className="h-4 w-4" /></Button>
                <Button variant="outline" size="sm" disabled={loading || Boolean(busyAction) || forest.page >= forest.pages} onClick={() => setPage(forest.page + 1)} aria-label={t("common.nextPage")}><ChevronRight className="h-4 w-4" /></Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      <Dialog open={selected !== null} onOpenChange={(open) => { if (!open && !busyAction) setSelectedUid(null); }}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{selected?.username}</DialogTitle>
            <DialogDescription>UID {selected?.uid}</DialogDescription>
          </DialogHeader>
          {selected && (
            <div className="space-y-3 text-sm">
              <div className="flex flex-wrap gap-2">
                <Badge variant={selected.active ? "success" : "secondary"}>{selected.active ? t("adminInvite.enable") : t("adminInvite.disable")}</Badge>
                <Badge variant={selected.emby_disabled ? "destructive" : selected.emby_bound ? "outline" : "secondary"}>
                  {selected.emby_disabled ? t("adminInvite.embyDisabled") : selected.emby_bound ? t("adminInvite.boundEmby") : t("adminInvite.noEmby")}
                </Badge>
                {selected.is_root && <Badge>{t("adminInvite.root")}</Badge>}
              </div>
              <dl className="space-y-2">
                <div className="flex justify-between gap-3">
                  <dt className="text-muted-foreground">{t("adminInvite.role")}</dt>
                  <dd>{t(roleLabelKey(selected.role))}</dd>
                </div>
                <div className="flex justify-between gap-3">
                  <dt className="text-muted-foreground">{t("adminInvite.registeredAt")}</dt>
                  <dd>{formatUnix(selected.register_time, locale, t("adminInvite.permanent"))}</dd>
                </div>
                <div className="flex justify-between gap-3">
                  <dt className="text-muted-foreground">{t("adminInvite.expiresAt")}</dt>
                  <dd>{formatUnix(selected.expired_at, locale, t("adminInvite.permanent"))}</dd>
                </div>
                <div className="flex justify-between gap-3">
                  <dt className="text-muted-foreground">{t("adminInvite.root")}</dt>
                  <dd>{selected.root_uid}</dd>
                </div>
                <div className="flex justify-between gap-3">
                  <dt className="text-muted-foreground">{t("adminInvite.subtree")}</dt>
                  <dd>{t("adminInvite.subtreeChildren", { count: selected.descendants })}</dd>
                </div>
              </dl>
              <div className="grid gap-2 pt-2">
                <Button variant="outline" size="sm" onClick={() => void handleDetach()} disabled={selected.is_root || (Boolean(busyAction) || loading)}>
                  <Ban className="mr-2 h-4 w-4" />
                  {selected.is_root ? t("adminInvite.alreadyRoot") : t("adminInvite.detach")}
                </Button>
                <Button variant="destructive" size="sm" onClick={() => void handleDetachAndDeleteEmby()} disabled={selected.role === 0 || (Boolean(busyAction) || loading)}>
                  <Trash2 className="mr-2 h-4 w-4" />
                  {t("adminInvite.detachDeleteEmby")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => void handleSubtreeQuickMaintenance()} disabled={(selected.descendants) === 0 || (Boolean(busyAction) || loading)}>
                  <ShieldCheck className="mr-2 h-4 w-4" />
                  {t("adminInvite.quickSubtree")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => void handleCascadeToggle(false)} disabled={(Boolean(busyAction) || loading)}>
                  <Ban className="mr-2 h-4 w-4" />
                  {t("adminInvite.cascadeDisable")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => void handleCascadeToggle(true)} disabled={(Boolean(busyAction) || loading)}>
                  <ShieldCheck className="mr-2 h-4 w-4" />
                  {t("adminInvite.cascadeEnable")}
                </Button>
                <Button variant="destructive" size="sm" onClick={() => void handleCascadeDelete()} disabled={(Boolean(busyAction) || loading)}>
                  <Trash2 className="mr-2 h-4 w-4" />
                  {t("adminInvite.cascadeDelete")}
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={depthPrompt !== null} onOpenChange={(open) => { if (!open) closeDepthPrompt(null); }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{depthPrompt?.title}</DialogTitle>
            <DialogDescription>{depthPrompt?.description}</DialogDescription>
          </DialogHeader>
          <Input
            type="number"
            min={depthPrompt?.min ?? 0}
            max={depthPrompt?.max}
            value={depthPrompt?.value ?? "1"}
            onChange={(event) => setDepthPrompt((current) => current ? { ...current, value: event.target.value } : current)}
          />
          {depthPrompt?.min === -1 && depthPrompt?.max === 36500 && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setDepthPrompt((current) => current ? { ...current, value: "-1" } : current)}
            >
              {t("adminInvite.setPermanent")}
            </Button>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => closeDepthPrompt(null)}>{t("common.cancel")}</Button>
            <Button disabled={!depthPrompt?.value.trim()} onClick={() => closeDepthPrompt(depthPrompt?.value ?? null)}>{depthPrompt?.confirmLabel || t("adminInvite.continue")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AdminConfigSections
        sectionKeys={["SAR"]}
        sectionFieldKeys={{
          SAR: [
            "invite_enabled",
            "invite_limit",
            "invite_root_user_limit",
            "invite_max_depth",
            "invite_require_emby",
            "invite_code_default_days",
            "permanent_invite_max_days",
            "invite_code_format",
            "invite_code_random_algorithm",
          ],
        }}
        title={t("adminInvite.configTitle")}
        description={t("adminInvite.configDescription")}
        notice={t("adminInvite.configNotice")}
      />
    </div>
  );
}
