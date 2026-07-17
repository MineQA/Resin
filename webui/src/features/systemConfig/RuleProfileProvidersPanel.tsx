import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { useI18n } from "../../i18n";
import type { ModeledProvider, ProviderItem, VisualDraft } from "./ruleProfileVisualModel";
import {
  canDeleteModeledItem,
  canMoveItemPastRawAnchors,
  createHttpProvider,
  moveItemInList,
  parseOptionalNumber,
  removeModeledById,
  updateProviderAt,
} from "./ruleProfileVisualUi";

type RuleProfileProvidersPanelProps = {
  draft: VisualDraft;
  disabled?: boolean;
  onChange: (next: VisualDraft) => void;
};

export function RuleProfileProvidersPanel({ draft, disabled, onChange }: RuleProfileProvidersPanelProps) {
  const { t } = useI18n();
  const blocked = draft.blockedSections.includes("providers");

  const setProviders = (providers: ProviderItem[]) => {
    onChange({ ...draft, providers });
  };

  const add = () => {
    if (disabled || blocked) {
      return;
    }
    setProviders([...draft.providers, createHttpProvider()]);
  };

  const move = (index: number, dir: -1 | 1) => {
    if (disabled || blocked || !canMoveItemPastRawAnchors(draft.providers, index, dir)) {
      return;
    }
    setProviders(moveItemInList(draft.providers, index, dir));
  };

  const remove = (id: string) => {
    if (disabled || blocked || !canDeleteModeledItem(draft.providers, id)) {
      return;
    }
    setProviders(removeModeledById(draft.providers, id));
  };

  const patch = (id: string, next: Partial<ModeledProvider>) => {
    if (disabled || blocked) {
      return;
    }
    setProviders(updateProviderAt(draft.providers, id, next));
  };

  return (
    <div className="rule-profile-visual-panel" role="tabpanel" id="rule-profile-tabpanel-providers" aria-labelledby="rule-profile-tab-providers">
      {blocked ? (
        <div className="callout callout-warning" role="status">
          <span>
            {t("rule-providers 区段因错误类型或锚点/别名被阻断，仅可在 YAML 中编辑。请修复后从 YAML 重新加载。")}
          </span>
        </div>
      ) : null}

      <div className="callout callout-info rule-profile-visual-note" role="note">
        <span>
          {t("仅建模 HTTP + classical + text 的 rule-providers。远程 URL 由 Mihomo 客户端拉取，Resin 不代拉。proxy-providers 始终仅 YAML 透传。")}
        </span>
      </div>

      <div className="rule-profile-visual-panel-actions">
        <Button variant="secondary" size="sm" onClick={add} disabled={disabled || blocked}>
          <Plus size={14} />
          {t("添加 HTTP provider")}
        </Button>
        <span className="muted rule-profile-visual-count">
          {t("Provider {{count}}", { count: draft.providers.length })}
        </span>
      </div>

      {draft.providers.length === 0 ? (
        <div className="empty-box rule-profile-visual-empty">
          <p>{t("暂无 rule-providers。可添加，或从 YAML / ACL4SSR 导入。")}</p>
        </div>
      ) : (
        <div className="rule-profile-visual-card-list">
          {draft.providers.map((provider, index) => (
            <ProviderCard
              key={provider.id}
              provider={provider}
              index={index}
              total={draft.providers.length}
              disabled={disabled || blocked}
              canUp={canMoveItemPastRawAnchors(draft.providers, index, -1)}
              canDown={canMoveItemPastRawAnchors(draft.providers, index, 1)}
              canDelete={provider.kind === "modeled" && canDeleteModeledItem(draft.providers, provider.id)}
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

function ProviderCard({
  provider,
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
  provider: ProviderItem;
  index: number;
  total: number;
  disabled: boolean;
  canUp: boolean;
  canDown: boolean;
  canDelete: boolean;
  onMove: (index: number, dir: -1 | 1) => void;
  onRemove: (id: string) => void;
  onPatch: (id: string, patch: Partial<ModeledProvider>) => void;
}) {
  const { t } = useI18n();

  if (provider.kind === "raw") {
    return (
      <article className="rule-profile-visual-card rule-profile-visual-card-raw">
        <div className="rule-profile-visual-card-head">
          <div className="rule-profile-visual-card-title">
            <span className="rule-profile-raw-badge">{t("仅 YAML")}</span>
            <strong>{provider.label || t("未命名 Provider")}</strong>
            <span className="muted">{provider.sourceType || "raw"}</span>
          </div>
          <span className="muted">#{index + 1}/{total}</span>
        </div>
        <p className="muted rule-profile-raw-reason">
          {provider.reason || t("不支持的 provider 形态；请在 YAML 中精确编辑。")}
        </p>
        {provider.text ? <pre className="rule-profile-raw-pre">{provider.text}</pre> : null}
      </article>
    );
  }

  return (
    <article className="rule-profile-visual-card">
      <div className="rule-profile-visual-card-head">
        <div className="rule-profile-visual-card-title">
          <span className="rule-profile-type-badge">http / classical / text</span>
          <strong>{provider.key.trim() || t("未命名 Provider")}</strong>
          {provider.unknownKeyCount > 0 ? (
            <span className="rule-profile-unknown-chip">
              {t("未知字段 {{count}}", { count: provider.unknownKeyCount })}
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
            aria-label={t("上移 Provider {{name}}", { name: provider.key || String(index + 1) })}
          >
            <ArrowUp size={15} />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="rule-profile-icon-btn"
            disabled={disabled || !canDown}
            onClick={() => onMove(index, 1)}
            aria-label={t("下移 Provider {{name}}", { name: provider.key || String(index + 1) })}
          >
            <ArrowDown size={15} />
          </Button>
          <Button
            variant="danger"
            size="sm"
            className="rule-profile-icon-btn"
            disabled={disabled || !canDelete}
            onClick={() => onRemove(provider.id)}
            title={!canDelete ? t("删除会移动后面的原始项位置，请先在 YAML 中处理原始项") : undefined}
            aria-label={
              !canDelete
                ? t("无法删除 Provider {{name}}：其后有原始项锚点", { name: provider.key || String(index + 1) })
                : t("删除 Provider {{name}}", { name: provider.key || String(index + 1) })
            }
          >
            <Trash2 size={14} />
          </Button>
        </div>
      </div>

      <div className="rule-profile-visual-form-grid">
        <div className="field-group">
          <label className="field-label" htmlFor={`${provider.id}-key`}>{t("Provider key")}</label>
          <Input
            id={`${provider.id}-key`}
            value={provider.key}
            disabled={disabled}
            spellCheck={false}
            onChange={(event) => onPatch(provider.id, { key: event.target.value })}
          />
        </div>
        <div className="field-group">
          <label className="field-label" htmlFor={`${provider.id}-interval`}>{t("interval（秒）")}</label>
          <Input
            id={`${provider.id}-interval`}
            inputMode="numeric"
            value={provider.interval}
            disabled={disabled}
            onChange={(event) => {
              const n = parseOptionalNumber(event.target.value);
              onPatch(provider.id, { interval: n ?? 0 });
            }}
          />
        </div>
      </div>

      <div className="field-group">
        <label className="field-label" htmlFor={`${provider.id}-url`}>{t("HTTPS URL")}</label>
        <Input
          id={`${provider.id}-url`}
          value={provider.url}
          disabled={disabled}
          spellCheck={false}
          onChange={(event) => onPatch(provider.id, { url: event.target.value })}
        />
        <p className="muted rule-profile-field-hint">
          {t("须为绝对 HTTPS URL；不要放入含 secret 的私有地址。客户端而非 Resin 会访问此 URL。")}
        </p>
      </div>
    </article>
  );
}
