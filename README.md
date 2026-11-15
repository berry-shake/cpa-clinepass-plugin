# CPA ClinePass Plugin

CLIProxyAPI（CPA）插件：把 [ClinePass](https://docs.cline.bot/getting-started/clinepass)（Cline Pass 订阅）的 API Key 作为 CPA 认证文件接入，通过 Cline 的 OpenAI Chat Completions 接口调用 ClinePass 模型。

- 每个 Key 是一个认证文件，和其他凭证一样参与 CPA 的选号、冷却和故障转移，可以单独设出口代理
- 自动拉取 ClinePass 模型目录注册到 CPA，默认模型名 `clinepass/<模型 ID>`（如 `clinepass/kimi-k3`），也可以只注册选中的模型
- Cline 只提供 Chat Completions；Claude Code 等用 Messages、Responses 的客户端由 CPA 转换后照样能用
- 非流式请求改用流式发给上游，再合并成一个完整回答返回，避开 Cline 原生非流式回答的包装格式和偶发空回复
- 查看账号套餐的 5 小时 / 每周 / 每月用量和套餐名称，CPA 额度面板也能看
- 自带管理页：Key、模型、出口代理、请求日志

> 社区插件，与 Cline 官方无关。

## 安装

需要 CLIProxyAPI v8 并开启动态插件（Unix 上 CPA 要用 cgo 编译）；支持 Linux / macOS（amd64、arm64）和 Windows amd64。需要一个已订阅 ClinePass 的 Cline 账号的 API Key。

**插件商店**：在 CPA 配置里加上商店源，然后在管理面板的插件商店里安装 **ClinePass**。

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/berry-shake/CLIProxyAPI-Plugins-Store/main/registry.json
```

**手动安装**：从 [Release](https://github.com/berry-shake/cpa-clinepass-plugin/releases) 下载对应平台的 zip，把里面的 `clinepass.so`（macOS 是 `.dylib`，Windows 是 `.dll`）放到 `plugins/<系统>/<架构>/` 下，再在配置里打开 `plugins.configs.clinepass.enabled`。配置里已有 `store.version` 时，CPA 只加载版本号对得上的文件，手动换文件要同时改 `store.version` 和 `release-tag`。

**和 ClinePassBridge 一起装**：[ClinePassBridge](https://github.com/xiao-qiu-qiu/ClinePassBridge) 的认证文件类型是 `cline-pass`，本插件是 `clinepass`，两者各管各的 Key，互不接管。

## 配置

都是可选项，写在 `plugins.configs.clinepass` 下；填错的值回退默认并记一条警告。

```yaml
plugins:
  configs:
    clinepass:
      enabled: true
      base-url: https://api.cline.bot             # 末尾的 /api/v1 会自动去掉
      model-prefix: clinepass                     # 认证文件没写 prefix 时的默认前缀，"" = 不加
      models-refresh-minutes: 60                  # 模型目录轮询间隔，0 = 不轮询
      request-timeout-seconds: 600                # 非流式请求超时
      stream-idle-timeout-seconds: 300            # 流式响应多久没数据就断开
      data-dir: plugins/clinepass-data            # 请求日志和代理列表的目录
      log-retention: 100000                       # 请求日志最多保留条数，0 = 不记录
      log-retention-days: 30                      # 请求日志保留天数，0 = 只按条数清理
```

## 认证文件

`type` 为 `clinepass` 的认证文件归本插件管理；管理页添加的 Key 保存为 `clinepass-<指纹>.json`。

```json
{
  "type": "clinepass",
  "api_key": "<ClinePass API Key>",
  "label": "主账号",
  "models": ["kimi-k3", "deepseek-v4.1-flash-1017"],
  "prefix": "clinepass",
  "proxy_url": "socks5://127.0.0.1:1080"
}
```

| 字段 | 说明 |
|---|---|
| `api_key` | 必填 |
| `label` | 显示名称，默认用文件名 |
| `models` | 要注册的模型（不带 `cline-pass/`）。不写或 `null` = 目录里全部模型，`[]` = 不注册 |
| `prefix` | 模型名前缀。不写用配置里的 `model-prefix`，`""` = 不加前缀 |
| `proxy_url` | 出口代理，支持 `http(s)://`、`socks5(h)://`，`direct` = 强制直连；不写时依次用 CPA 的 `proxy-url`、环境变量 |
| `base_url` | 覆盖配置里的 `base-url` |
| `priority` / `note` / `disabled` | CPA 通用字段 |

`model_revision` 由插件维护，不要手改。

> CPA 的 `routing.force-model-prefix` 默认关闭，这时模型会同时注册成 `clinepass/kimi-k3` 和 `kimi-k3`，后者会和其他渠道的同名模型混在一起轮询。只想用带前缀的名字就把它打开（全局生效）。

## 工作方式

**模型**：每隔 `models-refresh-minutes` 拉一次 `https://api.cline.bot/api/v1/ai/cline/recommended-models`（不需要鉴权，不发 API Key），取其中的 `clinePass` 列表。上游模型 ID 都带 `cline-pass/` 前缀，注册到 CPA 时去掉，转发时再补回：客户端用 `clinepass/kimi-k3`，Cline 收到 `cline-pass/kimi-k3`。目录里暂时没有的模型（比如选过后下架）也照样补前缀转发。拉取失败保留上一次的目录。目录不带上下文长度和价格。

**转发**：

| 客户端格式 | 处理 |
|---|---|
| OpenAI Chat Completions | `POST /api/v1/chat/completions` |
| Anthropic Messages、OpenAI Responses | CPA 先转成 Chat Completions 再交给插件 |
| `count_tokens` | 不支持，回 501 |

- 只改写请求体里的 `model`，其余原样转发；流式请求会自动加上 `stream_options.include_usage`
- **非流式请求**：改成 `"stream": true` 发给 Cline，把收到的流合并成一个 `chat.completion` 返回。正文、推理内容按顺序拼接，工具调用按序号合并参数，`usage` 取最后一个。流里任何位置出错、没收到结束标记、或者整个回答为空，都按失败处理（空回答报 502 `empty_response`，CPA 可以重试或换 Key）
- 流式响应在第一帧真正的输出之前，上游报错（限流、额度用完）按普通错误处理，CPA 照常冷却或换 Key；流没收到结束帧就断了，记为 `stream_truncated`（502）
- Cline 在流里用 `{"success":false,...}` 报的错，按错误事件处理

**上游错误**：

| Cline 返回 | 交给 CPA | 效果 |
|---|---|---|
| 401 / 403 | 原状态码 | 冷却这个 Key |
| 403「不支持客户端的 providerOptions 路由参数」 | 400 | 只失败这一次，不冷却 Key |
| 429（套餐窗口用完或限流） | 429，带 `Retry-After` | 按上游给的时间冷却 |
| 5xx | 原状态码 | 可重试 |

## 套餐用量

插件用 Key 查 `GET /api/v1/users/me/plan/usage-limits` 和 `GET /api/v1/users/me/plan`，得到：

| 内容 | 说明 |
|---|---|
| 5 小时 / 每周 / 每月 | 各窗口已用百分比和重置时间 |
| 套餐 | 套餐名称和计费周期结束时间 |

- 用量是账号级的，包括这个账号在其他客户端的使用；同一账号的多个 Key 数字相同，汇总时只算一次（按 Cline 用户 ID 的哈希识别，不保存原始 ID）
- Cline 没报百分比的窗口显示 `—`，不当作 0
- 用量查询失败时整个查询失败；只有套餐信息查不到时照样显示用量，且这次结果不缓存
- 结果缓存 60 秒；查询按出口排队，同一个出口一次只查一个 Key

CPA 额度面板显示 **ClinePass** 一组，`5h` / `7d` / `monthly` 三条进度条，套餐名称显示在订阅一栏。

## 管理页

在 CPA 管理面板的插件菜单里点 **ClinePass**（`/v0/resource/plugins/clinepass/manage`）。

- **凭据**：Key 卡片，添加、编辑、启停、删除，选模型，看套餐用量
- **代理**：常用出口代理列表，编辑 Key 时直接选；可以检测出口 IP、地区和延迟
- **模型目录**：模型 ID、上游 ID、描述和标签，以及在 CPA 里注册的名字
- **请求日志**：每次调用的模型、状态、首字时间、耗时、Token 和速度，可以用 CPA 日志里的 8 位请求 ID 搜索；只记元数据，不记内容。合并成非流式的请求按非流式记，首字时间是上游流的首个输出
- **配置概览**：当前生效的配置

数据都在 `data-dir` 下：`clinepass.db`（请求日志，SQLite）和 `proxies.json`（代理列表，含代理密码，权限 0600）。

## 管理接口

都在 `/v0/management/plugins/clinepass/` 下，CPA 先校验管理密钥再转给插件。

| 接口 | 用途 |
|---|---|
| `GET status` | 插件状态和 Key 列表 |
| `GET models`、`POST models/refresh` | 模型目录、强制重新拉取 |
| `POST credentials`、`PATCH credentials` | 添加、编辑 Key（添加前用套餐用量接口校验） |
| `GET credentials/quota?auth_index=` | 账号的套餐用量 |
| `GET logs`、`DELETE logs` | 查询、清空请求日志 |
| `GET` / `POST` / `PATCH` / `DELETE proxies`，`POST proxies/import`、`proxies/order`、`proxies/check` | 代理列表 |

编辑只改传了的字段：`null` 恢复默认，`""` 清空。`api_key` 不能改，换 Key 就新增一个再删掉旧的。启停和删除用 CPA 原生的认证文件接口。

## 构建

需要 Go 和 Node 20。各供应商插件共用的代码（Go 和管理页的公共部分）在 [cpa-provider-core](https://github.com/berry-shake/cpa-provider-core)，版本跟着 `go.mod` 走；`make web` 会先把它的公共前端拷到 `web/src/core/`。要同时改 core，用 `go.work` 把本地的 core 加进来，或者设 `CPA_PROVIDER_CORE_DIR` 指向它。

```bash
make web    # 构建管理页（web/，React + Vite），嵌进插件
CGO_ENABLED=1 go build -buildmode=c-shared -o clinepass.so .
make web-test && go test ./...
```

推送 `v*` tag 后，GitHub Actions 跑测试、构建五个平台并发布 Release。

## 致谢

Cline 接口的细节（模型目录、套餐用量接口、`{success,data}` 包装、非流式回答的问题）参考了 [ClinePassBridge](https://github.com/xiao-qiu-qiu/ClinePassBridge)（MIT）的源码和文档；本插件基于 cpa-provider-core 独立实现，没有复制它的代码。

## License

MIT。国旗图标来自 [Twemoji](https://github.com/jdecked/twemoji)（CC-BY 4.0）。
