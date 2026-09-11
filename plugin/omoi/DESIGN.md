# Omoi 对话插件设计方案

## Context

Gokohime（QQ Bot）需要一个对话插件连接 Omoi（多租户 AI Agent 平台），实现：
- **被动对话**：群聊 @bot / 私聊直接对话
- **主动发言**：监听群消息，缓冲后按概率触发，由 Agent 判断是否参与（回复 `[SKIP]` 则静默）

Omoi 提供完整 HTTP/SSE 接口，Gokohime 插件直接调用，无需改动 Omoi。

---

## 连接信息

- **Base URL**: `https://nvli-omoi.centaurea.dev/api`
- **API Key**: 通过 `omoi.api_key` 或 `OMOI_API_KEY` 配置，不写入仓库
- **Agent ID**: 直接在配置中指定

---

## 架构

```
┌─ 群聊消息流 ───────────────────────────────────────────────────┐
│                                                                │
│  每条消息 → 跳过指令前缀(./#!/) → 环形缓冲区 (per-group, 20条)  │
│                    │                                           │
│          ┌────────┴────────┐                                   │
│          │  触发条件判定     │                                   │
│          │  (N条/T秒 + 概率)│                                   │
│          └────────┬────────┘                                   │
│                   ↓                                            │
│     构造 prompt (最近 20 条消息)                                 │
│     → 发送给 Omoi Agent session                                │
│     → 检测 [SKIP] → 静默                                       │
│     → 否则按 <<<SPLIT>>> 分段延迟发送                            │
│                                                                │
└────────────────────────────────────────────────────────────────┘

┌─ @bot / 私聊 ─────────────────────────────────────────────────┐
│                                                                │
│  跳过指令前缀(./#!/) → 不处理                                   │
│  否则:                                                         │
│  构造 prompt (最近 20 条上下文 + 标注触发消息)                    │
│  → 发送给 Omoi Agent session → 必定回复                         │
│  → 按 <<<SPLIT>>> 分段延迟发送                                  │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

---

## 消息过滤

以下前缀开头的消息**一律忽略**（不入缓冲、不触发回复、@bot 也不处理）：
- `.`（英文句点）
- `/`
- `#`

---

## Prompt 格式

### 被 @ 时 / 被动回复

```
以下是群「{群名}」的最近聊天记录：
---
[2026-06-14 14:01] 小明(123456): 今天天气真好啊
[2026-06-14 14:02] 阿花(789012): 是啊，想出去玩
[2026-06-14 14:03] CC(345678): @鸽子姬 你要不要一起来
---

⬇️ 需要回复的消息：
[CC(345678)]: @鸽子姬 你要不要一起来
```

### 主动触发

```
以下是群「{群名}」的最近聊天记录：
---
[2026-06-14 14:01] 小明(123456): 今天天气真好啊
[2026-06-14 14:02] 阿花(789012): 是啊，想出去玩
[2026-06-14 14:03] CC(345678): 有人想一起去吃火锅吗
---

你在旁边听到了这些对话，如果觉得有话想说可以自然地加入聊天，如果觉得没什么好说的就回复 [SKIP]。
```

### 私聊

```
[{昵称}({QQ号})]: {消息内容}
```

---

## 回复处理

1. **[SKIP] 检测**：回复文本 trim 后等于 `[SKIP]` 或包含 `[SKIP]` 时，不发送任何消息
2. **分段发送**：按 `<<<SPLIT>>>` 分割回复文本为多段
3. **延迟模拟**：每段之间根据上一段字数计算延迟（约 50~80ms/字，加随机抖动），模拟人类打字节奏
4. **超长保护**：单段超过 `max_message_len`（默认 2000 字符）时再按段落 `\n\n` 拆分

---

## 插件文件结构

```
plugin/omoi/
├── DESIGN.md     # 本文档
├── init.go       # 注册消息处理器 + 启动初始化
├── client.go     # Omoi HTTP/SSE 客户端封装
├── session.go    # Session 映射管理（per-group / per-user）
├── buffer.go     # 群消息环形缓冲区 + 触发逻辑
├── prompt.go     # Prompt 构造（格式化消息历史）
└── sender.go     # 分段延迟发送逻辑
```

---

## 详细设计

### 1. 消息处理器 (init.go)

```go
func init() {
    // @bot 群聊 — 必定回复（指令消息除外）
    zero.OnMessage(zero.OnlyToMe, zero.OnlyGroup).
        SetBlock(true).SetPriority(50).Handle(handleGroupMention)

    // 私聊 — 必定回复（指令消息除外）
    zero.OnMessage(zero.OnlyPrivate).
        SetBlock(true).SetPriority(50).Handle(handlePrivateChat)

    // 群消息监听 — 缓冲 + 主动发言（不 block，低优先级）
    zero.OnMessage(zero.OnlyGroup).
        SetPriority(99).Handle(handleGroupObserve)

    // .chat-reset — 重置会话
    zero.OnCommand("chat-reset").SetBlock(true).Handle(handleReset)
}
```

所有 handler 入口先检查消息是否以 `.` `/` `#` 开头，是则直接 return。

### 2. 群消息缓冲 (buffer.go)

```go
type GroupBuffer struct {
    mu       sync.Mutex
    messages []BufferedMessage  // 环形，最多 20 条
    count    int                // 自上次触发后累计消息数
    lastSent time.Time          // 上次主动触发时间
}

type BufferedMessage struct {
    UserID   int64
    Nickname string
    Text     string
    Time     time.Time
}
```

**触发逻辑** (`handleGroupObserve`)：
1. 检查消息是否以指令前缀开头 → 是则跳过（不入缓冲、不计数）
2. 消息入缓冲区
3. 检查触发条件：`count >= trigger_count` 且 `time.Since(lastSent) >= trigger_interval`
4. 概率判定：`rand.Float64() < trigger_probability`
5. 触发 → 异步构造 prompt + 调用 Omoi
6. 重置计数器和时间戳

### 3. Prompt 构造 (prompt.go)

```go
// BuildGroupMentionPrompt 构造被 @ 时的 prompt
func BuildGroupMentionPrompt(groupName string, history []BufferedMessage, trigger BufferedMessage) string

// BuildGroupActivePrompt 构造主动触发的 prompt
func BuildGroupActivePrompt(groupName string, history []BufferedMessage) string

// BuildPrivatePrompt 构造私聊 prompt
func BuildPrivatePrompt(nickname string, userID int64, text string) string
```

时间格式：`2006-01-02 15:04`
消息格式：`[{time}] {nickname}({userID}): {text}`

### 4. HTTP/SSE 客户端 (client.go)

```go
type OmoiClient struct {
    baseURL    string
    apiKey     string
    agentID    string       // 直接从配置读取
    httpClient *http.Client
}

// CreateSession POST /api/sessions
func (c *OmoiClient) CreateSession(ctx context.Context, title string) (sessionID string, err error)

// SendMessage POST /api/chat/messages (SSE response)
// 逐行解析 SSE events，拼接 delta content，返回完整回复
func (c *OmoiClient) SendMessage(ctx context.Context, sessionID, text string) (reply string, err error)
```

SSE 解析：
- `event: delta` → 追加 `data.content`
- `event: tool_start` / `event: tool_end` → 忽略（或可选日志）
- `event: error` → 返回 error
- `event: done` → 结束，返回累积文本

请求头：
```
X-API-Key: <config.omoi.api_key>
Content-Type: application/json
```

### 5. Session 映射 (session.go)

PluginKV namespace = `"omoi"`：

| Key | 用途 |
|-----|------|
| `session:group:<groupID>` | 群 session ID |
| `session:private:<userID>` | 私聊 session ID |

Session 创建时设置 title：
- 群: `"群:{群名}"`
- 私聊: `"私聊:{昵称}"`

获取 session 流程：
1. 从 KV 读取 session_id
2. 不存在 → 调用 `CreateSession` → 存入 KV
3. `SendMessage` 返回 404 → 自动重建 session

### 6. 分段延迟发送 (sender.go)

```go
// SendSplitReply 处理 Omoi 回复并分段发送
func SendSplitReply(ctx *zero.Ctx, reply string) {
    // 1. 检测 [SKIP]
    if isSkip(reply) {
        return
    }

    // 2. 按 <<<SPLIT>>> 分割
    parts := strings.Split(reply, "<<<SPLIT>>>")

    // 3. 逐段发送，段间延迟
    for i, part := range parts {
        part = strings.TrimSpace(part)
        if part == "" {
            continue
        }
        if i > 0 {
            delay := calcTypingDelay(parts[i-1])
            time.Sleep(delay)
        }
        ctx.Send(message.Text(part))
    }
}

// calcTypingDelay 根据字数模拟打字延迟
// 基础: 60ms/字 + 随机 ±20ms/字，最小 1s，最大 4s
func calcTypingDelay(text string) time.Duration
```

### 7. 用户昵称解析

优先级链：
1. `ctx.CardOrNickname(ctx.Event.UserID)` — 群名片 / QQ 昵称
2. `fmt.Sprintf("%d", userID)` — QQ 号 fallback

发给 Omoi 的格式：`{昵称}({QQ号})`

---

## Config

`internal/config/config.go` 新增 `OmoiConfig`：

```go
type OmoiConfig struct {
    Address            string  `yaml:"address"`              // "https://nvli-omoi.centaurea.dev/api"
    APIKey             string  `yaml:"api_key"`              // "omoi_xxx"
    AgentID            string  `yaml:"agent_id"`             // 直接指定 agent ID
    TimeoutSec         int     `yaml:"timeout_sec"`          // 请求超时，默认 120
    MaxMessageLen      int     `yaml:"max_message_len"`      // 单段最大字符数，默认 2000
    BufferSize         int     `yaml:"buffer_size"`          // 缓冲区大小，默认 20
    TriggerCount       int     `yaml:"trigger_count"`        // 触发消息数，默认 15
    TriggerIntervalSec int     `yaml:"trigger_interval_sec"` // 触发最小间隔，默认 180
    TriggerProbability float64 `yaml:"trigger_probability"`  // 触发概率，默认 0（关闭）
    EnabledGroups      []int64 `yaml:"enabled_groups"`       // 启用主动发言的群（空=全部）
    SkipMarker         string  `yaml:"skip_marker"`          // 跳过标记，默认 "[SKIP]"
    SplitMarker        string  `yaml:"split_marker"`         // 分段标记，默认 "<<<SPLIT>>>"
    TypingDelayMs      int     `yaml:"typing_delay_ms"`      // 每字延迟基数(ms)，默认 60
    MaxTypingDelaySec  int     `yaml:"max_typing_delay_sec"` // 最大段间延迟(秒)，默认 4
    SkipPrefixes       []string `yaml:"skip_prefixes"`       // 忽略的消息前缀，默认 [".", "/", "#"]
}
```

`config.example.yaml` 新增：

```yaml
omoi:
  address: "https://nvli-omoi.centaurea.dev/api"
  api_key: ""
  agent_id: ""
  timeout_sec: 120
  max_message_len: 2000
  buffer_size: 20
  trigger_count: 15
  trigger_interval_sec: 180
  trigger_probability: 0
  enabled_groups: []
  skip_marker: "[SKIP]"
  split_marker: "<<<SPLIT>>>"
  typing_delay_ms: 60
  max_typing_delay_sec: 4
  skip_prefixes: [".", "/", "#"]
```

环境变量覆盖：`OMOI_API_KEY`、`OMOI_ADDRESS`、`OMOI_AGENT_ID`。

---

## 核心流程一览

### @bot 群聊

```
1. handleGroupMention 触发
2. 检查消息前缀 → 指令则 return
3. 从缓冲区取最近 20 条消息
4. BuildGroupMentionPrompt(群名, 历史, 触发消息)
5. 获取/创建群 session（title: "群:{群名}"）
6. client.SendMessage(session_id, prompt)
7. SendSplitReply(ctx, reply)  // 含 SKIP 检测 + 分段延迟
```

### 私聊

```
1. handlePrivateChat 触发
2. 检查消息前缀 → 指令则 return
3. BuildPrivatePrompt(昵称, QQ号, 文本)
4. 获取/创建私聊 session（title: "私聊:{昵称}"）
5. client.SendMessage(session_id, prompt)
6. SendSplitReply(ctx, reply)
```

### 主动发言

```
1. handleGroupObserve: 检查前缀 → 指令则跳过
2. 消息入缓冲
3. 满足触发条件 + 概率通过
4. 异步:
   a. BuildGroupActivePrompt(群名, 缓冲区消息)
   b. 获取/创建群 session
   c. client.SendMessage(session_id, prompt)
   d. SendSplitReply(ctx, reply)  // [SKIP] → 静默
```

### .chat-reset

```
1. 删除 KV 中对应的 session_id
2. 下次对话自动创建新 session
3. 回复确认消息
```

---

## 实现顺序

1. **Config** — 添加 OmoiConfig + example yaml + env 覆盖
2. **plugin/omoi/client.go** — HTTP 客户端 + SSE 解析
3. **plugin/omoi/session.go** — Session CRUD + KV 映射
4. **plugin/omoi/buffer.go** — 群消息环形缓冲 + 触发判定
5. **plugin/omoi/prompt.go** — Prompt 格式化构造
6. **plugin/omoi/sender.go** — 分段延迟发送
7. **plugin/omoi/init.go** — 处理器注册 + 前缀过滤
8. **cmd/bot/main.go** — 添加 `_ "github.com/colanns/gokohime/plugin/omoi"` import

---

## 验证

1. 启动 Gokohime bot，确认 omoi 插件加载日志
2. **@bot 测试**：群内 @bot 发消息 → 带上下文的回复，分段延迟发送
3. **指令过滤测试**：@bot `.help` / `#tag` → 无回复
4. **私聊测试**：私聊 bot → 独立会话回复
5. **主动发言测试**：设置 trigger_probability > 0，群内多条消息后 → bot 按概率触发
6. **分段测试**：诱导长回复 → 验证 <<<SPLIT>>> 拆分 + 延迟效果
7. **重置测试**：`.chat-reset` → 新 session
8. **降级测试**：Omoi 不可达 → 友好错误提示（不刷屏）

## 2026-09-11 行为修正

- 单独 @（没有正文）也会调用 Omoi，并提示模型自然回应；模型返回空内容或静默标记时，回复“我在，怎么啦？”。
- 主动聊天只在收到新的有效群消息时检查；达到消息数阈值且距上次主动触发达到间隔后，逐条按概率尝试，不是定时发言。概率 0 表示关闭，空群列表表示全部群。
- 主动触发的条件检查、概率判定与计数重置在同一把群缓冲锁内完成，避免并发重复触发。
- 主动聊天与 @ 回复共用失效会话重建；会话映射按 namespace/key 更新已有记录。
- 主动提示词使用配置的 skip_marker；模型选择静默时不发送 QQ 消息。
