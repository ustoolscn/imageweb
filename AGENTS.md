# AGENTS.md

本文件用于帮助后续代码修改时快速理解项目。每次修改项目结构、入口、核心模块职责、构建方式或重要功能边界时，必须同步更新本文件。

## 协作规则

- 每次修改后，如果目录结构、技术栈、启动方式、构建命令、核心功能流或重要模块职责发生变化，必须同步更新本文件。
- 如果某一项功能的修改可能造成其他功能、接口、数据结构、数据库字段、任务流程、部署方式或用户体验的变动，必须先向用户说明影响范围并请示，等待明确同意后再进行修改。
- 不要提交 `.env`、`node_modules/`、前端构建产物、临时文件或数据库数据。
- 当前项目是中文图片/视频生成工作台，界面文案应优先保持中文。

## 项目概览

Image Web 是一个前后端同仓库项目：

- 前端：Vue 3 + Vite + TypeScript，提供图片/视频生成工作台、任务列表、公开广场、画布工作区和相关弹窗。
- 后端：Go HTTP 服务，负责静态文件托管、API 路由、PostgreSQL 持久化、异步任务执行、上游生成接口调用、图床转存和缩略图处理。
- 数据库：仅支持 PostgreSQL。启动时由后端按最新 schema 建表，不保留旧 schema 的增量 `ALTER TABLE` 兼容迁移。
- 部署：Docker 多阶段构建，先构建前端，再编译 Go 服务，最终由 Go 服务托管 `/app/static`。

## 根目录结构

- `README.md`：用户侧说明，包含功能、部署、环境变量、本地开发和验证命令。
- `AGENTS.md`：给后续代码代理/协作者看的项目结构与修改约束说明。
- `.env.example`：环境变量模板。
- `.env`：本地私密配置，已被忽略，不应提交。
- `Dockerfile`：多阶段构建，生成前端静态资源并编译后端二进制；运行镜像安装 `ffmpeg` 用于视频首帧/尾帧抽取。
- `docker-compose.yml`：默认只启动 `image-web` 应用服务，适合连接远程 PostgreSQL。
- `docker-compose.postgres.yml`：可选叠加启动本地 PostgreSQL，并使用 `postgres-data` volume。
- `backend/`：Go 后端。
- `frontend/`：Vue 前端。
- `docs/`：项目文档，目前包含 Vue Flow 类型文档索引。

## 后端结构

后端模块名为 `image-web/backend`，Go 版本为 `1.25.0`。

- `backend/cmd/server/main.go`：服务入口。加载配置、创建应用、启动 HTTP 服务，并处理 SIGINT/SIGTERM 优雅关闭。
- `backend/internal/app/app.go`：应用装配层。连接数据库、重置过期运行任务、创建 generator/imagehost/handler/worker、注册 API 与静态文件服务、添加安全响应头，并为 HTTP 请求生成 trace 日志，便于和数据库事务日志对应。
- `backend/internal/config/config.go`：配置加载。会尝试读取当前目录、上级目录和可执行文件附近的 `.env`。
- `backend/internal/db/db.go`：PostgreSQL Store。包含最新建表 schema、workspace/API key 加密、任务 CRUD、任务源数据与媒体素材拆表读写、画布状态、广场分享/点赞、任务调度状态更新、视频轮询状态更新；关键事务、启动 migration、画布 advisory lock、`task_sources` 和 `task_media_assets` 写入都会输出带 trace/事务 id 的日志，用于排查数据库锁等待。DB 日志必须保持异步非阻塞，不能在事务提交前用同步日志阻塞 goroutine。
- `backend/internal/handler/handlers.go`：HTTP API 层。注册 `/api/*` 路由，做参数校验、baseurl 白名单检查、任务创建、任务列表、上传、LLM、模型查询、画布和广场接口。
- `backend/internal/model/types.go`：后端 API、任务、媒体素材、站点配置等共享类型。
- `backend/internal/generator/`：上游生成客户端。负责图片生成、图片编辑、视频提交/轮询/下载、LLM 请求、请求/响应记录和传输层测试。
- `backend/internal/imagehost/scdn.go`：图床适配器。支持 HTTP JSON 图床和 local provider，并负责上传原图/结果、图片缩略图、视频首帧/尾帧抽取上传；HTTP multipart 上传会显式传递文件 MIME 类型，避免图床对象落成 `application/octet-stream`。本地直接运行后端时也需要系统 PATH 中存在 `ffmpeg`，Docker 镜像已内置。
- `backend/internal/worker/worker.go`：异步任务 Worker。周期性派发 pending 任务，按站点配置控制并发；图片任务直接生成并上传，视频任务先提交再轮询完成。

主要后端依赖：

- `github.com/jackc/pgx/v5`：PostgreSQL 驱动。
- `github.com/google/uuid`：任务和广场条目 ID。
- `golang.org/x/image`：WebP 等图片处理能力。

## 前端结构

前端位于 `frontend/`，使用 Vite + Vue 3 + TypeScript。

- `frontend/package.json`：前端脚本和依赖。
- `frontend/vite.config.ts`：Vite 配置，开发环境将 `/api` 代理到 `http://localhost:8080`。
- `frontend/src/main.ts`：Vue 应用入口。
- `frontend/src/App.vue`：主应用状态与流程中枢。管理 baseurl/apikey、任务、广场、画布视图、当前视图持久化、模型选择、生成表单、轮询、预览、蒙板编辑和各类弹窗。
- `frontend/src/api.ts`：前端 API 封装，集中访问后端 `/api/*`。
- `frontend/src/types.ts`：后端数据类型对应的前端类型。
- `frontend/src/uiTypes.ts`：前端 UI 状态、表单和画布相关类型。
- `frontend/src/styles.css`：全局样式。

前端组件：

- `AppToolbar.vue`：顶部工具栏、视图切换、筛选、搜索、主题切换等。
- `Composer.vue`：生成控制台，包含任务类型、模型、尺寸、参考素材、提交等输入。
- `TaskGrid.vue` / `TaskDetailModal.vue`：任务列表与任务详情。
- `PlazaGrid.vue` / `PlazaDetailModal.vue`：公开广场列表与详情。
- `CanvasWorkspace.vue`：画布工作区，基于 Vue Flow 组织节点式生成/LLM 工作流；包含分区本地保存、节点/连线级 PATCH 云端同步、保存状态反馈、窄屏底部工具抽屉和进入画布时的自动视图定位。
- `ImageViewer.vue`：图片预览和蒙板编辑。
- `SettingsModal.vue`：baseurl/apikey 设置。
- `SizeModal.vue` / `VideoSizeModal.vue` / `RatioPicker.vue`：图片和视频尺寸选择。
- `SourceModal.vue`：请求/响应源数据查看。
- `AdminContactModal.vue`：baseurl 未授权时展示管理员联系图。
- `CanvasVideoPlayer.vue`、`ViewControl3D.vue`、`AppIcon.vue`、`InlineSelect.vue`：媒体播放、视图控制、图标和通用输入组件。所有来自图床、任务、广场和画布的图片/视频展示都应优先使用 `crossorigin="anonymous"`，保证首次缓存就是 CORS 模式，避免后续 Three.js、Canvas 蒙版或视频取帧复用到无 CORS 缓存。

前端工具库：

- `frontend/src/lib/sizes.ts`：图片尺寸、比例、模型尺寸映射。
- `frontend/src/lib/videoModels.ts`：视频模型能力、比例、分辨率和尺寸规范化。
- `frontend/src/lib/canvasPreview.ts`：画布预览相关逻辑。
- `frontend/src/lib/view.ts`：展示层辅助函数，如 baseurl 脱敏、分享/源数据可见性判断。

主要前端依赖：

- `vue`、`vite`、`typescript`、`vue-tsc`
- `@vitejs/plugin-vue`
- `@lucide/vue`
- `@vue-flow/core`、`@vue-flow/minimap`、`@vue-flow/node-resizer`
- `three`
- `video.js`

## 核心功能流

1. 用户通过 URL 参数或设置弹窗提供 `baseurl` 和 `apikey`，前端保存到 `localStorage` 并清理 URL 中的敏感参数。
2. 前端通过 `/api/site-brand`、`/api/models` 等接口读取站点品牌、模型和能力配置。
3. 后端收到带 `baseurl + apikey` 的请求后，会解析/创建 `workspaces` 行：业务表只保存 `workspace_id`，API key 以 hash 做查找、以 `APP_CREDENTIAL_KEY` 派生密钥加密保存，worker 调度时再解密回填到 `Task.APIKey`。数据库连接默认带 `lock_timeout`、`statement_timeout` 和 `idle_in_transaction_session_timeout`，事务内也会设置同样保护，避免单个异常任务写入把 PostgreSQL 长时间锁死。
4. 创建任务时，前端调用 `/api/tasks`；后端写入 `tasks`，状态为 `pending`，参考图片/视频/音频写入 `task_media_assets`。任务页图片生成的“数量”是前端批量提交次数，范围 1-5，不作为上游大模型参数；每次提交仍固定传 `n: 1`，数量为 5 时会创建 5 个独立任务。
5. Worker 周期性读取 pending 任务并置为 `running`；空队列时只允许单个取任务查询在途，并短暂退避，避免远程 PostgreSQL 上无任务时被并发空轮询压垮。
6. 图片任务调用上游图片生成/编辑接口，下载结果，转存到图床，结果写入 `task_media_assets`；请求/响应源数据写入 `task_sources`，但源数据只作为诊断信息在任务状态/素材事务提交后 best-effort 保存，不能因为源数据 upsert 卡住而阻塞任务完成或持有事务锁。图片任务总执行时间限制为 10 分钟：超过后自动取消当前上游请求并标记为 `failed`，启动或 Worker 调度时发现超过 10 分钟仍处于 `running` 的图片任务也会直接标记失败，不再重置回 `pending` 自动重跑，避免上游实际已生成但本地重复调用。前端不提供背景选择；后端不做透明背景/抠图后处理。`gpt-image-2` 的上游请求不发送 `background` 参数。
7. 视频任务先提交上游任务，保存 upstream task id；后续轮询完成后下载视频、转存图床，并用 `ffmpeg` 以低占用参数抽取最长边约 480px 的首帧/尾帧图片，结果写入 `task_media_assets`，轮询响应源数据更新到 `task_sources`。
8. 前端通过共享的 `/api/tasks/updates` 以 10 秒间隔轮询更新运行中任务状态；画布会把节点引用的任务 id 和轻量任务快照同步给 App，刷新页面后即使节点只有 `task_id` 也会先补拉一次任务详情，再把画布中的 pending/running 任务纳入轮询，并在完成后回填节点结果。生成节点成功产出内容后会自动进入固化状态，后续流程会跳过该节点，用户可手动取消固化后重新运行。画布节点读取任务时要在节点本地 `task_snapshot`、父级画布任务缓存、任务列表和素材页缓存中选择结果最完整/状态最新的一份，避免刚完成的上游素材无法立刻传给下游节点。画布任务缓存与全局任务列表分离，不能为了追踪画布运行态把画布任务塞进任务列表，否则会污染任务页和素材列表顺序。视频素材封面和尾帧必须优先使用 `thumbnail_url`、`first_frame_url`、`last_frame_url` 这些已固化字段，不要再让浏览器临时读取远程视频抽帧。画布节点等待任务完成时也复用这条更新通道，避免同时高频请求单个 `/api/tasks/{id}` 详情。任务还可查看详情、收藏、重试、删除、分享到广场。
9. 广场功能通过 `plaza_items` 和 `plaza_likes` 表实现公开展示、点赞、导入/复用；任务分享使用真实 nullable `task_id`，画布分享使用真实 `workspace_id + canvas_id`，不允许再伪造 `task_id = canvas:*`。前端进入广场时使用 60 秒内存缓存：列表为空、缓存过期或页面初始视图就是广场时会请求 `/api/plaza`，否则复用已有 `plazaItems`。
10. 画布状态通过 `/api/canvases` 保存到 PostgreSQL，数据库层是一张画布一行，主键为 `workspace_id + canvas_id`。前端同时按当前 `baseurl + apikey` 分区保存本地副本和保存元数据，加载云端时需要比较本地/云端更新时间，避免旧云端内容静默覆盖较新的本地画布。实时同步使用 5 秒防抖，优先使用 `PATCH /api/canvases` 只上传新增画布、变更节点/连线、删除 id 和画布名变更；首次同步或无基线时才使用 `PUT /api/canvases` 全量保存。后端 `PUT/PATCH /api/canvases` 使用单语句 autocommit 写入，不要再包显式长事务或画布 advisory lock，避免远程 PostgreSQL 在日志/网络慢时留下 `idle in transaction`；PATCH 必须只更新受影响的画布行，不要恢复成把同一用户所有画布塞进单行 JSONB 的设计。节点拖拽或调整大小期间暂停深度保存、云端同步和历史快照，结束交互后只保存一次，避免连接线较多时卡顿。节点内的 `task_snapshot` 必须保持轻量，只保留展示、结果 URL、进度和继续轮询需要的字段，不要保存 `request_json`、`response_json` 等源数据大字段。`GET /api/canvases` 返回完整画布数组，`PUT/PATCH /api/canvases` 保存成功只返回 `updated_at`，避免保存响应体再次传回所有画布。

## 数据库与配置

- 只支持 PostgreSQL，连接串由 `DATABASE_DSN` 提供。
- `APP_CREDENTIAL_KEY` 必须配置，用于加密保存 `workspaces.api_key_encrypted`；更换该值会导致历史 workspace 的 API key 无法解密。
- 启动时只接受空库或最新 schema；如果检测到旧宽表/旧画布表结构会直接报错，不做兼容迁移。
- `workspaces` 保存 `base_url`、`api_key_hash` 和加密后的 API key，业务表通过 `workspace_id` 关联，不再在 `tasks`、`canvases` 中明文保存 `api_key`。
- `site_config` 保存 baseurl 白名单、管理员联系图、站点标题/图标、worker 并发数等配置。
- `tasks` 保存轻量任务输入、状态、视频 upstream 状态和时间字段；不要再把参考素材、结果素材或源数据塞回任务宽表。
- `task_sources` 保存任务 request/response headers/json；任务列表不加载源数据，任务详情才读取。
- `task_media_assets` 保存参考素材和结果素材，按 `role + sort_order` 还原为前端现有数组字段；视频素材字段包括 `thumbnail_url`、`first_frame_url`、`last_frame_url`，展示封面和画布尾帧节点应优先使用这些字段。
- `plaza_items` 保存公开广场条目，任务分享使用 nullable `task_id`，画布分享使用 `workspace_id + canvas_id` 和 `canvas_json` 快照；`plaza_likes` 通过外键级联删除。
- `canvases` 保存用户画布状态，一张画布一行，字段包括 `workspace_id`、`canvas_id`、`name`、`canvas_json`、`sort_order`、`created_at`、`updated_at`。

常用环境变量见 `.env.example` 和 `README.md`。重要项包括：

- `PORT`
- `DATABASE_DSN`
- `APP_CREDENTIAL_KEY`
- `IMAGE_HOST_PROVIDER`
- `IMAGE_HOST_UPLOAD_URL`
- `IMAGE_HOST_AUTH_HEADER`
- `IMAGE_HOST_AUTH_VALUE`
- `IMAGE_HOST_FIELD_NAME`
- `IMAGE_HOST_RESPONSE_URL_PATH`
- `IMAGE_HOST_LOCAL_DIR`
- `IMAGE_HOST_PUBLIC_BASE_URL`

## 常用命令

前端开发：

```bash
cd frontend
npm install
npm run dev
```

后端开发：

```bash
cd backend
go run ./cmd/server
```

前端构建验证：

```bash
npm --prefix frontend run build
```

后端测试：

```bash
cd backend
go test ./...
```

Docker 部署：

```bash
docker compose up -d --build
```

带本地 PostgreSQL：

```bash
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d --build
```

## 修改注意事项

- 改 API 时，需要同时检查 `backend/internal/handler/handlers.go`、`backend/internal/model/types.go`、`frontend/src/api.ts`、`frontend/src/types.ts` 和相关组件。
- 改任务字段或数据库字段时，需要同步检查 `backend/internal/db/db.go` 的最新 `CREATE TABLE` schema、workspace 解析、任务媒体/源数据拆表读写、插入、查询列、扫描函数和前端类型；当前不维护旧 schema 的 `ALTER TABLE` 兼容迁移。
- 改图片/视频生成参数时，需要同时检查前端 `Composer.vue`、尺寸工具库、后端 `createTask` 归一化逻辑、`generator` 请求构造和 worker 执行路径。
- 改图床或上传逻辑时，需要检查参考图上传、结果上传、缩略图、multipart 文件 MIME 类型、前端预览和下载/复用流程。
- 改视频素材逻辑时，需要同时检查 `/api/upload`、`/api/video-frames`、worker 视频完成路径、`task_media_assets` 首尾帧字段、画布 URL 导入和尾帧节点；后端抽帧应保持低占用，失败不应阻断原视频保存。
- 改画布功能时，需要检查 `CanvasWorkspace.vue`、`frontend/src/api.ts` 的 canvas 接口、后端 `/api/canvases` 和 `canvases` 表。
- 改画布保存机制时，需要保留页面上的保存状态反馈，并同时验证本地保存、云端同步、云端读取失败、本地较新覆盖云端、节点/连线级 PATCH 合并、删除节点/连线同步、删除画布同步这几条路径。
- 改画布素材列表时，应保持后端分页顺序为主；本地任务状态只更新当前页已有素材，不要把画布运行态缓存插入素材列表。
- 改广场功能时，需要检查任务分享、画布分享、点赞、列表分页、详情复用和 `plaza_items` 数据结构。
- 改 UI 布局时，需要检查桌面和移动端表现，尤其是任务视图、广场视图、画布视图和生成控制台。
