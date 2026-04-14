# Gokohime

基于 [ZeroBot](https://github.com/wdvxdr1123/ZeroBot) + [agentsdk-go](https://github.com/stellarlinkco/agentsdk-go) 的 QQ 群聊 AI 机器人。

通过 OneBot V11 协议连接 QQ（需搭配 [NapCat](https://github.com/NapNeko/NapCatQQ) / [Lagrange](https://github.com/LagrangeDev/Lagrange.Core) 等实现端），使用 Claude / OpenAI 作为 AI 核心，以完整 Agent 模式运行——LLM 自主决定何时调用天气查询、网络搜索、长期记忆等工具。

## 架构

```
ZeroBot (消息层)                agentsdk-go (AI 核心)
  OnMessage ──────────────────▶  Agent Loop
  OnCommand (独立插件)            ├── Custom Tools (天气/搜索/记忆/表情)
                                 ├── Middleware (日志)
                                 └── AutoCompact (上下文压缩)
                                          │
                                          ▼
                                PostgreSQL + pgvector (数据层)
```

## 功能一览

### AI 对话
- @机器人 或随机概率触发 AI 对话（可配置概率和群白名单）
- Agent 自动调用工具：天气查询、网络搜索、记忆回忆/存储、表情包分析
- 流式响应，支持 `///` 分段发送，模拟自然聊天节奏
- 自动上下文压缩，长对话不丢失关键信息
- 人设通过 `.agents/rules/` 目录下的 Markdown 文件配置，支持热重载

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
| `.saying` / `.语录` | 随机语录 |
| `.pic [分类]` / `.随机图片` | 随机图片 |
| `.save` (回复表情) | 保存 QQ 表情 |
| `.cg jstart` / `.猜歌` | 猜歌游戏 |
| `.cg hint` | 猜歌提示 |
| `.cg gg` | 放弃猜歌 |
| `.ban <命令> [分钟]` | 临时禁用命令（管理员） |
| `.unban <命令>` | 解禁命令（管理员） |

复读机：群内连续 3 条相同消息自动触发复读。

## 前置要求

- Go 1.22+
- PostgreSQL 16+ 并安装 [pgvector](https://github.com/pgvector/pgvector) 扩展
- OneBot V11 实现端（NapCat / Lagrange / LLOneBot 等）
- Anthropic API Key 或 OpenAI API Key（至少一个）

## 快速开始

### 1. 启动 PostgreSQL

使用 Docker 最为便捷：

```bash
docker run -d \
  --name gokohime-db \
  -e POSTGRES_USER=gokohime \
  -e POSTGRES_PASSWORD=gokohime \
  -e POSTGRES_DB=gokohime \
  -p 5432:5432 \
  pgvector/pgvector:pg17
```

> pgvector 扩展会在程序启动时自动启用（`CREATE EXTENSION IF NOT EXISTS vector`），无需手动操作。

### 2. 克隆项目

```bash
git clone https://github.com/colanns/gokohime.git
cd gokohime
```

### 3. 配置

复制并编辑配置文件：

```bash
cp config.yaml config.local.yaml
```

编辑 `config.local.yaml`，至少填写以下内容：

```yaml
bot:
  rws_url: "ws://127.0.0.1:6700"  # 反向 WS 链接
  super_users: [123456789]         # 管理员 QQ 号

ai:
  anthropic_api_key: "sk-ant-..."  # 或通过环境变量 ANTHROPIC_API_KEY 设置
  model: "claude-sonnet-4-5-20250929"

database:
  host: "localhost"
  port: 5432
  user: "gokohime"
  password: "gokohime"
  dbname: "gokohime"
```

也可以通过环境变量设置敏感信息，不写入配置文件：

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export WEATHER_API_KEY="你的高德地图 API Key"
export SEARCH_API_KEY="你的豆包搜索 API Key"
```

### 4. 编译运行

```bash
go build -o gokohime .
./gokohime -c config.local.yaml
```

如果是用于开发，可以使用 `air` 来运行，直接运行 `air` 即可。

启用调试日志：

```bash
./gokohime -c config.local.yaml -d
```

### 5. 连接 OneBot

确保你的 OneBot 实现端（NapCat 等）设置好了与 config 中相同的链接。

## Docker 部署

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o gokohime .

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /app/gokohime .
COPY config.yaml .
COPY .agents .agents
ENTRYPOINT ["./gokohime"]
```

配合 `docker-compose.yml`：

```yaml
services:
  db:
    image: pgvector/pgvector:pg17
    environment:
      POSTGRES_USER: gokohime
      POSTGRES_PASSWORD: gokohime
      POSTGRES_DB: gokohime
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  bot:
    build: .
    depends_on:
      - db
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - WEATHER_API_KEY=${WEATHER_API_KEY}
      - SEARCH_API_KEY=${SEARCH_API_KEY}
    volumes:
      - ./config.local.yaml:/app/config.yaml
      - ./data:/app/data
    restart: unless-stopped

volumes:
  pgdata:
```

```bash
# 启动
docker compose up -d

# 查看日志
docker compose logs -f bot
```

## 项目结构

```
gokohime/
├── main.go                          # 入口
├── config.yaml                      # 默认配置
├── .agents/rules/                   # AI 人设规则（Markdown，支持热重载）
│   └── personality.md
├── internal/
│   ├── config/                      # 配置加载
│   ├── database/                    # GORM + PostgreSQL + pgvector
│   ├── aicore/                      # agentsdk-go 封装
│   │   ├── runtime.go               # Agent Runtime 管理
│   │   ├── middleware.go            # 中间件
│   │   └── tools/                   # 自定义 Agent Tools
│   │       ├── weather.go           # 天气查询（高德 API）
│   │       ├── websearch.go         # 网络搜索（豆包 API）
│   │       ├── memory_recall.go     # 记忆回忆（pgvector 语义搜索）
│   │       ├── memory_store.go      # 记忆存储
│   │       └── sticker.go          # 表情包搜索 & 分析
│   └── imageutil/                   # 图片下载压缩
├── plugin/                          # ZeroBot 插件（init() 自注册）
│   ├── simplegpt/                   # AI 对话（ZeroBot ↔ agentsdk-go 桥接）
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
│   ├── stickersaver/                # 表情保存
│   └── guesssong/                   # 猜歌游戏
└── data/                            # 运行时数据
```

## 自定义人设

编辑 `.agents/rules/personality.md` 即可修改 AI 的性格和行为规则，无需重启。

## 可选配置

### 天气查询

注册 [高德开放平台](https://lbs.amap.com/) 获取 Web 服务 API Key，填入 `weather.api_key`。

### 网络搜索

配置豆包（或其他 OpenAI 兼容）搜索 API，填入 `search` 相关字段。

### Embedding 模型

默认使用 `text-embedding-3-small`（512 维），用于记忆系统的语义搜索。可在 `embedding` 配置段调整模型和维度。

## 技术栈

| 组件 | 技术 |
|------|------|
| 消息框架 | [ZeroBot](https://github.com/wdvxdr1123/ZeroBot) (OneBot V11) |
| AI 核心 | [agentsdk-go](https://github.com/stellarlinkco/agentsdk-go) (Agent Loop + Tool Use) |
| 数据库 | PostgreSQL + [pgvector](https://github.com/pgvector/pgvector) |
| ORM | [GORM](https://gorm.io/) |
| 日志 | [logrus](https://github.com/sirupsen/logrus) |
| 配置 | YAML + 环境变量 |

## License

MIT
