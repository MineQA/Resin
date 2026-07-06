# AGENTS.md

## Commands that are easy to guess wrong
- Go requires 1.25.x (`go.mod`) and common builds/tests should use sing-box feature tags: `with_quic with_wireguard with_grpc with_utls`.
- Build the embedded WebUI before building the binary: `cd webui && npm ci && npm run build`, then from repo root `go build -trimpath -tags "with_quic with_wireguard with_grpc with_utls" -o resin ./cmd/resin`.
- Release builds add `with_embedded_tor with_naive_outbound` and ldflags for `internal/buildinfo` (see `.github/workflows/release.yml`).
- Run all Go tests with tags: `go test -tags "with_quic with_wireguard with_grpc with_utls" ./...`.
- Run focused Go tests the same way, for example: `go test -tags "with_quic with_wireguard with_grpc with_utls" ./internal/routing/...` or `go test -tags "with_quic with_wireguard with_grpc with_utls" ./cmd/resin -run TestName`.
- Frontend commands live under `webui/`: `npm run dev`, `npm run build` (`tsc -b && vite build`), and `npm run lint`.

## Runtime/setup gotchas
- Source/binary runs load `.env` from the current working directory before config; existing OS/shell env vars win over `.env` values.
- `RESIN_AUTH_VERSION`, `RESIN_ADMIN_TOKEN`, and `RESIN_PROXY_TOKEN` must be defined; empty admin/proxy tokens are allowed only when explicitly set. `.env.example` contains the minimal local shape.
- `RESIN_AUTH_VERSION=V1` enables SOCKS5. `LEGACY_V0` is compatibility mode and disables SOCKS5.
- V1 proxy tokens reject punctuation such as `. : | / \ @ ? # % ~` and whitespace; `api`, `healthz`, and `ui` are reserved proxy-token values.
- The service is intentionally single-port (default `2260`): TCP first-byte sniff routes `0x05` to SOCKS5 and everything else to HTTP for API, WebUI, HTTP forward proxy, or reverse proxy.
- Local state is SQLite only (`state.db`, `cache.db`, `metrics.db`, rolling request logs); no Redis/MySQL/Kafka services are expected.

## Architecture map
- Entrypoint is `cmd/resin/main.go` -> `run()` in `cmd/resin/app_runtime.go`; `newResinApp()` wires config, persistence, topology, router, observability, and network servers.
- `cmd/resin/inbound_demux.go` owns single-port SOCKS5-vs-HTTP sniffing; `cmd/resin/inbound_mux.go` routes HTTP between API, WebUI, forward proxy, and reverse proxy.
- `internal/config` separates static env config from hot-updatable runtime config. Prefer `internal/config/env.go` and `runtime.go` over README prose for exact config behavior.
- `internal/state` is the single-writer persistence layer and owns migrations under `internal/state/migrations`.
- `internal/topology`, `internal/probe`, `internal/node`, and `internal/routing` are the node pool, health/probe, latency/hash, P2C routing, and lease machinery.
- `internal/proxy` contains forward/reverse/SOCKS proxy logic; `internal/api` plus `internal/service` are the control-plane HTTP layer and business logic.
- `webui/` is a React 19 + Vite SPA; `webui/embed.go` uses `//go:embed dist`, so missing `webui/dist` breaks normal embedded binary builds.

## Repo-specific behavior to preserve
- Default `Platform` is auto-created and must not be deleted.
- New nodes start circuit-open/fused and become routable only after successful probes.
- Node identity is based on canonical outbound options with tag stripped; dedup across subscriptions is expected.
- Hot-path routing uses precomputed platform routable views; avoid adding cold-path filtering work to request routing.
- Node pool exports are public-control endpoints protected by WebUI-managed export tokens, not `RESIN_ADMIN_TOKEN`. Keep `/api/v1/node-pool/export` converter-friendly: default `format=clash`, support `format=base64|uri|sing-box`, and prefer `?export_token=<token>` for sub-web/subconverter integrations because converter backends usually do not forward custom headers or User-Agent.
- Export token auth supports `Authorization: Bearer <token>`, `?export_token=<token>`, and only the `User-Agent: ResinExport/<token>` prefix form; do not accept arbitrary User-Agent values as tokens.
- Node export filtering reuses node list filters: `platform_id`, `subscription_id`, `region`, `egress_ip`, `tag_keyword`, `circuit_open`, `has_outbound`, `enabled`, `routable`, `probed_since`, `limit`, `offset`. Missing `routable` means no routable-only filter.
- `DESIGN.md` is the detailed architecture source (Chinese); README is bilingual and less authoritative than executable config/tests when they conflict.
- Root `Dockerfile` builds from source; release GHCR images use `.github/Dockerfile.release` with prebuilt Linux binaries.
