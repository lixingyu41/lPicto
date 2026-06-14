package api

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
)

const maxNFOBytes = 256 * 1024
const maxSubtitleBytes = 4 * 1024 * 1024

type AssetSidecarsDTO struct {
	NFO               *NFODTO       `json:"nfo"`
	Subtitles         []SubtitleDTO `json:"subtitles"`
	DefaultSubtitleID *string       `json:"defaultSubtitleId"`
}

type NFODTO struct {
	Filename string            `json:"filename"`
	Fields   map[string]string `json:"fields"`
	Text     string            `json:"text"`
}

type SubtitleDTO struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Format   string `json:"format"`
	Default  bool   `json:"default"`
}

type sidecarFile struct {
	ID       string
	AbsPath  string
	Filename string
	Format   string
	Default  bool
}

func (s *Server) assetSidecars(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	sidecars, err := s.sidecarsForAsset(asset)
	if err != nil {
		s.logger.Warn("load asset sidecars failed", "assetID", asset.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "sidecars_failed", "读取附加信息失败")
		return
	}
	writeJSON(w, http.StatusOK, sidecars)
}

func (s *Server) assetSubtitle(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	subtitleID := strings.TrimSpace(chi.URLParam(r, "subtitleID"))
	subtitles, err := s.subtitleFiles(asset)
	if err != nil {
		s.logger.Warn("load subtitle files failed", "assetID", asset.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "subtitles_failed", "读取字幕失败")
		return
	}
	var selected *sidecarFile
	for i := range subtitles {
		if subtitles[i].ID == subtitleID {
			selected = &subtitles[i]
			break
		}
	}
	if selected == nil {
		writeError(w, http.StatusNotFound, "subtitle_not_found", "字幕不存在")
		return
	}
	data, err := readLimitedFile(selected.AbsPath, maxSubtitleBytes)
	if err != nil {
		writeError(w, http.StatusNotFound, "subtitle_not_found", "字幕不可读取")
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if strings.EqualFold(selected.Format, "srt") {
		_, _ = w.Write([]byte(srtToVTT(string(data))))
		return
	}
	_, _ = w.Write(data)
}

func (s *Server) sidecarsForAsset(asset model.Asset) (AssetSidecarsDTO, error) {
	subtitles, err := s.subtitleFiles(asset)
	if err != nil {
		return AssetSidecarsDTO{}, err
	}
	subtitleDTOs := make([]SubtitleDTO, 0, len(subtitles))
	var defaultID *string
	for _, subtitle := range subtitles {
		subtitleDTOs = append(subtitleDTOs, SubtitleDTO{
			ID: subtitle.ID, Filename: subtitle.Filename, Format: subtitle.Format, Default: subtitle.Default,
		})
		if subtitle.Default && defaultID == nil {
			id := subtitle.ID
			defaultID = &id
		}
	}
	nfo, err := s.nfoForAsset(asset)
	if err != nil {
		return AssetSidecarsDTO{}, err
	}
	return AssetSidecarsDTO{NFO: nfo, Subtitles: subtitleDTOs, DefaultSubtitleID: defaultID}, nil
}

func (s *Server) nfoForAsset(asset model.Asset) (*NFODTO, error) {
	assetPath, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(assetPath)
	base := strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	path, ok := findSidecarPath(s.store.PhotoRoot, dir, base, []string{".nfo"})
	if !ok {
		return nil, nil
	}
	data, err := readLimitedFile(path, maxNFOBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(data))
	return &NFODTO{Filename: filepath.Base(path), Fields: parseNFOFields(text), Text: text}, nil
}

func (s *Server) subtitleFiles(asset model.Asset) ([]sidecarFile, error) {
	if asset.MediaType != model.MediaTypeVideo {
		return nil, nil
	}
	assetPath, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(assetPath)
	base := strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	extensions := []string{".vtt", ".srt"}
	files := make([]sidecarFile, 0, len(extensions))
	for _, ext := range extensions {
		path, ok := findSidecarPath(s.store.PhotoRoot, dir, base, []string{ext})
		if !ok {
			continue
		}
		id := sidecarID(asset.ID, filepath.Base(path))
		files = append(files, sidecarFile{
			ID: id, AbsPath: path, Filename: filepath.Base(path), Format: strings.TrimPrefix(ext, "."), Default: true,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Format == files[j].Format {
			return files[i].Filename < files[j].Filename
		}
		return files[i].Format < files[j].Format
	})
	return files, nil
}

func findSidecarPath(root string, dir string, base string, extensions []string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	want := make(map[string]struct{}, len(extensions))
	for _, ext := range extensions {
		want[strings.ToLower(base+ext)] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := want[strings.ToLower(entry.Name())]; !ok {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if safeReadableSidecar(root, path) {
			return path, true
		}
	}
	return "", false
}

func safeReadableSidecar(root string, path string) bool {
	if !storage.IsWithinRoot(root, path) {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		inside, _, err := storage.SymlinkTargetWithinRoot(root, path)
		if err != nil || !inside {
			return false
		}
		info, err = os.Stat(path)
		if err != nil {
			return false
		}
	}
	return info.Mode().IsRegular()
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit+1))
}

func sidecarID(assetID int64, filename string) string {
	sum := sha1.Sum([]byte(strconv.FormatInt(assetID, 10) + ":" + filename))
	return hex.EncodeToString(sum[:])[:12]
}

func parseNFOFields(text string) map[string]string {
	fields := map[string]string{}
	if text == "" || !strings.Contains(text, "<") {
		return fields
	}
	labels := map[string]string{
		"title":         "标题",
		"originaltitle": "原名",
		"year":          "年份",
		"premiered":     "首映",
		"releasedate":   "发布日期",
		"runtime":       "片长",
		"genre":         "类型",
		"studio":        "制作方",
		"director":      "导演",
		"credits":       "编剧",
		"rating":        "评分",
		"plot":          "简介",
		"outline":       "概述",
	}
	decoder := xml.NewDecoder(strings.NewReader(text))
	current := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			current = strings.ToLower(value.Name.Local)
		case xml.CharData:
			label, ok := labels[current]
			if !ok {
				continue
			}
			content := strings.TrimSpace(string(value))
			if content == "" {
				continue
			}
			if existing := fields[label]; existing != "" {
				if !strings.Contains(existing, content) {
					fields[label] = existing + " / " + content
				}
			} else {
				fields[label] = content
			}
		case xml.EndElement:
			current = ""
		}
	}
	return fields
}

func srtToVTT(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, " --> ") {
			lines[i] = strings.ReplaceAll(line, ",", ".")
		}
	}
	return "WEBVTT\n\n" + strings.Join(lines, "\n")
}
