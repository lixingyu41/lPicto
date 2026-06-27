package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/media"
	"lpicto/backend/internal/model"
)

const maxNFOBytes = 256 * 1024
const maxSubtitleBytes = 16 * 1024 * 1024

type AssetSidecarsDTO struct {
	NFO               *NFODTO       `json:"nfo"`
	Subtitles         []SubtitleDTO `json:"subtitles"`
	DefaultSubtitleID *string       `json:"defaultSubtitleId"`
}

type NFODTO = media.NFOInfo
type NFOGroupDTO = media.NFOGroup
type NFOFieldDTO = media.NFOField

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
	data, err := media.ReadLimitedFile(selected.AbsPath, maxSubtitleBytes)
	if err != nil {
		writeError(w, http.StatusNotFound, "subtitle_not_found", "字幕不可读取")
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if strings.EqualFold(selected.Format, "bilibili") {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	if strings.EqualFold(selected.Format, "srt") {
		_, _ = w.Write([]byte(srtToVTT(string(data))))
		return
	}
	if strings.EqualFold(selected.Format, "ass") || strings.EqualFold(selected.Format, "ssa") {
		_, _ = w.Write([]byte(assToVTT(string(data))))
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
	root, err := s.store.RootForPath(assetPath)
	if err != nil {
		return nil, err
	}
	return media.ReadNFOForAsset(assetPath, root.Path, maxNFOBytes)
}

func (s *Server) subtitleFiles(asset model.Asset) ([]sidecarFile, error) {
	if asset.MediaType != model.MediaTypeVideo {
		return nil, nil
	}
	assetPath, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		return nil, err
	}
	root, err := s.store.RootForPath(assetPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(assetPath)
	base := strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	extensions := map[string]string{
		".ass": "ass",
		".srt": "srt",
		".ssa": "ssa",
		".vtt": "vtt",
		".xml": "bilibili",
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	files := make([]sidecarFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		format, ok := extensions[ext]
		if !ok {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !media.SafeReadableSidecar(root.Path, path) {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		matched := subtitleStemMatches(base, stem)
		id := media.SidecarID(asset.ID, entry.Name())
		files = append(files, sidecarFile{
			ID: id, AbsPath: path, Filename: entry.Name(), Format: format, Default: matched,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Default != files[j].Default {
			return files[i].Default
		}
		if files[i].Filename == files[j].Filename {
			return files[i].Format < files[j].Format
		}
		return strings.ToLower(files[i].Filename) < strings.ToLower(files[j].Filename)
	})
	if len(files) > 0 && !hasDefaultSubtitle(files) {
		files[0].Default = true
	}
	return files, nil
}

func subtitleStemMatches(assetBase string, subtitleStem string) bool {
	base := strings.ToLower(strings.TrimSpace(assetBase))
	stem := strings.ToLower(strings.TrimSpace(subtitleStem))
	if stem == base {
		return true
	}
	return strings.HasPrefix(stem, base+".") ||
		strings.HasPrefix(stem, base+" ") ||
		strings.HasPrefix(stem, base+"-") ||
		strings.HasPrefix(stem, base+"_")
}

func hasDefaultSubtitle(files []sidecarFile) bool {
	for _, file := range files {
		if file.Default {
			return true
		}
	}
	return false
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

func assToVTT(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	inEvents := false
	format := []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}
	cues := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inEvents = strings.EqualFold(trimmed, "[Events]")
			continue
		}
		if !inEvents {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "format:") {
			format = parseASSFormat(trimmed[len("format:"):])
			continue
		}
		if !strings.HasPrefix(lower, "dialogue:") {
			continue
		}
		payload := strings.TrimSpace(trimmed[len("dialogue:"):])
		fields := strings.SplitN(payload, ",", len(format))
		startIndex := assFormatIndex(format, "start")
		endIndex := assFormatIndex(format, "end")
		textIndex := assFormatIndex(format, "text")
		if startIndex < 0 || endIndex < 0 || textIndex < 0 || startIndex >= len(fields) || endIndex >= len(fields) || textIndex >= len(fields) {
			continue
		}
		body := cleanASSText(fields[textIndex])
		if body == "" {
			continue
		}
		cues = append(cues, assTimeToVTT(fields[startIndex])+" --> "+assTimeToVTT(fields[endIndex])+"\n"+body)
	}
	return "WEBVTT\n\n" + strings.Join(cues, "\n\n")
}

func parseASSFormat(value string) []string {
	parts := strings.Split(value, ",")
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name != "" {
			fields = append(fields, name)
		}
	}
	return fields
}

func assFormatIndex(format []string, name string) int {
	for index, field := range format {
		if field == name {
			return index
		}
	}
	return -1
}

func assTimeToVTT(value string) string {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return strings.TrimSpace(value)
	}
	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	secondsPart := strings.SplitN(parts[2], ".", 2)
	seconds, _ := strconv.Atoi(secondsPart[0])
	milliseconds := 0
	if len(secondsPart) == 2 {
		fraction := secondsPart[1]
		if len(fraction) > 3 {
			fraction = fraction[:3]
		}
		for len(fraction) < 3 {
			fraction += "0"
		}
		milliseconds, _ = strconv.Atoi(fraction)
	}
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds)
}

func cleanASSText(value string) string {
	replacer := strings.NewReplacer(`\N`, "\n", `\n`, "\n", `\h`, " ")
	text := replacer.Replace(strings.TrimSpace(value))
	var builder strings.Builder
	inOverride := false
	for _, r := range text {
		switch r {
		case '{':
			inOverride = true
		case '}':
			inOverride = false
		default:
			if !inOverride {
				builder.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(builder.String())
}
