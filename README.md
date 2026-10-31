# Gokohime

基于 [ZeroBot](https://github.com/wdvxdr1123/ZeroBot) 的 QQ 群聊机器人。

通过 OneBot V11 协议连接 QQ（需搭配 [NapCat](https://github.com/NapNeko/NapCatQQ) / [Lagrange](https://github.com/LagrangeDev/Lagrange.Core) 等实现端），提供娱乐、运势、随机图片、猜歌、临时禁用等群聊插件，并提供独立 Web Admin 进程查看管理 Bot 数据。

## 架构

```
cmd/bot   ──▶ ZeroBot 插件 ─┐
                            ├──▶ GORM ──▶ SQLite + WAL（默认）
cmd/admin ──▶ Web Admin ────┘

web/      ──▶ React + Rsbuild + shadcn/ui 风格管理界面
```

## 功能一览

### 当前状态
- 数据库默认使用 SQLite，并在初始化时开启 WAL。
- PostgreSQL 初始化路径仍保留，便于后续迁回或做兼容测试。
- Bot 和 Web Admin 是同一 Go module 下的两个可执行入口，共享同一个 SQLite 文件。

### 娱乐插件

| 命令 | 功能 |
|------|------|
| `.help` / `.帮助` | 帮助菜单 |
| `.eat` / `.吃什么` | 随机推荐吃什么 |
| `.eats` / `.零食` | 随机推荐零食 |
| `.jrluck` / `.luck` / `.运势` | 今日运势（每人每天固定） |
| `.chp A B` | CP 名生成 |
| `.cp A B` | CP 短打故事 |
| `.kk` / `.ktv` | KTV 随机推荐歌曲 |
| `.song` | 唱歌命令帮助；带参数同 `.sing` |
| `.sing [音源] 歌名或歌词` / `.ssing` | 自选音源翻唱，默认洛天依（DiffSinger，score） |
| `.bsong [音源] 歌名或歌词` / `.bmsong` | 带原曲伴奏的 score / mimic 翻唱 |
| `.msing [音源] 歌名或歌词` | 自选音源翻唱，默认洛天依（DiffSinger，mimic） |
| `.voices` | 查看音源和别名 |
| `.saying` / `.语录` | 随机语录 |
| `.pic [分类]` / `.随机图片` | 随机图片 |
| `.cg cstart` / `.猜歌` | 中V猜歌（内置歌单题库） |
| `.cg jstart` | 日V猜歌（内置歌单题库） |
| `.cg hint` | 猜歌提示 |
| `.cg gg` | 放弃猜歌 |
| `.ban <命令> [分钟]` | 临时禁用命令（管理员） |
| `.unban <命令>` | 解禁命令（管理员） |

复读机：群内连续 3 条相同消息自动触发复读。

### 点歌翻唱

使用 `.sing [音源] 歌名 [原唱] 或歌词`，`.sing` / `.ssing` 为 score，`.msing` 为 mimic。所有点歌命令的 `sing` / `song` 拼法互通：`.sing` / `.song`、`.ssing` / `.ssong`、`.msing` / `.msong`、`.bsing` / `.bsong`、`.bmsing` / `.bmsong`。默认音源为洛天依。音源名和别名来自 API 的 `/voices`，缓存五分钟；用 `.voices` 查看完整名单。

```text
.sing 洛 雨爱 杨丞琳
.sing 初中 歌名
.msing 心 歌名
.sing 雨爱 杨丞琳
.sing 初音 第一行歌词
第二行歌词
第三行歌词
```

日语初音使用「初日」，不设单字别名；其余音源的单字别名：绮萱「绮」、中文初音「初」、镜音铃「铃」、镜音连「连」、洛天依「洛」、言和「言」、心华「心」。支持多个别名，例如“洛天依／天依／洛”“初音中文／中文初音／初中／初”“心华／心”。仅在参数开头匹配完整名称或别名，名称后必须有空白（英文忽略大小写）。未命中时保留完整查询，使用洛天依；“心愿”“洛天依的消失”不会被拆开。名称已命中但音源不可用时明确报错，不切换音源。只输入音源名会提示补充歌名或歌词。

`.bsong` / `.bsing` 是带伴奏的 score 版本，`.bmsong` / `.bmsing` 是带伴奏的 mimic 版本。两者支持相同的音源别名、完整歌词和末尾秒数范围，不带参数时显示唱歌帮助。例如 `.bsong 洛 雨爱 杨丞琳`、`.bmsong 初 晴天 周杰伦 0-120`。机器人提交 `with_accompaniment=true`，只发送 `score_mix` / `mimic_mix` 及对应分段；若混音不可用则报错，不改发干声。服务端从原曲分离伴奏，保持与合成干声同一时间轴，可能存在少量原唱残留。

不带参数的 `.song` 显示唱歌命令帮助列表；带参数同 `.sing`。所有唱歌命令末尾可加独立的秒数范围，例如 `.sing 初 雨爱 杨丞琳 0-120`、`.msing 洛 歌名 12.5-90`。起止秒数必须满足 `0 ≤ 起始 < 结束`，优先于自动选段，不受 32/40 秒上限限制。保留范围内完整歌词行，裁剪加少量首尾缓冲并受歌曲实际时长约束。未提供范围时保持自动选段。

机器人先回复准备提示，再通过 `POST /resolve_song` 解析歌曲，完成后发送语音。一至两句歌词仍按原来的短片段逻辑选段。三句及以上经完整匹配后，按指定歌词的连续范围演唱，不受短片段目标时长、32/40 秒上限和间奏间隔限制，也不补唱额外歌词。换行和句末标点可用于分句；无分隔文本也可按三个完整 LRC 行判断。首尾部分歌词按整行边界处理。多句歌词未完整匹配会报错，不随机唱另一段；定位仍依赖上游搜索召回和 LRC 与音频版本一致。

服务端需先更新至支持 `/voices`、`POST /resolve_song`、`voice_id`、`lyric_extent` 和 `with_accompaniment` 的版本，并安装 VOCALOID 模型包，再更新机器人。

后台只提交所选模式，每三秒查询一次。这些命令共用等待限制：同一用户在同一会话中只能同时等待一首，群聊和私聊均支持；等待不会占用命令处理器。普通任务默认等待二十分钟；指定或匹配范围超过 32 秒时，生成等待预算额外增加每秒音频一分钟。失败或超时会回复提示，Bot 重启不恢复未完成等待，超时不取消服务端任务。

配置 `sing.api_key` 或环境变量 `DSAPI_KEY`。`sing.base_url` 默认是 `https://kirameki-dsapi.colanns.me`，也可用 `DSAPI_BASE_URL`；`sing.poll_interval_sec`、`sing.poll_timeout_sec` 调整轮询和基础等待时间。配置文件值优先。

音频由 Bot 携带 Bearer Key 下载后以 base64 语音发送，不向 QQ 暴露密钥，也不要求共享文件系统。每次下载上限 32 MiB，音频不落盘；服务端提供分段时按顺序逐段下载并发送，保留完整内容。音频由所选声库和 DiffSinger 引擎进行 AI 合成。

### 猜歌与按需缓存

Bot 启动时会将以下元数据快照增量合并到中V、日V题库（2026-10-30），按各题库内的网易云歌曲 ID 去重。同名但不同 ID 的版本保留；已有自定义条目和元数据不覆盖，软删除的歌曲不恢复。即使已有旧题库也会补入缺少的歌曲。

| 题库 | 旧数据 | 歌单 | 重合歌曲 | 合并后 |
|------|--------|------|----------|--------|
| 中V | `cv.json`：646 条，645 个唯一 ID | [8854508490](https://music.163.com/#/playlist?id=8854508490)：282 首 | 84 首 | 843 首 |
| 日V | `rv.json`：685 条，684 个唯一 ID | [3160107494](https://music.163.com/#/playlist?id=3160107494)：960 首 | 658 首 | 986 首 |

中V有 15 首、日V有 7 首暂时无法取得详情，保留 ID 并在抽到时重试；无法播放时跳过。各快照的 `merge_stats` 记录合并统计及缺失详情的 ID。启动不请求音乐接口，也不预下载任何音频；歌单后续变动不会自动同步。

使用 `.cg cstart` / `.猜歌` 抽中V，`.cg jstart` 抽日V，使用 `.cg 歌名` 作答。首次抽到歌曲时，优先通过[枫叶音乐盒](https://mu-jie.cc/musicBox/#/search)的网易云音源获取完整音频（接口 `https://fy-musicbox-api.mu-jie.cc/meting/`），直接复用现有网易云歌曲 ID。解析、下载或完整性校验失败时，回退到原网易云匿名播放接口；两个来源都不可用才换歌。下载后校验大小、校验和（接口提供时）及实际完整时长，再生成两段不重叠的 6 秒音频，试听片段不会作为完整歌曲缓存。无需 Cookie、yt-dlp 或额外 Node 服务。两个来源均不能完整播放的歌曲会被跳过，不能保证歌单内每首歌都可用；音乐盒的其他平台 ID 不与当前题库混用。

- 已验证的音频按歌曲 ID 存在 `data/guesssong/cache/`，缓存有效期间再次抽到时直接复用，每局重新随机切片。每次使用刷新保留时间。
- 每个群聊或私聊的当前切片存放在 `data/guesssong/sessions/`，不会将整份歌单预缓存。
- 下载先写临时文件，验证成功才进入缓存；损坏缓存会重新下载。同一首歌的并发请求共用缓存。
- 每次开局最多尝试 5 首歌曲，准备上限 90 秒；不可用歌曲在当前进程内暂避 30 分钟，接口限流时停止重试。
- 启动时和运行期间每 5 分钟清理一次：整曲缓存闲置 30 分钟后删除；正在下载或切片的文件不会被清理。崩溃遗留的过期下载临时文件也会清理。
- 答对、放弃或题目发送失败后立即删除本场切片；未结束的游戏从开局起 30 分钟后在下一次清理时结束并删除切片。清理兼容旧版会话目录，保留其他非音频文件。

本地运行需要安装 `ffmpeg`（含 `ffprobe`）；`Dockerfile.bot` 已包含这项依赖。音频发送仍由 OneBot 实现端处理。

## 前置要求

- Go 1.22+
- Node.js + pnpm（仅开发或构建 Web Admin 前端时需要）
- OneBot V11 实现端（NapCat / Lagrange / LLOneBot 等）
- ffmpeg + ffprobe（猜歌下载校验和切片；Bot Docker 镜像已安装）

## 快速开始

### 1. 克隆项目

```bash
git clone https://github.com/colanns/gokohime.git
cd gokohime
```

### 2. 配置

复制并编辑配置文件：

```bash
cp config.yaml config.local.yaml
```

编辑 `config.local.yaml`，至少填写以下内容：

```yaml
bot:
  rws_url: "ws://127.0.0.1:6700"  # 反向 WS 链接
  super_users: [123456789]         # 管理员 QQ 号

database:
  dialect: "sqlite"
  path: "data/gokohime.db"

admin:
  listen: "127.0.0.1:8080"
  jwt_secret: "change-me"
  initial_username: "admin"
  initial_password: "请改成首次登录密码"
```

SQLite 会在启动时自动启用 `journal_mode=WAL`、`foreign_keys=ON` 和 `busy_timeout=5000`。

如果需要保留或测试 PostgreSQL 兼容路径，可以改为：

```yaml
database:
  dialect: "postgres"
  host: "localhost"
  port: 5432
  user: "gokohime"
  password: "gokohime"
  dbname: "gokohime"
  sslmode: "disable"
```

也可以通过环境变量设置敏感信息，不写入配置文件：

```bash
export WEATHER_API_KEY="你的高德地图 API Key"
export SEARCH_API_KEY="你的豆包搜索 API Key"
```

### 3. 编译运行 Bot

```bash
go build -o bin/gokohime-bot ./cmd/bot
./bin/gokohime-bot -c config.local.yaml
```

如果是用于开发，可以使用 `air` 来运行，直接运行 `air` 即可。

启用调试日志：

```bash
./bin/gokohime-bot -c config.local.yaml -d
```

### 4. 运行 Web Admin

后端 API + 生产静态文件服务：

```bash
go build -o bin/gokohime-admin ./cmd/admin
./bin/gokohime-admin -c config.local.yaml
```

前端开发服务：

```bash
pnpm --dir web install
pnpm --dir web run dev
```

前端生产构建后由 Admin 后端托管：

```bash
pnpm --dir web run build
./bin/gokohime-admin -c config.local.yaml
```

### 5. 连接 OneBot

确保你的 OneBot 实现端（NapCat 等）设置好了与 config 中相同的链接。

## Docker 部署

项目提供 Bot 和 Admin 两个独立镜像，通过 `docker-compose.yml` 编排。

```bash
# 启动（首次或代码变更后自动构建）
docker compose up --build -d

# 查看日志
docker compose logs -f
```

### 开发模式（自动监听文件变更并重建）

```bash
docker compose watch
```

文件变更后 Docker 会自动重新构建镜像并重启容器。`data/` 和 `config.yaml` 通过 volume 挂载，不会触发重建。

### 生产部署

也可以单独构建镜像运行：

```bash
docker build -f Dockerfile.bot -t gokohime-bot .
docker build -f Dockerfile.admin -t gokohime-admin .

docker run -d --network host \
  -v ./data:/app/data \
  -v ./config.yaml:/app/config.yaml \
  gokohime-bot

docker run -d -p 23333:23333 \
  -v ./data:/app/data \
  -v ./config.yaml:/app/config.yaml \
  gokohime-admin
```

## 项目结构

```
gokohime/
├── cmd/
│   ├── bot/                         # QQ Bot 入口
│   ├── admin/                       # Web Admin 后端入口
│   ├── migrate-data/                # 数据迁移工具
│   └── merge-sayings/               # 语录维护工具
├── config.yaml                      # 默认配置
├── internal/
│   ├── admin/                       # Web Admin API / Auth / 静态托管
│   ├── config/                      # 配置加载
│   ├── database/                    # GORM + SQLite/PostgreSQL
│   └── imageutil/                   # 图片下载压缩
├── web/                             # React + Rsbuild 管理端前端
├── plugin/                          # ZeroBot 插件（init() 自注册）
│   ├── help/                        # 帮助菜单
│   ├── repeater/                    # 复读机
│   ├── eatwhat/                     # 吃什么
│   ├── jrluck/                      # 今日运势
│   ├── chp/                         # CP 名
│   ├── cp/                          # CP 短打
│   ├── kk/                          # KTV 随机歌
│   ├── saying/                      # 语录
│   ├── tempban/                     # 命令禁用
│   ├── randpic/                     # 随机图片
│   └── guesssong/                   # 猜歌游戏
└── data/                            # 运行时数据
```

## 可选配置

### 天气查询

注册 [高德开放平台](https://lbs.amap.com/) 获取 Web 服务 API Key，填入 `weather.api_key`。

### 网络搜索

配置豆包（或其他 OpenAI 兼容）搜索 API，填入 `search` 相关字段。

## 技术栈

| 组件 | 技术 |
|------|------|
| 消息框架 | [ZeroBot](https://github.com/wdvxdr1123/ZeroBot) (OneBot V11) |
| 数据库 | SQLite + WAL（默认），PostgreSQL（兼容保留） |
| ORM | [GORM](https://gorm.io/) |
| Web Admin | Go net/http + React + Rsbuild + Tailwind/shadcn/ui 风格 |
| 日志 | [charmbracelet/log](https://github.com/charmbracelet/log) |
| 配置 | YAML + 环境变量 |

## License

MIT
