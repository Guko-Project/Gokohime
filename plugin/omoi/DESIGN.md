# Omoi 对话插件设计方案

## Context

Gokohime（QQ Bot）需要一个对话插件连接 Omoi（多租户 AI Agent 平台），实现：
- **被动对话**：群聊 @bot / 私聊直接对话
- **主动发言**：监听群消息，缓冲后按概率触发，由 agent 判断是否参与

Omoi 已有完整 HTTP/SSE 接口，Gokohime 插件直接调用，无需改动 Omoi。

---

## 架构

```
┌─ 群聊消息流 ───────────────────────────────────────────────────┐
│                                                                │
│  每条消息 → 环形缓冲区 (per-group, 最近 30 条)                    │
│                    │                                           │
│          ┌────────┴────────┐                                   │
│          │  触发条件判定     │                                   │
│          │  (N条/T秒 + 概率)│                                   │
│          └────────┬────────┘                                   │
│                   ↓                                            │
│    ┌──── 双阶段处理 ────────────────────────┐                  │
│    │                                         │                 │
│    │  阶段1: 摘要 Agent (轻量模型)            │                 │
│    │  输入: 最近 N 条格式化消息               │                 │
│    │  输出: 2-3 句话题摘要                   │                 │
│    │          ↓                              │                 │
│    │  阶段2: 主 Agent (大模型)               │                 │
│    │  输入: 摘要文本                         │                 │
│    │  输出: 回复内容 或 [SKIP]               │                 │
│    │                                         │                 │
│    └─────────────────────────────────────────┘                 │
│                   ↓                                            │
│        [SKIP] → 静默 / 正文 → 发送到群                          │
└────────────────────────────────────────────────────────────────┘

┌─ @bot / 私聊 ─────────────────────────────────────────────────┐
│                                                                │
│  用户消息 + 最近几条上下文 → 直接发给主 Agent session → 必定回复  │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

---

## 插件文件结构

```
plugin/omoi/
├── DESIGN.md     # 本文档
├── init.go       # 注册消息处理器
├── client.go     # HTTP/SSE 客户端封装
├── session.go    # Session 映射管理
├── buffer.go     # 群消息环形缓冲区 + 触发逻辑
└── nickname.go   # 用户昵称解析
```

---

## 详细设计

### 1. 消息处理器 (init.go)

```go
func init() {
    // @bot 群聊 — 必定回复
    zero.OnMessage(zero.OnlyToMe, zero.OnlyGroup).
        SetBlock(true).SetPriority(50).Handle(handleGroupMention)

    // 私聊 — 必定回复
    zero.OnMessage(zero.OnlyPrivate).
        SetBlock(true).SetPriority(50).Handle(handlePrivateChat)

    // 群消息监听 — 主动发言缓冲（不 block，低优先级）
    zero.OnMessage(zero.OnlyGroup).
        SetPriority(99).Handle(handleGroupObserve)

    // .chat-reset — 重置会话
    zero.OnCommand("chat-reset").SetBlock(true).Handle(handleReset)

    // .nickname <name> — 设置自定义昵称
    zero.OnCommand("nickname").SetBlock(true).Handle(handleSetNickname)
}
```

### 2. 群消息缓冲 (buffer.go)

```go
type GroupBuffer struct {
    mu       sync.Mutex
    messages []BufferedMessage  // 环形，最多 N 条
    count    int                // 自上次触发后累计消息数
    lastSent time.Time          // 上次触发时间
}

type BufferedMessage struct {
    UserID   int64
    Nickname string   // 已解析的昵称
    Text     string
    Time     time.Time
}
```

**触发逻辑** (`handleGroupObserve`)：
1. 消息入缓冲区
2. 检查触发条件：`count >= N` 或 `time.Since(lastSent) >= T`
3. 概率判定：`rand.Float64() < P`
4. 触发 → 异步调用双阶段流程
5. 重置计数器和时间戳

### 3. 双阶段处理流程

**阶段 1 — 摘要 Agent**：
- 使用独立的 Omoi session（配置一个轻量摘要 agent）
- 发送格式化的缓冲消息作为 user message
- 收集回复（简短的话题摘要）
- 摘要 agent session 可以是短生命周期的（每次新建或复用同一个）

**阶段 2 — 主 Agent**：
- 将摘要发送到群的主 session
- 消息格式：`[群聊动态] <摘要内容>`（通过前缀区分是被 @ 还是主动观察）
- Agent 通过 system prompt/skill 被告知：收到 `[群聊动态]` 前缀的消息时可以选择不回复，回复 `[SKIP]` 即可
- 插件检测 `[SKIP]` → 不发送；否则将内容发到群里

### 4. @bot 直接对话流程

```
handleGroupMention(ctx):
  1. 提取纯文本消息
  2. 获取发言者昵称
  3. 获取最近 3~5 条缓冲消息作为附加上下文
  4. 格式化：
     "[上下文]
      [用户A(123)]: xxx
      [用户B(456)]: yyy
      ---
      [小明(789)]: @bot 你觉得呢？"
  5. 获取/创建该群的主 session
  6. 调用 sendMessage → 必定回复
  7. 发送回复到群
```

### 5. 用户昵称解析 (nickname.go)

优先级链：
1. PluginKV `omoi:nickname:<userID>` — 用户自设昵称
2. `ctx.GetGroupMemberInfo(groupID, userID, false).Card` — 群名片
3. `ctx.GetGroupMemberInfo(groupID, userID, false).Nickname` — QQ 昵称
4. `fmt.Sprintf("%d", userID)` — QQ 号 fallback

发给 Omoi 的格式：`[昵称(QQ号)]`

### 6. HTTP/SSE 客户端 (client.go)

```go
var httpClient = &http.Client{}

// createSession: POST /api/sessions
// body: {"agent_id": "xxx", "title": ""}
// headers: X-API-Key, X-Tenant-ID
func createSession(agentID, title string) (sessionID string, err error)

// sendMessage: POST /api/chat/messages (SSE response)
// body: {"session_id": "xxx", "content": [{"type":"text","text":"..."}]}
// 逐行解析 SSE events，拼接 delta content
func sendMessage(ctx context.Context, sessionID, text string) (reply string, err error)

// deleteSession: DELETE /api/sessions/{id}
func deleteSession(sessionID string) error
```

SSE 解析：
- 读 `event:` 行获取事件类型
- 读 `data:` 行 JSON 解析
- `delta` → 追加 content
- `error` → 返回 error
- `done` → 结束

请求头：
```
X-API-Key: <config.omoi.api_key>
X-Tenant-ID: <config.omoi.tenant_id>
Content-Type: application/json
```

请求体（`POST /api/chat/messages`）：
```json
{
  "session_id": "xxx",
  "content": [{"type": "text", "text": "用户消息"}]
}
```

### 7. Session 映射 (session.go)

PluginKV namespace = `"omoi"`：

| Key | 用途 |
|-----|------|
| `session:group:<groupID>` | 群主 session ID |
| `session:private:<userID>` | 私聊 session ID |
| `session:summarizer:<groupID>` | 群摘要 agent session ID |
| `agent:group:<groupID>` | 群自定义主 agent ID |
| `nickname:<userID>` | 用户自定义昵称 |

错误恢复：sendMessage 返回 session 不存在时自动重建。

### 8. 消息拆分

回复超过 `max_message_len`（默认 4000 字符）时：
1. 按 `\n\n` 拆分为段落
2. 贪心合并相邻段落直到接近限制
3. 逐段发送

---

## Config

`internal/config/config.go` 新增 `OmoiConfig`：

```go
type OmoiConfig struct {
    Address            string  `yaml:"address"`              // "http://localhost:8080"
    APIKey             string  `yaml:"api_key"`              // "omoi_xxx"
    TenantID           string  `yaml:"tenant_id"`
    DefaultAgentID     string  `yaml:"default_agent_id"`     // 主对话 agent
    SummarizerAgentID  string  `yaml:"summarizer_agent_id"`  // 摘要 agent
    TimeoutSec         int     `yaml:"timeout_sec"`          // 请求超时，默认 120
    MaxMessageLen      int     `yaml:"max_message_len"`      // 拆分阈值，默认 4000
    BufferSize         int     `yaml:"buffer_size"`          // 缓冲区大小，默认 30
    TriggerCount       int     `yaml:"trigger_count"`        // 触发消息数，默认 20
    TriggerIntervalSec int     `yaml:"trigger_interval_sec"` // 触发时间间隔，默认 120
    TriggerProbability float64 `yaml:"trigger_probability"`  // 触发概率，默认 0.3
    EnabledGroups      []int64 `yaml:"enabled_groups"`       // 启用主动发言的群（空=全部）
    SkipMarker         string  `yaml:"skip_marker"`          // 跳过标记，默认 "[SKIP]"
}
```

`config.example.yaml`：

```yaml
omoi:
  address: "http://localhost:8080"
  api_key: ""
  tenant_id: ""
  default_agent_id: ""
  summarizer_agent_id: ""
  timeout_sec: 120
  max_message_len: 4000
  buffer_size: 30
  trigger_count: 20
  trigger_interval_sec: 120
  trigger_probability: 0.3
  enabled_groups: []
  skip_marker: "[SKIP]"
```

环境变量覆盖：`OMOI_API_KEY`、`OMOI_ADDRESS`、`OMOI_TENANT_ID`、`OMOI_DEFAULT_AGENT_ID`。

---

## 实现顺序

1. **Config** — 添加 OmoiConfig + example yaml + env 覆盖
2. **plugin/omoi/nickname.go** — 昵称解析
3. **plugin/omoi/client.go** — HTTP 客户端 + SSE 解析
4. **plugin/omoi/session.go** — Session CRUD + 映射
5. **plugin/omoi/buffer.go** — 群消息缓冲 + 触发
6. **plugin/omoi/init.go** — 处理器注册 + 核心流程
7. **cmd/bot/main.go** — 添加 plugin import

---

## 验证

1. 启动 Omoi server，创建 tenant + API Key + 主 agent + 摘要 agent
2. 启动 Gokohime bot
3. **@bot 测试**：群内 @bot → 带上下文的 AI 回复
4. **私聊测试**：私聊 bot → 独立会话回复
5. **主动发言测试**：群内 20+ 条消息 → bot 按概率触发
6. **重置测试**：`.chat-reset` → 新 session
7. **昵称测试**：`.nickname 小明` → 上下文使用自定义昵称
8. **降级测试**：Omoi 不可达 → 友好错误
9. **超长回复**：验证正确拆分
