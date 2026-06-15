# 架构说明

当前架构是单容器：Go 后端服务 `/api` 和前端静态文件，React 前端通过 `/api` 获取数据，SQLite 与缓存都在 `/data`。

```text
PHOTO_ROOT 或 PHOTO_ROOTS
  -> scanner
  -> SQLite assets/folders/scan_runs
  -> jobs queue
  -> libvips / ffmpeg / ffprobe / exiftool
  -> /data/cache
  -> API DTO / media endpoints
  -> React library / albums / folders / viewer
```

## 数据流

1. scan：扫描已配置的照片存储根，识别支持的图片/视频，按 `rel_path + size + mtime` 判断新增或修改，删除文件标记 `deleted_at`；多存储模式下 `rel_path` 第一段是存储 ID。
2. db：写入 assets、folders、scan_runs，folders 统计在扫描后刷新。
3. jobs：新增或修改的资源进入内存队列，图片和视频 worker 按配置并发。
4. cache：图片输出 WebP thumb/preview，视频输出 JPG poster 和必要的 MP4 proxy；视频处理可通过 `FFMPEG_HWACCEL` 尝试硬件解码。
5. API：分页返回 DTO；媒体文件通过 asset id 访问，不接受路径参数。
6. frontend：图库、相册、文件夹共享 Asset DTO 和 Viewer context。

## Library / Albums / Folders

Library 对全部未删除资源分页筛选；Albums 保存相册名称、来源文件夹和筛选条件，并在查询时按当前 assets 动态计算内容；Folders 保留 `parent_rel_path` 表示的 NAS 原始层级。三者只改变查询上下文，最终进入同一个 Viewer。

## Viewer context / neighbors

Viewer URL 保存 `context`、筛选、排序、albumId 或 folderId。`/api/assets/:id/neighbors` 根据上下文返回 current、previous、next，前端用同一顺序切换。视频旋转角度保存在 `asset_preferences`，Viewer 与缩略图只按角度展示，不修改原文件。

## 缓存策略

`cache_key = rel_path + size + mtime` 的 SHA1 前 20 位。文件修改后 cache key 改变，thumb/preview/poster/proxy URL 带 `?v=cacheKey`，响应使用 `Cache-Control: public, max-age=31536000, immutable`。后台媒体任务共用资源闸门，并以低 CPU/I/O 优先级启动外部处理命令。

## 视频代理策略

MP4/M4V H.264 + AAC/MP3/无音频和 WebM VP8/VP9/AV1 + Opus/Vorbis/无音频标记为浏览器可播放；其他视频进入 proxy 队列，FFmpeg 输出 H.264/AAC MP4，最大高度由 `VIDEO_PROXY_MAX_HEIGHT` 控制。`FFMPEG_HWACCEL=auto/cuda/vaapi/qsv` 时先尝试硬件解码，失败后按 `FFMPEG_HWACCEL_FALLBACK` 决定是否回退 CPU。

## 未来 AI 扩展

asset 是核心实体。未来扩展 AI 时新增独立表：`asset_tags` 存标签，`asset_faces` 存人脸框和聚类，`asset_embeddings` 存向量引用；V1 不包含 AI 字段和 AI API。
