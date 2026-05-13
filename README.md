# Gokohime

基于 [ZeroBot](https://github.com/wdvxdr1123/ZeroBot) 的 QQ 群聊机器人。

通过 OneBot V11 协议连接 QQ（需搭配 [NapCat](https://github.com/NapNeko/NapCatQQ) / [Lagrange](https://github.com/LagrangeDev/Lagrange.Core) 等实现端），提供娱乐、运势、随机图片、猜歌、临时禁用等群聊插件。AI Core 当前已从主程序入口关闭，相关代码暂留仓库以便后续独立服务迁移。

## 架构

```
ZeroBot (消息层) ───▶ 插件命令/消息处理 ───▶ GORM 数据层
                                           │
                                           ▼
                                    SQLite + WAL（默认）
                                    PostgreSQL（兼容保留）
```

## 功能一览

### 当前状态
- AI Core、`simplegpt`、`stickersaver` 当前不在主程序中注册。
- 数据库默认使用 SQLite，并在初始化时开启 WAL。
- PostgreSQL 初始化路径仍保留，便于后续迁回或做兼容测试。

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
| `.cg jstart` / `.猜歌` | 猜歌游戏 |
| `.cg hint` | 猜歌提示 |
| `.cg gg` | 放弃猜歌 |
| `.ban <命令> [分钟]` | 临时禁用命令（管理员） |
| `.unban <命令>` | 解禁命令（管理员） |

复读机：群内连续 3 条相同消息自动触发复读。

## 前置要求

- Go 1.22+
- OneBot V11 实现端（NapCat / Lagrange / LLOneBot 等）

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

### 3. 编译运行

```bash
go build -o gokohime .
./gokohime -c config.local.yaml
```

如果是用于开发，可以使用 `air` 来运行，直接运行 `air` 即可。

启用调试日志：

```bash
./gokohime -c config.local.yaml -d
```

### 4. 连接 OneBot

确保你的 OneBot 实现端（NapCat 等）设置好了与 config 中相同的链接。

## Docker 部署

```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache build-base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o gokohime .

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
  bot:
    build: .
    environment:
      - WEATHER_API_KEY=${WEATHER_API_KEY}
      - SEARCH_API_KEY=${SEARCH_API_KEY}
    volumes:
      - ./config.local.yaml:/app/config.yaml
      - ./data:/app/data
    restart: unless-stopped
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
├── internal/
│   ├── config/                      # 配置加载
│   ├── database/                    # GORM + SQLite/PostgreSQL
│   ├── aicore/                      # 暂未注册，保留待独立服务迁移
│   └── imageutil/                   # 图片下载压缩
├── plugin/                          # ZeroBot 插件（init() 自注册）
│   ├── simplegpt/                   # 暂未注册，待 AI Core 独立迁移
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
│   ├── stickersaver/                # 暂未注册，待 AI Core 独立迁移
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
| 日志 | [logrus](https://github.com/sirupsen/logrus) |
| 配置 | YAML + 环境变量 |

## License

MIT
