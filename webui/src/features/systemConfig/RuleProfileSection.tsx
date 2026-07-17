import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Check, Copy, FileCode2, Pencil, Plus, Save, ShieldAlert, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Input";
import { Switch } from "../../components/ui/Switch";
import { Textarea } from "../../components/ui/Textarea";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { formatDateTime } from "../../lib/time";
import { createRuleProfile, deleteRuleProfile, getRuleProfile, listRuleProfiles, updateRuleProfile } from "./api";
import type { RuleProfileSummary } from "./types";

type ToastFn = (tone: "success" | "error", text: string) => void;

type RuleProfileSectionProps = {
  showToast: ToastFn;
};

type ProfileModalMode = "create" | "edit" | null;

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

  const resetErrors = () => {
    setNameError("");
    setTemplateError("");
    setSubmitError("");
  };

  const openCreate = () => {
    modalSessionRef.current += 1;
    resetErrors();
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
    setEditingID(profile.id);
    setInitializedEditID("");
    setName("");
    setTemplateYAML("");
    setEnabled(profile.enabled);
    setModalMode("edit");
  };

  const closeModal = () => {
    if (createMutation.isPending || editMutation.isPending) {
      return;
    }
    modalSessionRef.current += 1;
    setModalMode(null);
    setEditingID("");
    setInitializedEditID("");
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
    if (templateYAML.trim() && templateYAML !== MIHOMO_EXAMPLE_TEMPLATE) {
      const confirmed = window.confirm(t("载入示例模板会覆盖当前 YAML，是否继续？"));
      if (!confirmed) {
        return;
      }
    }
    setTemplateYAML(MIHOMO_EXAMPLE_TEMPLATE);
    setTemplateError("");
    setSubmitError("");
  };

  const submitProfile = async (event: FormEvent) => {
    event.preventDefault();
    resetErrors();
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
  const editingLoading = modalMode === "edit" && (
    detailQuery.isFetching
    || initializedEditID !== editingID
  ) && !detailQuery.isError;
  const editingError = modalMode === "edit" && detailQuery.isError;

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
              <Button variant="ghost" size="sm" onClick={closeModal} aria-label={t("关闭")}>
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

                <div className="field-group">
                  <div className="rule-profile-editor-head">
                    <div>
                      <label className="field-label" htmlFor="rule-profile-template">{t("Mihomo 模板 YAML")}</label>
                      <p className="muted">
                        {t("顶层 proxies 可省略或保持为空，导出时由 Resin 替换。动态组使用 include-all-proxies/filter。")}
                      </p>
                    </div>
                    <Button variant="secondary" size="sm" onClick={loadExampleTemplate}>
                      <FileCode2 size={14} />
                      {t("载入示例模板")}
                    </Button>
                  </div>
                  <Textarea
                    id="rule-profile-template"
                    className="rule-profile-template-editor"
                    rows={24}
                    value={templateYAML}
                    invalid={Boolean(templateError || submitError)}
                    spellCheck={false}
                    onChange={(event) => {
                      setTemplateYAML(event.target.value);
                      setTemplateError("");
                      setSubmitError("");
                    }}
                  />
                  {templateError ? <p className="field-error">{templateError}</p> : null}
                </div>

                <div className="callout callout-warning rule-profile-modal-secret" role="alert">
                  <AlertTriangle size={14} />
                  <span>
                    {t("不要保存 API keys、私有订阅 URL、headers、cookies 或其他 secret；模板会完整返回给持有 export token 且知道 Profile ID 的人。")}
                  </span>
                </div>

                {submitError ? (
                  <div className="callout callout-error" role="alert">
                    <AlertTriangle size={14} />
                    <span>{submitError}</span>
                  </div>
                ) : null}

                <div className="detail-actions rule-profile-modal-actions">
                  <Button variant="ghost" onClick={closeModal} disabled={modalPending}>{t("取消")}</Button>
                  <Button type="submit" disabled={modalPending}>
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
