package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"lpicto/backend/internal/model"
)

const directMediaChunkBytes int64 = 8 << 20
const directVideoChunkBytes int64 = directMediaChunkBytes
const directVideoSourceOpenTimeout = 10 * time.Second

func (s *Server) serveCachedOriginalImage(w http.ResponseWriter, r *http.Request, asset model.Asset) bool {
	ext := strings.TrimPrefix(strings.ToLower(asset.Ext), ".")
	if ext == "" || strings.ContainsAny(ext, `/\`) {
		ext = "bin"
	}
	dest, err := s.store.CachePath("originals", asset.CacheKey, ext)
	if err != nil {
		return false
	}
	if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() && info.Size() == asset.Size {
		s.cachePolicy.Touch(r.Context(), "originals", asset.CacheKey, dest)
		s.serveCachedMediaFile(w, r, asset, dest, info)
		return true
	}
	if r.Method == http.MethodHead {
		return false
	}
	s.cacheCopyMu.Lock()
	defer s.cacheCopyMu.Unlock()
	if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() && info.Size() == asset.Size {
		s.cachePolicy.Touch(r.Context(), "originals", asset.CacheKey, dest)
		s.serveCachedMediaFile(w, r, asset, dest, info)
		return true
	}
	if _, err := s.cachePolicy.EnsureCapacity(r.Context(), asset.Size); err != nil {
		return false
	}
	source, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		return false
	}
	batchID, _ := s.db.BeginSourceIOBatch(context.Background(), "viewer_image", 0)
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	input, _, err := openReadableSource(source, 2*time.Second)
	if err != nil {
		_ = s.db.FinishSourceIOBatch(context.Background(), batchID, "failed", 1, 0, err.Error())
		return false
	}
	output, err := os.Create(tmp)
	var copied int64
	if err == nil {
		copied, err = io.Copy(output, input)
		closeErr := output.Close()
		if err == nil {
			err = closeErr
		}
	}
	_ = input.Close()
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		_ = os.Remove(tmp)
		_ = s.db.FinishSourceIOBatch(context.Background(), batchID, "failed", 1, copied, err.Error())
		return false
	}
	assetID := asset.ID
	s.cachePolicy.Register(context.Background(), "originals", asset.CacheKey, dest, &assetID, 10*time.Minute)
	_ = s.db.FinishSourceIOBatch(context.Background(), batchID, "success", 1, copied, "")
	info, err := os.Stat(dest)
	if err != nil {
		return false
	}
	s.serveCachedMediaFile(w, r, asset, dest, info)
	return true
}

func (s *Server) serveCachedMediaFile(w http.ResponseWriter, r *http.Request, asset model.Asset, path string, info os.FileInfo) {
	releaseCache := s.cachePolicy.Pin(path)
	defer releaseCache()
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	defer file.Close()
	if asset.MimeType != nil && *asset.MimeType != "" {
		w.Header().Set("Content-Type", *asset.MimeType)
	} else if value := mime.TypeByExtension("." + asset.Ext); value != "" {
		w.Header().Set("Content-Type", value)
	}
	w.Header().Set("ETag", fmt.Sprintf(`W/"asset-%d-%s"`, asset.ID, asset.CacheKey))
	w.Header().Set("Content-Disposition", contentDisposition(asset.Filename))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, asset.Filename, info.ModTime(), file)
}

func (s *Server) serveChunkCachedVideo(w http.ResponseWriter, r *http.Request, asset model.Asset) bool {
	return s.serveChunkCachedMedia(w, r, asset, "video-chunks", "video_playback")
}

func (s *Server) serveChunkCachedAudio(w http.ResponseWriter, r *http.Request, asset model.Asset) bool {
	return s.serveChunkCachedMedia(w, r, asset, "audio-chunks", "audio_playback")
}

func (s *Server) serveChunkCachedMedia(w http.ResponseWriter, r *http.Request, asset model.Asset, cacheKind, sourceReason string) bool {
	if asset.Size <= 0 {
		return false
	}
	reader := &chunkedVideoReader{
		server: s, ctx: r.Context(), asset: asset, size: asset.Size,
		cacheKind: cacheKind, sourceReason: sourceReason,
	}
	defer reader.Close()
	if asset.MimeType != nil && *asset.MimeType != "" {
		w.Header().Set("Content-Type", *asset.MimeType)
	} else if value := mime.TypeByExtension("." + asset.Ext); value != "" {
		w.Header().Set("Content-Type", value)
	}
	w.Header().Set("ETag", fmt.Sprintf(`W/"asset-%d-%s"`, asset.ID, asset.CacheKey))
	w.Header().Set("Content-Disposition", contentDisposition(asset.Filename))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-Accel-Buffering", "no")
	if err := serveBoundedVideoContent(w, r, reader, asset.Size, time.Unix(asset.Mtime, 0)); err != nil && r.Context().Err() == nil && s.logger != nil {
		s.logger.Warn("direct video response stopped", "assetID", asset.ID, "error", err)
	}
	return true
}

func serveBoundedVideoContent(w http.ResponseWriter, r *http.Request, reader io.ReadSeeker, size int64, modTime time.Time) error {
	if !modTime.IsZero() {
		w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	}
	rangeHeader := strings.TrimSpace(r.Header.Get("Range"))
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return nil
		}
		_, err := io.CopyN(w, reader, size)
		return err
	}

	start, end, err := boundedByteRange(rangeHeader, size, 0)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		writeError(w, http.StatusRequestedRangeNotSatisfiable, "invalid_range", "请求的媒体范围无效")
		return nil
	}
	length := end - start + 1
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusPartialContent)
	if r.Method == http.MethodHead {
		return nil
	}
	if _, err := reader.Seek(start, io.SeekStart); err != nil {
		return err
	}
	_, err = io.CopyN(w, reader, length)
	return err
}

func boundedByteRange(header string, size, limit int64) (int64, int64, error) {
	if size <= 0 || limit < 0 || !strings.HasPrefix(header, "bytes=") {
		return 0, 0, errors.New("invalid byte range")
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, errors.New("multiple or empty byte range")
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid byte range")
	}

	var start, end int64
	var err error
	if parts[0] == "" {
		suffix, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || suffix <= 0 {
			return 0, 0, errors.New("invalid suffix byte range")
		}
		if suffix > size {
			suffix = size
		}
		start = size - suffix
		end = size - 1
	} else {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || start < 0 || start >= size {
			return 0, 0, errors.New("invalid byte range start")
		}
		if parts[1] == "" {
			end = size - 1
		} else {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || end < start {
				return 0, 0, errors.New("invalid byte range end")
			}
			if end >= size {
				end = size - 1
			}
		}
	}
	if limit > 0 {
		if maximum := start + limit - 1; end > maximum {
			end = maximum
		}
	}
	return start, end, nil
}

func (s *Server) prewarmDirectVideo(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo || !asset.BrowserPlayable || asset.Duration == nil || *asset.Duration <= 0 {
		writeJSON(w, http.StatusOK, map[string]any{"accepted": false, "chunks": 0})
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if start < 0 {
		start = 0
	}
	bytesPerSecond := float64(asset.Size) / *asset.Duration
	startByte := int64(start * bytesPerSecond)
	endByte := int64((start + 50) * bytesPerSecond)
	if endByte > asset.Size {
		endByte = asset.Size
	}
	first := startByte / directMediaChunkBytes
	last := endByte / directMediaChunkBytes
	if last > first+15 {
		last = first + 15
	}
	chunks := int(last-first) + 1
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		stopPriority := s.holdPlaybackPriority(ctx)
		defer stopPriority()
		reader := &chunkedVideoReader{server: s, ctx: ctx, asset: asset, size: asset.Size, cacheKind: "video-chunks", sourceReason: "video_playback"}
		defer reader.Close()
		for index := first; index <= last; index++ {
			if _, err := reader.ensureChunk(index); err != nil {
				if s.logger != nil {
					s.logger.Warn("direct video read-ahead stopped", "assetID", asset.ID, "chunk", index, "error", err)
				}
				return
			}
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "chunks": chunks})
}

type chunkedVideoReader struct {
	server       *Server
	ctx          context.Context
	asset        model.Asset
	size         int64
	offset       int64
	source       *os.File
	batchID      int64
	bytesRead    int64
	readErr      error
	cacheKind    string
	sourceReason string
}

func (r *chunkedVideoReader) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.offset + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return 0, errors.New("invalid seek")
	}
	if next < 0 {
		return 0, errors.New("negative seek")
	}
	r.offset = next
	return next, nil
}

func (r *chunkedVideoReader) Read(target []byte) (int, error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}
	total := 0
	for len(target) > 0 && r.offset < r.size {
		index := r.offset / directMediaChunkBytes
		chunkOffset := r.offset % directMediaChunkBytes
		path, err := r.ensureChunk(index)
		if err != nil {
			r.readErr = err
			return total, err
		}
		releaseCache := r.server.cachePolicy.Pin(path)
		file, err := os.Open(path)
		if err != nil {
			releaseCache()
			r.readErr = err
			return total, err
		}
		count, readErr := file.ReadAt(target, chunkOffset)
		_ = file.Close()
		releaseCache()
		total += count
		r.offset += int64(count)
		target = target[count:]
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			r.readErr = readErr
			return total, readErr
		}
		if count == 0 {
			break
		}
	}
	if total == 0 && r.offset >= r.size {
		return 0, io.EOF
	}
	return total, nil
}

func (r *chunkedVideoReader) ensureChunk(index int64) (string, error) {
	key := r.asset.CacheKey + "-c" + strconv.FormatInt(index, 10)
	if r.cacheKind == "" {
		r.cacheKind = "video-chunks"
	}
	if r.sourceReason == "" {
		r.sourceReason = "video_playback"
	}
	path, err := r.server.store.CachePath(r.cacheKind, key, "bin")
	if err != nil {
		return "", err
	}
	releaseCache := r.server.cachePolicy.Pin(path)
	defer releaseCache()
	length := expectedVideoChunkBytes(r.size, index)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() == length {
		r.server.cachePolicy.Touch(r.ctx, r.cacheKind, key, path)
		return path, nil
	}
	r.server.cacheCopyMu.Lock()
	defer r.server.cacheCopyMu.Unlock()
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() == length {
		return path, nil
	}
	_ = os.Remove(path)
	if _, err := r.server.cachePolicy.EnsureCapacity(r.ctx, length); err != nil {
		return "", err
	}
	if r.source == nil {
		sourcePath, err := r.server.store.PhotoPath(r.asset.RelPath)
		if err != nil {
			return "", err
		}
		source, _, err := openReadableSource(sourcePath, directVideoSourceOpenTimeout)
		if err != nil {
			return "", err
		}
		r.source = source
		r.batchID, _ = r.server.db.BeginSourceIOBatch(context.Background(), r.sourceReason, 0)
	}
	data := make([]byte, length)
	count, err := r.source.ReadAt(data, index*directMediaChunkBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	r.bytesRead += int64(count)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data[:count], 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	assetID := r.asset.ID
	r.server.cachePolicy.Register(context.Background(), r.cacheKind, key, path, &assetID, 10*time.Minute)
	return path, nil
}

func expectedVideoChunkBytes(size, index int64) int64 {
	remaining := size - index*directMediaChunkBytes
	if remaining <= 0 {
		return 0
	}
	if remaining < directMediaChunkBytes {
		return remaining
	}
	return directMediaChunkBytes
}

func (r *chunkedVideoReader) Close() {
	if r.source != nil {
		_ = r.source.Close()
	}
	if r.batchID > 0 {
		state, message := "success", ""
		if r.readErr != nil {
			state, message = "failed", r.readErr.Error()
		}
		_ = r.server.db.FinishSourceIOBatch(context.Background(), r.batchID, state, 1, r.bytesRead, message)
	}
}
