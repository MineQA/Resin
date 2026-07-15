import { useMutation, useQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { CheckCircle2, Download, List, Play, RefreshCw, XCircle } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { DataTable } from "../../components/ui/DataTable";
import { Input } from "../../components/ui/Input";
import { Select } from "../../components/ui/Select";
import { Textarea } from "../../components/ui/Textarea";
import { ToastContainer } from "../../components/ui/Toast";
import { useToast } from "../../hooks/useToast";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { cfStatusLabel, normalizeCFStatus, type CloudflareStatusToken } from "../../lib/cloudflareStatus";
import { CloudflareStatusBadge, ScoreBreakdownExplanation } from "../../components/ScoreBreakdown";
import {
  createProxyCheckTask,
  fetchProxySources,
  getProxyCheckTask,
  importProxies,
  listProxySources,
} from "./api";
import type {
  ProxyCandidate,
  ProxyCheckOptions,
  ProxyCheckPreset,
  ProxyCheckResultItem,
  ProxyCheckTask,
  ProxyScore,
  SourceProxyCandidate,
} from "./types";

// ---------------------------------------------------------------------------
// Preset -> ProxyCheckOptions mapping
// ---------------------------------------------------------------------------

const PRESET_OPTIONS: Record<ProxyCheckPreset, ProxyCheckOptions> = {
  quick: {
    service_reachability: true,
    rounds: 1,
  },
  standard: {
    service_reachability: true,
    cloudflare_detection: true,
    rounds: 1,
  },
  deep: {
    service_reachability: true,
    api_reachability: true,
    cloudflare_detection: true,
    multi_round: true,
    rounds: 3,
  },
};

const PRESETS: ProxyCheckPreset[] = ["quick", "standard", "deep"];
const PROFILES = ["generic", "openai", "grok", "gemini", "claude"];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a unique candidate ID. */
function candidateId(proxy: string, source: string): string {
  return `${source}::${proxy}`;
}

/** Parse a textarea block into trimmed non-empty lines. */
function parseProxyLines(text: string): string[] {
  return text
    .split(/[\n\r]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

/** Grade color map. */
function gradeBadgeVariant(grade: string) {
  switch (grade) {
    case "A":
      return "success" as const;
    case "B":
      return "info" as const;
    case "C":
      return "warning" as const;
    case "D":
    case "F":
      return "danger" as const;
    default:
      return "neutral" as const;
  }
}

/** Default selection: non-error items with a score that has a non-F grade. */
function isDefaultSelected(item: ProxyCheckResultItem): boolean {
  if (item.error) return false;
  if (!item.score) return false;
  if (item.score.Grade === "F") return false;
  if (item.score.Grade === "D" && item.score.CloudflareChallenged) return false;
  return true;
}

// ---------------------------------------------------------------------------
// Page component
// ---------------------------------------------------------------------------

const POLL_INTERVAL_MS = 1500;

export function ProxyCheckPage() {
  const { t } = useI18n();
  const { toasts, showToast, dismissToast } = useToast();

  // ── Source state ──────────────────────────────────────────────────────
  const [selectedSource, setSelectedSource] = useState("");
  const [fetchLimit, setFetchLimit] = useState(50);
  const [fetchedProxies, setFetchedProxies] = useState<SourceProxyCandidate[]>([]);

  // ── Manual input state ────────────────────────────────────────────────
  const [manualText, setManualText] = useState("");

  // ── Combined candidates ───────────────────────────────────────────────
  // Candidates shown in the check list
  const [candidates, setCandidates] = useState<ProxyCandidate[]>([]);

  // ── Check settings ────────────────────────────────────────────────────
  const [profile, setProfile] = useState("generic");
  const [preset, setPreset] = useState<ProxyCheckPreset>("standard");

  // ── Task state ────────────────────────────────────────────────────────
  const [taskId, setTaskId] = useState<string | null>(null);
  const [task, setTask] = useState<ProxyCheckTask | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // ── Results selection ─────────────────────────────────────────────────
  const [selectedResults, setSelectedResults] = useState<string[]>([]); // proxy strings

  // ── Import state ──────────────────────────────────────────────────────
  const [importedCount, setImportedCount] = useState<number | null>(null);

  // ── Queries ──────────────────────────────────────────────────────────
  const sourcesQuery = useQuery({
    queryKey: ["proxy-sources"],
    queryFn: listProxySources,
    staleTime: 30_000,
  });
  const sources = sourcesQuery.data ?? [];
  const selectedSourceValue = selectedSource || sources[0]?.id || "";

  // ── Mutations ─────────────────────────────────────────────────────────
  const fetchMutation = useMutation({
    mutationFn: fetchProxySources,
    onSuccess: (data) => {
      const fetched = data.results.flatMap((result) => result.candidates ?? []);
      setFetchedProxies(fetched);
      const newCands: ProxyCandidate[] = fetched.map((fp) => ({
        id: candidateId(fp.proxy, fp.source_id),
        proxy: fp.proxy,
        source: fp.source_id,
        selected: true,
      }));
      setCandidates((prev) => mergeCandidates(prev, newCands));
      const returnedCount = data.results.reduce((sum, result) => sum + result.returned_count, 0);
      const failedSources = data.results.filter((result) => result.error).length;
      showToast(
        failedSources > 0 ? "error" : "success",
        `${t("已获取")} ${returnedCount} ${t("个候选代理")}${failedSources > 0 ? `，${failedSources} ${t("个源失败")}` : ""}`,
      );
    },
    onError: (err) => {
      showToast("error", formatApiErrorMessage(err, t));
    },
  });

  const startCheckMutation = useMutation({
    mutationFn: (req: { proxies: string[]; profile: string; options: ProxyCheckOptions }) =>
      createProxyCheckTask(req.proxies, req.profile, req.options),
    onSuccess: (data) => {
      setTaskId(data.id);
      setTask(data);
      setSelectedResults([]);
      setImportedCount(null);
      showToast("success", `${t("检测任务已创建")}: ${data.id}`);
    },
    onError: (err) => {
      showToast("error", formatApiErrorMessage(err, t));
    },
  });

  const importMutation = useMutation({
    mutationFn: (proxies: string[]) => importProxies(proxies),
    onSuccess: (data) => {
      setImportedCount(data.imported_count);
      showToast(
        "success",
        `${t("已导入")} ${data.imported_count} ${t("个代理，跳过")} ${data.skipped_count} ${t("个重复")}`,
      );
    },
    onError: (err) => {
      showToast("error", formatApiErrorMessage(err, t));
    },
  });

  // ── Task polling ──────────────────────────────────────────────────────
  useEffect(() => {
    if (!taskId) return;

    const poll = async () => {
      try {
        const updated = await getProxyCheckTask(taskId);
        setTask(updated);
        if (isTerminal(updated.status)) {
          if (pollRef.current) {
            clearInterval(pollRef.current);
            pollRef.current = null;
          }
          // Auto-select good results
          if (updated.result?.results) {
            const selected = updated.result.results
              .filter(isDefaultSelected)
              .map((r) => r.proxy ?? r.hash ?? "");
            setSelectedResults(selected);
          }
        }
      } catch {
        // Ignore transient errors; poll will retry
      }
    };

    poll(); // immediate first poll
    pollRef.current = setInterval(poll, POLL_INTERVAL_MS);

    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, [taskId]);

  // ── Handlers ──────────────────────────────────────────────────────────

  const handleFetch = () => {
    if (!selectedSourceValue) return;
    fetchMutation.mutate({ source: selectedSourceValue, limit: fetchLimit || undefined });
  };

  const handleAddManual = () => {
    const lines = parseProxyLines(manualText);
    if (lines.length === 0) return;
    const newCands: ProxyCandidate[] = lines.map((proxy) => ({
      id: candidateId(proxy, "manual"),
      proxy,
      source: "manual",
      selected: true,
    }));
    setCandidates((prev) => mergeCandidates(prev, newCands));
    setManualText("");
  };

  const toggleCandidate = (id: string) => {
    setCandidates((prev) =>
      prev.map((c) => (c.id === id ? { ...c, selected: !c.selected } : c)),
    );
  };

  const toggleAllCandidates = (selected: boolean) => {
    setCandidates((prev) => prev.map((c) => ({ ...c, selected })));
  };

  const handleStartCheck = () => {
    const selectedProxies = candidates.filter((c) => c.selected).map((c) => c.proxy);
    if (selectedProxies.length === 0) {
      showToast("error", t("请至少选择一个代理进行检测"));
      return;
    }
    const options = PRESET_OPTIONS[preset];
    startCheckMutation.mutate({ proxies: selectedProxies, profile, options });
  };

  const toggleResult = (proxyOrHash: string) => {
    setSelectedResults((prev) =>
      prev.includes(proxyOrHash) ? prev.filter((p) => p !== proxyOrHash) : [...prev, proxyOrHash],
    );
  };

  const handleImport = () => {
    if (selectedResults.length === 0) {
      showToast("error", t("请选择要导入的检测结果"));
      return;
    }
    const confirmed = window.confirm(
      `${t("确认导入所选的")} ${selectedResults.length} ${t("个代理到节点池？\n\n导入代表您确认这些代理已经过检测/审核。")}`,
    );
    if (!confirmed) return;
    importMutation.mutate(selectedResults);
  };

  // ── Results table columns ────────────────────────────────────────────

  const resultColumns = useMemo(() => {
    const ch = createColumnHelper<ProxyCheckResultItem>();
    return [
      ch.display({
        id: "select",
        header: () => {
          const all = task?.result?.results ?? [];
          const allSelected = all.length > 0 && all.every((r) => selectedResults.includes(r.proxy ?? r.hash ?? ""));
          return (
            <input
              type="checkbox"
              checked={allSelected}
              onChange={() => {
                if (allSelected) {
                  setSelectedResults([]);
                } else {
                  setSelectedResults(
                    all
                      .filter(isDefaultSelected)
                      .map((r) => r.proxy ?? r.hash ?? ""),
                  );
                }
              }}
            />
          );
        },
        cell: ({ row }) => {
          const key = row.original.proxy ?? row.original.hash ?? "";
          return (
            <input
              type="checkbox"
              checked={selectedResults.includes(key)}
              onChange={() => toggleResult(key)}
            />
          );
        },
      }),
      ch.accessor("proxy", {
        header: t("代理") as string,
        cell: (info) => {
          const v = info.getValue();
          return v ? <code style={{ fontSize: "0.8rem" }}>{v}</code> : "-";
        },
      }),
      ch.accessor("hash", {
        header: "Hash",
        cell: (info) => {
          const v = info.getValue();
          if (!v) return "-";
          return <code style={{ fontSize: "0.75rem" }}>{v.substring(0, 12)}…</code>;
        },
      }),
      ch.accessor("score", {
        id: "grade",
        header: t("等级") as string,
        cell: (info) => {
          const s = info.getValue();
          if (!s) return "-";
          return <Badge variant={gradeBadgeVariant(s.Grade)}>{s.Grade}</Badge>;
        },
      }),
      ch.accessor("score", {
        id: "score_num",
        header: t("分数") as string,
        cell: (info) => {
          const s = info.getValue();
          if (!s) return "-";
          return <span>{Math.round(s.Score)}</span>;
        },
      }),
      ch.accessor("score", {
        id: "cf_status",
        header: "CF",
        cell: (info) => {
          const s = info.getValue() as ProxyScore | null | undefined;
          if (!s) return "-";
          const cfStatus = normalizeCFStatus(s.cloudflare_status) as CloudflareStatusToken;
          return <CloudflareStatusBadge status={cfStatus} compact />;
        },
      }),
      ch.accessor("score", {
        id: "service",
        header: "Svc",
        cell: (info) => {
          const s = info.getValue();
          if (!s) return "-";
          return s.ServiceReachable ? (
            <CheckCircle2 size={14} style={{ color: "var(--color-success)" }} />
          ) : (
            <XCircle size={14} style={{ color: "var(--color-danger)" }} />
          );
        },
      }),
      ch.accessor("score", {
        id: "api",
        header: "API",
        cell: (info) => {
          const s = info.getValue();
          if (!s) return "-";
          return s.APIReachable ? (
            <CheckCircle2 size={14} style={{ color: "var(--color-success)" }} />
          ) : (
            <XCircle size={14} style={{ color: "var(--color-danger)" }} />
          );
        },
      }),
      ch.accessor("score", {
        id: "latency",
        header: t("延迟") as string,
        cell: (info) => {
          const s = info.getValue();
          if (!s) return "-";
          return <span>{s.AvgLatencyMs.toFixed(0)}ms</span>;
        },
      }),
      ch.display({
        id: "breakdown",
        header: t("评分解释") as string,
        cell: ({ row }) => {
          const s = row.original.score;
          if (!s || (!s.scoring_breakdown && !s.cloudflare_status)) return null;
          return (
            <details className="proxy-check-breakdown-details">
              <summary className="proxy-check-breakdown-summary" style={{ cursor: "pointer", fontSize: "0.75rem", color: "var(--primary)" }}>
                {t("展开")}
              </summary>
              <div style={{ marginTop: 8, minWidth: 320 }}>
                {s.cloudflare_status ? (
                  <div style={{ marginBottom: 8, display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                    <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("Cloudflare 状态")}</span>
                    <CloudflareStatusBadge status={normalizeCFStatus(s.cloudflare_status) as CloudflareStatusToken} />
                    <span style={{ fontSize: 11, color: "var(--text-muted)" }}>
                      {t(cfStatusLabel(normalizeCFStatus(s.cloudflare_status) as CloudflareStatusToken))}
                    </span>
                  </div>
                ) : null}
                <ScoreBreakdownExplanation
                  breakdown={s.scoring_breakdown}
                  policyVersion={s.scoring_breakdown?.version}
                />
              </div>
            </details>
          );
        },
      }),
      ch.accessor("error", {
        header: t("错误") as string,
        cell: (info) => {
          const v = info.getValue();
          if (!v) return null;
          return <span style={{ color: "var(--color-danger)", fontSize: "0.8rem" }}>{v}</span>;
        },
      }),
    ];
  }, [task, selectedResults, t]);

  // ── Render ───────────────────────────────────────────────────────────

  const isChecking = startCheckMutation.isPending || (taskId !== null && task !== null && !isTerminal(task.status));
  const selectedCandidates = candidates.filter((c) => c.selected);
  const canImport =
    task?.status === "completed" || task?.status === "completed_with_errors";

  return (
    <div className="page">
      <div className="page-header">
        <h1 className="page-title">{t("代理检测")}</h1>
        <p className="page-desc">{t("拉取免费代理源，手动输入代理，检测后导入节点池")}</p>
      </div>

      <div className="page-body" style={{ display: "flex", flexDirection: "column", gap: "1rem" }}>
        {/* ── Section: Source Fetch ───────────────────────────────────── */}
        <Card>
          <div className="card-header">
            <h2 className="card-title">
              <Download size={16} /> {t("免费代理源")}
            </h2>
          </div>
          <div className="card-content">
            <div className="form-row" style={{ display: "flex", gap: "0.5rem", alignItems: "flex-end", flexWrap: "wrap" }}>
              <div style={{ flex: "1 1 200px" }}>
                <label className="form-label">{t("代理源")}</label>
                <Select
                  value={selectedSourceValue}
                  onChange={(e) => setSelectedSource(e.target.value)}
                >
                  {sources.length === 0 && <option value="">{t("加载中...")}</option>}
                  {sources.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              </div>
              <div style={{ flex: "0 0 100px" }}>
                <label className="form-label">{t("数量限制")}</label>
                <Input
                  type="number"
                  min={1}
                  max={50000}
                  value={fetchLimit}
                  onChange={(e) => setFetchLimit(Number(e.target.value) || 50)}
                />
              </div>
              <Button
                variant="secondary"
                onClick={handleFetch}
                disabled={fetchMutation.isPending || !selectedSourceValue}
              >
                {fetchMutation.isPending ? (
                  <>{t("拉取中...")}</>
                ) : (
                  <>
                    <RefreshCw size={14} /> {t("拉取")}
                  </>
                )}
              </Button>
            </div>

            {fetchedProxies.length > 0 && (
              <div style={{ marginTop: "0.75rem", fontSize: "0.85rem" }}>
                <p style={{ marginBottom: "0.25rem", color: "var(--color-muted)" }}>
                  {t("已获取")} {fetchedProxies.length} {t("个代理，已合并到下方候选列表")}
                </p>
              </div>
            )}

            <p className="callout callout-info" style={{ marginTop: "0.75rem", fontSize: "0.8rem" }}>
              {t("拉取的候选代理不会自动导入节点池，需要先检测再手动确认导入。")}
            </p>
          </div>
        </Card>

        {/* ── Section: Manual Input ───────────────────────────────────── */}
        <Card>
          <div className="card-header">
            <h2 className="card-title">
              <List size={16} /> {t("手动输入代理")}
            </h2>
          </div>
          <div className="card-content">
            <div className="form-row" style={{ display: "flex", gap: "0.5rem", flexDirection: "column" }}>
              <Textarea
                placeholder={t("每行一个代理，支持格式：ip:port / protocol://user:pass@host:port")}
                rows={4}
                value={manualText}
                onChange={(e) => setManualText(e.target.value)}
              />
              <div style={{ display: "flex", gap: "0.5rem" }}>
                <Button variant="secondary" onClick={handleAddManual} disabled={!manualText.trim()}>
                  {t("添加到候选列表")}
                </Button>
              </div>
            </div>
          </div>
        </Card>

        {/* ── Section: Candidates List ────────────────────────────────── */}
        <Card>
          <div className="card-header">
            <h2 className="card-title">
              {t("候选代理")}
              <span className="badge badge-neutral" style={{ marginLeft: "0.5rem" }}>
                {candidates.length}
              </span>
            </h2>
          </div>
          <div className="card-content">
            {candidates.length === 0 ? (
              <p className="empty-state">{t("暂无候选代理，请先拉取免费源或手动输入")}</p>
            ) : (
              <>
                <div style={{ display: "flex", gap: "0.5rem", marginBottom: "0.5rem", flexWrap: "wrap" }}>
                  <Button size="sm" variant="ghost" onClick={() => toggleAllCandidates(true)}>
                    {t("全选")}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => toggleAllCandidates(false)}>
                    {t("取消全选")}
                  </Button>
                  <span style={{ fontSize: "0.8rem", color: "var(--color-muted)", alignSelf: "center" }}>
                    {t("已选")}: {selectedCandidates.length}/{candidates.length}
                  </span>
                </div>

                <div className="candidate-grid" style={{ display: "flex", flexDirection: "column", gap: "2px" }}>
                  {candidates.map((c) => (
                    <label
                      key={c.id}
                      className="candidate-row"
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: "0.5rem",
                        padding: "0.25rem 0.5rem",
                        borderRadius: "4px",
                        cursor: "pointer",
                        fontSize: "0.85rem",
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={c.selected ?? false}
                        onChange={() => toggleCandidate(c.id)}
                      />
                      <code style={{ flex: 1 }}>{c.proxy}</code>
                      {c.source && c.source !== "manual" && (
                        <Badge variant="muted" style={{ fontSize: "0.7rem" }}>
                          {c.source}
                        </Badge>
                      )}
                      {c.source === "manual" && (
                        <Badge variant="info" style={{ fontSize: "0.7rem" }}>
                          {t("手动")}
                        </Badge>
                      )}
                    </label>
                  ))}
                </div>
              </>
            )}
          </div>
        </Card>

        {/* ── Section: Check Settings ─────────────────────────────────── */}
        <Card>
          <div className="card-header">
            <h2 className="card-title">
              <Play size={16} /> {t("检测设置")}
            </h2>
          </div>
          <div className="card-content">
            <div className="form-row" style={{ display: "flex", gap: "0.75rem", flexWrap: "wrap", alignItems: "flex-end" }}>
              <div style={{ flex: "1 1 150px" }}>
                <label className="form-label">{t("检测 Profile")}</label>
                <Select value={profile} onChange={(e) => setProfile(e.target.value)}>
                  {PROFILES.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </Select>
              </div>
              <div style={{ flex: "1 1 150px" }}>
                <label className="form-label">{t("检测预设")}</label>
                <Select
                  value={preset}
                  onChange={(e) => setPreset(e.target.value as ProxyCheckPreset)}
                >
                  {PRESETS.map((p) => (
                    <option key={p} value={p}>
                      {t(p === "quick" ? "快速" : p === "standard" ? "标准" : "深度")}
                    </option>
                  ))}
                </Select>
              </div>
              <div style={{ flex: "1 1 150px" }}>
                <label className="form-label">
                  {t("预设说明")}
                </label>
                <div style={{ fontSize: "0.8rem", color: "var(--color-muted)", paddingTop: "0.25rem" }}>
                  {preset === "quick" && t("仅检测服务可达性，1 轮")}
                  {preset === "standard" && t("服务可达性 + Cloudflare 检测，1 轮")}
                  {preset === "deep" && t("全面检测，3 轮")}
                </div>
              </div>
              <Button
                onClick={handleStartCheck}
                disabled={isChecking || selectedCandidates.length === 0}
              >
                {isChecking ? (
                  <>{t("检测中...")}</>
                ) : (
                  <>
                    <Play size={14} /> {t("开始检测")} ({selectedCandidates.length})
                  </>
                )}
              </Button>
            </div>
          </div>
        </Card>

        {/* ── Section: Task Progress ──────────────────────────────────── */}
        {task && !canImport && task.status !== "failed" && (
          <Card>
            <div className="card-header">
              <h2 className="card-title">
                <RefreshCw size={16} className={task && !isTerminal(task.status) ? "spin" : undefined} /> {t("检测进度")}
              </h2>
            </div>
            <div className="card-content">
              <div style={{ display: "flex", alignItems: "center", gap: "1rem", flexWrap: "wrap" }}>
                <Badge variant={task.status === "running" ? "info" : "neutral"}>
                  {t(task.status === "running" ? "运行中" : task.status === "pending" ? "等待中" : task.status)}
                </Badge>
                <span style={{ fontSize: "0.85rem" }}>
                  {t("进度")}: {task.done}/{task.total}
                </span>
                {task.failed > 0 && (
                  <span style={{ fontSize: "0.85rem", color: "var(--color-danger)" }}>
                    {t("失败")}: {task.failed}
                  </span>
                )}
              </div>
              {/* Simple progress bar */}
              {task.total > 0 && (
                <div
                  style={{
                    marginTop: "0.5rem",
                    height: "4px",
                    background: "var(--color-border)",
                    borderRadius: "2px",
                    overflow: "hidden",
                  }}
                >
                  <div
                    style={{
                      height: "100%",
                      width: `${Math.round((task.done / task.total) * 100)}%`,
                      background: "var(--color-primary)",
                      transition: "width 0.3s ease",
                    }}
                  />
                </div>
              )}
            </div>
          </Card>
        )}

        {/* ── Section: Results ────────────────────────────────────────── */}
        {task?.result && task.result.results.length > 0 && (
          <Card>
            <div className="card-header">
              <h2 className="card-title">
                {t("检测结果")}
                <span className="badge badge-neutral" style={{ marginLeft: "0.5rem" }}>
                  {task.result.results.length}
                </span>
              </h2>
              {canImport && (
                <div style={{ display: "flex", gap: "0.5rem" }}>
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={handleImport}
                    disabled={selectedResults.length === 0 || importMutation.isPending}
                  >
                    {importMutation.isPending ? (
                      <>{t("导入中...")}</>
                    ) : (
                      <>
                        <Download size={14} /> {t("导入选中")} ({selectedResults.length})
                      </>
                    )}
                  </Button>
                </div>
              )}
            </div>
            <div className="card-content" style={{ overflowX: "auto" }}>
              <DataTable
                data={task.result.results}
                columns={resultColumns}
              />
              {importedCount !== null && (
                <p className="callout callout-success" style={{ marginTop: "0.5rem" }}>
                  {t("已成功导入")} {importedCount} {t("个代理到节点池")}
                </p>
              )}
            </div>
          </Card>
        )}

        {/* ── Section: No-results task ────────────────────────────────── */}
        {task && canImport && (!task.result || task.result.results.length === 0) && (
          <Card>
            <div className="card-header">
              <h2 className="card-title">{t("检测结果")}</h2>
            </div>
            <div className="card-content">
              <p className="empty-state">{t("无检测结果")}</p>
            </div>
          </Card>
        )}

        {task?.status === "failed" && (
          <Card>
            <div className="card-header">
              <h2 className="card-title" style={{ color: "var(--color-danger)" }}>
                {t("检测失败")}
              </h2>
            </div>
            <div className="card-content">
              <p style={{ color: "var(--color-danger)" }}>{task.error || t("未知错误")}</p>
            </div>
          </Card>
        )}
      </div>

      <ToastContainer toasts={toasts} onDismiss={dismissToast} />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Merge new candidates into the existing list, deduplicating by proxy string. */
function mergeCandidates(
  existing: ProxyCandidate[],
  incoming: ProxyCandidate[],
): ProxyCandidate[] {
  const seen = new Set(existing.map((c) => c.id));
  const merged = [...existing];
  for (const c of incoming) {
    if (!seen.has(c.id)) {
      seen.add(c.id);
      merged.push(c);
    }
  }
  return merged;
}

function isTerminal(status: string): boolean {
  return status === "completed" || status === "completed_with_errors" || status === "failed";
}
