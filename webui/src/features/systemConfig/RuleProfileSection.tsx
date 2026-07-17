import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Diagnostic } from "@codemirror/lint";
import {
  AlertTriangle,
  Check,
  Copy,
  Download,
  FileCode2,
  FileUp,
  Info,
  Pencil,
  Plus,
  Save,
  ShieldAlert,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Input";
import { Switch } from "../../components/ui/Switch";
import { Textarea } from "../../components/ui/Textarea";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { formatDateTime } from "../../lib/time";
import {
  createRuleProfile,
  deleteRuleProfile,
  getRuleProfile,
  listRuleProfiles,
  previewACL4SSR,
  updateRuleProfile,
} from "./api";
import { RuleProfileVisualEditor } from "./RuleProfileVisualEditor";
import { RuleProfileYamlEditor } from "./RuleProfileYamlEditor";
import type { VisualDraft } from "./ruleProfileVisualModel";
import {
  EMPTY_VISUAL_STATE,
  loadVisualStateFromYaml,
  type EditorTab,
  type VisualEditorState,
} from "./ruleProfileVisualState";
import type { ACL4SSRPreviewResponse, RuleProfileSummary } from "./types";
import { ACL4SSR_ONLINE_FULL_SOURCE_ID } from "./types";
import { summarizeYamlTemplate } from "./yamlTemplateSummary";

type ToastFn = (tone: "success" | "error", text: string) => void;

type RuleProfileSectionProps = {
  showToast: ToastFn;
};

type ProfileModalMode = "create" | "edit" | null;
type AclImportMode = "source" | "paste";

const MIHOMO_EXAMPLE_TEMPLATE = `# Resin replaces the top-level proxies list during export.
# US/HK filters may match no nodes. Adjust/remove filters for your pool, or set a
# suitable Mihomo empty-fallback. Do not default empty groups to DIRECT unless intended.
proxies: []

proxy-groups:
  - name: AUTO
    type: url-test
    include-all-proxies: true
    url: https://www.gstatic.com/generate_204
    interval: 300

  - name: MANUAL
    type: select
    include-all-proxies: true

  - name: US
    type: select
    include-all-proxies: true
    filter: '(?:^|/)\\[US\\](?: [^/]*|)$'

  - name: HK
    type: select
    include-all-proxies: true
    filter: '(?:^|/)\\[HK\\](?: [^/]*|)$'

  - name: PROXY
    type: select
    proxies:
      - AUTO
      - MANUAL
      - US
      - HK
      - DIRECT

  # AI/NETFLIX/TELEGRAM are policy-group skeletons only. Add your own rules or
  # rule-providers targeting these groups before the final MATCH rule.
  - name: AI
    type: select
    proxies:
      - AUTO
      - MANUAL
      - US
      - HK
      - DIRECT

  - name: NETFLIX
    type: select
    proxies:
      - AUTO
      - MANUAL
      - US
      - HK
      - DIRECT

  - name: TELEGRAM
    type: select
    proxies:
      - AUTO
      - MANUAL
      - US
      - HK
      - DIRECT

rules:
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`;

function resetAclImportState(
  setAclMode: (mode: AclImportMode) => void,
  setIniPaste: (value: string) => void,
  setPreview: (value: ACL4SSRPreviewResponse | null) => void,
  setPreviewError: (value: string) => void,
  aclModeRef?: { current: AclImportMode },
  iniPasteRef?: { current: string },
) {
  setAclMode("source");
  setIniPaste("");
  setPreview(null);
  setPreviewError("");
  if (aclModeRef) {
    aclModeRef.current = "source";
  }
  if (iniPasteRef) {
    iniPasteRef.current = "";
  }
}

export function RuleProfileSection({ showToast }: RuleProfileSectionProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [modalMode, setModalMode] = useState<ProfileModalMode>(null);
  const [editingID, setEditingID] = useState("");
  const [name, setName] = useState("");
  const [templateYAML, setTemplateYAML] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [nameError, setNameError] = useState("");
  const [templateError, setTemplateError] = useState("");
  const [submitError, setSubmitError] = useState("");
  const [copiedID, setCopiedID] = useState("");
  const [initializedEditID, setInitializedEditID] = useState("");
  const modalSessionRef = useRef(0);

  // ACL4SSR import / preview (local draft only; never auto-saves).
  const [aclMode, setAclMode] = useState<AclImportMode>("source");
  const [iniPaste, setIniPaste] = useState("");
  const [preview, setPreview] = useState<ACL4SSRPreviewResponse | null>(null);
  const [previewError, setPreviewError] = useState("");
  // Refs track latest import inputs so in-flight previews can be discarded after await.
  // Updated in the same event handlers that clear preview (not during render).
  const aclModeRef = useRef<AclImportMode>("source");
  const iniPasteRef = useRef("");

  // Phase 4 visual draft (separate from templateYAML; explicit Apply only).
  const [editorTab, setEditorTab] = useState<EditorTab>("yaml");
  const [visual, setVisual] = useState<VisualEditorState>(EMPTY_VISUAL_STATE);
  const [visualBaseline, setVisualBaseline] = useState<VisualDraft | null>(null);
  const [visualSaveBlock, setVisualSaveBlock] = useState("");

  const profilesQuery = useQuery({
    queryKey: ["rule-profiles", "all"],
    queryFn: () => listRuleProfiles(),
    staleTime: 30_000,
  });

  const detailQuery = useQuery({
    queryKey: ["rule-profiles", "detail", editingID],
    queryFn: () => getRuleProfile(editingID),
    enabled: modalMode === "edit" && Boolean(editingID),
    staleTime: 0,
  });

  useEffect(() => {
    const detail = detailQuery.data;
    if (
      modalMode !== "edit"
      || !editingID
      || !detailQuery.isSuccess
      || detailQuery.isFetching
      || !detail
      || detail.id !== editingID
      || initializedEditID === editingID
    ) {
      return;
    }
    let cancelled = false;
    void Promise.resolve().then(() => {
      if (cancelled) {
        return;
      }
      setName(detail.name);
      setTemplateYAML(detail.template_yaml);
      setEnabled(detail.enabled);
      setInitializedEditID(editingID);
    });
    return () => {
      cancelled = true;
    };
  }, [detailQuery.data, detailQuery.isFetching, detailQuery.isSuccess, editingID, initializedEditID, modalMode]);

  const invalidateProfiles = async () => {
    await queryClient.invalidateQueries({ queryKey: ["rule-profiles"] });
  };

  const createMutation = useMutation({
    mutationFn: createRuleProfile,
  });

  const editMutation = useMutation({
    mutationFn: ({ id, name: nextName, template_yaml, enabled: nextEnabled }: {
      id: string;
      name: string;
      template_yaml: string;
      enabled: boolean;
    }) => updateRuleProfile(id, { name: nextName, template_yaml, enabled: nextEnabled }),
  });

  const toggleMutation = useMutation({
    mutationFn: ({ profile, nextEnabled }: { profile: RuleProfileSummary; nextEnabled: boolean }) =>
      updateRuleProfile(profile.id, { enabled: nextEnabled }),
    onSuccess: async (updated) => {
      await invalidateProfiles();
      showToast(
        "success",
        updated.enabled
          ? t("Rule Profile {{name}} 已启用", { name: updated.name })
          : t("Rule Profile {{name}} 已禁用", { name: updated.name }),
      );
    },
    onError: (error) => showToast("error", formatApiErrorMessage(error, t)),
  });

  const deleteMutation = useMutation({
    mutationFn: deleteRuleProfile,
    onSuccess: async () => {
      await invalidateProfiles();
      showToast("success", t("Rule Profile 已删除"));
    },
    onError: (error) => showToast("error", formatApiErrorMessage(error, t)),
  });

  const previewMutation = useMutation({
    mutationFn: (body: Parameters<typeof previewACL4SSR>[0]) => previewACL4SSR(body),
  });

  const yamlSummary = useMemo(() => summarizeYamlTemplate(templateYAML), [templateYAML]);

  const editorDiagnostics = useMemo<Diagnostic[]>(() => {
    if (!yamlSummary.parseError) {
      return [];
    }
    const len = templateYAML.length;
    const from = yamlSummary.parseErrorFrom != null
      ? Math.min(Math.max(0, yamlSummary.parseErrorFrom), len)
      : 0;
    let to = yamlSummary.parseErrorTo != null
      ? Math.min(Math.max(0, yamlSummary.parseErrorTo), len)
      : Math.min(from + 1, len);
    if (to < from) {
      to = from;
    }
    if (to === from && len > 0) {
      to = Math.min(from + 1, len);
    }
    const message = yamlSummary.parseErrorKind === "yaml"
      ? t("YAML 解析错误：{{detail}}", { detail: yamlSummary.parseError })
      : t(yamlSummary.parseError);
    return [
      {
        from,
        to,
        severity: "error",
        message,
      },
    ];
  }, [t, templateYAML.length, yamlSummary.parseError, yamlSummary.parseErrorFrom, yamlSummary.parseErrorKind, yamlSummary.parseErrorTo]);

  const resetErrors = () => {
    setNameError("");
    setTemplateError("");
    setSubmitError("");
    setVisualSaveBlock("");
  };

  const resetVisualState = () => {
    setEditorTab("yaml");
    setVisual(EMPTY_VISUAL_STATE);
    setVisualBaseline(null);
    setVisualSaveBlock("");
  };

  /** Mark visual draft stale when YAML changes outside visual Apply. */
  const markVisualStaleFromYamlEdit = () => {
    setVisual((prev) => {
      if (!prev.draft && !prev.loadedFingerprint) {
        return prev;
      }
      return { ...prev, stale: true };
    });
    setVisualSaveBlock("");
  };

  const openCreate = () => {
    modalSessionRef.current += 1;
    resetErrors();
    resetAclImportState(setAclMode, setIniPaste, setPreview, setPreviewError, aclModeRef, iniPasteRef);
    resetVisualState();
    setEditingID("");
    setInitializedEditID("");
    setName("");
    setTemplateYAML(MIHOMO_EXAMPLE_TEMPLATE);
    setEnabled(true);
    setModalMode("create");
  };

  const openEdit = (profile: RuleProfileSummary) => {
    modalSessionRef.current += 1;
    resetErrors();
    resetAclImportState(setAclMode, setIniPaste, setPreview, setPreviewError, aclModeRef, iniPasteRef);
    resetVisualState();
    setEditingID(profile.id);
    setInitializedEditID("");
    setName("");
    setTemplateYAML("");
    setEnabled(profile.enabled);
    setModalMode("edit");
  };

  const closeModal = () => {
    if (createMutation.isPending || editMutation.isPending || previewMutation.isPending) {
      return;
    }
    if (visual.dirty) {
      const confirmed = window.confirm(
        t("可视化草稿有未应用更改。关闭将丢弃这些更改（YAML 本身若已改动仍会丢失未保存内容）。是否关闭？"),
      );
      if (!confirmed) {
        return;
      }
    }
    modalSessionRef.current += 1;
    setModalMode(null);
    setEditingID("");
    setInitializedEditID("");
    resetAclImportState(setAclMode, setIniPaste, setPreview, setPreviewError, aclModeRef, iniPasteRef);
    resetVisualState();
  };

  useEffect(() => {
    if (!modalMode) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closeModal();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  });

  const loadExampleTemplate = () => {
    if (visual.dirty) {
      const confirmedDirty = window.confirm(
        t("可视化草稿有未应用更改。载入示例会覆盖 YAML 并丢弃可视化草稿，是否继续？"),
      );
      if (!confirmedDirty) {
        return;
      }
    } else if (templateYAML.trim() && templateYAML !== MIHOMO_EXAMPLE_TEMPLATE) {
      const confirmed = window.confirm(t("载入示例模板会覆盖当前 YAML，是否继续？"));
      if (!confirmed) {
        return;
      }
    }
    setTemplateYAML(MIHOMO_EXAMPLE_TEMPLATE);
    setTemplateError("");
    setSubmitError("");
    // Example replaces YAML → drop visual draft (reload on next visual tab open).
    setVisual(EMPTY_VISUAL_STATE);
    setVisualBaseline(null);
  };

  const runPreview = async () => {
    setPreviewError("");
    setPreview(null);
    const session = modalSessionRef.current;
    const modeAtStart = aclModeRef.current;
    const iniAtStart = iniPasteRef.current;
    try {
      const result = await previewMutation.mutateAsync(
        modeAtStart === "source"
          ? { source_id: ACL4SSR_ONLINE_FULL_SOURCE_ID }
          : { ini_content: iniAtStart },
      );
      // Drop result if the modal closed, import mode changed, or pasted INI changed.
      if (
        modalSessionRef.current !== session
        || !modalMode
        || aclModeRef.current !== modeAtStart
        || (modeAtStart === "paste" && iniPasteRef.current !== iniAtStart)
      ) {
        return;
      }
      setPreview(result);
    } catch (error) {
      if (
        modalSessionRef.current !== session
        || !modalMode
        || aclModeRef.current !== modeAtStart
        || (modeAtStart === "paste" && iniPasteRef.current !== iniAtStart)
      ) {
        return;
      }
      const message = formatApiErrorMessage(error, t);
      setPreviewError(message);
      showToast("error", message);
    }
  };

  const applyPreviewToYaml = () => {
    if (!preview?.template_yaml) {
      return;
    }
    const next = preview.template_yaml;
    if (visual.dirty) {
      const confirmedDirty = window.confirm(
        t("可视化草稿有未应用更改。应用 ACL4SSR 预览会覆盖 YAML 并丢弃可视化草稿，是否继续？"),
      );
      if (!confirmedDirty) {
        return;
      }
    } else if (templateYAML.trim() && templateYAML !== next) {
      const confirmed = window.confirm(
        t("应用预览会覆盖当前编辑器中的 YAML（尚未保存到服务器）。是否继续？"),
      );
      if (!confirmed) {
        return;
      }
    }
    setTemplateYAML(next);
    setTemplateError("");
    setSubmitError("");
    setVisual(EMPTY_VISUAL_STATE);
    setVisualBaseline(null);
    showToast("success", t("已应用到本地 YAML（尚未保存）"));
  };

  const onTemplateChange = (value: string) => {
    setTemplateYAML(value);
    setTemplateError("");
    setSubmitError("");
    markVisualStaleFromYamlEdit();
  };

  /** First-time load only. Never auto-reloads a stale draft (dirty or clean). */
  const ensureVisualLoadedFirstTime = () => {
    // Existing draft of any kind (including stale / parse-error state) must not be overwritten here.
    if (visual.draft || visual.stale || visual.parseErrors.length > 0 || visual.loadedFingerprint) {
      return;
    }
    const next = loadVisualStateFromYaml(templateYAML);
    setVisual(next);
    setVisualBaseline(next.draft);
  };

  const reloadVisualFromYaml = () => {
    if (visual.dirty) {
      const confirmed = window.confirm(
        t("从 YAML 重新加载会丢弃未应用的可视化更改，是否继续？"),
      );
      if (!confirmed) {
        return;
      }
    }
    const next = loadVisualStateFromYaml(templateYAML);
    setVisual(next);
    setVisualBaseline(next.draft);
    setVisualSaveBlock("");
    if (next.parseErrors.length > 0) {
      showToast("error", t("无法从 YAML 解析可视化草稿，请先修复 YAML。"));
    }
  };

  const onEditorTabChange = (tab: EditorTab) => {
    setEditorTab(tab);
    if (tab !== "yaml") {
      // Auto-load only when there is no draft yet. Stale drafts stay stale until explicit reload.
      ensureVisualLoadedFirstTime();
    }
  };

  const onVisualAppliedYaml = (yaml: string) => {
    setTemplateYAML(yaml);
    setTemplateError("");
    setSubmitError("");
    setVisualSaveBlock("");
    const next = loadVisualStateFromYaml(yaml);
    setVisual(next);
    setVisualBaseline(next.draft);
  };

  const submitProfile = async (event: FormEvent) => {
    event.preventDefault();
    resetErrors();
    if (visual.dirty) {
      setVisualSaveBlock(
        t("可视化草稿有未应用更改。请先「应用可视化到 YAML」，或丢弃可视化更改后再保存。"),
      );
      if (editorTab === "yaml") {
        setEditorTab("groups");
        // Do not auto-reload stale drafts; only first-time load if nothing exists.
        ensureVisualLoadedFirstTime();
      }
      return;
    }
    const trimmedName = name.trim();
    if (!trimmedName) {
      setNameError(t("名称不能为空"));
    }
    if (!templateYAML.trim()) {
      setTemplateError(t("模板 YAML 不能为空"));
    }
    if (!trimmedName || !templateYAML.trim()) {
      return;
    }

    const session = modalSessionRef.current;
    if (modalMode === "create") {
      try {
        const created = await createMutation.mutateAsync({ name: trimmedName, template_yaml: templateYAML, enabled });
        await invalidateProfiles();
        if (modalSessionRef.current !== session || modalMode !== "create") {
          return;
        }
        modalSessionRef.current += 1;
        setModalMode(null);
        resetAclImportState(setAclMode, setIniPaste, setPreview, setPreviewError, aclModeRef, iniPasteRef);
        resetVisualState();
        showToast("success", t("Rule Profile {{name}} 已创建", { name: created.name }));
      } catch (error) {
        if (modalSessionRef.current !== session || modalMode !== "create") {
          return;
        }
        const message = formatApiErrorMessage(error, t);
        setSubmitError(message);
        showToast("error", message);
      }
      return;
    }
    if (modalMode === "edit" && editingID && initializedEditID === editingID) {
      const submittedID = editingID;
      if (detailQuery.data?.id === submittedID && detailQuery.data.enabled && !enabled) {
        const confirmed = window.confirm(
          t(
            "确认禁用 Rule Profile {{name}}？所有带有该 Profile ID 的现有导出 URL 将返回 404 RULE_PROFILE_UNAVAILABLE，不会回退为仅 proxies 输出。",
            { name: trimmedName },
          ),
        );
        if (!confirmed) {
          return;
        }
      }
      try {
        const updated = await editMutation.mutateAsync({ id: submittedID, name: trimmedName, template_yaml: templateYAML, enabled });
        await invalidateProfiles();
        if (
          modalSessionRef.current !== session
          || modalMode !== "edit"
          || editingID !== submittedID
        ) {
          return;
        }
        modalSessionRef.current += 1;
        setModalMode(null);
        resetAclImportState(setAclMode, setIniPaste, setPreview, setPreviewError, aclModeRef, iniPasteRef);
        resetVisualState();
        showToast("success", t("Rule Profile {{name}} 已更新", { name: updated.name }));
      } catch (error) {
        if (
          modalSessionRef.current !== session
          || modalMode !== "edit"
          || editingID !== submittedID
        ) {
          return;
        }
        const message = formatApiErrorMessage(error, t);
        setSubmitError(message);
        showToast("error", message);
      }
    }
  };

  const discardVisualAndSaveHint = () => {
    if (!visual.dirty) {
      return;
    }
    const confirmed = window.confirm(
      t("丢弃未应用的可视化更改？当前 YAML 保持不变，可再次点击创建/保存写入服务器。"),
    );
    if (!confirmed) {
      return;
    }
    const next = loadVisualStateFromYaml(templateYAML);
    setVisual(next);
    setVisualBaseline(next.draft);
    setVisualSaveBlock("");
  };

  const toggleProfile = (profile: RuleProfileSummary, nextEnabled: boolean) => {
    if (!nextEnabled) {
      const confirmed = window.confirm(
        t(
          "确认禁用 Rule Profile {{name}}？所有带有该 Profile ID 的现有导出 URL 将返回 404 RULE_PROFILE_UNAVAILABLE，不会回退为仅 proxies 输出。",
          { name: profile.name },
        ),
      );
      if (!confirmed) {
        return;
      }
    }
    toggleMutation.mutate({ profile, nextEnabled });
  };

  const removeProfile = (profile: RuleProfileSummary) => {
    const confirmed = window.confirm(
      t(
        "确认删除 Rule Profile {{name}}？所有带有该 Profile ID 的现有导出 URL 将返回 404 RULE_PROFILE_UNAVAILABLE，不会回退为仅 proxies 输出。此操作不可撤销。",
        { name: profile.name },
      ),
    );
    if (confirmed) {
      deleteMutation.mutate(profile.id);
    }
  };

  const copyProfileID = async (profile: RuleProfileSummary) => {
    try {
      await navigator.clipboard.writeText(profile.id);
      setCopiedID(profile.id);
      window.setTimeout(() => setCopiedID(""), 1500);
      showToast("success", t("Profile ID 已复制"));
    } catch {
      showToast("error", t("复制失败，请手动复制"));
    }
  };

  const modalPending = createMutation.isPending || editMutation.isPending;
  const previewPending = previewMutation.isPending;
  const editingLoading = modalMode === "edit" && (
    detailQuery.isFetching
    || initializedEditID !== editingID
  ) && !detailQuery.isError;
  const editingError = modalMode === "edit" && detailQuery.isError;
  const pastePreviewDisabled = aclMode === "paste" && !iniPaste.trim();

  return (
    <>
      <Card className="syscfg-form-card platform-directory-card rule-profile-card">
        <div className="detail-header">
          <div>
            <h3>{t("Rule Profile / 规则配置")}</h3>
            <p>{t("保存完整 Mihomo YAML 模板，并在 Clash 节点导出时按不可变 Profile ID 选择使用。")}</p>
          </div>
          <Button variant="secondary" size="sm" onClick={openCreate}>
            <Plus size={15} />
            {t("新建 Rule Profile")}
          </Button>
        </div>

        <div className="callout callout-warning rule-profile-secret-warning" role="alert">
          <ShieldAlert size={16} />
          <div>
            <strong>{t("模板内容会完整下发给订阅持有者")}</strong>
            <p>
              {t(
                "任何持有有效 export token 并知道 Profile ID 的人都能读取模板完整内容。不要在模板中放入 API keys、私有订阅 URL、Authorization headers、cookies 或其他 secret。",
              )}
            </p>
          </div>
        </div>

        <p className="muted rule-profile-section-help">
          {t(
            "Resin 导出时会替换顶层 proxies。动态组请使用 Mihomo include-all-proxies 和 filter，不要手写动态节点名。未知地区节点标记为 [??]；远程 provider URL 由 Mihomo 客户端访问，Resin 不代拉。",
          )}
        </p>

        {profilesQuery.isLoading ? <p className="muted">{t("正在加载 Rule Profile...")}</p> : null}
        {profilesQuery.isError ? (
          <div className="callout callout-error">
            <AlertTriangle size={14} />
            <span>{formatApiErrorMessage(profilesQuery.error, t)}</span>
          </div>
        ) : null}

        {profilesQuery.data?.length ? (
          <div className="platform-ops-list rule-profile-list">
            {profilesQuery.data.map((profile) => {
              const togglePending = toggleMutation.isPending && toggleMutation.variables?.profile.id === profile.id;
              const deletePending = deleteMutation.isPending && deleteMutation.variables === profile.id;
              return (
                <div key={profile.id} className="platform-op-item rule-profile-row">
                  <div className="platform-op-copy rule-profile-row-copy">
                    <div className="rule-profile-name-row">
                      <h5>{profile.name}</h5>
                      <span className={profile.enabled ? "rule-profile-status-enabled" : "rule-profile-status-disabled"}>
                        {profile.enabled ? t("已启用") : t("已停用")}
                      </span>
                    </div>
                    <div className="rule-profile-id-row">
                      <code title={profile.id}>{profile.id}</code>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="rule-profile-copy-btn"
                        onClick={() => void copyProfileID(profile)}
                        aria-label={t("复制 Profile ID")}
                        title={t("复制 Profile ID")}
                      >
                        {copiedID === profile.id ? <Check size={13} /> : <Copy size={13} />}
                      </Button>
                    </div>
                    <p className="platform-op-hint">
                      {t("更新于 {{updated}} · 创建于 {{created}}", {
                        updated: formatDateTime(profile.updated_at),
                        created: formatDateTime(profile.created_at),
                      })}
                    </p>
                  </div>
                  <div className="rule-profile-row-actions">
                    <div className="rule-profile-switch-label">
                      <span>{profile.enabled ? t("启用") : t("停用")}</span>
                      <Switch
                        checked={profile.enabled}
                        disabled={togglePending || deletePending}
                        onChange={(event) => toggleProfile(profile, event.target.checked)}
                        aria-label={t("启停 Rule Profile {{name}}", { name: profile.name })}
                      />
                    </div>
                    <Button variant="secondary" size="sm" onClick={() => openEdit(profile)} disabled={deletePending}>
                      <Pencil size={14} />
                      {t("编辑")}
                    </Button>
                    <Button variant="danger" size="sm" onClick={() => removeProfile(profile)} disabled={deletePending}>
                      <Trash2 size={14} />
                      {deletePending ? t("删除中...") : t("删除")}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        ) : !profilesQuery.isLoading && !profilesQuery.isError ? (
          <div className="empty-box">
            <FileCode2 size={17} />
            <p>{t("尚未创建 Rule Profile")}</p>
          </div>
        ) : null}
      </Card>

      {modalMode ? (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-label={t(modalMode === "create" ? "新建 Rule Profile" : "编辑 Rule Profile")}>
          <Card className="modal-card rule-profile-modal">
            <div className="modal-header">
              <div>
                <h3>{t(modalMode === "create" ? "新建 Rule Profile" : "编辑 Rule Profile")}</h3>
                <p className="muted">{t("Mihomo 优先；Resin 保存时由服务端校验 YAML 结构。")}</p>
              </div>
              <Button variant="ghost" size="sm" onClick={closeModal} aria-label={t("关闭")} disabled={modalPending || previewPending}>
                <X size={16} />
              </Button>
            </div>

            {editingLoading ? <p className="muted rule-profile-modal-loading">{t("正在加载 Rule Profile 详情...")}</p> : null}
            {editingError ? (
              <div className="callout callout-error rule-profile-modal-loading">
                <AlertTriangle size={14} />
                <div>
                  <span>{formatApiErrorMessage(detailQuery.error, t)}</span>
                  <div className="detail-actions">
                    <Button variant="secondary" size="sm" onClick={() => void detailQuery.refetch()}>
                      {t("重试")}
                    </Button>
                  </div>
                </div>
              </div>
            ) : null}

            {!editingLoading && !editingError ? (
              <form className="rule-profile-form" onSubmit={submitProfile}>
                {modalMode === "edit" ? (
                  <div className="field-group">
                    <label className="field-label" htmlFor="rule-profile-id">{t("Profile ID（不可变）")}</label>
                    <Input id="rule-profile-id" readOnly disabled value={editingID} />
                  </div>
                ) : null}

                <div className="rule-profile-form-top">
                  <div className="field-group">
                    <label className="field-label" htmlFor="rule-profile-name">{t("名称")}</label>
                    <Input
                      id="rule-profile-name"
                      value={name}
                      maxLength={128}
                      invalid={Boolean(nameError)}
                      onChange={(event) => {
                        setName(event.target.value);
                        setNameError("");
                        setSubmitError("");
                      }}
                    />
                    {nameError ? <p className="field-error">{nameError}</p> : null}
                  </div>
                  <div className="rule-profile-enabled-field">
                    <div>
                      <span className="field-label">{t("启用")}</span>
                      <small>{t("禁用后，使用此 ID 的公开导出会返回 404，不会降级。")}</small>
                    </div>
                    <Switch
                      checked={enabled}
                      onChange={(event) => setEnabled(event.target.checked)}
                      aria-label={t("启用此 Rule Profile")}
                    />
                  </div>
                </div>

                <section className="rule-profile-acl-panel" aria-labelledby="rule-profile-acl-heading">
                  <div className="rule-profile-acl-head">
                    <div>
                      <h4 id="rule-profile-acl-heading">{t("从 ACL4SSR 导入（预览）")}</h4>
                      <p className="muted">
                        {t("预览与「应用到 YAML」只更新本地草稿，不会保存 Rule Profile。只有下方创建/保存才会写入服务器。")}
                      </p>
                    </div>
                  </div>

                  <div className="rule-profile-acl-mode" role="tablist" aria-label={t("导入方式")}>
                    <button
                      type="button"
                      role="tab"
                      aria-selected={aclMode === "source"}
                      className={`rule-profile-acl-mode-btn${aclMode === "source" ? " is-active" : ""}`}
                      onClick={() => {
                        if (aclMode === "source") {
                          return;
                        }
                        setAclMode("source");
                        aclModeRef.current = "source";
                        // Drop any stale preview so apply cannot use the wrong import inputs.
                        setPreview(null);
                        setPreviewError("");
                      }}
                    >
                      <Download size={14} />
                      {t("ACL4SSR Online Full")}
                    </button>
                    <button
                      type="button"
                      role="tab"
                      aria-selected={aclMode === "paste"}
                      className={`rule-profile-acl-mode-btn${aclMode === "paste" ? " is-active" : ""}`}
                      onClick={() => {
                        if (aclMode === "paste") {
                          return;
                        }
                        setAclMode("paste");
                        aclModeRef.current = "paste";
                        setPreview(null);
                        setPreviewError("");
                      }}
                    >
                      <FileUp size={14} />
                      {t("粘贴 [custom] INI")}
                    </button>
                  </div>

                  {aclMode === "source" ? (
                    <p className="muted rule-profile-acl-source-note">
                      {t("使用服务端固定来源 acl4ssr-online-full（ACL4SSR Online Full）。Resin 仅在你点击预览时抓取一次，不会在导出时自动同步。")}
                    </p>
                  ) : (
                    <div className="field-group">
                      <label className="field-label" htmlFor="rule-profile-ini-paste">{t("ACL4SSR [custom] INI")}</label>
                      <Textarea
                        id="rule-profile-ini-paste"
                        className="rule-profile-ini-editor"
                        rows={8}
                        value={iniPaste}
                        spellCheck={false}
                        placeholder={t("粘贴包含 [custom]、custom_proxy_group= 与 ruleset= 的 INI…")}
                        onChange={(event) => {
                          const next = event.target.value;
                          setIniPaste(next);
                          iniPasteRef.current = next;
                          // INI changed: invalidate previous conversion preview.
                          setPreview(null);
                          setPreviewError("");
                        }}
                      />
                    </div>
                  )}

                  <div className="rule-profile-acl-actions">
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => void runPreview()}
                      disabled={previewPending || modalPending || pastePreviewDisabled}
                    >
                      <Download size={14} />
                      {previewPending ? t("预览中...") : t("转换预览")}
                    </Button>
                    <span className="muted rule-profile-acl-hint">
                      {t("预览不会保存；应用后仍需创建/保存才会持久化。")}
                    </span>
                  </div>

                  {previewError ? (
                    <div className="callout callout-error" role="alert">
                      <AlertTriangle size={14} />
                      <span>{previewError}</span>
                    </div>
                  ) : null}

                  {preview ? (
                    <div className="rule-profile-preview-report" aria-live="polite">
                      <div className="rule-profile-preview-report-head">
                        <strong>{t("转换报告")}</strong>
                        <span className="muted">{t("未保存")}</span>
                      </div>
                      <div className="rule-profile-stat-row">
                        <span className="rule-profile-stat-chip">
                          {t("组 {{count}}", { count: preview.group_count })}
                        </span>
                        <span className="rule-profile-stat-chip">
                          {t("Provider {{count}}", { count: preview.provider_count })}
                        </span>
                        <span className="rule-profile-stat-chip">
                          {t("规则 {{count}}", { count: preview.rule_count })}
                        </span>
                      </div>
                      <div className="rule-profile-preview-source">
                        <Info size={14} />
                        <div>
                          <p>
                            <strong>{t("来源")}:</strong> {preview.source.name}
                            {preview.source.url ? (
                              <>
                                {" · "}
                                <a href={preview.source.url} target="_blank" rel="noreferrer noopener">
                                  {preview.source.url}
                                </a>
                              </>
                            ) : null}
                          </p>
                          <p>
                            <strong>{t("许可")}:</strong> {preview.source.license}
                          </p>
                          <p className="muted">{preview.attribution}</p>
                        </div>
                      </div>
                      {preview.warnings?.length ? (
                        <div className="rule-profile-preview-warnings">
                          <strong>{t("转换警告（{{count}}）", { count: preview.warnings.length })}</strong>
                          <ul>
                            {preview.warnings.map((warning) => (
                              <li key={warning}>{warning}</li>
                            ))}
                          </ul>
                        </div>
                      ) : (
                        <p className="muted">{t("无转换警告。")}</p>
                      )}
                      <div className="rule-profile-acl-actions">
                        <Button variant="primary" size="sm" onClick={applyPreviewToYaml} disabled={modalPending}>
                          <FileCode2 size={14} />
                          {t("应用到 YAML（尚未保存）")}
                        </Button>
                      </div>
                    </div>
                  ) : null}
                </section>

                <RuleProfileVisualEditor
                  activeTab={editorTab}
                  onTabChange={onEditorTabChange}
                  visual={visual}
                  baselineDraft={visualBaseline}
                  onVisualChange={setVisual}
                  onReloadFromYaml={reloadVisualFromYaml}
                  onAppliedYaml={onVisualAppliedYaml}
                  showToast={showToast}
                  disabled={modalPending || previewPending}
                  templateYAML={templateYAML}
                  yamlTab={(
                    <div className="field-group rule-profile-yaml-tab">
                      <div className="rule-profile-editor-head">
                        <div>
                          <span className="field-label" id="rule-profile-template-label">{t("Mihomo 模板 YAML")}</span>
                          <p className="muted">
                            {t("顶层 proxies 可省略或保持为空，导出时由 Resin 替换。动态组使用 include-all-proxies/filter。")}
                          </p>
                          <p className="muted rule-profile-yaml-visual-hint">
                            {t("YAML 为唯一持久化源。分组/Provider/规则页签的更改需点「应用可视化到 YAML」才会写入本地草稿；创建/保存才会写入服务器。")}
                          </p>
                        </div>
                        <Button variant="secondary" size="sm" onClick={loadExampleTemplate}>
                          <FileCode2 size={14} />
                          {t("载入示例模板")}
                        </Button>
                      </div>

                      <div className="rule-profile-yaml-summary" aria-live="polite">
                        {yamlSummary.parseError ? (
                          <div className="rule-profile-yaml-summary-error">
                            <AlertTriangle size={13} />
                            <span>
                              {yamlSummary.parseErrorKind === "yaml"
                                ? t("YAML 解析错误：{{detail}}", { detail: yamlSummary.parseError })
                                : t(yamlSummary.parseError)}
                              {yamlSummary.parseErrorLine != null
                                ? t("（第 {{line}} 行）", { line: yamlSummary.parseErrorLine })
                                : ""}
                            </span>
                          </div>
                        ) : (
                          <div className="rule-profile-stat-row">
                            <span className="rule-profile-stat-chip">
                              {t("组 {{count}}", {
                                count: yamlSummary.groupCount != null ? yamlSummary.groupCount : "—",
                              })}
                            </span>
                            <span className="rule-profile-stat-chip">
                              {t("Provider {{count}}", {
                                count: yamlSummary.providerCount != null ? yamlSummary.providerCount : "—",
                              })}
                            </span>
                            <span className="rule-profile-stat-chip">
                              {t("规则 {{count}}", {
                                count: yamlSummary.ruleCount != null ? yamlSummary.ruleCount : "—",
                              })}
                            </span>
                            <span className="rule-profile-stat-chip">
                              {yamlSummary.matchTarget
                                ? t("MATCH → {{target}}{{order}}", {
                                    target: yamlSummary.matchTarget,
                                    order: yamlSummary.matchIsLast === false ? t("（非末条）") : "",
                                  })
                                : t("无 MATCH")}
                            </span>
                          </div>
                        )}
                        {yamlSummary.warnings.length > 0 ? (
                          <ul className="rule-profile-yaml-summary-warnings">
                            {yamlSummary.warnings.map((warning) => {
                              const text = t(warning.key, warning.params);
                              return <li key={warning.key + text}>{text}</li>;
                            })}
                          </ul>
                        ) : null}
                        <p className="muted rule-profile-yaml-summary-note">
                          {t("本地结构摘要仅供参考；保存时仍以服务端校验为准。")}
                        </p>
                      </div>

                      <RuleProfileYamlEditor
                        id="rule-profile-template"
                        value={templateYAML}
                        invalid={Boolean(templateError || submitError)}
                        aria-labelledby="rule-profile-template-label"
                        diagnostics={editorDiagnostics}
                        onChange={onTemplateChange}
                      />
                      {templateError ? <p className="field-error">{templateError}</p> : null}
                    </div>
                  )}
                />

                <div className="callout callout-warning rule-profile-modal-secret" role="alert">
                  <AlertTriangle size={14} />
                  <span>
                    {t("不要保存 API keys、私有订阅 URL、headers、cookies 或其他 secret；模板会完整返回给持有 export token 且知道 Profile ID 的人。")}
                  </span>
                </div>

                {visualSaveBlock ? (
                  <div className="callout callout-error" role="alert">
                    <AlertTriangle size={14} />
                    <div className="rule-profile-visual-save-block">
                      <span>{visualSaveBlock}</span>
                      <div className="rule-profile-acl-actions">
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => {
                            setEditorTab("groups");
                            // Never auto-reload stale drafts; only first-time load.
                            ensureVisualLoadedFirstTime();
                          }}
                        >
                          {t("前往可视化并应用")}
                        </Button>
                        <Button variant="ghost" size="sm" onClick={discardVisualAndSaveHint}>
                          {t("丢弃可视化更改")}
                        </Button>
                      </div>
                    </div>
                  </div>
                ) : null}

                {submitError ? (
                  <div className="callout callout-error" role="alert">
                    <AlertTriangle size={14} />
                    <span>{submitError}</span>
                  </div>
                ) : null}

                <div className="detail-actions rule-profile-modal-actions">
                  <Button variant="ghost" onClick={closeModal} disabled={modalPending || previewPending}>{t("取消")}</Button>
                  <Button type="submit" disabled={modalPending || previewPending}>
                    <Save size={14} />
                    {modalPending ? t("保存中...") : t(modalMode === "create" ? "创建 Rule Profile" : "保存 Rule Profile")}
                  </Button>
                </div>
              </form>
            ) : null}
          </Card>
        </div>
      ) : null}
    </>
  );
}
