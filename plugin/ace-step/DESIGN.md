# ACE-Step DeepSeek Formatter 设计

## 目标

将 Gokohime 的 `.ace` 插件从 ACE-Step 内置 5Hz LM / sample mode / format_input 流程，改为使用 DeepSeek 做前置音乐规划，再沿用 legacy `/release_task` 提交给 ACE-Step 后端生成音频

核心要求：

- 沿用 legacy `/release_task`、`/query_result`、`/v1/audio`，不使用 `/v1/chat/completions`
- 不再调用 ACE-Step `/format_input`
- 不再使用 ACE-Step `sample_mode`
- ACE-Step `thinking` 固定为 `false`
- DeepSeek 负责输出 caption、lyrics、bpm、key_scale、time_signature、vocal_language、instrumental、duration
- duration 不写进 caption 或歌词
- duration 优先用户显式传入，否则使用 DeepSeek 推断值，最终上限 180 秒

## 当前插件现状

当前 `plugin/ace-step` 流程：

1. `.ace <prompt> | <lyrics> [duration]` 解析为 `generationInput`
2. 有显式 lyrics 时调用 `/format_input`
3. 无 lyrics 时设置 `SampleMode=true`，让 ACE-Step 内置 LM 通过 `sample_query` 生成完整样本
4. `/release_task` 中 `thinking=true`
5. `/query_result` 轮询
6. `/v1/audio` 下载并发送 QQ 语音

需要替换第 2、3、4 步，保留第 1、5、6 步和 legacy API 形态

## 新流程

```text
用户 .ace 命令
  ↓
parseGenerationInput 解析 prompt / lyrics / 显式 duration
  ↓
DeepSeek Formatter 生成结构化 JSON
  ↓
合并 duration：用户显式值 > DeepSeek duration > 默认值，最终 cap 到 180 秒
  ↓
构造 legacy /release_task payload
  ↓
ACE-Step 生成任务，thinking=false, sample_mode=false
  ↓
/query_result 轮询
  ↓
/v1/audio 下载
  ↓
QQ record 发送
```

## 用户命令格式

保持现有格式：

```text
.ace <音乐需求>
.ace <音乐需求> | <歌词>
.ace <音乐需求> | <歌词> <时长秒数>
```

例：

```text
.ace 日本VOCALOID摇滚 | 我不想上班
我真的不想上班 120
```

解析规则：

- 最后一个 token 是正整数时视为用户显式 duration
- duration 从用户文本中剥离后再发给 DeepSeek
- 显式 duration 不进入 DeepSeek prompt
- 显式 duration 不进入 ACE caption 或 lyrics

## 配置设计

扩展 `internal/config/config.go` 的 `AceStepConfig`：

```go
type AceStepConfig struct {
    APIKey          string `yaml:"api_key"`
    BaseURL         string `yaml:"base_url"`
    MaxDuration     int    `yaml:"max_duration"`
    DefaultDuration int    `yaml:"default_duration"`
    PollIntervalSec int    `yaml:"poll_interval_sec"`
    PollTimeoutSec  int    `yaml:"poll_timeout_sec"`

    DeepSeekAPIKey  string `yaml:"deepseek_api_key"`
    DeepSeekBaseURL string `yaml:"deepseek_base_url"`
    DeepSeekModel   string `yaml:"deepseek_model"`
    UseDeepSeekLM   bool   `yaml:"use_deepseek_lm"`
}
```

默认值：

```yaml
ace_step:
  api_key: ""
  base_url: "https://colasama--ace-step-api-serve.modal.run"
  max_duration: 180
  default_duration: 30
  poll_interval_sec: 3
  poll_timeout_sec: 300
  use_deepseek_lm: true
  deepseek_api_key: ""
  deepseek_base_url: "https://api.deepseek.com/v1"
  deepseek_model: "deepseek-v4-flash"
```

环境变量：

- `ACE_STEP_API_KEY`
- `ACE_STEP_BASE_URL`
- `ACE_STEP_DEEPSEEK_API_KEY`
- `ACE_STEP_DEEPSEEK_BASE_URL`
- `ACE_STEP_DEEPSEEK_MODEL`
- `ACE_STEP_USE_DEEPSEEK_LM`

`MaxDuration` 默认从 60 调整为 180，以符合本次规则

## DeepSeek 请求

请求端点：

```http
POST <deepseek_base_url>/chat/completions
Authorization: Bearer <deepseek_api_key>
Content-Type: application/json
```

请求体：

```json
{
  "model": "deepseek-v4-flash",
  "temperature": 0.7,
  "top_p": 0.9,
  "response_format": {"type": "json_object"},
  "messages": [
    {"role": "system", "content": "<system prompt>"},
    {"role": "user", "content": "<json input>"}
  ]
}
```

用户消息 JSON：

```json
{
  "user_prompt": "日本VOCALOID摇滚",
  "user_lyrics": "我不想上班\n我真的不想上班",
  "vocal_language_hint": "zh",
  "instrumental_hint": false,
  "has_explicit_duration": true
}
```

说明：

- 不传 `duration` 的具体数值给 DeepSeek
- `has_explicit_duration` 只告诉 DeepSeek 不要推断时长，但不暴露用户传入值
- 如果没有显式 duration，可以传 `has_explicit_duration:false`，允许 DeepSeek 返回 `duration`

## DeepSeek 输出 JSON

DeepSeek 必须只输出 JSON 对象：

```json
{
  "caption": "Japanese VOCALOID rock, energetic guitars, crisp punk-pop drums, bright synthetic female vocal, anime opening energy, punchy bass, polished studio mix, catchy melodic chorus, dynamic intro-to-chorus build",
  "lyrics": "[Intro - guitar riff]\n\n[Verse 1]\n我不想上班\n闹钟又在呼喊\n\n[Chorus - anthemic]\n我不想上班\n让我逃进云端",
  "bpm": 176,
  "key_scale": "D major",
  "time_signature": "4/4",
  "vocal_language": "zh",
  "instrumental": false,
  "duration": 120
}
```

字段规则：

- `caption` 必填
- `lyrics` 可为空，但纯音乐建议 `[Instrumental]`
- `bpm` 可选，合法范围 30 到 300
- `key_scale` 可选，例如 `C major`、`A minor`、`D major`
- `time_signature` 可选，默认 `4/4`
- `vocal_language` 可选，默认由本地检测或 `unknown`
- `instrumental` 必填，无法判断时由本地根据 lyrics 兜底
- `duration` 可选，仅在用户没有显式 duration 时使用

## DeepSeek system prompt

```text
你是 ACE-Step 1.5 音乐生成规划器

你的任务是把用户的音乐需求改写成适合 ACE-Step legacy /release_task 直接渲染的结构化 JSON

你熟悉 ACE-Step 1.5 的控制方式：
caption 描述整体音乐画像，包括风格、流派、情绪、氛围、乐器、音色质感、制作风格、人声特点、时代参考、结构倾向
lyrics 描述时间脚本，包括歌词、段落结构、演唱方式、器乐段落、能量变化、开始和结束方式
bpm、key_scale、time_signature、vocal_language、instrumental、duration 是独立元数据，不要写进 caption

你也熟悉 The Complete Guide to Mastering Suno 中的专业音乐提示词思路，请运用其中关于结构标签、风格锚点、动态演进、可唱歌词、段落控制、重复强化、避免冲突提示的原则，但不要提及文章名

输出要求：
只输出 JSON 对象
不要输出 markdown
不要输出解释
不要输出思考过程
不要把 duration、具体秒数、BPM、调性、拍号写进 caption
不要虚构真实歌手名的仿作请求，若用户要求某歌手风格，改写为抽象音乐特征
如果 has_explicit_duration 为 true，可以省略 duration 或输出 null
如果 has_explicit_duration 为 false，可以根据歌曲结构推断 duration，但不要超过 180

JSON schema：
{
  "caption": "string",
  "lyrics": "string",
  "bpm": 120,
  "key_scale": "C major",
  "time_signature": "4/4",
  "vocal_language": "zh|ja|en|unknown",
  "instrumental": false,
  "duration": 120
}

caption 写作要求：
具体优于模糊
组合多个维度
覆盖风格、情绪、乐器、音色、人声、制作、结构等可控维度
避免互相冲突的风格堆叠
如果用户需求很短，补全为可生成的音乐画像
如果用户需要惊喜，可以保留适度自由度
参考下面这种 caption 密度和专业粒度，但不要固定复刻内容：
A mid-tempo Mandopop ballad built on a steady electronic drum machine groove and a clean synth bassline. The arrangement features layered synthesizers, including chordal pads and melodic counterpoints, complemented by a clean electric guitar playing arpeggiated figures. The emotional male lead vocal, sung in Mandarin, soars through the verses and choruses, often reaching into a powerful falsetto. The track includes an expressive, melodic electric guitar solo and a dynamic bridge that builds tension before a final, passionate chorus filled with vocal ad-libs and a climactic guitar flourish.

lyrics 写作要求：
如果用户提供歌词，保留核心文本和语义，只做结构化、断行、补充必要段落标签
如果用户没提供歌词但不是纯音乐，创作完整歌词
如果是纯音乐，输出 [Instrumental] 或器乐结构标签
每段之间空行分隔
标签简洁，不要堆叠过多修饰
避免 AI 味歌词：空泛意象堆砌、混乱押韵、段落边界模糊、隐喻混用、每行太长不可唱
```

注意，本版本已删除“Caption 与 Lyrics 必须一致”作为硬性 prompt 文案

## Duration 合并规则

实现函数建议：

```go
func resolveDuration(input generationInput, formatted *deepSeekFormatResult, defaultDuration int) float64 {
    var duration float64
    switch {
    case input.DurationSet:
        duration = input.Duration
    case formatted != nil && formatted.Duration > 0:
        duration = formatted.Duration
    default:
        duration = float64(defaultDuration)
    }
    if duration > 180 {
        duration = 180
    }
    if duration <= 0 {
        duration = float64(defaultDuration)
    }
    return duration
}
```

规则：

1. 用户显式 duration 优先级最高
2. 用户未显式传入时，使用 DeepSeek 推断 duration
3. DeepSeek 未返回合法 duration 时，使用默认 duration
4. 最终统一 cap 到 180 秒
5. duration 只进入 legacy payload 的 `audio_duration`

## Legacy `/release_task` payload

提交给 ACE-Step 的数据：

```json
{
  "prompt": "<DeepSeek caption>",
  "lyrics": "<DeepSeek lyrics>",
  "vocal_language": "zh",
  "instrumental": false,
  "bpm": 176,
  "key_scale": "D major",
  "time_signature": "4/4",
  "audio_duration": 120,
  "thinking": false,
  "inference_steps": 8,
  "audio_format": "mp3",
  "batch_size": 1,
  "sample_mode": false
}
```

必须保证：

- `thinking=false`
- `sample_mode=false`
- 不发送 `sample_query`
- 不依赖 `/format_input`
- 不依赖 `/v1/create_sample`

## 文件改动

### `plugin/ace-step/formatter.go`

新增 DeepSeek formatter：

- OpenAI-compatible request/response structs
- `formatWithDeepSeek(input generationInput, hasExplicitDuration bool) (*deepSeekFormatResult, error)`
- JSON 解析和字段兜底
- system prompt 常量

### `plugin/ace-step/init.go`

调整 `handleAce`：

- 移除 `client.formatInput` 调用
- 移除 `SampleMode=true` 分支
- 调用 DeepSeek formatter
- 应用 DeepSeek 结果
- 解析并覆盖 duration

### `plugin/ace-step/client.go`

调整 `releaseTaskPayload`：

- `Thinking: false`
- `InferenceSteps: 8`
- `SampleMode: false`
- 不设置 `SampleQuery`
- `AudioDuration` 使用 `resolveDuration` 后的值

保留现有 `/release_task`、`/query_result`、`downloadAudio` 逻辑

### `internal/config/config.go`

增加 DeepSeek 配置和 env override

### `config.example.yaml`

补充 `ace_step` 示例配置

### `plugin/ace-step/client_test.go`

增加测试：

- 用户显式 duration 优先于 DeepSeek duration
- 用户无显式 duration 时使用 DeepSeek duration
- duration 大于 180 时 cap 到 180
- DeepSeek prompt 不包含具体用户 duration
- legacy payload `thinking=false`
- legacy payload `sample_mode=false`
- legacy payload 不包含 `sample_query`
- legacy payload 使用 `audio_duration`

## 失败策略

建议：DeepSeek 失败时直接返回错误，不 fallback 到 ACE-Step 内置 LM

理由：fallback 会悄悄重新启用内置语言模型，和“替代内置语言模型、关闭 thinking”目标冲突

用户提示：

```text
❌ 音乐规划失败：<错误>
```

## 验证命令

```bash
go test ./plugin/ace-step ./internal/config
go build ./...
```

如果部署到 Docker：

```bash
docker builder prune --filter type=exec.cachemount -f
docker compose build --no-cache bot
docker compose stop bot
docker compose rm -f bot
docker compose create bot && docker compose start bot
docker compose logs bot --tail 50
```
