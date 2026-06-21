# 架构说明

当前架构是多容器：Nginx 是入口，Go API 服务 `/api` 和前端静态文件，Go worker 执行扫描与媒体派生任务，React 前端通过 `/api` 获取数据，PostgreSQL 是唯一主库，Redis 是任务队列和扫描热状态，缓存落在 `/cache`。

```text
/Media
  -> scanner
  -> PostgreSQL media_asset/file_instance/media_variant/folder/albums
  -> Redis job queue
  -> libvips / ffmpeg / ffprobe / exiftool
  -> /cache
  -> API DTO / media endpoints
  -> React library / search / folders / viewer
```

## 数据流

1. scan：默认扫描 `/Media`，识别支持的图片/视频，按 `rel_path + size + mtime` 判断新增或修改，删除文件标记 `deleted_at`。
2. db：写入 `media_asset`、`file_instance`、`media_variant`、`folder`、`scan_runs`，folder 统计在扫描后刷新。
3. jobs：新增或修改的资源进入 Redis Sorted Set 队列，worker 按配置并发消费。
4. cache：图片输出 WebP thumb/preview，视频输出 JPG poster 和必要的 MP4 proxy；视频处理按内置配置选择硬件或 CPU 解码。
5. API：分页返回 DTO；媒体文件通过 asset id 访问，不接受路径参数。
6. frontend：图库、相册、搜索、文件夹共享 Asset DTO 和 Viewer context。

## Library / Albums / Search / Folders

Library 对全部未删除资源分页筛选；Albums 保存来源规则、媒体类型和横竖屏筛选；Search 追加尺寸、时长、大小、NFO 文本等筛选；Folders 保留 NAS 原始层级。四者只改变查询上下文，最终进入同一个 Viewer。

## Viewer context / neighbors

Viewer URL 保存 `context`、筛选、排序、albumId 或 folderId。`/api/assets/:id/neighbors` 根据上下文返回 current、previous、next，前端用同一顺序切换。视频旋转角度保存在 `asset_preferences`，Viewer 只按角度展示，不修改原文件。

## 缓存策略

`cache_key = rel_path + size + mtime` 的 SHA1 前 20 位。文件修改后 cache key 改变，thumb/preview/poster/proxy URL 带 `?v=cacheKey`，响应使用 `Cache-Control: public, max-age=31536000, immutable`。缓存文件路径记录在 `media_variant`，二进制不进 PostgreSQL。

## 视频代理策略

MP4/M4V H.264 + AAC/MP3/无音频和 WebM VP8/VP9/AV1 + Opus/Vorbis/无音频标记为浏览器可播放；其他视频进入 proxy 队列，FFmpeg 输出 H.264/AAC MP4，最大高度和硬件解码策略由应用内置配置控制。

## 未来 AI 扩展

asset 是核心实体。未来扩展 AI 时新增独立表：`asset_tags` 存标签，`asset_faces` 存人脸框和聚类，`asset_embeddings` 存向量引用；V1 不包含 AI 字段和 AI API。
