# LPicto

LPicto 是一个运行在 NAS Docker 上的 Web 图片/视频相册：只读扫描一个或多个照片存储根，在 `/data` 保存 SQLite 数据库、相册设置与媒体缓存，通过浏览器提供图库、相册、文件夹和快速 Viewer。

## 功能列表

- 图库：全部资源列表，支持全部/图片/视频筛选、排序、文件名搜索。
- 相册：按已加入 LIB 的文件夹创建相册，支持照片/视频、横屏/竖屏筛选。
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

Go 后端负责配置、SQLite、扫描、任务队列、媒体缓存生成和静态前端服务；React 前端只通过 `/api` 读取 DTO，不访问真实文件路径。数据流为 `scan -> db -> jobs -> cache -> API -> frontend`。

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

访问 `http://localhost:8080`。

## NAS 目录挂载说明

默认 compose 将两个示例照片目录只读挂载到容器 `/storage/C666`、`/storage/D666`，通过 `PHOTO_ROOTS=C666=/storage/C666;D666=/storage/D666` 注册为两个存储根，并将本项目 `./data` 挂载到 `/data`。容器内进程使用非 root 用户 `10001`，宿主机数据目录需要允许该 UID 写入。

## 环境变量

| 变量 | 默认值 |
| --- | --- |
| `PHOTO_ROOT` | `/photos` |
| `PHOTO_ROOTS` | 空，设置后覆盖 `PHOTO_ROOT`，格式 `ID=/path;ID2=/path2` |
| `DATA_ROOT` | `/data` |
| `HTTP_ADDR` | `:8080` |
| `SCAN_INTERVAL_MINUTES` | `30` |
| `THUMB_WORKERS` | `2` |
| `VIDEO_WORKERS` | `1` |
| `BACKGROUND_MAX_ACTIVE` | CPU 核心数 - 1，最小 1 |
| `BACKGROUND_LOAD_TARGET` | `BACKGROUND_MAX_ACTIVE` |
| `BACKGROUND_MIN_FREE_MB` | `512` |
| `PAGE_SIZE_DEFAULT` | `100` |
| `PAGE_SIZE_MAX` | `500` |
| `ENABLE_FS_WATCH` | `true` |
| `THUMB_LONG_EDGE` | `320` |
| `PREVIEW_LONG_EDGE` | `2560` |
| `PREVIEW_QUALITY` | `82` |
| `VIDEO_PROXY_ENABLED` | `true` |
| `VIDEO_PROXY_MAX_HEIGHT` | `1080` |
| `VIDEO_PROXY_CRF` | `23` |
| `FFMPEG_HWACCEL` | `none` |
| `FFMPEG_HWACCEL_DEVICE` | 空 |
| `FFMPEG_HWACCEL_FALLBACK` | `true` |

## 首次扫描说明

容器启动后 HTTP 服务先可用，然后后台扫描全部已配置存储根；扫描不会阻塞页面和 API。

## 手动扫描说明

点击页面顶部“重新扫描”，或调用：

```bash
curl -X POST http://localhost:8080/api/scan
```

扫描状态：

```bash
curl http://localhost:8080/api/scan/status
```

## LIB 设置

左下角“设置”页可以管理加入应用的 LIB。LIB 由一个或多个相对存储根的文件夹组成；多存储模式下第一段是存储 ID，例如 `/C666/2024`。空路径表示全部存储；移除所有 LIB 后资源列表会清空，原文件不会被删除。

## 相册说明

相册从已加入 LIB 的文件夹创建，支持照片/视频、横屏/竖屏筛选。横竖屏按资源宽高计算；视频设置了旋转角度时，按旋转后的宽高计算。

## 缩略图/预览图/视频代理说明

图片使用 `vipsthumbnail` 生成 WebP thumb 和 preview；视频使用 FFmpeg 生成 JPG poster；浏览器不稳定或不支持的视频生成 H.264/AAC MP4 proxy。后台媒体任务由 `BACKGROUND_MAX_ACTIVE` 限制同时运行数量，Linux 容器内启动外部处理命令时使用 `nice -n 10` 和可用时的 `ionice -c 3` 降低 CPU/I/O 优先级；`BACKGROUND_LOAD_TARGET` 和 `BACKGROUND_MIN_FREE_MB` 用于高负载或低内存时延后启动新任务。`FFMPEG_HWACCEL` 可设为 `auto`、`cuda`、`vaapi`、`qsv` 等，让视频抽帧和 proxy 先尝试硬件解码；失败时默认回退 CPU。缓存写入 `/data/cache`，URL 带 `cacheKey` 并使用 immutable 缓存。

## GPU 视频抽帧

默认 compose 不请求 GPU，保证无 GPU 机器可启动。NVIDIA Docker 环境可用：

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up --build
```

Intel/VAAPI 可改为 `FFMPEG_HWACCEL=vaapi`，并把宿主机 `/dev/dri` 映射进容器；图片缩略图仍由 libvips 处理，不走 GPU。

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

前端 dev server 默认代理 `/api` 到 `http://localhost:8080`。

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

- 页面没有资源：确认 `PHOTO_ROOTS` 或 `PHOTO_ROOT` 指向的容器内路径可读，并查看 `/api/scan/status`。
- 缩略图一直处理中：确认 runtime 镜像内存在 `vipsthumbnail`、`ffmpeg`、`ffprobe`、`exiftool`。
- 开 GPU 后仍走 CPU：查看容器日志里的 `ffmpeg hardware acceleration failed`，驱动或设备不可用时会自动回退 CPU。
- `/data` 写入失败：确认宿主机数据目录允许 UID `10001` 写入。
- fsnotify 不触发：NAS 挂载可能不可靠，定时扫描仍会兜底。

## 未来 AI 扩展方向

后续可新增 `asset_tags`、`asset_faces`、`asset_embeddings` 等表实现 AI 标签、人脸识别和 CLIP 语义搜索；V1 不包含 AI API 和 AI 后台任务。
