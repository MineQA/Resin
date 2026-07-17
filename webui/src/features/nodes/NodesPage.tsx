import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { AlertTriangle, CheckCircle2, Copy, Download, Eraser, Gauge, Globe, RefreshCw, Settings, Sparkles, X, XCircle, Zap } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { useLocation } from "react-router-dom";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { DataTable } from "../../components/ui/DataTable";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Input";
import { OffsetPagination } from "../../components/ui/OffsetPagination";
import { Select } from "../../components/ui/Select";
import { Switch } from "../../components/ui/Switch";
import { ToastContainer } from "../../components/ui/Toast";
import { useToast } from "../../hooks/useToast";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { formatDateTime, formatRelativeTime } from "../../lib/time";
import { PROTOCOL_OPTIONS } from "../../lib/protocolOptions";
import {
  PROTOCOL_PILL_ROW_STYLE,
  PROTOCOL_PILL_SELECTED_STYLE,
  PROTOCOL_PILL_STYLE,
} from "../../lib/protocolPillStyles";
import {
  CF_STATUS_TOKENS,
  cfStatusLabel,
  gradeBadgeVariant,
  isCFContradiction,
  normalizeCFStatus,
  normalizeCFStatusSet,
  type CloudflareStatusToken,
} from "../../lib/cloudflareStatus";
import {
  CloudflareStatusBadge,
  ScoreBreakdownExplanation,
} from "../../components/ScoreBreakdown";
import { listPlatforms } from "../platforms/api";
import type { Platform } from "../platforms/types";
import { listSubscriptions } from "../subscriptions/api";
import { listRuleProfiles } from "../systemConfig/api";
import { buildNodePoolExportURL, exportNodePoolText, getNode, listNodes, probeEgress, probeLatency, probeQuality } from "./api";
import type { NodeQuality, NodeSummary } from "./types";
import { getAllRegions, getRegionName } from "./regions";
import type { NodeListFilters, NodeSortBy, QualityGradeFilter, SortOrder } from "./types";

type NodeStatusFilter = "all" | "healthy" | "circuit_open" | "error" | "disabled";
type NodeRoutableFilter = "all" | "routable" | "unroutable";
type ExportFormat = "clash" | "base64" | "uri" | "sing-box";
type ExportRoutableMode = "current" | "all" | "routable" | "unroutable";
type ExportBooleanMode = "any" | "true" | "false";
type NodeDisplayStatus = "healthy" | "circuit_open" | "pending_test" | "error" | "disabled";
type ProbeAction = "egress" | "latency" | "quality";

type NodeExportSettings = {
  format: ExportFormat;
  routable: ExportRoutableMode;
  enabled: ExportBooleanMode;
  hasOutbound: ExportBooleanMode;
  tagKeyword: string;
  protocol: string;
  excludeProtocol: string;
  ruleProfileID: string;
};

type NodeListSettings = {
  pageSize: number;
  autoRefresh: boolean;
  defaultRoutableOnly: boolean;
};

type NodeFilterDraft = {
  platform_id: string;
  subscription_id: string;
  tag_keyword: string;
  region: string;
  egress_ip: string;
  status: NodeStatusFilter;
  routable: NodeRoutableFilter;
  protocolInclude: string[];
  protocolExclude: string[];
  quality_grade: string;
  quality_min_score: string;
  quality_cloudflare_challenged: string;
  quality_cloudflare_status: CloudflareStatusToken[];
  quality_checked_since: string;
  quality_profile: string;
};

const defaultFilterDraft: NodeFilterDraft = {
  platform_id: "",
  subscription_id: "",
  tag_keyword: "",
  region: "",
  egress_ip: "",
  status: "all",
  routable: "all",
  protocolInclude: [],
  protocolExclude: [],
  quality_grade: "",
  quality_min_score: "",
  quality_cloudflare_challenged: "any",
  quality_cloudflare_status: [],
  quality_checked_since: "",
  quality_profile: "",
};

const NODE_LIST_SETTINGS_KEY = "resin_node_list_settings";
const EXPORT_TOKEN_STORAGE_KEY = "resin_export_token";
const DEFAULT_EXPORT_SETTINGS: NodeExportSettings = {
  format: "clash",
  routable: "current",
  enabled: "any",
  hasOutbound: "any",
  tagKeyword: "",
  protocol: "",
  excludeProtocol: "",
  ruleProfileID: "",
};
const DEFAULT_NODE_LIST_SETTINGS: NodeListSettings = {
  pageSize: 200,
  autoRefresh: true,
  defaultRoutableOnly: false,
};
const PAGE_SIZE_OPTIONS = [20, 50, 100, 200, 500, 1000, 2000, 5000] as const;
const EMPTY_PLATFORMS: Platform[] = [];
const NODE_FILTER_ITEM_STYLE: CSSProperties = {
  flex: "1 1 120px",
  minWidth: "80px",
  display: "flex",
  flexDirection: "column",
  gap: "0.25rem",
};
const NODE_FILTER_CONTROL_STYLE: CSSProperties = {
  width: "100%",
  padding: "4px 8px",
  fontSize: "0.875rem",
  minHeight: "32px",
  height: "32px",
};

function parseBoolParam(value: string | null): boolean | undefined {
  if (value === null) {
    return undefined;
  }

  const normalized = value.trim().toLowerCase();
  if (normalized === "true" || normalized === "1") {
    return true;
  }
  if (normalized === "false" || normalized === "0") {
    return false;
  }

  return undefined;
}

function parseStatusParam(value: string | null): NodeStatusFilter | undefined {
  if (value === null) {
    return undefined;
  }

  const normalized = value.trim().toLowerCase();
  if (normalized === "all" || normalized === "healthy" || normalized === "circuit_open" || normalized === "error" || normalized === "disabled") {
    return normalized;
  }

  return undefined;
}

function parseRoutableParam(value: string | null): NodeRoutableFilter | undefined {
  const parsed = parseBoolParam(value);
  if (parsed === true) {
    return "routable";
  }
  if (parsed === false) {
    return "unroutable";
  }

  const normalized = value?.trim().toLowerCase();
  if (normalized === "all" || normalized === "routable" || normalized === "unroutable") {
    return normalized;
  }
  return undefined;
}

function loadNodeListSettings(): NodeListSettings {
  if (typeof window === "undefined") {
    return DEFAULT_NODE_LIST_SETTINGS;
  }
  try {
    const raw = window.localStorage.getItem(NODE_LIST_SETTINGS_KEY);
    if (!raw) {
      return DEFAULT_NODE_LIST_SETTINGS;
    }
    const parsed = JSON.parse(raw) as Partial<NodeListSettings>;
    const pageSize = PAGE_SIZE_OPTIONS.includes(parsed.pageSize as (typeof PAGE_SIZE_OPTIONS)[number])
      ? Number(parsed.pageSize)
      : DEFAULT_NODE_LIST_SETTINGS.pageSize;
    return {
      pageSize,
      autoRefresh: typeof parsed.autoRefresh === "boolean" ? parsed.autoRefresh : DEFAULT_NODE_LIST_SETTINGS.autoRefresh,
      defaultRoutableOnly:
        typeof parsed.defaultRoutableOnly === "boolean"
          ? parsed.defaultRoutableOnly
          : DEFAULT_NODE_LIST_SETTINGS.defaultRoutableOnly,
    };
  } catch {
    return DEFAULT_NODE_LIST_SETTINGS;
  }
}

function saveNodeListSettings(settings: NodeListSettings) {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(NODE_LIST_SETTINGS_KEY, JSON.stringify(settings));
}

function loadStoredExportToken(): string {
  if (typeof window === "undefined") {
    return "";
  }
  return window.localStorage.getItem(EXPORT_TOKEN_STORAGE_KEY) ?? "";
}

function persistExportToken(value: string) {
  if (typeof window === "undefined") {
    return;
  }
  const trimmed = value.trim();
  if (trimmed) {
    window.localStorage.setItem(EXPORT_TOKEN_STORAGE_KEY, trimmed);
  } else {
    window.localStorage.removeItem(EXPORT_TOKEN_STORAGE_KEY);
  }
}

function applyExportBooleanFilter(filters: NodeListFilters, key: "enabled" | "has_outbound", value: ExportBooleanMode) {
  if (value === "any") {
    delete filters[key];
    return;
  }
  filters[key] = value === "true";
}

function statusFromQuery(params: URLSearchParams): NodeStatusFilter {
  const explicitStatus = parseStatusParam(params.get("status"));
  if (explicitStatus) {
    return explicitStatus;
  }

  const hasOutbound = parseBoolParam(params.get("has_outbound"));
  const circuitOpen = parseBoolParam(params.get("circuit_open"));
  const enabled = parseBoolParam(params.get("enabled"));

  if (enabled === false) {
    return "disabled";
  }

  if (hasOutbound === false) {
    return "error";
  }
  if (hasOutbound === true && circuitOpen === true) {
    return "circuit_open";
  }
  if (hasOutbound === true && circuitOpen === false) {
    return "healthy";
  }

  return "all";
}

function trimQueryValue(params: URLSearchParams, key: string): string {
  return params.get(key)?.trim() ?? "";
}

function parseProtocolList(raw: string): string[] {
  if (!raw) return [];
  return raw
    .split(",")
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean);
}

/** Convert an RFC3339 timestamp to a value usable in <input type="datetime-local">. */
function toDatetimeLocalValue(rfc3339: string): string {
  if (!rfc3339) return "";
  try {
    const d = new Date(rfc3339);
    if (Number.isNaN(d.getTime())) return "";
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  } catch {
    return "";
  }
}

/** Convert a datetime-local input value back to an RFC3339 string. Empty returns "". */
function fromDatetimeLocalValue(localValue: string): string {
  if (!localValue) return "";
  try {
    const d = new Date(localValue);
    if (Number.isNaN(d.getTime())) return "";
    return d.toISOString();
  } catch {
    return "";
  }
}

function draftFromQuery(search: string, settings: NodeListSettings = DEFAULT_NODE_LIST_SETTINGS): NodeFilterDraft {
  const params = new URLSearchParams(search);
  const tagKeyword = trimQueryValue(params, "tag_keyword") || trimQueryValue(params, "tag");
  const routable = parseRoutableParam(params.get("routable")) ?? (settings.defaultRoutableOnly ? "routable" : "all");

  const qualityCloudflare = params.get("quality_cloudflare_challenged");
  const qualityCloudflareVal = qualityCloudflare === null ? "any" : (parseBoolParam(qualityCloudflare) === undefined ? "any" : String(parseBoolParam(qualityCloudflare)));

  // Parse repeated quality_cloudflare_status keys (OR within values).
  const cfStatusValues = params.getAll("quality_cloudflare_status");
  const cfStatusDraft = normalizeCFStatusSet(cfStatusValues);

  return {
    platform_id: trimQueryValue(params, "platform_id"),
    subscription_id: trimQueryValue(params, "subscription_id"),
    tag_keyword: tagKeyword,
    region: trimQueryValue(params, "region").toUpperCase(),
    egress_ip: trimQueryValue(params, "egress_ip"),
    status: statusFromQuery(params),
    routable,
    protocolInclude: parseProtocolList(params.get("protocol") ?? ""),
    protocolExclude: parseProtocolList(params.get("exclude_protocol") ?? ""),
    quality_grade: trimQueryValue(params, "quality_grade").toUpperCase(),
    quality_min_score: trimQueryValue(params, "quality_min_score"),
    quality_cloudflare_challenged: qualityCloudflareVal,
    quality_cloudflare_status: cfStatusDraft,
    quality_checked_since: trimQueryValue(params, "quality_checked_since"),
    quality_profile: trimQueryValue(params, "quality_profile"),
  };
}



function draftToActiveFilters(draft: NodeFilterDraft): NodeListFilters {
  let circuit_open: boolean | undefined = undefined;
  let has_outbound: boolean | undefined = undefined;
  let enabled: boolean | undefined = undefined;

  switch (draft.status) {
    case "healthy":
      enabled = true;
      has_outbound = true;
      circuit_open = false;
      break;
    case "circuit_open":
      enabled = true;
      has_outbound = true;
      circuit_open = true;
      break;
    case "error":
      enabled = true;
      has_outbound = false;
      break;
    case "disabled":
      enabled = false;
      break;
    case "all":
    default:
      break;
  }

  const filters: NodeListFilters = {
    platform_id: draft.platform_id,
    subscription_id: draft.subscription_id,
    tag_keyword: draft.tag_keyword,
    region: draft.region,
    egress_ip: draft.egress_ip,
    enabled,
    circuit_open,
    has_outbound,
    routable: draft.routable === "all" ? undefined : draft.routable === "routable",
    protocol: draft.protocolInclude.length > 0 ? draft.protocolInclude.join(",") : undefined,
    exclude_protocol: draft.protocolExclude.length > 0 ? draft.protocolExclude.join(",") : undefined,
  };

  // Quality filters
  const grade = draft.quality_grade.trim().toUpperCase();
  if (grade && (QUALITY_GRADE_OPTIONS as string[]).includes(grade)) {
    filters.quality_grade = grade as QualityGradeFilter;
  }

  const minScoreRaw = draft.quality_min_score.trim();
  if (minScoreRaw) {
    const minScore = Number(minScoreRaw);
    if (Number.isFinite(minScore)) {
      filters.quality_min_score = Math.max(0, Math.min(100, Math.round(minScore)));
    }
  }

  if (draft.quality_cloudflare_challenged === "true" || draft.quality_cloudflare_challenged === "false") {
    filters.quality_cloudflare_challenged = draft.quality_cloudflare_challenged === "true";
  }

  if (draft.quality_cloudflare_status.length > 0) {
    filters.quality_cloudflare_status = normalizeCFStatusSet(draft.quality_cloudflare_status);
  }

  const checkedSince = draft.quality_checked_since.trim();
  if (checkedSince) {
    filters.quality_checked_since = checkedSince;
  }

  const profile = draft.quality_profile.trim().toLowerCase();
  if (profile && (QUALITY_PROFILE_OPTIONS as string[]).includes(profile)) {
    filters.quality_profile = profile;
  }

  return filters;
}

/** Detect contradiction between legacy challenged bool and detailed status filter. */
function cfContradiction(draft: NodeFilterDraft): boolean {
  return isCFContradiction(draft.quality_cloudflare_status, draft.quality_cloudflare_challenged);
}

function firstTag(node: { display_tag?: string; tags: { tag: string }[] }): string {
  if (node.display_tag && node.display_tag.trim()) {
    return node.display_tag;
  }
  if (!node.tags.length) {
    return "-";
  }
  return node.tags[0].tag;
}

function hasReferenceLatency(node: NodeSummary): node is NodeSummary & { reference_latency_ms: number } {
  return typeof node.reference_latency_ms === "number";
}

function isPendingTestNode(node: NodeSummary): boolean {
  return Boolean(node.circuit_open_since) && node.failure_count === 0;
}

function getNodeDisplayStatus(node: NodeSummary): NodeDisplayStatus {
  if (!node.enabled) {
    return "disabled";
  }
  if (!node.has_outbound) {
    return "error";
  }
  if (isPendingTestNode(node)) {
    return "pending_test";
  }
  if (node.circuit_open_since) {
    return "circuit_open";
  }
  return "healthy";
}

function referenceLatencyColor(latencyMs: number): string {
  if (!Number.isFinite(latencyMs)) {
    return "var(--text-secondary)";
  }
  if (latencyMs <= 400) {
    return "var(--success)";
  }
  if (latencyMs <= 1000) {
    return "var(--warning)";
  }
  return "var(--danger)";
}

function displayableReferenceLatencyMs(node: NodeSummary): number | null {
  if (getNodeDisplayStatus(node) !== "healthy") {
    return null;
  }
  if (!hasReferenceLatency(node)) {
    return null;
  }
  return node.reference_latency_ms;
}


function formatLatency(value: number): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  return `${value.toFixed(0)} ms`;
}

function sortIndicator(active: boolean, order: SortOrder): string {
  if (!active) {
    return "↕";
  }
  return order === "asc" ? "▲" : "▼";
}

function regionToFlag(region: string | undefined): string {
  if (!region || region.length !== 2) {
    return region || "-";
  }
  const code = region.toUpperCase();
  const flag = String.fromCodePoint(...[...code].map((c) => c.charCodeAt(0) + 127397));
  const name = getRegionName(code);
  return name ? `${flag} ${code} (${name})` : `${flag} ${code}`;
}

function formatNodeProtocol(protocol?: string): string {
  const value = protocol?.trim();
  if (!value) {
    return "-";
  }
  switch (value.toLowerCase()) {
    case "ss":
    case "shadowsocks":
      return "Shadowsocks";
    case "vmess":
    case "vmess1":
      return "VMess";
    case "trojan":
      return "Trojan";
    case "vless":
      return "VLess";
    case "hysteria2":
    case "hy2":
      return "Hysteria2";
    case "http":
      return "HTTP";
    case "socks":
    case "socks5":
      return "SOCKS";
    default:
      return value;
  }
}

function formatQualityLatency(value: number | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return "-";
  }
  return `${value.toFixed(0)} ms`;
}

function qualityScoreColor(score: number): string {
  if (!Number.isFinite(score)) {
    return "var(--text-secondary)";
  }
  if (score >= 80) return "var(--success)";
  if (score >= 60) return "var(--warning)";
  return "var(--danger)";
}

const QUALITY_GRADE_OPTIONS: QualityGradeFilter[] = ["A", "B", "C", "D", "F"];
const QUALITY_PROFILE_OPTIONS: string[] = ["generic", "openai", "grok", "gemini", "claude"];

export function NodesPage() {
  const { t } = useI18n();
  const location = useLocation();
  const [listSettings, setListSettings] = useState<NodeListSettings>(() => loadNodeListSettings());
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [exportToken, setExportToken] = useState(loadStoredExportToken);
  const [exportSettings, setExportSettings] = useState<NodeExportSettings>(DEFAULT_EXPORT_SETTINGS);
  const [draftFilters, setDraftFilters] = useState<NodeFilterDraft>(() => draftFromQuery(location.search, loadNodeListSettings()));
  const [activeFilters, setActiveFilters] = useState<NodeListFilters>(() =>
    draftToActiveFilters(draftFromQuery(location.search, loadNodeListSettings()))
  );
  const [sortBy, setSortBy] = useState<NodeSortBy>("tag");
  const [sortOrder, setSortOrder] = useState<SortOrder>("asc");
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState<number>(() => loadNodeListSettings().pageSize);
  const [selectedNodeHash, setSelectedNodeHash] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [pendingEgressHashes, setPendingEgressHashes] = useState<Set<string>>(() => new Set());
  const [pendingLatencyHashes, setPendingLatencyHashes] = useState<Set<string>>(() => new Set());
  const [pendingQualityHashes, setPendingQualityHashes] = useState<Set<string>>(() => new Set());
  const { toasts, showToast, dismissToast } = useToast();
  const pendingEgressHashesRef = useRef<Set<string>>(new Set());
  const pendingLatencyHashesRef = useRef<Set<string>>(new Set());
  const pendingQualityHashesRef = useRef<Set<string>>(new Set());

  const queryClient = useQueryClient();

  const allRegions = useMemo(() => getAllRegions(), []);

  const platformsQuery = useQuery({
    queryKey: ["platforms", "all"],
    queryFn: async () => {
      const data = await listPlatforms({
        limit: 100000,
        offset: 0,
      });
      return data.items;
    },
    staleTime: 60_000,
  });
  const platforms = platformsQuery.data ?? EMPTY_PLATFORMS;

  const subscriptionsQuery = useQuery({
    queryKey: ["subscriptions", "all"],
    queryFn: async () => {
      const data = await listSubscriptions({
        limit: 100000,
        offset: 0,
      });
      return data.items;
    },
    staleTime: 60_000,
  });
  const subscriptions = subscriptionsQuery.data ?? [];

  // Enabled rule profiles for the Clash export selector. Only enabled profiles
  // are routable for export; the selector is shown only when format === "clash".
  const ruleProfilesQuery = useQuery({
    queryKey: ["rule-profiles", "enabled"],
    queryFn: () => listRuleProfiles(true),
    staleTime: 30_000,
  });
  const enabledRuleProfiles = ruleProfilesQuery.data ?? [];
  const selectedRuleProfile = exportSettings.ruleProfileID
    ? enabledRuleProfiles.find((profile) => profile.id === exportSettings.ruleProfileID) ?? null
    : null;
  const selectedRuleProfileUnavailable = Boolean(exportSettings.ruleProfileID)
    && (
      ruleProfilesQuery.data === undefined
      || ruleProfilesQuery.isError
      || ruleProfilesQuery.isFetching
      || !selectedRuleProfile
    );

  const nodesQuery = useQuery({
    queryKey: ["nodes", activeFilters, sortBy, sortOrder, page, pageSize],
    queryFn: () =>
      listNodes({
        ...activeFilters,
        sort_by: sortBy,
        sort_order: sortOrder,
        limit: pageSize,
        offset: page * pageSize,
      }),
    refetchInterval: listSettings.autoRefresh ? 30_000 : false,
    placeholderData: (prev) => prev,
  });

  const nodesPage = nodesQuery.data ?? {
    items: [],
    total: 0,
    limit: pageSize,
    offset: page * pageSize,
    unique_egress_ips: 0,
    unique_healthy_egress_ips: 0,
  };
  const nodes = nodesPage.items;

  const totalPages = Math.max(1, Math.ceil(nodesPage.total / pageSize));

  const selectedNode = useMemo(() => {
    if (!selectedNodeHash) {
      return null;
    }
    return nodes.find((item) => item.node_hash === selectedNodeHash) ?? null;
  }, [nodes, selectedNodeHash]);

  const selectedHash = selectedNode?.node_hash || "";

  const nodeDetailQuery = useQuery({
    queryKey: ["node", selectedHash],
    queryFn: () => getNode(selectedHash),
    enabled: Boolean(selectedHash) && drawerOpen,
    refetchInterval: 30_000,
  });

  const detailNode = nodeDetailQuery.data ?? selectedNode;
  const drawerVisible = drawerOpen && Boolean(detailNode);

  useEffect(() => {
    if (!drawerVisible) {
      return;
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }
      setDrawerOpen(false);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [drawerVisible]);

  const openDrawer = (hash: string) => {
    setSelectedNodeHash(hash);
    setDrawerOpen(true);
  };

  const refreshNodes = async () => {
    await queryClient.invalidateQueries({ queryKey: ["nodes"] });
    if (selectedHash) {
      await queryClient.invalidateQueries({ queryKey: ["node", selectedHash] });
    }
  };

  const probeEgressMutation = useMutation({
    mutationFn: async (hash: string) => probeEgress(hash),
    onSuccess: async (result) => {
      await refreshNodes();
      showToast(
        "success",
        t("出口探测完成：出口 IP={{ip}}，区域={{region}}，延迟={{latency}}", {
          ip: result.egress_ip || "-",
          region: result.region || "-",
          latency: formatLatency(result.latency_ewma_ms),
        })
      );
    },
    onError: async (error) => {
      await refreshNodes();
      showToast("error", formatApiErrorMessage(error, t));
    },
  });

  const probeLatencyMutation = useMutation({
    mutationFn: async (hash: string) => probeLatency(hash),
    onSuccess: async (result) => {
      await refreshNodes();
      showToast("success", t("延迟探测完成：延迟={{latency}}", { latency: formatLatency(result.latency_ewma_ms) }));
    },
    onError: async (error) => {
      await refreshNodes();
      showToast("error", formatApiErrorMessage(error, t));
    },
  });

  const probeQualityMutation = useMutation({
    mutationFn: async (hash: string) => probeQuality(hash),
    onSuccess: async (result) => {
      await refreshNodes();
      showToast(
        "success",
        t("质量探测完成：等级={{grade}}，分数={{score}}", {
          grade: result.Grade,
          score: Math.round(result.Score),
        }),
      );
    },
    onError: async (error) => {
      await refreshNodes();
      showToast("error", formatApiErrorMessage(error, t));
    },
  });

  const markProbePending = (hash: string, action: ProbeAction): boolean => {
    if (action === "egress") {
      if (pendingEgressHashesRef.current.has(hash)) {
        return false;
      }
      const next = new Set(pendingEgressHashesRef.current);
      next.add(hash);
      pendingEgressHashesRef.current = next;
      setPendingEgressHashes(next);
      return true;
    }

    if (action === "latency") {
      if (pendingLatencyHashesRef.current.has(hash)) {
        return false;
      }
      const next = new Set(pendingLatencyHashesRef.current);
      next.add(hash);
      pendingLatencyHashesRef.current = next;
      setPendingLatencyHashes(next);
      return true;
    }

    if (pendingQualityHashesRef.current.has(hash)) {
      return false;
    }
    const next = new Set(pendingQualityHashesRef.current);
    next.add(hash);
    pendingQualityHashesRef.current = next;
    setPendingQualityHashes(next);
    return true;
  };

  const clearProbePending = (hash: string, action: ProbeAction) => {
    if (action === "egress") {
      if (!pendingEgressHashesRef.current.has(hash)) {
        return;
      }
      const next = new Set(pendingEgressHashesRef.current);
      next.delete(hash);
      pendingEgressHashesRef.current = next;
      setPendingEgressHashes(next);
      return;
    }

    if (action === "latency") {
      if (!pendingLatencyHashesRef.current.has(hash)) {
        return;
      }
      const next = new Set(pendingLatencyHashesRef.current);
      next.delete(hash);
      pendingLatencyHashesRef.current = next;
      setPendingLatencyHashes(next);
      return;
    }

    if (!pendingQualityHashesRef.current.has(hash)) {
      return;
    }
    const next = new Set(pendingQualityHashesRef.current);
    next.delete(hash);
    pendingQualityHashesRef.current = next;
    setPendingQualityHashes(next);
  };

  const isProbePending = (hash: string, action: ProbeAction): boolean => {
    if (action === "egress") return pendingEgressHashes.has(hash);
    if (action === "latency") return pendingLatencyHashes.has(hash);
    return pendingQualityHashes.has(hash);
  };

  const runProbeEgress = async (hash: string) => {
    if (!markProbePending(hash, "egress")) {
      return;
    }
    try {
      await probeEgressMutation.mutateAsync(hash);
    } catch {
      // Mutation callbacks already surface the failure to the user.
    } finally {
      clearProbePending(hash, "egress");
    }
  };

  const runProbeLatency = async (hash: string) => {
    if (!markProbePending(hash, "latency")) {
      return;
    }
    try {
      await probeLatencyMutation.mutateAsync(hash);
    } catch {
      // Mutation callbacks already surface the failure to the user.
    } finally {
      clearProbePending(hash, "latency");
    }
  };

  const runProbeQuality = async (hash: string) => {
    if (!markProbePending(hash, "quality")) {
      return;
    }
    try {
      await probeQualityMutation.mutateAsync(hash);
    } catch {
      // Mutation callbacks already surface the failure to the user.
    } finally {
      clearProbePending(hash, "quality");
    }
  };

  const handleFilterChange = (key: Exclude<keyof NodeFilterDraft, "protocolInclude" | "protocolExclude" | "quality_cloudflare_status">, value: string) => {
    setDraftFilters((prev) => {
      const next = { ...prev, [key]: value } as NodeFilterDraft;
      setActiveFilters(draftToActiveFilters(next));
      setSelectedNodeHash("");
      setDrawerOpen(false);
      setPage(0);
      return next;
    });
  };

  const toggleCFStatusFilter = (token: CloudflareStatusToken) => {
    setDraftFilters((prev) => {
      const current = prev.quality_cloudflare_status;
      const nextStatus = current.includes(token)
        ? current.filter((t) => t !== token)
        : [...current, token];
      const next = { ...prev, quality_cloudflare_status: nextStatus };
      setActiveFilters(draftToActiveFilters(next));
      setSelectedNodeHash("");
      setDrawerOpen(false);
      setPage(0);
      return next;
    });
  };

  const toggleProtocolFilter = (key: "protocolInclude" | "protocolExclude", protocol: string) => {
    setDraftFilters((prev) => {
      const current = prev[key];
      const next = current.includes(protocol)
        ? current.filter((p) => p !== protocol)
        : [...current, protocol];
      const updated = { ...prev, [key]: next } as NodeFilterDraft;
      setActiveFilters(draftToActiveFilters(updated));
      setSelectedNodeHash("");
      setDrawerOpen(false);
      setPage(0);
      return updated;
    });
  };

  const resetFilters = () => {
    const next = {
      ...defaultFilterDraft,
      routable: listSettings.defaultRoutableOnly ? ("routable" as const) : ("all" as const),
    };
    setDraftFilters(next);
    setActiveFilters(draftToActiveFilters(next));
    setSelectedNodeHash("");
    setDrawerOpen(false);
    setPage(0);
  };

  const changeSort = (target: NodeSortBy) => {
    if (sortBy === target) {
      setSortOrder((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortBy(target);
      setSortOrder("asc");
    }
    setPage(0);
  };

  const changePageSize = (next: number) => {
    const nextSettings = { ...listSettings, pageSize: next };
    setListSettings(nextSettings);
    saveNodeListSettings(nextSettings);
    setPageSize(next);
    setPage(0);
  };

  const updateListSettings = (patch: Partial<NodeListSettings>) => {
    const next = { ...listSettings, ...patch };
    setListSettings(next);
    saveNodeListSettings(next);
    if (patch.pageSize !== undefined) {
      setPageSize(next.pageSize);
      setPage(0);
    }
    if (patch.defaultRoutableOnly !== undefined) {
      const routable: NodeRoutableFilter = next.defaultRoutableOnly ? "routable" : "all";
      setDraftFilters((prev) => {
        const updated = { ...prev, routable };
        setActiveFilters(draftToActiveFilters(updated));
        setSelectedNodeHash("");
        setDrawerOpen(false);
        setPage(0);
        return updated;
      });
    }
  };

  const handleExportTokenChange = (value: string) => {
    setExportToken(value);
    persistExportToken(value);
  };

  const updateExportSettings = (patch: Partial<NodeExportSettings>) => {
    setExportSettings((prev) => ({ ...prev, ...patch }));
  };

  const toggleExportProtocol = (value: string, field: "protocol" | "excludeProtocol") => {
    // If toggling include while __all is active, start from empty instead of ["__all"]
    let raw = exportSettings[field];
    if (field === "protocol" && raw === "__all") {
      raw = "";
    }
    const current = raw ? raw.split(",").filter(Boolean) : [];
    const next = current.includes(value) ? current.filter((v) => v !== value) : [...current, value];
    const patch: Partial<NodeExportSettings> = { [field]: next.length > 0 ? next.join(",") : "" };
    // If toggling exclude while __all is active, clear __all so the exclude is meaningful
    if (field === "excludeProtocol" && exportSettings.protocol === "__all") {
      patch.protocol = "";
    }
    updateExportSettings(patch);
  };

  const exportProtocolsActive = (field: "protocol" | "excludeProtocol"): string[] =>
    exportSettings[field] ? exportSettings[field].split(",").filter(Boolean) : [];

  const exportFilters = (): NodeListFilters => {
    const filters: NodeListFilters = { ...activeFilters };
    const tagKeyword = exportSettings.tagKeyword.trim();
    if (tagKeyword) {
      filters.tag_keyword = tagKeyword;
    }

    switch (exportSettings.routable) {
      case "all":
        delete filters.routable;
        break;
      case "routable":
        filters.routable = true;
        break;
      case "unroutable":
        filters.routable = false;
        break;
      case "current":
      default:
        break;
    }

    applyExportBooleanFilter(filters, "enabled", exportSettings.enabled);
    applyExportBooleanFilter(filters, "has_outbound", exportSettings.hasOutbound);
    if (exportSettings.protocol === "__all") {
      delete filters.protocol;
      delete filters.exclude_protocol;
    } else {
      if (exportSettings.protocol) {
        filters.protocol = exportSettings.protocol;
      }
      if (exportSettings.excludeProtocol) {
        filters.exclude_protocol = exportSettings.excludeProtocol;
      }
    }
    return filters;
  };

  const exportDownloadLabel = () => {
    switch (exportSettings.format) {
      case "base64":
        return t("下载 Base64");
      case "uri":
        return t("下载 URI");
      case "sing-box":
        return t("下载 sing-box JSON");
      case "clash":
      default:
        return t("下载 Clash YAML");
    }
  };

  const exportFileMeta = () => {
    switch (exportSettings.format) {
      case "base64":
        return { name: "resin-node-pool-base64.txt", type: "text/plain; charset=utf-8" };
      case "uri":
        return { name: "resin-node-pool-uri.txt", type: "text/plain; charset=utf-8" };
      case "sing-box":
        return { name: "resin-node-pool-sing-box.json", type: "application/json; charset=utf-8" };
      case "clash":
      default:
        return { name: "resin-node-pool-clash.yaml", type: "text/yaml; charset=utf-8" };
    }
  };

  const buildAbsoluteExportURL = () => {
    const trimmedToken = exportToken.trim();
    if (!trimmedToken) {
      return "";
    }
    const relative = buildNodePoolExportURL(
      {
        ...exportFilters(),
        limit: 100000,
        offset: 0,
      },
      trimmedToken,
      exportSettings.format,
      exportSettings.format === "clash" ? exportSettings.ruleProfileID : undefined,
    );
    if (typeof window === "undefined") {
      return relative;
    }
    return new URL(relative, window.location.origin).toString();
  };

  const copyExportURL = async () => {
    if (exportSettings.format === "clash" && selectedRuleProfileUnavailable) {
      showToast("error", t("所选 Rule Profile 已禁用、删除或无法加载，请重新选择。导出不会自动降级为仅 proxies。"));
      return;
    }
    const url = buildAbsoluteExportURL();
    if (!url) {
      showToast("error", t("请先填写导出令牌"));
      return;
    }
    try {
      await navigator.clipboard.writeText(url);
      showToast("success", t("导出 URL 已复制"));
    } catch {
      showToast("error", t("复制失败，请手动复制"));
    }
  };

  const downloadExportFile = async () => {
    const trimmedToken = exportToken.trim();
    if (!trimmedToken) {
      showToast("error", t("请先填写导出令牌"));
      return;
    }
    if (exportSettings.format === "clash" && selectedRuleProfileUnavailable) {
      showToast("error", t("所选 Rule Profile 已禁用、删除或无法加载，请重新选择。导出不会自动降级为仅 proxies。"));
      return;
    }
    try {
      const text = await exportNodePoolText(
        { ...exportFilters(), limit: 100000, offset: 0 },
        trimmedToken,
        exportSettings.format,
        exportSettings.format === "clash" ? exportSettings.ruleProfileID : undefined,
      );
      const meta = exportFileMeta();
      const blob = new Blob([text], { type: meta.type });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = meta.name;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
      showToast("success", t("导出文件已下载"));
    } catch (error) {
      showToast("error", formatApiErrorMessage(error, t));
    }
  };

  const col = createColumnHelper<NodeSummary>();

  const nodeColumns = [
    col.accessor((row) => firstTag(row), {
      id: "tag",
      header: () => (
        <button type="button" className="table-sort-btn" onClick={() => changeSort("tag")}>
          {t("节点名")}
          <span>{sortIndicator(sortBy === "tag", sortOrder)}</span>
        </button>
      ),
      cell: (info) => (
        <div className="nodes-tag-cell">
          <span title={info.getValue() as string}>{info.getValue() as string}</span>
        </div>
      ),
    }),
    col.accessor("region", {
      header: () => (
        <button type="button" className="table-sort-btn" onClick={() => changeSort("region")}>
          {t("区域")}
          <span>{sortIndicator(sortBy === "region", sortOrder)}</span>
        </button>
      ),
      cell: (info) => {
        const val = regionToFlag(info.getValue());
        return (
          <div style={{ maxWidth: "100px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={val}>
            {val}
          </div>
        );
      },
    }),
    col.accessor("egress_ip", {
      header: t("出口 IP"),
      cell: (info) => {
        const val = info.getValue() || "-";
        return (
          <div style={{ maxWidth: "100px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={val}>
            {val}
          </div>
        );
      },
    }),
    col.accessor("protocol", {
      header: t("协议"),
      cell: (info) => {
        const label = formatNodeProtocol(info.getValue());
        return label === "-" ? "-" : <Badge variant="info">{label}</Badge>;
      },
    }),
    col.display({
      id: "reference_latency_ms",
      header: t("参考延迟"),
      cell: (info) => {
        const node = info.row.original;
        const latencyMs = displayableReferenceLatencyMs(node);
        if (latencyMs === null) {
          return "-";
        }
        return (
          <span style={{ color: referenceLatencyColor(latencyMs), fontWeight: 600 }}>
            {formatLatency(latencyMs)}
          </span>
        );
      },
    }),
    col.display({
      id: "quality",
      header: () => (
        <button type="button" className="table-sort-btn" onClick={() => changeSort("quality_score")}>
          {t("质量")}
          <span>{sortIndicator(sortBy === "quality_score", sortOrder)}</span>
        </button>
      ),
      cell: (info) => {
        const node = info.row.original;
        const q = node.quality;
        if (!q) {
          return <span style={{ color: "var(--text-muted)" }}>-</span>;
        }
        const cfStatus = q.quality_cloudflare_status ?? normalizeCFStatus("");
        return (
          <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "nowrap" }}>
            <Badge variant={gradeBadgeVariant(q.quality_grade)}>{q.quality_grade}</Badge>
            <span style={{ color: qualityScoreColor(q.quality_score), fontWeight: 600, fontSize: "0.8rem" }}>
              {Math.round(q.quality_score)}
            </span>
            <CloudflareStatusBadge status={cfStatus} compact />
          </div>
        );
      },
    }),
    col.accessor("last_latency_probe_attempt", {
      header: t("上次探测"),
      cell: (info) => formatRelativeTime(info.getValue()),
    }),
    col.accessor("failure_count", {
      header: () => (
        <button type="button" className="table-sort-btn" onClick={() => changeSort("failure_count")}>
          {t("连续失败")}
          <span>{sortIndicator(sortBy === "failure_count", sortOrder)}</span>
        </button>
      ),
      cell: (info) => {
        const node = info.row.original;
        return !node.has_outbound ? "-" : node.failure_count;
      },
    }),
    col.display({
      id: "status",
      header: t("状态"),
      cell: (info) => {
        const node = info.row.original;
        const status = getNodeDisplayStatus(node);
        if (status === "disabled") return <Badge variant="neutral">{t("禁用")}</Badge>;
        if (status === "error") return <Badge variant="danger">{t("错误")}</Badge>;
        if (status === "pending_test") return <Badge variant="muted">{t("待测")}</Badge>;
        if (status === "circuit_open") return <Badge variant="warning">{t("熔断")}</Badge>;
        return <Badge variant="success">{t("健康")}</Badge>;
      },
    }),
    col.accessor("created_at", {
      header: () => (
        <button type="button" className="table-sort-btn" onClick={() => changeSort("created_at")}>
          {t("创建时间")}
          <span>{sortIndicator(sortBy === "created_at", sortOrder)}</span>
        </button>
      ),
      cell: (info) => {
        const val = formatDateTime(info.getValue());
        if (val === "-") return val;
        const parts = val.split(" ");
        if (parts.length >= 2) {
          return (
            <div className="logs-cell-stack">
              <span>{parts[0]}</span>
              <small>{parts.slice(1).join(" ")}</small>
            </div>
          );
        }
        return val;
      },
    }),
    col.display({
      id: "actions",
      header: t("操作"),
      cell: (info) => {
        const node = info.row.original;
        return (
          <div className="subscriptions-row-actions" onClick={(event) => event.stopPropagation()}>
            <Button
              size="sm"
              variant="ghost"
              title={t("触发出口探测")}
              onClick={() => void runProbeEgress(node.node_hash)}
              disabled={isProbePending(node.node_hash, "egress")}
            >
              <Globe size={14} />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              title={t("触发延迟探测")}
              onClick={() => void runProbeLatency(node.node_hash)}
              disabled={isProbePending(node.node_hash, "latency")}
            >
              <Zap size={14} />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              title={t("触发质量探测")}
              onClick={() => void runProbeQuality(node.node_hash)}
              disabled={isProbePending(node.node_hash, "quality")}
            >
              <Gauge size={14} />
            </Button>
          </div>
        );
      },
    }),
  ];

  return (
    <section className="nodes-page">
      <header className="module-header">
        <div>
          <h2>{t("节点池")}</h2>
          <p className="module-description">{t("快速定位异常节点并进行探测处理。")}</p>
        </div>
      </header>

      <ToastContainer toasts={toasts} onDismiss={dismissToast} />

      <Card className="filter-card platform-list-card platform-directory-card">
        <div className="list-card-header">
          <div>
            <h3>{t("节点列表")}</h3>
            <p>{t("共 {{total}} 个节点，{{healthy}} 个健康 IP", { total: nodesPage.total, healthy: nodesPage.unique_healthy_egress_ips })}</p>
          </div>

          <div
            className="nodes-inline-filters"
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: "0.5rem",
              alignItems: "flex-end",
            }}
          >
            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-tag-keyword" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("节点名")}
              </label>
              <Input
                id="node-tag-keyword"
                value={draftFilters.tag_keyword}
                onChange={(event) => handleFilterChange("tag_keyword", event.target.value)}
                placeholder={t("模糊搜索")}
                style={NODE_FILTER_CONTROL_STYLE}
              />
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-platform-id" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("被此平台路由")}
              </label>
              <Select
                id="node-platform-id"
                value={draftFilters.platform_id}
                onChange={(event) => handleFilterChange("platform_id", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="">{t("无限制")}</option>
                {platforms.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </Select>
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-subscription-id" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("来自此订阅")}
              </label>
              <Select
                id="node-subscription-id"
                value={draftFilters.subscription_id}
                onChange={(event) => handleFilterChange("subscription_id", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="">{t("全部")}</option>
                {subscriptions.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </Select>
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-region" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("区域")}
              </label>
              <Select
                id="node-region"
                value={draftFilters.region}
                onChange={(event) => handleFilterChange("region", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="">{t("全部")}</option>
                {allRegions.map((r) => (
                  <option key={r.code} value={r.code}>
                    {r.name}
                  </option>
                ))}
              </Select>
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-egress-ip" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("出口 IP")}
              </label>
              <Input
                id="node-egress-ip"
                value={draftFilters.egress_ip}
                onChange={(event) => handleFilterChange("egress_ip", event.target.value)}
                placeholder="IP / CIDR"
                style={NODE_FILTER_CONTROL_STYLE}
              />
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-status" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("状态")}
              </label>
              <Select
                id="node-status"
                value={draftFilters.status}
                onChange={(event) => handleFilterChange("status", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="all">{t("全部")}</option>
                <option value="healthy">{t("健康")}</option>
                <option value="circuit_open">{t("熔断 / 待测")}</option>
                <option value="error">{t("错误")}</option>
                <option value="disabled">{t("禁用")}</option>
              </Select>
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-routable" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("可路由")}
              </label>
              <Select
                id="node-routable"
                value={draftFilters.routable}
                onChange={(event) => handleFilterChange("routable", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="all">{t("全部")}</option>
                <option value="routable">{t("可路由")}</option>
                <option value="unroutable">{t("不可路由")}</option>
              </Select>
            </div>

            <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 220px" }}>
              <label style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>
                {t("协议")}
              </label>
              <div style={PROTOCOL_PILL_ROW_STYLE}>
                {PROTOCOL_OPTIONS.map(({ value, label }) => {
                  const selected = draftFilters.protocolInclude.includes(value);
                  return (
                    <button
                      key={value}
                      type="button"
                      onClick={() => toggleProtocolFilter("protocolInclude", value)}
                      style={selected ? PROTOCOL_PILL_SELECTED_STYLE : PROTOCOL_PILL_STYLE}
                      title={label}
                      aria-pressed={selected}
                    >
                      {label}
                    </button>
                  );
                })}
              </div>
            </div>
            <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 220px" }}>
              <label style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>
                {t("排除协议")}
              </label>
              <div style={PROTOCOL_PILL_ROW_STYLE}>
                {PROTOCOL_OPTIONS.map(({ value, label }) => {
                  const selected = draftFilters.protocolExclude.includes(value);
                  return (
                    <button
                      key={value}
                      type="button"
                      onClick={() => toggleProtocolFilter("protocolExclude", value)}
                      style={selected ? PROTOCOL_PILL_SELECTED_STYLE : PROTOCOL_PILL_STYLE}
                      title={label}
                      aria-pressed={selected}
                    >
                      {label}
                    </button>
                  );
                })}
              </div>
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-quality-grade" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("质量等级")}
              </label>
              <Select
                id="node-quality-grade"
                value={draftFilters.quality_grade}
                onChange={(event) => handleFilterChange("quality_grade", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="">{t("不限")}</option>
                {QUALITY_GRADE_OPTIONS.map((g) => (
                  <option key={g} value={g}>{g}</option>
                ))}
              </Select>
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-quality-min-score" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("最低质量分")}
              </label>
              <Input
                id="node-quality-min-score"
                type="number"
                min={0}
                max={100}
                value={draftFilters.quality_min_score}
                onChange={(event) => handleFilterChange("quality_min_score", event.target.value)}
                placeholder="0-100"
                style={NODE_FILTER_CONTROL_STYLE}
              />
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-quality-cf" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("Cloudflare 拦截")}
              </label>
              <Select
                id="node-quality-cf"
                value={draftFilters.quality_cloudflare_challenged}
                onChange={(event) => handleFilterChange("quality_cloudflare_challenged", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="any">{t("不限")}</option>
                <option value="true">{t("被拦截")}</option>
                <option value="false">{t("未拦截")}</option>
              </Select>
            </div>

            <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 220px" }}>
              <label style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>
                {t("Cloudflare 详细状态")}
              </label>
              <div style={{ display: "flex", flexWrap: "wrap", gap: "4px", padding: "4px 6px", minHeight: "32px", border: "1px solid var(--border)", borderRadius: "6px", background: "var(--surface-sunken, rgba(0,0,0,0.02))" }}>
                {CF_STATUS_TOKENS.map((token) => {
                  const selected = draftFilters.quality_cloudflare_status.includes(token);
                  return (
                    <button
                      key={token}
                      type="button"
                      onClick={() => toggleCFStatusFilter(token)}
                      style={{
                        padding: "2px 6px",
                        borderRadius: "4px",
                        fontSize: "0.7rem",
                        fontWeight: 600,
                        border: selected ? "1px solid var(--primary)" : "1px solid var(--border)",
                        background: selected ? "var(--primary)" : "transparent",
                        color: selected ? "#fff" : "var(--text-secondary)",
                        cursor: "pointer",
                      }}
                      title={t(cfStatusLabel(token))}
                      aria-pressed={selected}
                    >
                      {t(cfStatusLabel(token))}
                    </button>
                  );
                })}
              </div>
              {cfContradiction(draftFilters) ? (
                <small style={{ color: "var(--danger)", fontSize: 11, marginTop: 2, display: "block" }}>
                  {t("与“Cloudflare 拦截”筛选矛盾，结果为空。")}
                </small>
              ) : (
                <small style={{ color: "var(--text-muted)", fontSize: 11, marginTop: 2, display: "block" }}>
                  {t("多选 OR，与上方“Cloudflare 拦截”取交集。")}
                </small>
              )}
            </div>

            <div style={NODE_FILTER_ITEM_STYLE}>
              <label htmlFor="node-quality-profile" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("质量 Profile")}
              </label>
              <Select
                id="node-quality-profile"
                value={draftFilters.quality_profile}
                onChange={(event) => handleFilterChange("quality_profile", event.target.value)}
                style={NODE_FILTER_CONTROL_STYLE}
              >
                <option value="">{t("不限")}</option>
                {QUALITY_PROFILE_OPTIONS.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </Select>
            </div>

            <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 180px" }}>
              <label htmlFor="node-quality-checked-since" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                {t("检测时间起")}
              </label>
              <Input
                id="node-quality-checked-since"
                type="datetime-local"
                value={draftFilters.quality_checked_since ? toDatetimeLocalValue(draftFilters.quality_checked_since) : ""}
                onChange={(event) => handleFilterChange("quality_checked_since", fromDatetimeLocalValue(event.target.value))}
                style={NODE_FILTER_CONTROL_STYLE}
              />
            </div>

            <div style={{ display: "flex", gap: "0.5rem", marginBottom: "0.125rem", marginLeft: "auto" }}>
              <Button size="sm" variant="secondary" onClick={() => setSettingsOpen((open) => !open)} style={{ minHeight: "32px", height: "32px", padding: "0 0.75rem", display: "flex", alignItems: "center", gap: "0.25rem" }}>
                <Settings size={16} />
                {t("列表设置")}
              </Button>
              <Button size="sm" variant="secondary" onClick={refreshNodes} disabled={nodesQuery.isFetching} style={{ minHeight: "32px", height: "32px", padding: "0 0.75rem", display: "flex", alignItems: "center", gap: "0.25rem" }}>
                <RefreshCw size={16} className={nodesQuery.isFetching ? "spin" : undefined} />
                {t("刷新")}
              </Button>
              <Button size="sm" variant="secondary" onClick={resetFilters} style={{ minHeight: "32px", height: "32px", padding: "0 0.75rem", display: "flex", alignItems: "center", gap: "0.25rem" }}>
                <Eraser size={16} />
                {t("重置")}
              </Button>
            </div>
          </div>
          {settingsOpen ? (
            <div
              className="nodes-list-settings"
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: "1rem",
                alignItems: "center",
                paddingTop: "0.75rem",
                borderTop: "1px solid var(--border-subtle)",
              }}
            >
              <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "0 1 180px" }}>
                <label htmlFor="node-setting-page-size" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                  {t("默认页面大小")}
                </label>
                <Select
                  id="node-setting-page-size"
                  value={String(listSettings.pageSize)}
                  onChange={(event) => updateListSettings({ pageSize: Number(event.target.value) })}
                  style={NODE_FILTER_CONTROL_STYLE}
                >
                  {PAGE_SIZE_OPTIONS.map((size) => (
                    <option key={size} value={size}>
                      {size}
                    </option>
                  ))}
                </Select>
              </div>
              <label style={{ display: "flex", alignItems: "center", gap: "0.5rem", color: "var(--text-secondary)", fontSize: "0.875rem" }}>
                <Switch checked={listSettings.autoRefresh} onChange={(event) => updateListSettings({ autoRefresh: event.target.checked })} />
                {t("自动刷新")}
              </label>
              <label style={{ display: "flex", alignItems: "center", gap: "0.5rem", color: "var(--text-secondary)", fontSize: "0.875rem" }}>
                <Switch checked={listSettings.defaultRoutableOnly} onChange={(event) => updateListSettings({ defaultRoutableOnly: event.target.checked })} />
                {t("默认只看可路由节点")}
              </label>
              <div style={{ display: "flex", flexDirection: "column", gap: "0.35rem", flex: "1 1 360px", minWidth: 260 }}>
                <label htmlFor="node-export-token" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                  {t("导出令牌")}
                </label>
                <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
                  <Input
                    id="node-export-token"
                    type="password"
                    value={exportToken}
                    onChange={(event) => handleExportTokenChange(event.target.value)}
                    placeholder={t("从系统配置创建后粘贴到这里")}
                    style={{ flex: "1 1 220px", minHeight: 32, height: 32 }}
                  />
                  <Button size="sm" variant="secondary" onClick={() => void copyExportURL()}>
                    <Copy size={14} />
                    {t("复制导出 URL")}
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => void downloadExportFile()}>
                    <Download size={14} />
                    {exportDownloadLabel()}
                  </Button>
                </div>
                <div style={{ display: "flex", flexWrap: "wrap", gap: "0.5rem" }}>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 120px" }}>
                    <label htmlFor="node-export-format" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                      {t("导出格式")}
                    </label>
                    <Select
                      id="node-export-format"
                      value={exportSettings.format}
                      onChange={(event) => updateExportSettings({ format: event.target.value as ExportFormat })}
                      style={NODE_FILTER_CONTROL_STYLE}
                    >
                      <option value="clash">Clash YAML</option>
                      <option value="base64">Base64 URI</option>
                      <option value="uri">URI</option>
                      <option value="sing-box">sing-box JSON</option>
                    </Select>
                  </div>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 220px" }}>
                    <label htmlFor="node-export-rule-profile" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                      {t("Rule Profile / 规则配置")}
                    </label>
                    <Select
                      id="node-export-rule-profile"
                      value={exportSettings.ruleProfileID}
                      disabled={exportSettings.format !== "clash" || ruleProfilesQuery.isFetching}
                      onChange={(event) => updateExportSettings({ ruleProfileID: event.target.value })}
                      style={NODE_FILTER_CONTROL_STYLE}
                      aria-describedby="node-export-rule-profile-help"
                    >
                      <option value="">{t("不使用（仅 proxies）")}</option>
                      {exportSettings.ruleProfileID && !selectedRuleProfile ? (
                        <option value={exportSettings.ruleProfileID} disabled>
                          {t("当前选择不可用，请重新选择")}
                        </option>
                      ) : null}
                      {enabledRuleProfiles.map((profile) => (
                        <option key={profile.id} value={profile.id}>{profile.name}</option>
                      ))}
                    </Select>
                    <small
                      id="node-export-rule-profile-help"
                      style={{
                        color: selectedRuleProfileUnavailable ? "var(--danger)" : "var(--text-muted)",
                        fontSize: 11,
                        lineHeight: 1.35,
                      }}
                    >
                      {exportSettings.format !== "clash"
                        ? t("Rule Profile 仅适用于 Clash YAML，当前格式不会发送 rule_profile_id。")
                        : exportSettings.ruleProfileID && ruleProfilesQuery.isFetching
                          ? t("正在确认所选 Profile，复制和下载暂时不可用。")
                        : selectedRuleProfileUnavailable
                          ? t("所选 Profile 已禁用、删除或列表加载失败。请重新选择；复制和下载已阻止，不会静默降级。")
                          : ruleProfilesQuery.isError
                            ? t("Rule Profile 列表加载失败，不影响不使用 Profile 的导出。")
                            : selectedRuleProfile
                              ? t("将输出完整 Mihomo 配置。删除或禁用此 Profile 后，已有 URL 会返回 404，不会回退。")
                              : t("不选时保持旧 Clash proxies-only 输出。")}
                    </small>
                  </div>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 150px" }}>
                    <label htmlFor="node-export-routable" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                      {t("可路由")}
                    </label>
                    <Select
                      id="node-export-routable"
                      value={exportSettings.routable}
                      onChange={(event) => updateExportSettings({ routable: event.target.value as ExportRoutableMode })}
                      style={NODE_FILTER_CONTROL_STYLE}
                    >
                      <option value="current">{t("跟随列表")}</option>
                      <option value="all">{t("全部")}</option>
                      <option value="routable">{t("仅可路由")}</option>
                      <option value="unroutable">{t("仅不可路由")}</option>
                    </Select>
                  </div>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 130px" }}>
                    <label htmlFor="node-export-enabled" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                      {t("启用状态")}
                    </label>
                    <Select
                      id="node-export-enabled"
                      value={exportSettings.enabled}
                      onChange={(event) => updateExportSettings({ enabled: event.target.value as ExportBooleanMode })}
                      style={NODE_FILTER_CONTROL_STYLE}
                    >
                      <option value="any">{t("不限")}</option>
                      <option value="true">{t("仅启用")}</option>
                      <option value="false">{t("仅禁用")}</option>
                    </Select>
                  </div>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 130px" }}>
                    <label htmlFor="node-export-outbound" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                      {t("Outbound")}
                    </label>
                    <Select
                      id="node-export-outbound"
                      value={exportSettings.hasOutbound}
                      onChange={(event) => updateExportSettings({ hasOutbound: event.target.value as ExportBooleanMode })}
                      style={NODE_FILTER_CONTROL_STYLE}
                    >
                      <option value="any">{t("不限")}</option>
                      <option value="true">{t("仅有配置")}</option>
                      <option value="false">{t("仅缺配置")}</option>
                    </Select>
                  </div>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 280px" }}>
                    <label style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>
                      {t("协议")}
                    </label>
                    <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                      <div style={PROTOCOL_PILL_ROW_STYLE}>
                        {PROTOCOL_OPTIONS.map(({ value, label }) => {
                          const selected = exportProtocolsActive("protocol").includes(value);
                          return (
                            <button
                              key={value}
                              type="button"
                              onClick={() => toggleExportProtocol(value, "protocol")}
                              style={selected ? PROTOCOL_PILL_SELECTED_STYLE : PROTOCOL_PILL_STYLE}
                              title={label}
                              aria-pressed={selected}
                            >
                              {label}
                            </button>
                          );
                        })}
                      </div>
                      <div style={{ display: "flex", gap: "6px", alignItems: "center", fontSize: "0.7rem", color: "var(--text-muted)" }}>
                        <label style={{ display: "flex", alignItems: "center", gap: "4px", cursor: "pointer" }}>
                          <input
                            type="checkbox"
                            checked={exportSettings.protocol === "__all"}
                            onChange={(e) => updateExportSettings({ protocol: e.target.checked ? "__all" : "", excludeProtocol: e.target.checked ? "" : "" })}
                          />
                          {t("全部协议")}
                        </label>
                        {!exportSettings.protocol && !exportSettings.excludeProtocol ? (
                          <span>{t("跟随列表")}</span>
                        ) : null}
                        {exportSettings.protocol && exportSettings.protocol !== "__all" ? (
                          <span>{t("覆盖列表筛选")}</span>
                        ) : null}
                      </div>
                    </div>
                  </div>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 280px" }}>
                    <label style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>
                      {t("排除协议")}
                    </label>
                    <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                      <div style={PROTOCOL_PILL_ROW_STYLE}>
                        {PROTOCOL_OPTIONS.map(({ value, label }) => {
                          const selected = exportProtocolsActive("excludeProtocol").includes(value);
                          return (
                            <button
                              key={value}
                              type="button"
                              onClick={() => toggleExportProtocol(value, "excludeProtocol")}
                              style={selected ? PROTOCOL_PILL_SELECTED_STYLE : PROTOCOL_PILL_STYLE}
                              title={label}
                              aria-pressed={selected}
                            >
                              {label}
                            </button>
                          );
                        })}
                      </div>
                      <div style={{ display: "flex", gap: "6px", alignItems: "center", fontSize: "0.7rem", color: "var(--text-muted)" }}>
                        {!exportSettings.excludeProtocol ? (
                          <span>{t("跟随列表")}</span>
                        ) : (
                          <span>{t("覆盖列表筛选")}</span>
                        )}
                      </div>
                    </div>
                  </div>
                  <div style={{ ...NODE_FILTER_ITEM_STYLE, flex: "1 1 180px" }}>
                    <label htmlFor="node-export-tag" style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>
                      {t("导出标签关键词")}
                    </label>
                    <Input
                      id="node-export-tag"
                      value={exportSettings.tagKeyword}
                      onChange={(event) => updateExportSettings({ tagKeyword: event.target.value })}
                      placeholder={t("留空跟随列表")}
                      style={{ minHeight: 32, height: 32 }}
                    />
                  </div>
                </div>
                <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>
                  {t(
                    "导出默认跟随当前列表筛选；上方选项可覆盖可路由、启用状态、Outbound、协议、排除协议和标签条件。选择 Rule Profile 后输出完整 Mihomo 配置；不选保持旧 Clash proxies-only。转换器建议使用 URL query token。",
                  )}
                </span>
              </div>
            </div>
          ) : null}
        </div>
      </Card>

      <Card className="nodes-table-card platform-cards-container subscriptions-table-card">
        {nodesQuery.isLoading ? <p className="muted">{t("正在加载节点数据...")}</p> : null}

        {nodesQuery.isError ? (
          <div className="callout callout-error">
            <AlertTriangle size={14} />
            <span>{formatApiErrorMessage(nodesQuery.error, t)}</span>
          </div>
        ) : null}

        {!nodesQuery.isLoading && !nodes.length ? (
          <div className="empty-box">
            <Sparkles size={16} />
            <p>{t("没有匹配的节点")}</p>
          </div>
        ) : null}

        {nodes.length ? (
          <DataTable
            data={nodes}
            columns={nodeColumns}
            onRowClick={(node) => openDrawer(node.node_hash)}
            getRowId={(node) => node.node_hash}
          />
        ) : null}

        <OffsetPagination
          page={page}
          totalPages={totalPages}
          totalItems={nodesPage.total}
          pageSize={pageSize}
          pageSizeOptions={PAGE_SIZE_OPTIONS}
          onPageChange={setPage}
          onPageSizeChange={changePageSize}
        />
      </Card>

      {drawerVisible && detailNode ? (
        <div
          className="drawer-overlay"
          role="dialog"
          aria-modal="true"
          aria-label={t("节点详情 {{name}}", { name: firstTag(detailNode) })}
          onClick={() => setDrawerOpen(false)}
        >
          <Card className="drawer-panel" onClick={(event) => event.stopPropagation()}>
            <div className="drawer-header">
              <div>
                <h3>{firstTag(detailNode)}</h3>
                <p>{detailNode.node_hash}</p>
              </div>
              <div className="drawer-header-actions">
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t("关闭详情面板")}
                  onClick={() => setDrawerOpen(false)}
                >
                  <X size={16} />
                </Button>
              </div>
            </div>

            <div className="platform-drawer-layout">
              <section className="platform-drawer-section">
                <div className="platform-drawer-section-head">
                  <h4>{t("节点状态")}</h4>
                  <p>{t("节点的网络出口、探测状态以及失败历史。")}</p>
                </div>

                <div className="stats-grid">
                  <div>
                    <span>{t("创建时间")}</span>
                    <p>{formatDateTime(detailNode.created_at)}</p>
                  </div>
                  <div>
                    <span>{t("连续失败")}</span>
                    <p>{!detailNode.has_outbound ? "-" : detailNode.failure_count}</p>
                  </div>
                  <div>
                    <span>{t("状态")}</span>
                    <div>
                      {(() => {
                        const status = getNodeDisplayStatus(detailNode);
                        return (
                          <div style={{ display: "flex", alignItems: "baseline", gap: "4px", flexWrap: "wrap" }}>
                            {status === "error" ? (
                              <Badge variant="danger">{t("错误")}</Badge>
                            ) : status === "disabled" ? (
                              <Badge variant="neutral">{t("禁用")}</Badge>
                            ) : status === "pending_test" ? (
                              <Badge variant="muted">{t("待测")}</Badge>
                            ) : status === "circuit_open" ? (
                              <Badge variant="warning">{t("熔断")}</Badge>
                            ) : (
                              <Badge variant="success">{t("健康")}</Badge>
                            )}
                            {(status === "circuit_open" || status === "pending_test") && detailNode.circuit_open_since ? (
                              <span
                                style={{
                                  fontSize: "11px",
                                  color: "var(--text-muted)",
                                  fontWeight: "normal",
                                }}
                              >
                                ({formatRelativeTime(detailNode.circuit_open_since)})
                              </span>
                            ) : null}
                          </div>
                        );
                      })()}
                    </div>
                  </div>
                  <div>
                    <span>{t("协议")}</span>
                    <p>{formatNodeProtocol(detailNode.protocol)}</p>
                  </div>
                  <div>
                    <span>{t("出口 / 区域")}</span>
                    <p>
                      {detailNode.egress_ip || "-"} / {regionToFlag(detailNode.region)}
                    </p>
                  </div>
                  <div>
                    <span>{t("参考延迟")}</span>
                    {(() => {
                      const latencyMs = displayableReferenceLatencyMs(detailNode);
                      if (latencyMs === null) {
                        return <p>-</p>;
                      }
                      return <p style={{ color: referenceLatencyColor(latencyMs) }}>{formatLatency(latencyMs)}</p>;
                    })()}
                  </div>
                  <div>
                    <span>{t("上次探测")}</span>
                    <p>{formatDateTime(detailNode.last_latency_probe_attempt || "")}</p>
                  </div>
                </div>

                {detailNode.last_error ? (
                  <div className="callout callout-error">{t("最近错误：{{message}}", { message: detailNode.last_error })}</div>
                ) : null}
              </section>

              <section className="platform-drawer-section">
                <div className="platform-drawer-section-head">
                  <h4>{t("质量检测")}</h4>
                  <p>{t("后台质量检测对目标服务的评级，与节点健康/熔断相互独立。")}</p>
                </div>
                {(() => {
                  const q: NodeQuality | undefined = detailNode.quality;
                  if (!q) {
                    return <p className="muted">{t("尚未记录质量检测结果。")}</p>;
                  }
                  return (
                    <>
                      <div className="stats-grid">
                        <div>
                          <span>{t("质量等级")}</span>
                          <p>
                            <Badge variant={gradeBadgeVariant(q.quality_grade)}>{q.quality_grade}</Badge>
                          </p>
                        </div>
                        <div>
                          <span>{t("质量分")}</span>
                          <p style={{ color: qualityScoreColor(q.quality_score) }}>{Math.round(q.quality_score)}</p>
                        </div>
                        <div>
                          <span>{t("质量 Profile")}</span>
                          <p>{q.quality_profile || "-"}</p>
                        </div>
                        <div>
                          <span>{t("服务可达")}</span>
                          <p>
                            {q.quality_service_reachable ? (
                              <span style={{ display: "inline-flex", alignItems: "center", gap: 4, color: "var(--success)" }}>
                                <CheckCircle2 size={14} /> {t("可达")}
                              </span>
                            ) : (
                              <span style={{ display: "inline-flex", alignItems: "center", gap: 4, color: "var(--danger)" }}>
                                <XCircle size={14} /> {t("不可达")}
                              </span>
                            )}
                          </p>
                        </div>
                        <div>
                          <span>{t("API 可达")}</span>
                          <p>
                            {q.quality_api_reachable ? (
                              <span style={{ display: "inline-flex", alignItems: "center", gap: 4, color: "var(--success)" }}>
                                <CheckCircle2 size={14} /> {t("可达")}
                              </span>
                            ) : (
                              <span style={{ display: "inline-flex", alignItems: "center", gap: 4, color: "var(--danger)" }}>
                                <XCircle size={14} /> {t("不可达")}
                              </span>
                            )}
                          </p>
                        </div>
                        <div>
                          <span>{t("Cloudflare 状态")}</span>
                          <p>
                            {(() => {
                              const cfStatus = q.quality_cloudflare_status ?? normalizeCFStatus("");
                              return (
                                <span style={{ display: "inline-flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                                  <CloudflareStatusBadge status={cfStatus} />
                                  <span style={{ fontSize: 11, color: "var(--text-muted)" }}>
                                    {t(cfStatusLabel(cfStatus))}
                                  </span>
                                </span>
                              );
                            })()}
                          </p>
                        </div>
                        <div>
                          <span>{t("平均延迟")}</span>
                          <p>{formatQualityLatency(q.quality_avg_latency_ms)}</p>
                        </div>
                        <div>
                          <span>{t("不稳定")}</span>
                          <p>{q.quality_unstable ? <Badge variant="warning">{t("是")}</Badge> : <Badge variant="muted">{t("否")}</Badge>}</p>
                        </div>
                        <div>
                          <span>{t("上次质量检测")}</span>
                          <p>{q.quality_last_checked ? formatDateTime(q.quality_last_checked) : "-"}</p>
                        </div>
                      </div>
                      {q.quality_last_error ? (
                        <div className="callout callout-error">{t("质量检测错误：{{message}}", { message: q.quality_last_error })}</div>
                      ) : null}

                      <div className="platform-drawer-section-head" style={{ marginTop: 12 }}>
                        <h5 style={{ margin: 0, fontSize: "0.85rem" }}>{t("评分解释")}</h5>
                      </div>
                      <ScoreBreakdownExplanation
                        breakdown={q.quality_score_breakdown}
                        policyVersion={q.quality_scoring_policy_version}
                      />
                    </>
                  );
                })()}
              </section>

              <section className="platform-drawer-section">
                <div className="platform-drawer-section-head">
                  <h4>{t("节点别名")}</h4>
                </div>
                {!detailNode.tags.length ? (
                  <p className="muted">{t("无节点名信息")}</p>
                ) : (
                  <div className="tag-list">
                    {detailNode.tags.map((tag) => (
                      <div key={`${tag.subscription_id}:${tag.tag}`} className="tag-item">
                        <p>{tag.tag}</p>
                        <span>{tag.subscription_name}</span>
                        <code>{tag.subscription_id}</code>
                      </div>
                    ))}
                  </div>
                )}
              </section>

              <section className="platform-drawer-section platform-ops-section">
                <div className="platform-drawer-section-head">
                  <h4>{t("运维操作")}</h4>
                </div>
                <div className="platform-ops-list">
                  <div className="platform-op-item">
                    <div className="platform-op-copy">
                      <h5>{t("出口探测")}</h5>
                      <p className="platform-op-hint">{t("检查节点当前出口 IP。")}</p>
                    </div>
                    <Button
                      variant="secondary"
                      onClick={() => void runProbeEgress(detailNode.node_hash)}
                      disabled={isProbePending(detailNode.node_hash, "egress")}
                    >
                      {isProbePending(detailNode.node_hash, "egress") ? t("探测中...") : t("触发出口探测")}
                    </Button>
                  </div>
                  <div className="platform-op-item">
                    <div className="platform-op-copy">
                      <h5>{t("延迟探测")}</h5>
                      <p className="platform-op-hint">{t("检测节点网络延迟。")}</p>
                    </div>
                    <Button
                      variant="secondary"
                      onClick={() => void runProbeLatency(detailNode.node_hash)}
                      disabled={isProbePending(detailNode.node_hash, "latency")}
                    >
                      {isProbePending(detailNode.node_hash, "latency") ? t("探测中...") : t("触发延迟探测")}
                    </Button>
                  </div>
                  <div className="platform-op-item">
                    <div className="platform-op-copy">
                      <h5>{t("质量探测")}</h5>
                      <p className="platform-op-hint">{t("使用当前已保存质量配置检测该节点")}</p>
                    </div>
                    <Button
                      variant="secondary"
                      onClick={() => void runProbeQuality(detailNode.node_hash)}
                      disabled={isProbePending(detailNode.node_hash, "quality")}
                    >
                      {isProbePending(detailNode.node_hash, "quality") ? t("探测中...") : t("触发质量探测")}
                    </Button>
                  </div>
                </div>
              </section>
            </div>
          </Card>
        </div>
      ) : null}
    </section>
  );
}
