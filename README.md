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
- Node.js + pnpm（仅开发或构建 Web Admin 前端时需要）
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
