# Image Web

一个纯中文的图片生成工作台，前端使用 Vue 3 + Vite + TypeScript，后端使用 Go + SQLite。页面不需要登录，通过 URL 传入 `baseurl` 和 `apikey` 后保存到浏览器本地，并由后端异步执行图片生成任务。

## 功能特性

- 无登录访问，通过 URL 参数配置生成接口。
- 支持 GPT Image 2 文生图和参考图编辑生成。
- 任务在后端异步执行，关闭页面后仍会继续生成。
- SQLite 持久化保存任务历史、请求源数据、响应源数据和生成结果。
- 支持状态筛选、提示词搜索、收藏和只看收藏。
- 支持参考图上传、复用任务配置、图片预览放大。
- 上传参考图和生成结果都会转存到图床后保存。
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

```bash
docker compose up -d --build
```

默认访问地址：

```text
http://localhost:8080
```

数据会保存在本地目录：

```text
./data
```

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | 后端监听端口 |
| `DATA_DIR` | `/app/data` | 数据目录 |
| `DATABASE_PATH` | `/app/data/app.db` | SQLite 数据库路径 |
| `STATIC_DIR` | `/app/static` | 前端静态文件目录 |
| `SCDN_UPLOAD_URL` | `https://img.scdn.io/api/v1.php` | 图床上传接口 |

## 本地开发

### 后端

```bash
cd backend
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
- 请不要把 `data/`、数据库文件、`node_modules/` 或构建产物提交到仓库。
- 当前项目的 `.gitignore` 已默认排除这些本地文件。
