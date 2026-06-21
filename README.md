# LPicto

LPicto 是一个运行在 NAS Docker 上的 Web 图片/视频相册：只读扫描一个或多个媒体存储根，用 PostgreSQL 保存媒体元数据，用 Redis 做任务队列和热状态，用文件系统保存缩略图、预览图和转码缓存，通过浏览器提供图库、相册、文件夹、搜索、设置和 Viewer。

## 功能列表

- 图库：全部资源列表，支持全部/图片/视频筛选、排序、文件名搜索。
- 相册：按已加入 LIB 的文件夹创建相册，支持多条来源规则、照片/视频、横屏/竖屏筛选和分组。
- 文件夹：按 NAS 原始目录层级浏览，不暴露容器绝对路径。
- 设置：查看扫描状态、最近扫描记录，添加或移除加入应用的 LIB。
- Viewer：图片预览和按住放大；视频播放、进度拖动、音量控制、90°步进旋转记忆。
- 缓存：图片 thumb、图片 preview、视频 poster、非浏览器友好视频 proxy。
- 扫描：启动后台扫描、手动扫描、定时扫描、fsnotify 可用时增量触发。
- API：分页、统一错误格式、asset id 文件访问、原图和视频流式响应。

## 当前不做的功能

V1 不做 AI 标签、人脸识别、语义搜索、多用户权限、云同步、图片编辑、删除/移动/重命名原文件。

## 截图

当前仓库不包含真实截图；部署后可从浏览器自行截取图库、相册、文件夹和 Viewer 页面。

## 架构说明

Nginx 是浏览器入口；API 进程负责 HTTP 和 DTO；Worker 进程负责扫描、元数据解析、缩略图、预览图和视频 proxy；PostgreSQL 是唯一主库；Redis 保存任务队列和扫描热状态；React 前端只通过 `/api` 读取 DTO。

## 目录结构

```text
LPicto/
  backend/      Go 后端、migration、扫描、媒体处理、API
  frontend/     React + TypeScript + Vite 前端
  docs/         架构、API、路线图
  Dockerfile
  docker-compose.yml
  Makefile
  AGENTS.md
```

## Docker Compose 部署

```bash
docker compose up --build
```

访问 `http://localhost:18080`。

## 媒体目录挂载说明

Compose 只需要用户配置 3 个宿主路径：`LPICTO_MEDIA` 是只读媒体目录，容器内固定为 `/Media`；`LPICTO_DATA` 保存 PostgreSQL、Redis 和应用运行数据；`LPICTO_CACHE` 保存缩略图、预览图和视频代理缓存。默认值分别是 `./data/Media`、`./data/app`、`./data/cache`，宿主机写入目录需要允许容器写入。

## 用户配置

| 变量 | 默认值 |
| --- | --- |
| `LPICTO_MEDIA` | `./data/Media` |
| `LPICTO_DATA` | `./data/app` |
| `LPICTO_CACHE` | `./data/cache` |
| `LPICTO_PORT` | `18080` |

## 首次扫描说明

容器启动后 API 先可用；扫描由 API 写入 Redis 队列，worker 消费后执行，不阻塞页面和 API。

## 手动扫描说明

点击页面顶部“重新扫描”，或调用：

```bash
curl -X POST http://localhost:18080/api/scan
```

扫描状态：

```bash
curl http://localhost:18080/api/scan/status
```

## LIB 设置

左下角“设置”页可以管理加入应用的 LIB。LIB 由一个或多个相对 `/Media` 的文件夹组成；例如宿主目录挂到 `/Media` 后，`/Media/电影/2024` 对应 `电影/2024`。空路径表示全部媒体根；移除所有 LIB 后资源列表会清空，原文件不会被删除。

## 相册说明

相册从已加入 LIB 的文件夹创建，支持多条来源规则、照片/视频、横屏/竖屏筛选和分组。横竖屏按资源宽高计算；视频设置了旋转角度时，按旋转后的宽高计算。

## 缩略图/预览图/视频代理说明

图片使用 `vipsthumbnail` 生成 WebP thumb 和 preview；视频使用 FFmpeg 生成 poster；浏览器不稳定或不支持的视频生成 H.264/AAC MP4 proxy。后台媒体任务由 Redis 分发，并由应用内置并发策略限速。缓存写入 `/cache`，URL 带 `cacheKey` 并使用 immutable 缓存。

## GPU 视频抽帧

默认 compose 不请求 GPU，保证无 GPU 机器可启动。NVIDIA Docker 环境可用：

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up --build
```

图片缩略图仍由 libvips 处理，不走 GPU。

## 浏览器视频格式限制说明

浏览器原生优先播放 MP4/M4V H.264 + AAC/MP3/无音频，或 WebM VP8/VP9/AV1 + Opus/Vorbis/无音频；其他格式走 proxy，proxy 未就绪时显示 poster 和“视频预览生成中”。

## 原图访问说明

原图和原视频只读访问，接口按 asset id 解析真实路径，不接受路径参数；响应使用流式读取，视频支持 HTTP Range。

## 开发方式

后端：

```bash
cd backend
go run ./cmd/server
```

前端：

```bash
cd frontend
npm install
npm run dev
```

前端 dev server 默认代理 `/api` 到 `http://localhost:18080`。

## 后端开发命令

```bash
cd backend
go test ./...
go vet ./...
go build ./cmd/server
```

## 前端开发命令

```bash
cd frontend
npm install
npm run build
```

## 测试命令

```bash
make backend-test
make frontend-build
make check
```

## 构建命令

```bash
make build
make docker-build
```

## 常见问题

- 页面没有资源：确认 `LPICTO_MEDIA` 挂载到容器内 `/Media` 后可读，并查看 `/api/scan/status`。
- 缩略图一直处理中：确认 runtime 镜像内存在 `vipsthumbnail`、`ffmpeg`、`ffprobe`、`exiftool`。
- 开 GPU 后仍走 CPU：查看容器日志里的 `ffmpeg hardware acceleration failed`，驱动或设备不可用时会自动回退 CPU。
- `/data` 写入失败：确认宿主机数据目录允许 UID `10001` 写入。
- fsnotify 不触发：NAS 挂载可能不可靠，定时扫描仍会兜底。

## 未来 AI 扩展方向

后续可新增 `asset_tags`、`asset_faces`、`asset_embeddings` 等表实现 AI 标签、人脸识别和 CLIP 语义搜索；V1 不包含 AI API 和 AI 后台任务。
