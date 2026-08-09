# ccrouter

多 Provider AI API 网关，用 Go 实现。支持 Combo 路由、密钥轮换、健康检查规则和管理界面。

当上游返回配额超限等指定错误时，自动轮换密钥或切换到备用 Provider，对客户端完全透明。

## 功能

- **多 Provider** — 同时管理多个上游服务（SenseNova、DeepSeek 等），每个 Provider 独立配置密钥和规则
- **Combo 路由** — 将一个虚拟模型名映射到多个 `(provider, model)` 成员，按策略依次尝试
- **两级重试** — 先在当前 Provider 内轮换密钥，密钥全部冷却后自动切换到 Combo 的下一个成员
- **细粒度冷却** — 冷却粒度为 `(key, model)`，同一个 key 下不同模型的配额相互独立
- **多格式支持** — 同时支持 OpenAI Chat、Anthropic Messages、OpenAI Responses、OpenAI Images 四种 API 格式
- **Payload 脚本** — 转发前执行脚本改写请求 body / header（基于 [expr-lang/expr](https://github.com/expr-lang/expr)，天然沙箱）
- **Streaming** — 正确处理 SSE 流式响应，含 token usage 嗅探和首帧错误检测
- **管理页面** — 内置 Web UI，支持实时统计、请求明细、热重载配置（`/admin/`）
- **单二进制** — 前端静态资源通过 `go:embed` 内嵌，部署只需一个可执行文件

## 安装

### 下载预编译二进制（推荐）

从 [Releases](../../releases) 页面下载对应平台的二进制文件：

```
ccrouter-linux-amd64
ccrouter-linux-arm64
ccrouter-darwin-amd64
ccrouter-darwin-arm64
ccrouter-windows-amd64.exe
```

### 从源码构建

需要 Go 1.21+ 和 Node.js 18+。

```bash
git clone https://github.com/yourname/ccrouter
cd ccrouter
make build          # 先构建前端，再编译 Go 二进制
# 产物：./ccrouter
```

或分步执行：
```bash
make web            # 只构建前端（cd web && npm ci && npm run build）
go build -o ccrouter ./cmd/ccrouter
```

## 快速开始

```bash
# 1. 复制配置模板
cp config-example.yaml config.yaml

# 2. 填入你的 API 密钥
vim config.yaml

# 3. 启动
./ccrouter -c config.yaml
```

访问 `http://localhost:8000/admin/` 打开管理页面。

> **注意**：管理页面无认证，默认绑定 `127.0.0.1`，请勿暴露到公网。

## 命令行参数

```
Usage of ccrouter:
  -c string      配置文件路径（默认：$CCROUTER_CONFIG 或 config.yaml）
  -host string   监听地址（默认：127.0.0.1）
  -port int      监听端口（默认：8000）
```

也可用环境变量指定配置文件：

```bash
SENSE_ROLL_CONFIG=/etc/ccrouter/config.yaml ./ccrouter
```

## 管理页面

| 页面 | 功能 |
|------|------|
| **概览** | 请求量、Token 消耗（含 Cache Read/Write）、成功率趋势图、密钥池实时状态 |
| **请求** | 分页日志，含 combo / provider / model / key / 状态码 / token 用量 / matched_payload |
| **配置** | 编辑 Provider 和 Combo，保存后热重载，无需重启 |
| **测试** | 直接调用代理端点验证配置，支持流式展示、thinking 模式和图像生成 |
| **日志** | 详细请求报文（需开启 verbose_logging），可展开查看完整 client/upstream 请求与响应 |
| **信息** | 版本、Go 运行时、当前 Provider 和 Combo 列表 |

## 配置说明

完整字段说明见 `config-schema.yaml`，完整示例见 `config-example.yaml`。

### 顶层结构

```yaml
providers:        # 上游 Provider 列表（至少一个）
  - ...

combos:           # 虚拟模型名到 Provider 的映射（至少一个）
  - ...

verbose_logging: false  # 可选；true 时在 cwd/logs/ 记录完整请求报文（含明文密钥）

payload_scripts:        # 可选；转发前按顺序执行的改写脚本
  - name: "..."
    enabled: true
    script: |
      ...
```

### `providers`

```yaml
providers:
  - name: sensenova               # 唯一标识符，供 combo 引用
    api:
      - api_format: openai        # openai | anthropic | openai-responses | openai-images
        base_url: "https://token.sensenova.cn/v1"
      - api_format: anthropic     # 同一组 key 可同时支持多种格式
        base_url: "https://token.sensenova.cn/v1"
    max_retries: 3                # 单 Provider 内最多尝试次数（含首次）；默认 3
    key_strategy: "fill-first"    # fill-first（默认）| round-robin
    keys:
      - key: "sk-xxxx-1"
      - key: "sk-xxxx-2"
    health_check_rules:
      - description: "quota_exceeded"
        jsonpath: "$.error.type"           # JSONPath 表达式；默认 $.error.type
        match_value: "quota_exceeded_error"
        match_type: "equals"               # equals（默认）| contains | regex
        action: "rotate"
        cooldown_seconds: 18000            # 冷却秒数；默认 60
        models: ["deepseek-v4-flash"]      # 空列表 = 适用所有模型
```

### `combos`

```yaml
combos:
  - name: "fast"                  # 客户端请求时 model 字段填此值
    api_format: openai            # 单值或列表；决定监听哪些代理端点
    strategy: "fill-first"        # fill-first（默认）| round-robin
    aliases: ["gpt-4o", "claude"] # 可选；这些名称也路由到本 combo
    members:
      - provider: sensenova
        model: "deepseek-v4-flash"
      - provider: deepseek        # 前一个成员所有 key 耗尽后的备用
        model: "deepseek-chat"
```

`api_format` 支持列表，使同一 Combo 同时服务多个端点：

```yaml
api_format:
  - openai      # → POST /v1/chat/completions
  - anthropic   # → POST /v1/messages
```

### Payload 改写脚本

转发请求给上游之前，按顺序执行已启用的脚本。每个脚本的输出是下一个脚本的输入。脚本基于 [expr-lang/expr](https://github.com/expr-lang/expr)，天然沙箱（无 IO / 系统调用）。

**脚本环境：**

| 变量 | 类型 | 说明 |
|------|------|------|
| `body` | map | 请求 body（JSON 解析后的 map，可读写） |
| `headers` | map | 请求 headers（可读写） |
| `combo` | string | 客户端传的 combo 名（只读） |
| `path` | string | 请求路径，如 `/v1/chat/completions`（只读） |

**内置函数：**

| 函数 | 说明 |
|------|------|
| `del(body, "key")` | 删除 body 字段 |
| `set(body, "key", value)` | 设置 body 字段 |
| `setpath(body, "a", "b", value)` | 嵌套赋值（自动创建中间层） |
| `get(body.sub, "key", default)` | 带默认值读取 |
| `delh(headers, "name")` | 删除 header |
| `seth(headers, "name", value)` | 设置 header |
| `clamp(v, min, max)` | 数值约束 |

**示例：**

```yaml
payload_scripts:
  - name: "隐藏客户端标识"
    enabled: true
    script: |
      delh(headers, "user-agent")
      delh(headers, "User-Agent")
      del(body, "x_client_info")

  - name: "限制 thinking 预算"
    enabled: true
    script: |
      combo == "fast" && "thinking" in body
        ? setpath(body, "thinking", "budget_tokens",
            clamp(get(body.thinking, "budget_tokens", 8000), 0, 1024))
        : nil
```

多行表达式（三目运算符跨行、括号未闭合、`,` 结尾）自动续行。每行一条语句，`#` 开头为注释。

脚本执行情况（名称 + `ok` 或错误摘要）写入请求记录的 `matched_payload` 字段，可在请求明细中查看。

### Verbose Logging（详细日志）

> ⚠️ **安全警告**：详细日志会完整记录 HTTP header，其中包含上游 Provider 的**明文 API 密钥**。`logs/` 目录已加入 `.gitignore`，请勿将其暴露至公网或提交至版本控制。

通过管理页面「日志」选项卡的开关控制，状态持久化写入 `config.yaml`，重启后保留。

- **日志位置**：`<启动目录>/logs/requests.jsonl`
- **滚动策略**：单文件超 20 MB 自动 gzip 压缩归档为 `requests.jsonl.1.gz`，最多保留 10 个 `.gz`
- **记录内容**：每次上游请求写一条 JSONL，包含完整的 client 入站信息、upstream 转发信息、响应信息

## API 端点

| 端点 | 说明 |
|------|------|
| `POST /v1/chat/completions` | OpenAI Chat Completions（格式：`openai`） |
| `POST /v1/messages` | Anthropic Messages（格式：`anthropic`） |
| `POST /v1/responses` | OpenAI Responses API（格式：`openai-responses`） |
| `POST /v1/images/generations` | OpenAI Images（格式：`openai-images`） |
| `GET /v1/models` | 返回所有可用 combo（含 alias），OpenAI 兼容格式 |
| `GET /health` | 健康检查 |
| `GET /keys/status` | 实时密钥池状态 |
| `GET /admin/` | 管理页面 |
| `GET /admin/api/config` | 查看当前配置（JSON，含明文密钥） |
| `PUT /admin/api/config` | 更新配置并热重载 |
| `GET /admin/api/stats/summary` | 聚合统计（按 combo / provider / model / key_prefix 分组） |
| `GET /admin/api/stats/trend` | 时间分桶趋势（minute / hour / day） |
| `GET /admin/api/requests` | 分页请求明细 |
| `GET /admin/api/stats/keys` | 实时密钥池状态（同 `/keys/status`） |
| `GET /admin/api/logs` | 详细日志（需开启 verbose_logging） |
| `GET/PUT /admin/api/logs/settings` | 查看/切换 verbose_logging |
| `GET /admin/api/info` | 版本、运行时、combo 和 provider 列表 |
| `GET /admin/api/health` | 进程健康（含 DB 队列状态） |

## 使用示例

```bash
# OpenAI 格式（streaming）
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"fast","messages":[{"role":"user","content":"hello"}],"stream":true}'

# Anthropic 格式
curl http://localhost:8000/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"fast","messages":[{"role":"user","content":"hello"}],"max_tokens":1024}'

# 查看密钥状态
curl http://localhost:8000/keys/status | jq .

# 热重载配置
curl -X PUT http://localhost:8000/admin/api/config \
  -H "Content-Type: application/json" \
  -d @config.json
```

## 本地开发

```bash
# 启动 Go 后端
go run ./cmd/ccrouter -c config.yaml

# 另一个终端：启动 Vite 开发服务器（HMR，代理到 :8000）
cd web && npm run dev
# 访问 http://localhost:5173/admin/
```

## 测试

```bash
go test ./...
```

## 发布

推送 `v*` 格式的 tag 触发 GitHub Actions，自动：

1. 构建前端（`npm ci && npm run build`）
2. 交叉编译 5 个平台的二进制（linux/amd64、linux/arm64、darwin/amd64、darwin/arm64、windows/amd64）
3. 发布到 GitHub Releases

```bash
git tag v1.0.0
git push origin v1.0.0
```

## 与 sense-roll 的关系

ccrouter 是 [sense-roll](https://github.com/yourname/sense-roll)（Python/FastAPI 实现）的 Go 重写版，API 行为和配置格式完全兼容，可直接使用同一份 `config.yaml`。

主要差异：

| | sense-roll | ccrouter |
|---|---|---|
| 运行时 | Python + uvicorn | Go 单二进制 |
| 部署 | `pip install` + Python 环境 | 下载单个可执行文件 |
| Payload 脚本语言 | Python（`exec()`） | expr 表达式（沙箱安全） |
| 内存占用 | ~50MB+ | ~20MB |
