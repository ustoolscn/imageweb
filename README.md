# Image Web

一个纯中文的图片生成工作台，前端使用 Vue 3 + Vite + TypeScript，后端使用 Go，并且只使用 MySQL 8.0+ 持久化。页面不需要登录，通过 URL 或页面设置配置 `baseurl` 和 `apikey` 后保存到浏览器本地，并由后端异步执行图片生成任务。

## 功能特性

- 无登录访问，通过 URL 参数配置生成接口。
- 支持 GPT Image 2 文生图和参考图编辑生成。
- 任务在后端异步执行，关闭页面后仍会继续生成。
- 使用 MySQL 保存任务历史、请求源数据、响应源数据和生成结果。
- 支持状态筛选、提示词搜索、收藏和只看收藏。
- 支持参考图上传、复用任务配置、图片预览放大。
- 上传参考图和生成结果都会保存到阿里云 OSS 内置图库。
- 后端自动生成缩略图，列表、广场和参考图区域优先加载缩略图，降低弱网下的图片加载压力。
- 视频素材会在上传、URL 导入或生成完成时固化首帧/尾帧图片，列表、广场和画布使用这些图片做封面和尾帧输入，不再由浏览器临时跨域读取视频抽帧。
- Docker 一键部署，允许 iframe 嵌入。

## 使用方式

部署后用下面的格式打开页面：

```text
http://你的域名或IP:8080/?baseurl=https://api.example.com&apikey=sk-xxx
```

页面会：

1. 读取 URL 中的 `baseurl` 和 `apikey`。
2. 保存到 `localStorage`。
3. 自动清理地址栏中的敏感参数。
4. 后续刷新页面时继续使用本地保存的配置。

## Docker 部署

先复制配置模板：

```bash
cp .env.example .env
```

按需修改 `.env` 后启动。默认 Compose 只启动应用，适合使用远程 MySQL：

```bash
docker compose up -d --build
```

如果希望一并启动内置 MySQL，叠加本地数据库配置：

```bash
docker compose -f docker-compose.yml -f docker-compose.mysql.yml up -d --build
```

默认访问地址：

```text
http://localhost:8080
```

默认 Docker Compose 只启动 `image-web` 服务：

- 图片会保存到阿里云 OSS 内置图库；应用临时文件使用系统临时目录，不需要额外挂载持久化目录。
- 视频封面和图片缩略图通过阿里云 OSS 图片/视频处理 URL 生成，容器内不再需要视频抽帧工具。
- 使用 `docker-compose.mysql.yml` 时会额外启动 `mysql`，并把 MySQL 数据保存在 Docker volume：`mysql-data`。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | 后端监听端口 |
| `DATABASE_DSN` | 无 | MySQL 连接串；远程数据库部署必填 |
| `APP_CREDENTIAL_KEY` | 无 | 加密保存上游 API Key 的应用密钥；必填，部署后需保持稳定 |
| `OSS_REGION` | 无 | 阿里云 OSS region，例如 `cn-hangzhou`；如果误填成 `oss-cn-hangzhou`，后端会自动兼容修正 |
| `OSS_BUCKET` | 无 | 阿里云 OSS bucket |
| `OSS_ENDPOINT` | 空 | 自定义 OSS endpoint，例如 `https://oss-cn-hangzhou.aliyuncs.com` |
| `OSS_PUBLIC_BASE_URL` | 无 | OSS 公开访问域名，用于生成素材 URL；部署必填 |
| `OSS_PREFIX` | `imageweb` | 图库对象 key 前缀 |
| `OSS_ACCESS_KEY_ID` | 无 | 阿里云访问密钥 ID；部署必填 |
| `OSS_ACCESS_KEY_SECRET` | 无 | 阿里云访问密钥 Secret；部署必填 |
| `GALLERY_ADMIN_USERNAME` | `admin` | 图库管理页面用户名 |
| `GALLERY_ADMIN_PASSWORD` | 无 | 图库管理页面密码；为空时管理登录不可用 |
| `GALLERY_SESSION_SECRET` | `APP_CREDENTIAL_KEY` | 图库管理 Cookie 签名密钥 |

## 图库配置

项目只保留阿里云 OSS 内置图库，不再支持 `http-json`、`local` 或旧的内置 HTTP 文件服务 provider。

```env
OSS_REGION=cn-hangzhou
OSS_BUCKET=your-bucket
OSS_ENDPOINT=https://oss-cn-hangzhou.aliyuncs.com
OSS_PUBLIC_BASE_URL=https://files.example.com
OSS_PREFIX=imageweb
OSS_ACCESS_KEY_ID=your-access-key-id
OSS_ACCESS_KEY_SECRET=your-access-key-secret
GALLERY_ADMIN_USERNAME=admin
GALLERY_ADMIN_PASSWORD=change-me
```

上传规则：

- 用户本地上传的图片、视频、音频会由前端计算 SHA-256，后端按该 SHA-256 查询 MySQL 的 `gallery_assets`；命中则直接返回历史 URL，不再上传 OSS。
- 后端按需求信任前端传入的 SHA-256，不重新计算校验。
- AI 生成结果会直接上传 OSS，不做查重校验。
- 用户提供的远程 URL 素材不转存、不上传，只按原 URL 使用；管理端会从用户任务里的参考素材读取这些外部 URL 引用，方便追溯任务参数。
- 上传时只上传原始文件；缩略图和视频帧 URL 使用阿里云 OSS 图片/视频处理参数生成，不再额外上传缩略图，也不再由后端抽帧。

图库管理页面不显示在主导航中，通过隐藏路径 `/gallery-admin` 访问；登录后页面复用普通任务页和画布页的外观和交互，并在顶部增加任务/画布切换和切换用户按钮。选择用户后，管理端按用户视角查看其任务、画布、参考素材、AI 生成结果和外部 URL 引用，并可在详情和源数据中追溯关联任务、模型、尺寸、质量和 prompt 等参数。管理端是只读视角，列表和画布节点资源统一使用缩略图、视频封面或首帧等预览图，详情主图可加载原图。
## 数据库配置

当前项目只支持 MySQL 8.0+。后端启动时会通过 `DATABASE_DSN` 连接数据库，并按最新 schema 自动创建所需表和索引；如果检测到旧 schema，会直接拒绝启动，请使用空库或重建数据库。

默认 Docker Compose 不启动 MySQL，请在 `.env` 中配置远程数据库连接：

```env
DATABASE_DSN=user:password@tcp(host:3306)/image_web?parseTime=true&charset=utf8mb4&loc=UTC
APP_CREDENTIAL_KEY=change-me-to-a-random-secret-at-least-32-chars
```

如果使用内置 MySQL，叠加 `docker-compose.mysql.yml`，连接信息固定在该文件中：

```env
DATABASE_DSN=image_web:image_web@tcp(mysql:3306)/image_web?parseTime=true&charset=utf8mb4&loc=UTC
APP_CREDENTIAL_KEY=change-me-to-a-random-secret-at-least-32-chars
```

直接编译或 `go run` 部署时，改成你的 MySQL 地址；本机数据库通常使用 `localhost`：

```env
DATABASE_DSN=user:password@tcp(localhost:3306)/image_web?parseTime=true&charset=utf8mb4&loc=UTC
APP_CREDENTIAL_KEY=change-me-to-a-random-secret-at-least-32-chars
```

项目提供 `.env.example`，部署时复制为 `.env` 并修改即可。`.env` 会被 `.gitignore` 忽略，适合保存数据库密码等本地配置。

`APP_CREDENTIAL_KEY` 用于 AES-GCM 加密用户通过页面传入的上游 API Key。生产环境请设置为随机长字符串，并在升级、重启、重建容器时保持不变；如果更换该值，历史任务所属 workspace 中保存的 API Key 将无法解密，后台运行/轮询任务会失败。

数据库当前采用 workspace + 任务拆表结构：`workspaces` 保存 `base_url + api_key_hash` 和加密后的 API Key，`tasks` 保存轻量任务状态与参数，`task_sources` 保存请求/响应源数据，`task_media_assets` 保存参考素材和结果素材及视频首尾帧字段，`canvases` 一张画布一行，`plaza_items` / `plaza_likes` 保存广场分享与点赞。旧 SQLite、旧 PostgreSQL 或旧 schema 数据不会自动迁移；如果需要历史数据，请单独导出并按新结构导入。

## 本地开发

### 后端

先准备 MySQL 8.0+，并在项目根目录 `.env` 中设置 `DATABASE_DSN`。

```bash
cd backend
# 可选：也可以在项目根目录准备 .env，后端会自动读取上级目录的 .env
go run ./cmd/server
```

后端默认监听：

```text
http://localhost:8080
```

### 前端

```bash
cd frontend
npm install
npm run dev
```

前端默认监听：

```text
http://localhost:5173
```

开发环境下，前端会把 `/api` 请求代理到 `http://localhost:8080`。

## 构建验证

前端构建：

```bash
npm --prefix frontend run build
```

后端测试：

```bash
cd backend
go test ./...
```

## 生成接口说明

当前后端按 GPT Image 2 接口实现：

- 无参考图：请求 `{baseurl}/v1/images/generations`。
- 有参考图：请求 `{baseurl}/v1/images/edits`，参考图通过 multipart form-data 的 `image` 字段传入。

当前发送参数包括：

- `model`: 固定为 `gpt-image-2`
- `prompt`
- `n`
- `size`
- `quality`
- `output_format`
- `output_compression`：仅 `jpeg` / `webp` 时发送
- `background`
- `moderation`

## 注意事项

- 后端不会在任务/画布表中明文保存 `apikey`；workspace 表只保存 hash 和加密后的 API Key。
- 当前只支持 MySQL 8.0+；请不要再配置 SQLite / PostgreSQL 相关变量。
- 请不要把 `.env`、`data/`、`node_modules/` 或构建产物提交到仓库。
- 当前项目的 `.gitignore` 已默认排除这些本地文件。
