# Image Web

一个纯中文的图片生成工作台，前端使用 Vue 3 + Vite + TypeScript，后端使用 Go，并且只使用 PostgreSQL 持久化。页面不需要登录，通过 URL 或页面设置配置 `baseurl` 和 `apikey` 后保存到浏览器本地，并由后端异步执行图片生成任务。

## 功能特性

- 无登录访问，通过 URL 参数配置生成接口。
- 支持 GPT Image 2 文生图和参考图编辑生成。
- 任务在后端异步执行，关闭页面后仍会继续生成。
- 使用 PostgreSQL 保存任务历史、请求源数据、响应源数据和生成结果。
- 支持状态筛选、提示词搜索、收藏和只看收藏。
- 支持参考图上传、复用任务配置、图片预览放大。
- 上传参考图和生成结果都会转存到图床后保存。
- 后端自动生成缩略图，列表、广场和参考图区域优先加载缩略图，降低弱网下的图片加载压力。
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

按需修改 `.env` 后启动。默认 Compose 只启动应用，适合使用远程 PostgreSQL：

```bash
docker compose up -d --build
```

如果希望一并启动内置 PostgreSQL，叠加本地数据库配置：

```bash
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d --build
```

默认访问地址：

```text
http://localhost:8080
```

默认 Docker Compose 只启动 `image-web` 服务：

- 图片会转存到配置的图床；应用临时文件使用系统临时目录，不需要额外挂载持久化目录。
- 使用 `docker-compose.postgres.yml` 时会额外启动 `postgres`，并把 PostgreSQL 数据保存在 Docker volume：`postgres-data`。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | 后端监听端口 |
| `DATABASE_DSN` | 无 | PostgreSQL 连接串；远程数据库部署必填 |
| `IMAGE_HOST_PROVIDER` | `http-json` | 图床适配器：`http-json` / `local` |
| `IMAGE_HOST_UPLOAD_URL` | `https://2bad.lujilujilujilujiluji.com/` | HTTP 图床上传接口 |
| `IMAGE_HOST_AUTH_HEADER` | `Authorization` | HTTP 图床鉴权 header 名；留空则不发送 |
| `IMAGE_HOST_AUTH_VALUE` | `Bearer cooper` | HTTP 图床鉴权 header 值；留空则不发送 |
| `IMAGE_HOST_FIELD_NAME` | `file` | multipart 文件字段名 |
| `IMAGE_HOST_RESPONSE_URL_PATH` | `url` | 返回 JSON 中图片 URL 路径，如 `url` 或 `data.url` |
| `IMAGE_HOST_LOCAL_DIR` | 系统临时目录下的 `image-web/uploads` | `local` 图床保存目录 |
| `IMAGE_HOST_PUBLIC_BASE_URL` | 空 | `local` 图床公开访问前缀 |

## 图床配置

默认配置使用 HTTP JSON 图床：

```env
IMAGE_HOST_PROVIDER=http-json
IMAGE_HOST_UPLOAD_URL=https://2bad.lujilujilujilujiluji.com/
IMAGE_HOST_AUTH_HEADER=Authorization
IMAGE_HOST_AUTH_VALUE=Bearer cooper
IMAGE_HOST_FIELD_NAME=file
IMAGE_HOST_RESPONSE_URL_PATH=url
```

如果你的 HTTP 图床返回 `{ "data": { "url": "..." } }`，可以改成：

```env
IMAGE_HOST_RESPONSE_URL_PATH=data.url
```

如果图床不需要鉴权，可以留空：

```env
IMAGE_HOST_AUTH_HEADER=
IMAGE_HOST_AUTH_VALUE=
```

也可以使用本地存储适配器：

```env
IMAGE_HOST_PROVIDER=local
IMAGE_HOST_LOCAL_DIR=/app/uploads
IMAGE_HOST_PUBLIC_BASE_URL=https://your-domain.com/uploads/
```

注意：`local` 适配器只负责保存文件并返回 URL。使用 `local` 时，建议显式设置 `IMAGE_HOST_LOCAL_DIR`，并在部署时让 Web 服务器或静态文件服务把该目录暴露到 `IMAGE_HOST_PUBLIC_BASE_URL`；如果需要跨容器重建保留本地图床文件，也需要自行挂载该目录。

后端在上传参考图和生成结果时，会额外生成最长边约 480px 的缩略图，并通过同一图床适配器上传。缩略图生成或上传失败不会阻断原图保存；前端会在列表、广场、详情参考图等小图场景优先使用 `thumbnail_url`，点击预览、下载、复用配置和蒙板编辑仍使用原图 `url`。

## 数据库配置

当前项目只支持 PostgreSQL。后端启动时会通过 `DATABASE_DSN` 连接数据库，并自动创建所需表和索引。

默认 Docker Compose 不启动 PostgreSQL，请在 `.env` 中配置远程数据库连接：

```env
DATABASE_DSN=postgres://user:password@host:5432/image_web?sslmode=require
```

如果使用内置 PostgreSQL，叠加 `docker-compose.postgres.yml`，连接信息固定在该文件中：

```env
DATABASE_DSN=postgres://image_web:image_web@postgres:5432/image_web?sslmode=disable
```

直接编译或 `go run` 部署时，改成你的 PostgreSQL 地址；本机数据库通常使用 `localhost`，Supabase 等云数据库通常需要 `sslmode=require`：

```env
DATABASE_DSN=postgres://user:password@localhost:5432/image_web?sslmode=disable
# Supabase pooler 示例：postgresql://user:password@host:6543/postgres?sslmode=require&default_query_exec_mode=simple_protocol
```

项目提供 `.env.example`，部署时复制为 `.env` 并修改即可。`.env` 会被 `.gitignore` 忽略，适合保存数据库密码等本地配置。

旧 SQLite / MySQL 数据不会自动迁移到 PostgreSQL；如果需要迁移历史数据，请单独导出旧数据并导入 PostgreSQL。

## 本地开发

### 后端

先准备 PostgreSQL，并在项目根目录 `.env` 中设置 `DATABASE_DSN`。

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

- 后端会以明文保存 `apikey`，用于任务归属查询和后台异步生成。
- 当前只支持 PostgreSQL；请不要再配置 SQLite / MySQL 相关变量。
- 请不要把 `.env`、`data/`、`node_modules/` 或构建产物提交到仓库。
- 当前项目的 `.gitignore` 已默认排除这些本地文件。
