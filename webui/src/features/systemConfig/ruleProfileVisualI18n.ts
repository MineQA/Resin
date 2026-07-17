/**
 * UI-only localization for visual validation / fidelity / apply finding codes.
 * Does not change core English messages; maps by stable `code`.
 */

export type VisualIssueLike = {
  code?: string;
  message: string;
};

type TranslateFn = (key: string, options?: Record<string, unknown>) => string;

/** Extract first double-quoted token from a core English message. */
export function extractQuoted(message: string): string | null {
  const match = message.match(/"([^"]+)"/);
  return match?.[1] ?? null;
}

/** Extract all double-quoted tokens. */
export function extractAllQuoted(message: string): string[] {
  const out: string[] = [];
  const re = /"([^"]+)"/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(message)) != null) {
    if (m[1]) {
      out.push(m[1]);
    }
  }
  return out;
}

/** Extract leading integer count from messages like "3 raw group(s)…". */
export function extractLeadingCount(message: string): number | null {
  const match = message.match(/^(\d+)\b/);
  if (!match?.[1]) {
    return null;
  }
  const n = Number(match[1]);
  return Number.isFinite(n) ? n : null;
}

/**
 * Localize a core validation/fidelity/apply finding by stable code.
 * Falls back to a bilingual generic line that includes the raw detail.
 */
export function localizeVisualIssue(t: TranslateFn, issue: VisualIssueLike): string {
  const code = issue.code ?? "";
  const message = issue.message;
  const q = extractQuoted(message);
  const quotes = extractAllQuoted(message);
  const count = extractLeadingCount(message);

  switch (code) {
    // ── Validation errors ──────────────────────────────────────────────
    case "EMPTY_GROUP_NAME":
      return t("组名称不能为空");
    case "DUPLICATE_GROUP_NAME":
      return t("重复的组名称：{{name}}", { name: q ?? "—" });
    case "MISSING_URL_TEST_URL":
      return t("url-test 组「{{name}}」需要测速 URL", { name: q ?? "—" });
    case "URL_TEST_URL_INVALID_PROTOCOL":
      return t("url-test 组「{{name}}」的 URL 必须使用 http 或 https", { name: q ?? "—" });
    case "URL_TEST_URL_NO_HOST":
      return t("url-test 组「{{name}}」的 URL 必须包含主机名", { name: q ?? "—" });
    case "INVALID_URL_TEST_URL":
      return t("url-test 组「{{name}}」的 URL 无效", { name: q ?? "—" });
    case "INVALID_URL_TEST_INTERVAL":
      return t("url-test 组「{{name}}」的 interval 必须是正有限数", { name: q ?? "—" });
    case "INVALID_URL_TEST_TIMEOUT":
      return t("url-test 组「{{name}}」的 timeout 必须是正有限数", { name: q ?? "—" });
    case "INVALID_URL_TEST_TOLERANCE":
      return t("url-test 组「{{name}}」的 tolerance 必须是非负有限数", { name: q ?? "—" });
    case "FILTER_WITHOUT_INCLUDE_ALL":
      return t("组「{{name}}」有 filter 但未开启 include-all-proxies", { name: q ?? "—" });
    case "EMPTY_GROUP_NO_INCLUDE_ALL":
      return t("组「{{name}}」既无 proxies 成员也未开启 include-all-proxies", { name: q ?? "—" });
    case "EMPTY_PROVIDER_KEY":
      return t("Provider key 不能为空");
    case "DUPLICATE_PROVIDER_KEY":
      return t("重复的 Provider key：{{key}}", { key: q ?? "—" });
    case "EMPTY_PROVIDER_URL":
      return t("Provider「{{key}}」的 URL 不能为空", { key: q ?? "—" });
    case "PROVIDER_URL_NOT_HTTPS":
      return t("Provider「{{key}}」的 URL 必须使用 HTTPS", { key: q ?? "—" });
    case "PROVIDER_URL_NO_HOST":
      return t("Provider「{{key}}」的 URL 必须包含主机名", { key: q ?? "—" });
    case "PROVIDER_URL_HAS_USERINFO":
      return t("Provider「{{key}}」的 URL 不能包含 userinfo", { key: q ?? "—" });
    case "PROVIDER_URL_HAS_FRAGMENT":
      return t("Provider「{{key}}」的 URL 不能包含 fragment", { key: q ?? "—" });
    case "INVALID_PROVIDER_URL":
      return t("Provider「{{key}}」的 URL 无效", { key: q ?? "—" });
    case "INVALID_PROVIDER_INTERVAL":
      return t("Provider「{{key}}」的 interval 必须是正有限数", { key: q ?? "—" });
    case "EMPTY_MATCH_POLICY":
      return t("MATCH 规则的策略目标不能为空");
    case "EMPTY_RULE_SET_PROVIDER":
      return t("RULE-SET 的 provider 不能为空");
    case "EMPTY_RULE_SET_POLICY":
      return t("RULE-SET 的策略目标不能为空");
    case "EMPTY_GEOIP_CODE":
      return t("GEOIP 的国家/地区代码不能为空");
    case "EMPTY_GEOIP_POLICY":
      return t("GEOIP 的策略目标不能为空");
    case "DUPLICATE_MATCH":
      return t("存在多条 MATCH 规则");
    case "MATCH_NOT_LAST":
      return t("MATCH 必须是最后一条规则");
    case "MISSING_MATCH":
      return t("缺少 MATCH 规则；有效配置需要一条 MATCH");
    case "RULE_SET_STALE_PROVIDER":
      return t("RULE-SET 引用了未定义的 provider「{{key}}」", { key: q ?? "—" });
    case "GROUP_MEMBER_STALE_REFERENCE":
      return t("组「{{name}}」的 proxies 引用了未知成员「{{member}}」", {
        name: quotes[0] ?? "—",
        member: quotes[1] ?? "—",
      });
    case "RULE_POLICY_STALE_REFERENCE":
      return t("规则策略「{{policy}}」不匹配任何已知组名", { policy: q ?? "—" });

    // ── Fidelity / plan ────────────────────────────────────────────────
    case "BASE_PARSE_ERROR":
      return t("基础 YAML 无法解析；已禁用应用");
    case "BASE_PARSE_ERRORS":
      return t("基础 YAML 存在解析错误；已禁用应用");
    case "STALE_DRAFT":
      return t("可视化草稿已过期：基础 YAML 已变化");
    case "ROOT_NOT_MAP":
      return t("根文档不是 YAML 映射");
    case "SECTION_BLOCKED":
      return t("区段因锚点/别名或错误类型被阻断，无法可视化编辑");
    case "UNKNOWN_TOP_LEVEL_KEY":
      return t("未知顶层键「{{key}}」将原样保留", { key: q ?? "—" });
    case "RAW_GROUPS_PRESERVED":
      return t("{{count}} 个原始组将保留（可视化只读）", { count: count ?? 0 });
    case "RAW_PROVIDERS_PRESERVED":
      return t("{{count}} 个原始 Provider 将保留（可视化只读）", { count: count ?? 0 });
    case "RAW_RULES_PRESERVED":
      return t("{{count}} 条原始规则将保留（可视化只读）", { count: count ?? 0 });
    case "GROUP_UNKNOWN_FIELDS":
      return t("组「{{name}}」有 {{count}} 个未知字段将保留", {
        name: q ?? "—",
        count: extractFieldCount(message),
      });
    case "PROVIDER_UNKNOWN_FIELDS":
      return t("Provider「{{key}}」有 {{count}} 个未知字段将保留", {
        key: q ?? "—",
        count: extractFieldCount(message),
      });
    case "PROXY_PROVIDERS_YAML_ONLY":
      return t("proxy-providers 将原样保留（仅 YAML，不可可视化编辑）");
    case "PROXIES_YAML_ONLY":
      return t("顶层 proxies 将原样保留（仅 YAML）");
    case "GROUP_RENAMED_STALE_REFS":
      return t("组已从「{{from}}」重命名为「{{to}}」。引用旧名称的规则或成员不会自动更新。", {
        from: quotes[0] ?? "—",
        to: quotes[1] ?? "—",
      });
    case "PROVIDER_RENAMED_STALE_REFS":
      return t("Provider 已从「{{from}}」重命名为「{{to}}」。引用旧 key 的 RULE-SET 不会自动更新。", {
        from: quotes[0] ?? "—",
        to: quotes[1] ?? "—",
      });
    case "DELETE_COMMENTED_ITEM":
      return t("正在删除带注释的建模项；相关注释可能丢失。");
    case "RULES_REORDERED_COMMENTS":
      return t("规则已重排；与节点关联的注释会随节点保留。");
    case "SECTION_KEY_COMMENT":
      return t("区段键前有注释；将随节点保留。");
    case "REORDER_COMMENT_RISK":
      return t("区段已重排；注释随原节点保留，但项间注释关联可能偏移。");

    // ── Apply errors ───────────────────────────────────────────────────
    case "PLAN_REJECTED":
      return t("应用计划表明当前草稿无法应用");
    case "BLOCKED_SECTIONS":
      return t("存在被阻断的区段，无法应用");
    case "PARSE_ERROR":
      return t("YAML 解析失败：{{detail}}", { detail: message });
    case "YAML_PARSE_ERROR":
      return t("YAML 解析错误：{{detail}}", { detail: message });
    case "APPLY_ERROR":
      return t("应用失败：{{detail}}", { detail: message });
    case "STRINGIFY_ERROR":
      return t("序列化失败：{{detail}}", { detail: message });
    case "RAW_GROUP_MUTATION":
      return t("原始组不可增删或移动位置");
    case "RAW_PROVIDER_MUTATION":
      return t("原始 Provider 不可增删或移动位置");
    case "RAW_RULE_MUTATION":
      return t("原始规则不可增删或移动位置");

    default:
      return t("（{{code}}）{{detail}}", {
        code: code || "UNKNOWN",
        detail: message,
      });
  }
}

function extractFieldCount(message: string): number {
  const match = message.match(/has (\d+) unknown field/);
  if (match?.[1]) {
    const n = Number(match[1]);
    if (Number.isFinite(n)) {
      return n;
    }
  }
  return 0;
}
