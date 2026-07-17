<div align="center">
  <img src="webui/public/vite.svg" width="48" alt="Resin Logo" />
  <h1>Resin</h1>
  <p><strong>将大量的代理订阅转化为一个稳定、智能、可观测且支持会话保持的统一代理入口。</strong></p>
</div>

<p align="center">
  <a href="https://github.com/Resinat/Resin/releases"><img src="https://img.shields.io/github/v/release/Resinat/Resin?style=flat-square&label=release&sort=semver" alt="Release" /></a>
  <a href="https://github.com/Resinat/Resin/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/Resinat/Resin/release.yml?style=flat-square&label=release%20pipeline" alt="Release Pipeline" /></a>
  <a href="https://github.com/Resinat/Resin/pkgs/container/resin"><img src="https://img.shields.io/badge/ghcr-ghcr.io%2Fresinat%2Fresin-2496ED?style=flat-square&logo=docker&logoColor=white" alt="GHCR Image" /></a>
  <a href="https://github.com/Resinat/Resin/blob/master/LICENSE"><img src="https://img.shields.io/github/license/Resinat/Resin?style=flat-square" alt="License" /></a>
  <a href="https://github.com/Resinat/Resin/blob/master/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/Resinat/Resin?style=flat-square" alt="Go Version" /></a>
  <a href="https://github.com/Resinat/Resin/releases"><img src="https://img.shields.io/badge/support-linux%20%7C%20macOS%20%7C%20windows%20%C2%B7%20amd64%20%7C%20arm64-0A7EA4?style=flat-square" alt="Support Matrix" /></a>
  <a href="DESIGN.md"><img src="https://img.shields.io/badge/docs-DESIGN.md-1F6FEB?style=flat-square" alt="Design Docs" /></a>
</p>

---

**Resin** 是一个专为接管海量节点设计的**高性能智能代理池网关**。

它用于在上层屏蔽底层代理节点的不稳定性，将分散节点聚合为一个支持 **“会话保持（粘性路由）”** 的统一代理入口。

## 💡 为什么选择 Resin？

- **海量接管**：轻松管理十万级规模的代理节点。高性能，原生支持高并发。
- **智能调度与熔断**：全自动的 **被动+主动** 健康探测、出口 IP 探测、延迟分析以及响应感知 Cloudflare 观测，精准剔除坏节点。采用 P2C 算法结合按域名的延迟加权评分实现最优节点选择。响应感知 Cloudflare 观测与可选的五维质量检查（服务可用率、API 可用率、Cloudflare 状态、延迟、稳定性）提供丰富的质量可见性，并支持平台级质量筛选。
- **业务友好的粘性代理**：让同一业务账号优先绑定同一出口 IP，节点异常时自动切换同 IP 节点，在多数场景下减少业务波动。
- **多种接入方式**：同时支持 HTTP 正向代理、SOCKS5 正向代理与 URL 反向代理，适配不同客户端与集成形态。
- **可观测性**：提供详细的性能指标与日志记录，快速掌控全局（可视化 Web 管理后台）。包括完整的结构化请求日志，支持按平台、账号、目标站点等维度查询与审计。
- **简单与强大兼得**：开箱即用的默认配置与深度自定义功能。无论你是只需几分钟跑通简单场景的个人使用者，还是需要高并发与高可用性的企业级团队，Resin 都能游刃有余。
- **跨订阅智能去重**：不同订阅中配置相同的节点自动合并，共享健康状态，避免重复探测。
- **热更新**：更新常用配置不用重启，更新订阅节点不断连。
- **状态持久化**：重启后仍可恢复节点健康数据、延迟统计与租约绑定，便于生产环境连续运行与故障恢复。
- **零侵入粘性接入**：支持从业务原有请求头（如 API Key）自动提取账号身份，在常见接入方式下可尽量减少代码改动。
- **订阅热更新**：节点订阅刷新时增量同步，不中断现有连接。
- **灵活的节点隔离**：通过 Platform 概念，按正则表达式、地区、协议、质量等级/评分/Cloudflare 状态等规则筛选节点，为不同业务构建独立的代理池。


> [!TIP]
> 您可以把本文档与项目详细设计文档 [`DESIGN.md`](DESIGN.md) 丢给 AI，然后问它任何你感兴趣的问题！


![](doc/images/dashboard_zh-cn.png)

---

## 🔌 支持协议与订阅格式

### 订阅来源

- 远程订阅 URL：`http://` 或 `https://`。
- 本地订阅内容：在 UI/API 中直接粘贴订阅内容。

### 订阅内容格式

- sing-box JSON：`{"outbounds":[...]}` 或原始出站数组 `[...]`。
- Clash JSON/YAML：`{"proxies":[...]}` 或 YAML `proxies:`。
- URI 行格式（每行一个节点）：`vmess://`、`vless://`、`trojan://`、`ss://`、`hysteria2://`、`http://`、`https://`、`socks5://`、`socks5h://`。
  其中 `http://`、`https://`、`socks5://`、`socks5h://` 需使用 `scheme://[user:pass@]host:port` 形式（可选 `#tag`；`https` 额外支持 `sni`/`servername`/`peer` 与 `allowInsecure`/`insecure` 查询参数）。
- 纯 HTTP 代理行：`IP:PORT` 或 `IP:PORT:USER:PASS`（支持 IPv4 和 IPv6）。
- Base64 包裹的文本订阅（例如 URI 行或纯文本节点列表）。

### 支持的出站节点协议类型

- 对于 sing-box JSON/原始 outbounds：`socks`、`http`、`shadowsocks`、`vmess`、`trojan`、`wireguard`、`hysteria`、`vless`、`shadowtls`、`tuic`、`hysteria2`、`anytls`、`ssh`。
- 对于 Clash 转换：`ss`/`shadowsocks`、`socks`/`socks4`/`socks4a`/`socks5`、`http`、`vmess`、`vless`、`trojan`、`wireguard`/`wg`、`hysteria`、`hysteria2`/`hy2`、`tuic`、`anytls`、`ssh`。

### 导出节点池

Resin 可以把已管理的节点导出为订阅源，供 sub-web-modify/subconverter 等工具使用。

1. 在 WebUI 的**系统设置**中创建导出令牌。
2. 到**节点池**页面，在节点导出区域填写/保存该导出令牌。
3. 在令牌附近选择导出选项，然后复制导出 URL 或下载生成文件。

支持的导出格式：

- `clash`（默认）：顶层为 `proxies:` 的 Clash YAML，推荐作为转换器输入。
- `base64`：Base64 包裹的 URI 行。
- `uri`：明文 URI 行。
- `sing-box`：裸 sing-box JSON，例如 `{"outbounds":[...]}`。

导出地址为 `GET /api/v1/node-pool/export`。它不使用管理后台 token，而是使用导出令牌鉴权，支持以下方式：

- `?export_token=<token>`（推荐给转换器后端使用）
- `Authorization: Bearer <token>`
- `User-Agent: ResinExport/<token>`

转换器后端拉取订阅源时通常不会透传自定义 Header 或 User-Agent，因此推荐使用 query token URL，例如：

```text
http://127.0.0.1:2260/api/v1/node-pool/export?format=clash&export_token=<token>
```

支持的导出筛选参数与节点列表一致：`platform_id`、`subscription_id`、`region`、`egress_ip`、`tag_keyword`、`circuit_open=true|false`、`has_outbound=true|false`、`enabled=true|false`、`routable=true|false`、`probed_since=<RFC3339 时间>`、`quality_profile`、`quality_grade`、`quality_min_score`、`quality_cloudflare_challenged=true|false`、`quality_cloudflare_status`（可重复，OR 语义）、`quality_checked_since=<RFC3339 时间>`、`limit`、`offset`。如果不传 `routable`，不会默认只导出可路由节点。

### 规则模板（Rule Profile / Mihomo YAML 模板）

规则模板（Rule Profile）是在**系统设置**中管理的持久化、命名的完整 Mihomo YAML 配置。它将 Resin 的节点池封装为完整的 Mihomo 配置，包含你自定义的代理组、规则以及其他高级设置。

**Mihomo 优先：** Resin 以 Mihomo（而非旧版 Clash）为目标。模板使用标准 Mihomo YAML — 不含 Resin DSL、占位符或自定义指令。未知的 Mihomo key 会被透传；Resin 不会完整校验组引用、DAG 完整性或 provider 可达性。

<details>
<summary><b>创建与管理规则模板</b></summary>

- **管理 API（admin-auth）：** `GET /api/v1/rule-profiles` 列出所有模板（仅摘要），`POST /api/v1/rule-profiles` 创建新模板，`GET /api/v1/rule-profiles/{id}`、`PATCH /api/v1/rule-profiles/{id}`、`DELETE /api/v1/rule-profiles/{id}`。
- **Web UI：** 在**系统设置** → **规则模板**中创建、编辑和删除模板。
- **导出区域：** 在**节点池**页面，当导出格式为 `clash` 时会出现规则模板选择器。
</details>

<details>
<summary><b>使用规则模板的导出 URL</b></summary>

在导出 URL 中添加 `&rule_profile_id=<不可变 UUID>`。此参数仅对 `format=clash` 有效；与 `base64`、`uri` 或 `sing-box` 格式共用将返回 400 错误。

```text
http://127.0.0.1:2260/api/v1/node-pool/export?format=clash&rule_profile_id=a1b2c3d4-e5f6-7890-abcd-ef1234567890&export_token=<token>
```

Resin 会将模板顶层的 `proxies:` 字段替换为经筛选和转换后的节点列表。模板**不能**声明非空的 `proxies:` — 可以省略、设为 `null` 或空数组 `[]`。所有代理组名称、provider URL 和规则均来自模板。

不传 `rule_profile_id` 时，`format=clash` 仍然只返回顶层的 `proxies:` 列表，适合作为 subconverter 等工具的转换器输入。`base64`、`uri` 和 `sing-box` 保持原有输出，不使用规则模板。
</details>

<details>
<summary><b>节点命名约定</b></summary>

当应用规则模板时，节点名称遵循以下规则以确保一致性：

- **地区标签：** 实际分配的地区会被规范到最终路径叶子的开头（例如 `[US] Node Name` 或 `provider/[HK] Node Name`）。如果地区未知或无效，则使用 `[??]`，并去除原叶子中可能存在的误导性国家标记。
- **冲突处理：** 只有发生同名冲突的名称组会追加稳定的节点哈希后缀；未冲突名称保持不变。
- **不支持的出站：** 无法转换为 Clash 格式的代理类型会被静默跳过；模板中的组仅引用最终的 `proxies:` 列表。
</details>

<details>
<summary><b>模板验证合同</b></summary>

保存规则模板前，Resin 会执行以下基本验证：

| 规则 | 说明 |
| :--- | :--- |
| 单一 YAML 文档 | 模板必须是单个 YAML 文档（顶层 mapping）。 |
| 必须包含 `rules` | `rules` key 必须存在。最后一条规则必须是 `MATCH,<目标>`。 |
| `proxy-group` 名称 | 所有组名称必须非空且唯一。 |
| `http` provider URL | 必须是绝对 HTTPS URL。 |
| 允许未知 key | Resin 不会剥离或拒绝未知的 Mihomo YAML key。 |
| 不验证引用 | Resin 不检查组引用、DAG 循环或 provider 可达性。 |

> **Provider 拉取：** Resin 永远不会主动访问模板中 `proxy-providers` 或 `rule-providers` 声明的 URL。Mihomo 客户端会直接下载它们。任意的 HTTPS provider URL 会导致 Mihomo 客户端访问对应地址 — 你需要自行信任这些 URL 并确保网络安全。
</details>

<details>
<summary><b>错误语义</b></summary>

| 场景 | HTTP 状态码 | 错误码 |
| :--- | :--- | :--- |
| `rule_profile_id` 不是有效 UUID | 400 | `INVALID_ARGUMENT` |
| `rule_profile_id` 与非 `clash` 格式共用 | 400 | `INVALID_ARGUMENT` |
| 模板不存在、已删除或已禁用 | 404 | `RULE_PROFILE_UNAVAILABLE` |

- 当请求了规则模板但无法获取时，Resin **绝不会**回退为仅 proxies 的响应。删除或禁用模板会导致旧的导出 URL 全部失效。
- 对于不存在、已删除和已禁用的模板，统一返回 404，避免泄漏存在性信息。
</details>

<details>
<summary><b>最小模板示例</b></summary>

```yaml
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
    filter: "(?:^|/)\\[US\\](?: [^/]*|)$"

  - name: PROXY
    type: select
    proxies:
      - AUTO
      - MANUAL
      - US
      - DIRECT

rules:
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
```

说明：
- `include-all-proxies: true` 动态组会自动包含 `proxies:` 列表中的所有节点。使用 `filter` 配合正则表达式匹配最终路径叶子以地区 marker 开头的节点名（示例筛选 `[US]`）。
- `AUTO`、`MANUAL` 和 `US` 是模板中定义的策略组，不是 Resin 注入的代理节点。
- 地区组可能在没有节点匹配筛选条件时为空 — 请根据你的节点池调整。
- 不要强制填写 `proxy-providers`；让 Resin 通过 `proxies:` 直接管理节点池。
- 服务端仅要求最后一条 `MATCH,<target>` 规则中的 target 非空。示例使用 `MATCH,PROXY`，让未匹配流量继续走代理策略，而不是静默直连。
- 如果 provider URL 首次拉取不可达且无本地缓存，该 provider 的规则无法匹配，规则评估会继续到后续规则（包括最终 `MATCH`）。请谨慎设计最终 target 和各策略组的 fallback 行为。
</details>

> **分页与 `limit`：** 分页（`limit`/`offset`）在 Clash 转换前应用。不支持的节点类型会计入 `limit` 槽位，因此最终的 `proxies:` 列表可能少于请求数量。
>
> **与 subconverter 的兼容性：** 如果你更偏好 subconverter 工作流，可继续使用纯 proxies 的导出 URL（不传 `rule_profile_id`）作为转换器输入。Resin 不自带内置 ACL4SSR 预设。

## 🚀 Quick Start

只需简单三步，即可将你的节点订阅转化为高可用代理池。

### 第一步：一键部署启动
推荐使用 Docker Compose 快速启动：

```yaml
# docker-compose.yml
services:
  resin:
    image: ghcr.io/resinat/resin:latest
    container_name: resin
    restart: unless-stopped
    environment:
      RESIN_AUTH_VERSION: "V1" # 必填：LEGACY_V0 或 V1
      RESIN_ADMIN_TOKEN: "admin123" # 修改为你的管理后台密码
      RESIN_PROXY_TOKEN: "my-token" # 修改为你的代理密码
      RESIN_LISTEN_ADDRESS: 0.0.0.0
      RESIN_PORT: 2260
    ports:
      - "2260:2260"
    volumes:
      - ./data/cache:/var/cache/resin
      - ./data/state:/var/lib/resin
      - ./data/log:/var/log/resin
```
运行 `docker compose up -d` 启动服务。

*(如果你不想使用 Docker，请跳转文末查看 [其他部署方式](#其他部署方式))*

### 第二步：导入代理节点
1. 浏览器打开 `http://127.0.0.1:2260`（请替换为你的服务器 IP）。
2. 输入刚才设置的 `RESIN_ADMIN_TOKEN` 登录后台。
3. 在左侧菜单找到 **「订阅管理」**，添加你的节点订阅。
4. 稍等片刻，等待节点池刷新出你的节点。

### 第三步：开始你的代理请求
客户端接入方式参考接下来的章节。常见场景下，你可以按客户端能力选择 HTTP 正向代理、SOCKS5 正向代理或反向代理。

## 🟢 基础使用（非粘性代理）

### 简单接入代理
如果你只需要一个高性能、大容量、且会自动健康管理的通用代理池，Resin 开箱即用。
启动 Resin 服务后，你可以按客户端能力选择 HTTP 正向代理、SOCKS5 正向代理或反向代理接入。  
如果你不想设置代理密码，请将环境变量显式设为空字符串：`RESIN_PROXY_TOKEN=""`（变量必须定义）。此时 HTTP 正向代理可直接接入 `http://127.0.0.1:2260`，SOCKS5 正向代理可直接接入 `socks5://127.0.0.1:2260`。
通过二进制文件或源码运行时，Resin 也会在读取配置前自动加载当前工作目录下的 `.env` 文件；已经由系统或 shell 设置的环境变量优先级高于 `.env`。

HTTP 正向代理例子：


```bash
curl -x http://127.0.0.1:2260 \
  -U ":my-token" \
  https://api.ipify.org
```

SOCKS5 正向代理例子（仅在 `RESIN_AUTH_VERSION=V1` 时可用）：

```bash
curl --proxy socks5h://127.0.0.1:2260 \
  -U "Default:my-token" \
  https://api.ipify.org
```

如果你当前仍运行在 `LEGACY_V0`，SOCKS5 入站不会启用；请继续使用 HTTP 正向代理，或先完成迁移后再切换到 `V1`。当 `RESIN_PROXY_TOKEN=""` 时，SOCKS5 也允许无认证接入。

如果你的客户端支持修改服务的 `BASE_URL`，你也可以尝试反向代理接入。URL 格式为：`/令牌/Platform(可选).Account(可选)/协议/目标地址`。例如，你可以通过下面的 curl 命令通过 Resin 访问 `https://api.ipify.org`。

```bash
curl http://127.0.0.1:2260/my-token/./https/api.ipify.org
```

> 正向代理与反向代理的选择：如果条件允许，推荐尽量使用反向代理，对于可观测性更友好。如果您的客户端不支持修改 BaseURL，或者客户端需要 utls、非纯 WebAPI 请求这种反向代理不擅长的情况，请使用正向代理。

### 筛选节点
如果你的服务对节点有筛选要求，例如只需要某个地区的节点，或者只需要来自某个订阅源的节点，或者只需要名字匹配特定正则表达式的节点，可以使用 Resin 的 Platform 概念来实现。

你可以打开 `http://127.0.0.1:2260/ui/platforms` Platform 管理页面，创建一个 Platform。例如希望只使用来自美国、香港的节点，你可以创建一个名为 “MyPlatform” 的 Platform，然后在地区过滤规则中填入：
```
us
hk
```

对于正向代理（HTTP / SOCKS5），你可以在认证信息中填入希望使用的 Platform。下面分别给出一个 curl 例子：

```bash
curl -x http://127.0.0.1:2260 \
  -U "MyPlatform:my-token" \
  https://api.ipify.org
```

```bash
curl --proxy socks5h://127.0.0.1:2260 \
  -U "MyPlatform:my-token" \
  https://api.ipify.org
```

对于反向代理，你可以在 URL 前缀中提供 Platform 信息。下面是一个使用 curl 的例子：

```
curl http://127.0.0.1:2260/my-token/MyPlatform/https/api.ipify.org
```

## 📖 进阶使用：粘性代理

当业务遇到**对 IP 变化敏感**的服务，或者需要持续交互时，你需要使用 Resin 的核心特性：**粘性代理**。

在此之前，先了解两个概念：

### 🎯 核心概念：平台 (Platform) 与 账号 (Account)
- **平台 (Platform)**：节点的隔离池。你可以通过规则筛选节点（例如只使用“美国”节点）组建成一个专有池。系统默认存在一个装载所有可用节点的 `Default` 平台。
- **账号 (Account)**：业务侧的唯一标识（如 `Tom` 或 `user_1`）。携带特定 Account 的请求，Resin 会优先为其分配稳定的出口节点；当节点不可用时，会重试并优先切换到同 IP 节点，以降低业务侧适配成本。

### 粘性代理接入格式

#### 方式一：正向代理接入（HTTP Proxy / SOCKS5）
当 `RESIN_AUTH_VERSION=V1` 时，HTTP 正向代理与 SOCKS5 正向代理共用同一套身份格式：`Platform.Account:RESIN_PROXY_TOKEN`。  

> 如需 V0 旧格式，可设置 `RESIN_AUTH_VERSION=LEGACY_V0`，继续使用 `RESIN_PROXY_TOKEN:Platform:Account`。但该模式下不会启用 SOCKS5 正向代理。  

直接将身份信息写入代理认证中即可：

```bash
# HTTP 正向代理：指定业务账号 user_tom，Resin 会为其长期分配一个稳定的专属 IP
curl -x http://127.0.0.1:2260 \
  -U "Default.user_tom:my-token" \
  https://api.ipify.org
```

```bash
# SOCKS5 正向代理：身份格式与 HTTP 正向代理保持一致
curl --proxy socks5h://127.0.0.1:2260 \
  -U "Default.user_tom:my-token" \
  https://api.ipify.org
```

#### 方式二：反向代理接入（URL 携带 Account，适合简单使用/手动调试）
你可以通过替换业务的 BaseURL 为 Resin 反代地址，将请求直接发给 Resin。
URL 格式进阶为：`http://部署IP:2260/密码/平台.账号/协议/目标地址`：

```bash
# 例如让 user_tom 访问 https 协议的 cloudflare.com：
curl "http://127.0.0.1:2260/my-token/Default.user_tom/https/api.ipify.org"
```

> URL 中携带 Account 的模式定位是“简单使用 / 手动调试”。
> 生产环境长期集成，推荐优先使用请求头 `X-Resin-Account` 传递 Account。

#### 方式三：反向代理接入 + `X-Resin-Account` 请求头（推荐正式集成）

如果你的客户端（或 SDK）支持自定义请求头，建议直接使用 `X-Resin-Account` 显式传递 Account，这是最稳定的方式。

Account 来源优先级：`X-Resin-Account` 请求头 > 反向代理 URL 中的 Account > 请求头提取规则。

示例：

```bash
curl "http://127.0.0.1:2260/my-token/MyPlatform/https/api.example.com/v1/orders" \
  -H "X-Resin-Account: user_tom"
```

#### 方式四：反向代理接入 + 请求头规则（零侵入/低侵入集成）
如果你的客户端不方便设置 `X-Resin-Account`，但业务请求本身已经有稳定身份头（例如发给目标网站的 API Key、Token、Cookie 等），Resin 也可以通过“请求头提取规则”自动提取 Account。

假设你的服务本来每次请求目标 API 时，都会携带 `Authorization` 请求头：

1. 在管理页面修改 Platform 的配置，把 “反向代理账号为空行为” 修改为 “提取指定请求头作为 Account”。
2. 在 “用于提取 Account 的 Headers” 输入 `Authorization`。

此时，就算你在反向代理 URL 里不填 `Account`，Resin 也会在流量经过时读取并解析该 Header。例如：

```bash
curl "http://127.0.0.1:2260/my-token/MyPlatform/https/api.example.com/v1/orders" \
  -H "Authorization: sk-abc123"
```

上面的请求中，Resin 发现 sk-abc123 后，会将其作为 Account。后续只要带着同一把 API Key 的请求，会优先保持在同一个出口 IP 上。

> [!TIP]
> 除了 Platform 请求头配置外，Resin 还支持更高级的根据 URL 前缀决定请求头的高级功能！尝试把当前文档与 [DESIGN.md](DESIGN.md) 扔给 AI，问它 “请使用简单易懂的语言，向我介绍 Resin 指定请求头提取规则的两种方式，尤其是根据 URL 前缀决定请求头的方式。”

> 请仅在具备合法处理依据（如用户授权、合同约定或其他适用法律基础）时启用请求头提取，并确保你的日志留存与访问控制策略符合所在地法律法规及目标服务条款。

---

## 🤖 接入第三方项目

各类第三方客户端使用 Resin 的方式有所不同，对于业务代码的侵入程度也不同，总结如下：

💡 **如果你不需要粘性代理**

| 接入方式 | 代码侵入程度 | 说明 |
| :--- | :--- | :--- |
| 接入正向代理 | 🟢 **零侵入** | 客户端填入 HTTP 或 SOCKS5 代理地址及认证信息即可。 |
| 接入反向代理 | 🟢 **零/低侵入** | 修改服务 BaseURL 即可接入，适配极易。 |

💡 **如果你需要粘性代理**

| 接入方式 | 代码侵入程度 | 说明 |
| :--- | :--- | :--- |
| 接入正向代理 | 🟡 **中侵入** | 需稍微修改代码：为不同用户附带不同认证信息。HTTP 与 SOCKS5 在 V1 下都可使用 `平台.账号:密码`。 |
| 接入反向代理 | 🟡 **中侵入** | 需稍微修改代码：加入 `X-Resin-Account` 请求头或动态拼接带有账号的反代 URL 路径。 |
| 接入反向代理 + 请求头规则 | 🟢 **零/低侵入** | Resin 允许通过识别业务原始头（如 `Authorization`）自动提取 Account 并进行粘性路由绑定，接入方式与非粘性反代接近。 |

👉 **极速集成脚本/提示词（Prompt）：**  
如果你是开发者，想要修改现有项目原生接入 Resin 粘性代理，你可以直接把下面这个模板喂给 AI 帮你写代码：
- [doc/integration-prompt.md](doc/integration-prompt.md)

---

## 其他部署方式

<details>
<summary><b>方式一：运行预编译二进制文件</b></summary>
<br>
前往项目的 <a href="https://github.com/Resinat/Resin/releases">Release</a> 页面，下载适合你操作系统架构的程序包。解压得到单个二进制文件 <code>resin</code>。

```bash
RESIN_ADMIN_TOKEN=【管理面板密码】 \
RESIN_AUTH_VERSION=V1 \
RESIN_PROXY_TOKEN=【代理密码】 \
RESIN_STATE_DIR=./data/state \
RESIN_CACHE_DIR=./data/cache \
RESIN_LOG_DIR=./data/log \
RESIN_LISTEN_ADDRESS=0.0.0.0 \
RESIN_PORT=2260 \
./resin
```

也可以在工作目录创建 `.env` 文件，然后直接运行 `./resin`：

```dotenv
RESIN_AUTH_VERSION=V1
RESIN_ADMIN_TOKEN=【管理面板密码】
RESIN_PROXY_TOKEN=【代理密码】
RESIN_STATE_DIR=./data/state
RESIN_CACHE_DIR=./data/cache
RESIN_LOG_DIR=./data/log
RESIN_LISTEN_ADDRESS=0.0.0.0
RESIN_PORT=2260
```
</details>

<details>
<summary><b>方式二：通过源码编译</b></summary>
<br>
前提条件：请确保环境中已安装 Go 1.25+ 和 Node.js。

```bash
# 1. 下载 Resin 源码
git clone https://github.com/Resinat/Resin.git

# 2. 编译 WebUI
cd Resin/webui
npm install && npm run build
cd ..

# 3. 编译 resin 核心
go build -tags "with_quic with_wireguard with_grpc with_utls" -o resin ./cmd/resin

# 4. 运行程序
RESIN_ADMIN_TOKEN=【管理面板密码】 \
RESIN_AUTH_VERSION=V1 \
RESIN_PROXY_TOKEN=【代理密码】 \
RESIN_STATE_DIR=./data/state \
RESIN_CACHE_DIR=./data/cache \
RESIN_LOG_DIR=./data/log \
RESIN_LISTEN_ADDRESS=127.0.0.1 \
RESIN_PORT=2260 \
./resin
```
</details>

---

## 🛠️ 常见错误 (FAQ)

- **Q: 如何让内网或本机目标不走代理节点？**
  - **A**: 配置 `RESIN_PROXY_BYPASS`，用分号、逗号或换行分隔规则。命中的请求会由 Resin 本机直连目标，而不是通过代理节点。例如：`RESIN_PROXY_BYPASS="localhost;127.*;10.*;172.16.0.0/12;192.168.*;<local>"`。规则支持精确主机、`*`/`?` 通配符、CIDR 网段，以及表示无点号本地域名的 `<local>`。
- **Q: 启动失败提示 `RESIN_PROXY_TOKEN` 未定义？**
  - **A**: 就算你不打算启用代理密码，也必须显式配置它为空：`RESIN_PROXY_TOKEN=""`。如果你的 shell 会丢弃空环境变量，请创建 `.env` 文件并写入 `RESIN_PROXY_TOKEN=`。
- **Q: 启动失败提示 `RESIN_AUTH_VERSION` 未定义？**
  - **A**: 请设置为 `LEGACY_V0` 或 `V1`。新用户设置成 V1 即可。有旧数据的老用户可以参考[迁移指南](doc/v1.0.0-migration-guide.zh-CN.md)。
- **Q: 为什么 SOCKS5 客户端连不上？**
  - **A**: 先确认你运行在 `RESIN_AUTH_VERSION=V1`；`LEGACY_V0` 下不会启用 SOCKS5 入站。若 `RESIN_PROXY_TOKEN` 非空，客户端需要发送 SOCKS5 用户名密码认证；若它被显式设为空字符串，则也允许 `NO AUTH`。
- **Q: 使用反向代理 WebSocket 协议（如 ws/wss）怎么写路径？**
  - **A**: 目标无论是不是 ws/wss，URL 路径里的协议字段**依然只能写 `http` 或 `https`**（不能写 ws/wss）。Resin 会自动探测并完成 WebSocket 协议升级（Upgrade）。

---

## ⚠️ 免责声明与许可证

- **许可证**：本项目采用 [MIT License](LICENSE)。
- **使用性质**：本项目用于网络代理调度与管理的技术研究及工程实践，不构成任何法律、合规、审计或安全建议。
- **合法使用要求**：你必须自行确保使用行为符合所在地法律法规、目标服务条款（ToS）及数据处理要求，并确认你对代理节点、目标资源和相关数据具有合法授权。
- **禁止用途**：不得将本项目用于未授权访问、规避控制措施、欺诈、攻击、滥发请求或其他违法违规活动。
- **无担保条款**：本项目按“现状（AS IS）”提供，不附带任何明示或默示担保（包括但不限于适销性、特定用途适用性、非侵权）。
- **责任限制**：在适用法律允许的最大范围内，作者与贡献者不对因使用或无法使用本项目导致的任何直接、间接、附带或衍生损失承担责任。
