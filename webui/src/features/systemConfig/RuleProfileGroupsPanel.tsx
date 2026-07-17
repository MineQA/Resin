import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { Select } from "../../components/ui/Select";
import { Switch } from "../../components/ui/Switch";
import { Textarea } from "../../components/ui/Textarea";
import { useI18n } from "../../i18n";
import type { GroupItem, ModeledGroup, VisualDraft } from "./ruleProfileVisualModel";
import {
  canDeleteModeledItem,
  canMoveItemPastRawAnchors,
  createSelectGroup,
  createUrlTestGroup,
  linesToMembers,
  membersToLines,
  moveItemInList,
  parseOptionalNumber,
  removeModeledById,
  updateGroupAt,
} from "./ruleProfileVisualUi";

type RuleProfileGroupsPanelProps = {
  draft: VisualDraft;
  disabled?: boolean;
  onChange: (next: VisualDraft) => void;
};

export function RuleProfileGroupsPanel({ draft, disabled, onChange }: RuleProfileGroupsPanelProps) {
  const { t } = useI18n();
  const blocked = draft.blockedSections.includes("groups");

  const setGroups = (groups: GroupItem[]) => {
    onChange({ ...draft, groups });
  };

  const addGroup = (type: "select" | "url-test") => {
    if (disabled || blocked) {
      return;
    }
    const item = type === "select" ? createSelectGroup() : createUrlTestGroup();
    setGroups([...draft.groups, item]);
  };

  const move = (index: number, dir: -1 | 1) => {
    if (disabled || blocked || !canMoveItemPastRawAnchors(draft.groups, index, dir)) {
      return;
    }
    setGroups(moveItemInList(draft.groups, index, dir));
  };

  const remove = (id: string) => {
    if (disabled || blocked || !canDeleteModeledItem(draft.groups, id)) {
      return;
    }
    setGroups(removeModeledById(draft.groups, id));
  };

  const patch = (id: string, next: Partial<ModeledGroup>) => {
    if (disabled || blocked) {
      return;
    }
    setGroups(updateGroupAt(draft.groups, id, next));
  };

  return (
    <div className="rule-profile-visual-panel" role="tabpanel" id="rule-profile-tabpanel-groups" aria-labelledby="rule-profile-tab-groups">
      {blocked ? (
        <div className="callout callout-warning" role="status">
          <span>
            {t("proxy-groups 区段因错误类型或锚点/别名被阻断，仅可在 YAML 中编辑。请修复后从 YAML 重新加载。")}
          </span>
        </div>
      ) : null}

      <div className="callout callout-info rule-profile-visual-note" role="note">
        <span>
          {t("顶层 proxies 由 Resin 导出时替换。动态节点请用 include-all-proxies 与 filter；不要手写池内动态节点名。原始/不支持组类型仅 YAML 可编辑。")}
        </span>
      </div>

      <div className="rule-profile-visual-panel-actions">
        <Button variant="secondary" size="sm" onClick={() => addGroup("select")} disabled={disabled || blocked}>
          <Plus size={14} />
          {t("添加 select 组")}
        </Button>
        <Button variant="secondary" size="sm" onClick={() => addGroup("url-test")} disabled={disabled || blocked}>
          <Plus size={14} />
          {t("添加 url-test 组")}
        </Button>
        <span className="muted rule-profile-visual-count">
          {t("组 {{count}}", { count: draft.groups.length })}
        </span>
      </div>

      {draft.groups.length === 0 ? (
        <div className="empty-box rule-profile-visual-empty">
          <p>{t("暂无 proxy-groups。可添加，或从 YAML / ACL4SSR 导入。")}</p>
        </div>
      ) : (
        <div className="rule-profile-visual-card-list">
          {draft.groups.map((group, index) => (
            <GroupCard
              key={group.id}
              group={group}
              index={index}
              total={draft.groups.length}
              disabled={disabled || blocked}
              canUp={canMoveItemPastRawAnchors(draft.groups, index, -1)}
              canDown={canMoveItemPastRawAnchors(draft.groups, index, 1)}
              canDelete={group.kind === "modeled" && canDeleteModeledItem(draft.groups, group.id)}
              onMove={move}
              onRemove={remove}
              onPatch={patch}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function GroupCard({
  group,
  index,
  total,
  disabled,
  canUp,
  canDown,
  canDelete,
  onMove,
  onRemove,
  onPatch,
}: {
  group: GroupItem;
  index: number;
  total: number;
  disabled: boolean;
  canUp: boolean;
  canDown: boolean;
  canDelete: boolean;
  onMove: (index: number, dir: -1 | 1) => void;
  onRemove: (id: string) => void;
  onPatch: (id: string, patch: Partial<ModeledGroup>) => void;
}) {
  const { t } = useI18n();

  if (group.kind === "raw") {
    return (
      <article className="rule-profile-visual-card rule-profile-visual-card-raw">
        <div className="rule-profile-visual-card-head">
          <div className="rule-profile-visual-card-title">
            <span className="rule-profile-raw-badge">{t("仅 YAML")}</span>
            <strong>{group.label || t("未命名组")}</strong>
            <span className="muted">{group.sourceType || "raw"}</span>
          </div>
          <span className="muted">#{index + 1}/{total}</span>
        </div>
        <p className="muted rule-profile-raw-reason">{group.reason || t("不支持的组类型或结构；请在 YAML 中精确编辑。")}</p>
        {group.text ? (
          <pre className="rule-profile-raw-pre">{group.text}</pre>
        ) : null}
      </article>
    );
  }

  return (
    <article className="rule-profile-visual-card">
      <div className="rule-profile-visual-card-head">
        <div className="rule-profile-visual-card-title">
          <span className="rule-profile-type-badge">{group.type}</span>
          <strong>{group.name.trim() || t("未命名组")}</strong>
          {group.unknownKeyCount > 0 ? (
            <span className="rule-profile-unknown-chip">
              {t("未知字段 {{count}}", { count: group.unknownKeyCount })}
            </span>
          ) : null}
        </div>
        <div className="rule-profile-order-btns">
          <Button
            variant="ghost"
            size="sm"
            className="rule-profile-icon-btn"
            disabled={disabled || !canUp}
            onClick={() => onMove(index, -1)}
            aria-label={t("上移组 {{name}}", { name: group.name || String(index + 1) })}
          >
            <ArrowUp size={15} />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="rule-profile-icon-btn"
            disabled={disabled || !canDown}
            onClick={() => onMove(index, 1)}
            aria-label={t("下移组 {{name}}", { name: group.name || String(index + 1) })}
          >
            <ArrowDown size={15} />
          </Button>
          <Button
            variant="danger"
            size="sm"
            className="rule-profile-icon-btn"
            disabled={disabled || !canDelete}
            onClick={() => onRemove(group.id)}
            title={!canDelete ? t("删除会移动后面的原始项位置，请先在 YAML 中处理原始项") : undefined}
            aria-label={
              !canDelete
                ? t("无法删除组 {{name}}：其后有原始项锚点", { name: group.name || String(index + 1) })
                : t("删除组 {{name}}", { name: group.name || String(index + 1) })
            }
          >
            <Trash2 size={14} />
          </Button>
        </div>
      </div>

      <div className="rule-profile-visual-form-grid">
        <div className="field-group">
          <label className="field-label" htmlFor={`${group.id}-name`}>{t("名称")}</label>
          <Input
            id={`${group.id}-name`}
            value={group.name}
            disabled={disabled}
            onChange={(event) => onPatch(group.id, { name: event.target.value })}
          />
        </div>
        <div className="field-group">
          <label className="field-label" htmlFor={`${group.id}-type`}>{t("类型")}</label>
          <Select
            id={`${group.id}-type`}
            value={group.type}
            disabled={disabled}
            onChange={(event) => {
              const type = event.target.value === "url-test" ? "url-test" : "select";
              if (type === "select") {
                onPatch(group.id, {
                  type,
                  url: null,
                  interval: null,
                  timeout: null,
                  tolerance: null,
                });
              } else {
                onPatch(group.id, {
                  type,
                  url: group.url || "https://www.gstatic.com/generate_204",
                  interval: group.interval ?? 300,
                });
              }
            }}
          >
            <option value="select">select</option>
            <option value="url-test">url-test</option>
          </Select>
        </div>
      </div>

      <div className="rule-profile-enabled-field rule-profile-visual-switch-row">
        <div>
          <span className="field-label">{t("include-all-proxies")}</span>
          <small>{t("与 proxies 成员列表相互独立；可同时存在。")}</small>
        </div>
        <Switch
          checked={group.includeAllProxies}
          disabled={disabled}
          onChange={(event) => onPatch(group.id, { includeAllProxies: event.target.checked })}
          aria-label={t("include-all-proxies")}
        />
      </div>

      <div className="field-group">
        <label className="field-label" htmlFor={`${group.id}-filter`}>{t("filter（可选）")}</label>
        <Input
          id={`${group.id}-filter`}
          value={group.filter ?? ""}
          disabled={disabled}
          spellCheck={false}
          placeholder={t("留空表示省略 filter")}
          onChange={(event) => {
            const value = event.target.value;
            onPatch(group.id, { filter: value.trim() === "" ? null : value });
          }}
        />
      </div>

      <div className="field-group">
        <label className="field-label" htmlFor={`${group.id}-members`}>{t("proxies 成员（每行一个）")}</label>
        <Textarea
          id={`${group.id}-members`}
          className="rule-profile-members-editor"
          rows={4}
          value={membersToLines(group.proxies)}
          disabled={disabled}
          spellCheck={false}
          placeholder={t("组名 / DIRECT / REJECT …")}
          onChange={(event) => {
            const lines = linesToMembers(event.target.value);
            // Empty textarea → empty array (present), not null, so user can clear members explicitly.
            onPatch(group.id, { proxies: lines });
          }}
        />
      </div>

      {group.type === "url-test" ? (
        <div className="rule-profile-visual-form-grid rule-profile-visual-form-grid-4">
          <div className="field-group">
            <label className="field-label" htmlFor={`${group.id}-url`}>{t("测速 URL")}</label>
            <Input
              id={`${group.id}-url`}
              value={group.url ?? ""}
              disabled={disabled}
              spellCheck={false}
              onChange={(event) => onPatch(group.id, { url: event.target.value || null })}
            />
          </div>
          <div className="field-group">
            <label className="field-label" htmlFor={`${group.id}-interval`}>{t("interval")}</label>
            <Input
              id={`${group.id}-interval`}
              inputMode="numeric"
              value={group.interval ?? ""}
              disabled={disabled}
              onChange={(event) => onPatch(group.id, { interval: parseOptionalNumber(event.target.value) })}
            />
          </div>
          <div className="field-group">
            <label className="field-label" htmlFor={`${group.id}-timeout`}>{t("timeout")}</label>
            <Input
              id={`${group.id}-timeout`}
              inputMode="numeric"
              value={group.timeout ?? ""}
              disabled={disabled}
              onChange={(event) => onPatch(group.id, { timeout: parseOptionalNumber(event.target.value) })}
            />
          </div>
          <div className="field-group">
            <label className="field-label" htmlFor={`${group.id}-tolerance`}>{t("tolerance")}</label>
            <Input
              id={`${group.id}-tolerance`}
              inputMode="numeric"
              value={group.tolerance ?? ""}
              disabled={disabled}
              onChange={(event) => onPatch(group.id, { tolerance: parseOptionalNumber(event.target.value) })}
            />
          </div>
        </div>
      ) : null}
    </article>
  );
}
