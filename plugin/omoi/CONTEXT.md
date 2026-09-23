# 上下文组织优化：增量对话协议（delta context）

日期：2026-09-23
状态：**已实现**（无配置开关，直接切换；存量会话历史已同步迁移）。
范围：`plugin/omoi` 插件改动 + Omoi 数据库历史消息重写 + Agent 系统提示词补充 `[SKIP]` 语义。Omoi 服务端代码零改动。

---

## 1. 现状与重复来源（已实测确认）

每条需要回复的 QQ 消息，插件都把**整个 20 条环形缓冲区**序列化成一段「最近聊天记录」作为一个 user message 发给 Omoi；Omoi 把每条 user message 持久化，`buildContext` 再加载**全部未压缩历史**。于是同一条群消息被反复携带。

生产库实测（session `01M2PCAH4830RAXHZ0QW4C9M9F`，群「鸽子姬医生的精神科候诊区」）：

| 指标 | 数值 |
|---|---|
| 已存 user 消息 | 312 条（≈483 KB） |
| 聊天记录行实例总数 | 5,636 |
| 去重后实际群消息 | 1,127 |
| **平均重复倍数** | **5.0×，单条最高 15×** |
| `【QQ 回复格式】` 模板 | 每条 user 消息一份，共 312 份 |
| 单次请求 token（deepseek-flash） | 37k–118k |

重复实际发生在四层：

1. **快照重复（主因）**：消息在 20 条窗口内时，每次 bot 回合都重发一遍；窗口滑动前最多重复 ~20 次。
2. **格式模板重复**：`withReplyFormat` 给每个 user message 追加约 150 字符的 SPLIT/SKIP 说明，永久留在会话历史里（DESIGN.md 中"Repeat the transport format near each request"是刻意的，但代价是全历史各一份）。
3. **附件重复**：`selectMedia` 从历史缓冲区选附件，同一张图在窗口存续期内每个回合都重新作为 content block 进模型上下文。
4. **三层信息同源**：同一句话同时存在于 ①N 份快照、②memory_events 证据、③召回注入的记忆事实。②是去重后的记忆证据管道（合理），③有 ~1.5KB 预算（合理）——问题只在 ①。

服务端另有兜底：32k token 阈值触发压缩。但压缩输入本身就含大量重复快照——摘要成本和噪声都被放大。

## 2. 设计原则（业界实践映射）

- **会话即流水（session-as-transcript）**：Omoi session 天然持久化每条消息。多人群聊 agent 的正确做法是把每条群消息**恰好投递一次**，会话历史本身就是连续 transcript（GroupGPT 的 chat state `C_i` 模型：状态在服务侧，客户端只提交增量）。
- **最小高信号 token 集**（Anthropic context engineering）：重复快照不产生新信息，只稀释注意力预算。相同 token 预算下，增量协议换来 ~5× 的实际对话深度。
- **新会话才需要窗口**（multica 的 `<recent_context>` 实践）：仅在会话为空（新建/重建/重置）时投递一次有界窗口做种子，之后全部增量。
- **发言人身份靠稳定 ID**：保持现有 `[时间] 昵称(QQ号): 文本` 行格式不变，模型已适配。
- **记忆层归记忆，流水层归流水**：memory_events / 召回注入不动，消除的是流水层的重复。

## 3. 协议设计

### 3.1 投递游标

`GroupBuffer` 增加单调序号与发送游标（均在 `b.mu` 下维护）：

```go
type BufferedMessage struct {
    seq uint64   // 新增：入缓冲时分配，单调递增
    // ...existing fields
}

type GroupBuffer struct {
    mu       sync.Mutex
    messages []BufferedMessage
    nextSeq  uint64 // 下一条分配的 seq
    sentSeq  uint64 // 已成功投递到当前 session 的最大 seq
    sendMu   sync.Mutex // 串行化「取增量 → 发送 → 推进游标」
    count    int
    lastSent time.Time
}
```

- `Push` / `Append` 分配 `seq = ++nextSeq`。
- `delta()` 返回 `seq > sentSeq` 的消息；若被缓冲淘汰产生空洞（`oldest.seq > sentSeq+1`），在增量块开头标注 `（中间省略 N 条更早消息）`。
- 仅当本次 `SendMessage` 成功返回才 `sentSeq = lastSentSeq`；失败不推进 → 下次回合自然重发（每批最多 20 行，偶发重复可接受，且会被压缩吸收）。
- 进程重启 sentSeq=0 → 下次发送附带整个缓冲区（≤20 行，一次性，无害）。

### 3.2 发送串行化

mention 与主动触发都可能并发向同一群 session 发送。`sendMu` 保证「取增量→发送→推进游标」是原子的，防止两个请求携带重叠增量造成重复。Omoi 服务端本来就按 session 串行执行，插件侧加锁语义一致且不阻塞缓冲写入（缓冲锁与发送锁分离）。

### 3.3 新会话种子

`getOrCreateSessionAfterFailure` 返回 `created bool`。群路径（`sendGroupMessage`）：

```
id, created := getGroupSession(...)
if created {
    b.ResetCursor()              // sentSeq = oldest_buffered.seq - 1
    prompt = BuildGroupSeedPrompt(...)   // 现有"最近聊天记录"全量格式
} else {
    prompt = BuildGroupDeltaPrompt(...)  // 增量格式
}
send → 成功后 MarkSent(delta 最大 seq)
404 → 重建会话 → 以种子格式重发
```

覆盖场景：首次使用、`.chat-reset`、session 服务端过期重建——新会话都能获得完整近期上下文，行为与今天一致。

### 3.4 Prompt 格式

增量-被@（delta 末尾即触发消息，保留一行指向标注）：

```
群「鸽子姬医生的精神科候诊区」的新消息：
---
[2026-09-23 15:14] 浪隐波风(2469158962): 再来几首金属一点的术力口一图流歌曲
[2026-09-23 15:15] Colanns(719147538): @鸽子姬 来点适合乐队演奏的
---
⬇️ 需要回复的消息：
[Colanns(719147538)]: @鸽子姬 来点适合乐队演奏的
```

增量-主动触发：同样头部 + 现有 `你在旁边听到了这些对话……[SKIP]` 结尾。

种子：沿用现有 `以下是群「X」的最近聊天记录：---…` 格式不变（行为兼容）。

私聊：本来就只发单条，不改。

### 3.5 格式说明去重

`【QQ 回复格式】`（SPLIT/SKIP 说明）不再随每条 user message 附加（原先每份 ~150 字符并永久留在历史）。SPLIT 用法已在 Agent 系统提示词【发送格式】中；`[SKIP]` 语义补充进同一节；定时任务投递绑定继续携带 `reply_instruction`。

### 3.6 附件选择

`selectMedia` 改为只看**本次增量**中的媒体：历史附件已作为 content block 留在会话早前消息里，无需重复携带。当前/被回复消息的附件逻辑不变。上传缓存（23h）保留。

### 3.7 不动的部分

- `captureObservedMemory` 逐条静默采集 + `SendMessage` 携带 `memory_events`：这是记忆证据管道与失败重放机制，服务端按 `(space, external_id)` 去重，与模型上下文无关，保持不变。
- 记忆召回注入（服务端，~1.5KB 预算）：不变。
- 压缩（32k 阈值）：机制不变，但输入从重复快照变为干净流水，摘要质量更好、触发更晚。
- 定时任务 delivery、`channel_context`、SPLIT/SKIP 出站处理：不变。

## 4. 失败语义

| 场景 | 行为 |
|---|---|
| `SendMessage` 在持久化前失败（404/400/网络错） | 游标不动，下回合重发该增量 |
| 已持久化但 LLM/流失败 | 游标不动，下回合增量含重复行（≤20 行一次性，压缩会吸收）；选择"可重复不丢失"是因为缺上下文比轻微重复更伤害群聊应答 |
| 缓冲淘汰快于发送 | 增量开头标注 `（中间省略 N 条）` |
| 插件重启 | sentSeq 归零，首回合带全缓冲（一次性 ≤20 行） |
| `.chat-reset` / 404 重建 | 种子全量 + 游标重置，UX 与今天一致 |

## 5. 收益估算（按实测外推）

- 群消息在会话中从平均 **5× → 1×**；最热 session 的 user 存储 ≈483KB → ≈100KB。
- 模型每回合看到的有效对话深度在同 token 预算下提升 ~5×（原来窗口只有 20 条，现在是压缩阈值内的完整流水）。
- 312 份格式模板 → 0（迁入 system prompt 后）。
- 附件跨回合重复携带 → 仅首达时携带。
- 主动触发与 @ 共用同一条流水，模型看到的是"群聊实况 + 自己说过的话"，而不是一堆相似快照。

## 6. 实施记录

1. `buffer.go`：`BufferedMessage.seq` + `GroupBuffer.nextSeq/sentSeq/sendMu`，`pending(seed)`、`markSent`。
2. `prompt.go`：新增 `BuildGroupDeltaPrompt`；种子沿用 `BuildGroupMentionPrompt`/`BuildGroupActivePrompt`；`withReplyFormat` 不再用于聊天消息，仅保留给 delivery `reply_instruction`。
3. `session.go`：`getOrCreateSessionAfterFailure` 返回 `created`；新 `sendGroupMessage(buf, groupID, groupName, trigger, refs)` 在 `sendMu` 内完成取增量→发送→推进游标，404 重建后以种子格式重发。
4. `init.go`：mention 的 trigger 先入缓冲（`Append` 返回 seq）再取增量；主动触发取增量发送。
5. `media.go`：`selectMedia` 输入改为增量消息列表（由 `sendGroupPrompt` 内调用）。
6. `client.go`：`SendMessage` 不再追加格式说明。
7. 测试：`TestDeltaPromptsSendEachGroupMessageOnce`（种子+增量不重复）、`TestPendingCursorTracksGapsAndResends`（游标/空洞/种子）。
8. 数据迁移：`/tmp/migrate_omoi_context.py`——群会话未压缩 user 消息内转录行按会话去重、首条保留种子头、其余改增量头、剥离 `【QQ 回复格式】`；私聊消息剥离模板；Agent 系统提示词补 `[SKIP]`。

## 7. 可选的后续（需动 Omoi，仅列出不实施）

- 会话 API 支持"追加事件流"语义（服务端原生理解 connector 增量，省掉 prompt 文本协议）。
- 记忆召回对"刚在流水中出现过的原句"降权，进一步消重。
- API Key 会话的历史 UI 隐藏传输模板噪音。
