# 架构说明

当前部署固定为 4 个容器：`api` 同时提供 React 静态文件、`/api`、媒体流和后台任务执行器，`ai` 负责本地识别，PostgreSQL 是唯一主库，Redis 保存任务队列与扫描热状态。媒体、应用数据和派生缓存分别挂载到 `/Media`、`/data`、`/cache`，容器升级不搬动宿主机数据。

```text
/Media
  -> scanner
  -> PostgreSQL media_asset/file_instance/media_variant/folder/albums
  -> Redis job queue
  -> libvips / ffmpeg / ffprobe / exiftool
  -> /cache
  -> API DTO / media endpoints / background workers
  -> React library / albums / collections / folders / viewer
```

## 数据流

1. scan：默认扫描 `/Media`，识别支持的图片/视频，按 `rel_path + size + mtime` 判断新增或修改，删除文件标记 `deleted_at`。
2. db：写入 `media_asset`、`file_instance`、`media_variant`、`folder`、`scan_runs`，folder 统计在扫描后刷新。
3. jobs：新增或修改的资源进入 Redis 队列，由 `api` 进程内的后台执行器按资源策略并发消费；播放优先级与任务执行处于同一进程，能够立即协调 CPU。
4. cache：图片输出 WebP thumb/preview，视频输出 JPG poster 和必要的 MP4 proxy；视频处理按内置配置选择硬件或 CPU 解码。
5. API：分页返回 DTO；媒体文件通过 asset id 访问，不接受路径参数。
6. frontend：图库、相册、搜索、文件夹共享 Asset DTO 和 Viewer context。

## Library / Albums / Collections / Folders

Library 对全部未删除资源分页筛选并承载组合搜索；Albums 保存来源规则；Collections 保存智能规则；Folders 保留 NAS 原始层级。各入口只改变查询上下文，最终进入同一个 Viewer。

## Viewer context / neighbors

Viewer URL 保存 `context`、筛选、排序、albumId 或 folderId。`/api/assets/:id/neighbors` 根据上下文返回 current、previous、next，前端用同一顺序切换。视频旋转角度保存在 `asset_preferences`，Viewer 只按角度展示，不修改原文件。

## 缓存策略

`cache_key = rel_path + size + mtime` 的 SHA1 前 20 位。文件修改后 cache key 改变，thumb/preview/poster/proxy URL 带 `?v=cacheKey`，响应使用 `Cache-Control: public, max-age=31536000, immutable`。缓存文件路径记录在 `media_variant`，二进制不进 PostgreSQL。

## 视频代理策略

MP4/M4V H.264 + AAC/MP3/无音频和 WebM VP8/VP9/AV1 + Opus/Vorbis/无音频标记为浏览器可播放；其他视频进入 proxy 队列，FFmpeg 输出 H.264/AAC MP4，最大高度和硬件解码策略由应用内置配置控制。

## 本地 AI

AI 使用独立容器提供 Chinese-CLIP 标签和 Qwen3-VL 中文描述，`api` 内的后台执行器通过内部网络串行提交任务。结果保存在 `asset_ai_result` 与 `asset_ai_tag`，手工标签保持独立；媒体版本变化后自动重新排队，播放需要 CPU 时可中断 AI 任务。
