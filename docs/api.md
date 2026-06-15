# API

所有 JSON 错误格式：

```json
{
  "error": {
    "code": "string",
    "message": "string"
  }
}
```

分页规则：`page` 从 1 开始，`pageSize` 默认 `PAGE_SIZE_DEFAULT`，最大 `PAGE_SIZE_MAX`。分页响应：

```json
{
  "items": [],
  "page": 1,
  "pageSize": 100,
  "hasMore": false
}
```

## 基础

- `GET /api/health`
- `GET /api/config/public`
- `POST /api/scan`
- `GET /api/scan/status`
- `GET /api/scan/runs?page=1&pageSize=20`

## Settings

- `GET /api/settings/libraries`
- `POST /api/settings/libraries`，请求体：`{"name":"家庭","relPaths":["Photo"]}`
- `DELETE /api/settings/libraries/:id`
- `POST /api/settings/libraries/:id/scan`
- `GET /api/settings/scan-folders`
- `POST /api/settings/scan-folders`，请求体：`{"relPath":"TIKTOK"}`
- `DELETE /api/settings/scan-folders?relPath=TIKTOK`
- `GET /api/source-folders?parentRelPath=`

LIB 路径是相对照片存储根的路径；多存储模式下第一段是存储 ID，例如 `C666/2024`，空字符串表示全部存储。

## Library

`GET /api/library/assets?page=1&pageSize=100&type=all&sort=timeline_desc&q=IMG`

`type` 支持 `all`、`image`、`video`。`sort` 支持 `timeline_desc`、`timeline_asc`、`filename`、`size`、`imported_desc`。

## Albums

- `GET /api/albums`
- `POST /api/albums`，请求体：`{"name":"竖屏视频","folderRelPaths":["Photo"],"mediaTypeFilter":"video","orientationFilter":"portrait"}`
- `GET /api/albums/source-folders?parentRelPath=`
- `GET /api/albums/:id`
- `DELETE /api/albums/:id`
- `POST /api/albums/:id/refresh`
- `GET /api/albums/:id/assets?page=1&pageSize=100&sort=timeline_desc&q=`

相册文件夹只能来自已加入 LIB 的扫描范围。`mediaTypeFilter` 支持 `all/image/video`，`orientationFilter` 支持 `all/landscape/portrait`。

## Folders

- `GET /api/folders?parentId=0`
- `GET /api/folders/tree`
- `GET /api/folders/:id`
- `GET /api/folders/:id/assets?page=1&pageSize=100&sort=filename&q=`

Folder 示例：

```json
{
  "id": 1,
  "relPath": "2024/01",
  "name": "01",
  "parentRelPath": "2024",
  "depth": 2,
  "assetCount": 20,
  "recursiveAssetCount": 20,
  "coverAssetId": 10
}
```

## Assets

- `GET /api/assets/:id`
- `GET /api/assets/:id/neighbors?context=library&type=all&sort=timeline_desc&q=`
- `GET /api/assets/:id/neighbors?context=album&albumId=1&sort=timeline_desc&q=`
- `GET /api/assets/:id/neighbors?context=folder&folderId=1&sort=filename&q=`
- `GET /api/assets/:id/preferences`
- `PUT /api/assets/:id/preferences`，请求体：`{"rotation":90}`
- `GET /api/assets/:id/thumb`
- `GET /api/assets/:id/preview`
- `GET /api/assets/:id/original`
- `GET /api/assets/:id/video`
- `GET /api/assets/:id/video-poster`
- `GET /api/assets/:id/video-proxy`

Asset 示例：

```json
{
  "id": 1,
  "filename": "IMG_001.jpg",
  "relPath": "2024/IMG_001.jpg",
  "parentRelPath": "2024",
  "mediaType": "image",
  "mimeType": "image/jpeg",
  "size": 123456,
  "mtime": 1710000000,
  "width": 4000,
  "height": 3000,
  "duration": null,
  "takenAt": 1700000000,
  "timelineAt": 1700000000,
  "importedAt": 1710000000,
  "cacheKey": "abc123",
  "browserPlayable": true,
  "thumbStatus": "ready",
  "previewStatus": "ready",
  "videoPosterStatus": "not_required",
  "videoProxyStatus": "not_required",
  "rotation": 0
}
```

neighbors 示例：

```json
{
  "current": {},
  "previous": [],
  "next": []
}
```

媒体端点返回二进制内容；缓存未就绪时返回 JSON 错误 `cache_not_ready`。
