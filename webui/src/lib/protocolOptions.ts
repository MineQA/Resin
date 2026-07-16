// Canonical protocol options shared across node list/export filters and
// platform protocol filters. Keep values lowercase and in sync with the
// backend canonical protocol names.

export type ProtocolOption = {
  value: string;
  label: string;
};

export const PROTOCOL_OPTIONS: readonly ProtocolOption[] = [
  { value: "shadowsocks", label: "Shadowsocks" },
  { value: "vmess", label: "VMess" },
  { value: "trojan", label: "Trojan" },
  { value: "vless", label: "VLess" },
  { value: "hysteria", label: "Hysteria" },
  { value: "hysteria2", label: "Hysteria2" },
  { value: "tuic", label: "TUIC" },
  { value: "anytls", label: "AnyTLS" },
  { value: "http", label: "HTTP" },
  { value: "socks", label: "SOCKS" },
] as const;

export const PROTOCOL_VALUES: readonly string[] = PROTOCOL_OPTIONS.map((option) => option.value);

export function isKnownProtocol(value: string): boolean {
  return PROTOCOL_VALUES.includes(value);
}

// Normalize an arbitrary list of protocol strings into a deduplicated array
// of known, lowercase protocols. Unknown values are dropped so the platform
// config never sends unsupported protocols to the backend.
export function normalizeProtocolList(input: unknown): string[] {
  if (!Array.isArray(input)) {
    return [];
  }
  const seen = new Set<string>();
  const result: string[] = [];
  for (const raw of input) {
    if (typeof raw !== "string") {
      continue;
    }
    const value = raw.trim().toLowerCase();
    if (!value || seen.has(value) || !isKnownProtocol(value)) {
      continue;
    }
    seen.add(value);
    result.push(value);
  }
  return result;
}
