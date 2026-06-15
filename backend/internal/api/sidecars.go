package api

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
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
	Groups   []NFOGroupDTO     `json:"groups"`
	Text     string            `json:"text"`
}

type NFOGroupDTO struct {
	Title string        `json:"title"`
	Items []NFOFieldDTO `json:"items"`
}

type NFOFieldDTO struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	Copyable bool   `json:"copyable"`
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
	dir := filepath.Dir(assetPath)
	base := strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	path, ok := findSidecarPath(root.Path, dir, base, []string{".nfo"})
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
	fields, groups := parseNFOFields(text)
	return &NFODTO{Filename: filepath.Base(path), Fields: fields, Groups: groups, Text: text}, nil
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
		if !safeReadableSidecar(root.Path, path) {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		matched := subtitleStemMatches(base, stem)
		id := sidecarID(asset.ID, entry.Name())
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

func parseNFOFields(text string) (map[string]string, []NFOGroupDTO) {
	flatFields := map[string]string{}
	if text == "" || !strings.Contains(text, "<") {
		return flatFields, nil
	}
	type nfoFieldMeta struct {
		label    string
		group    string
		copyable bool
	}
	labels := map[string]nfoFieldMeta{
		"country":       {label: "地区", group: "基本"},
		"credits":       {label: "编剧", group: "创作", copyable: true},
		"director":      {label: "导演", group: "创作", copyable: true},
		"edition":       {label: "版本", group: "基本"},
		"genre":         {label: "类型", group: "标记"},
		"mpaa":          {label: "分级", group: "基本"},
		"originaltitle": {label: "原名", group: "基本"},
		"outline":       {label: "概述", group: "简介"},
		"plot":          {label: "简介", group: "简介"},
		"premiered":     {label: "首映", group: "基本"},
		"rating":        {label: "评分", group: "基本"},
		"releasedate":   {label: "发布日期", group: "基本"},
		"runtime":       {label: "片长", group: "基本"},
		"sorttitle":     {label: "排序", group: "基本"},
		"studio":        {label: "制作方", group: "创作", copyable: true},
		"tag":           {label: "标签", group: "标记"},
		"tagline":       {label: "标语", group: "简介"},
		"title":         {label: "标题", group: "基本"},
		"writer":        {label: "作者", group: "创作", copyable: true},
		"year":          {label: "年份", group: "基本"},
	}
	groupOrder := []string{"基本", "创作", "演员", "ID", "标记", "简介"}
	grouped := make(map[string][]NFOFieldDTO, len(groupOrder))
	addItem := func(group string, item NFOFieldDTO) {
		value := cleanNFOValue(item.Value)
		if value == "" {
			return
		}
		item.Value = value
		for _, existing := range grouped[group] {
			if existing.Key == item.Key && existing.Value == item.Value {
				return
			}
		}
		grouped[group] = append(grouped[group], item)
		label := item.Label
		if label == "" {
			label = item.Key
		}
		if existing := flatFields[label]; existing != "" {
			if !strings.Contains(existing, value) {
				flatFields[label] = existing + " / " + value
			}
		} else {
			flatFields[label] = value
		}
	}
	decoder := xml.NewDecoder(strings.NewReader(text))
	stack := []string{}
	var content strings.Builder
	uniqueIDType := ""
	actor := map[string]string(nil)
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			name := strings.ToLower(value.Name.Local)
			stack = append(stack, name)
			content.Reset()
			if name == "actor" {
				actor = map[string]string{}
			}
			if name == "uniqueid" {
				uniqueIDType = ""
				for _, attr := range value.Attr {
					if strings.EqualFold(attr.Name.Local, "type") {
						uniqueIDType = strings.TrimSpace(attr.Value)
					}
				}
			}
		case xml.CharData:
			content.Write(value)
		case xml.EndElement:
			name := strings.ToLower(value.Name.Local)
			parent := ""
			if len(stack) >= 2 {
				parent = stack[len(stack)-2]
			}
			textContent := content.String()
			if parent == "actor" && actor != nil {
				switch name {
				case "name", "role", "type":
					actor[name] = textContent
				}
			} else if name == "actor" && actor != nil {
				actorValue := actor["name"]
				if role := cleanNFOValue(actor["role"]); role != "" {
					actorValue += " / " + role
				}
				if actorType := cleanNFOValue(actor["type"]); actorType != "" {
					actorValue += " / " + actorType
				}
				addItem("演员", NFOFieldDTO{Key: "actor", Label: "演员", Value: actorValue, Copyable: true})
				actor = nil
			} else if name == "uniqueid" {
				label := "ID"
				key := "uniqueid"
				if uniqueIDType != "" {
					label = strings.ToUpper(uniqueIDType)
					key += ":" + strings.ToLower(uniqueIDType)
				}
				addItem("ID", NFOFieldDTO{Key: key, Label: label, Value: textContent, Copyable: true})
				uniqueIDType = ""
			} else if meta, ok := labels[name]; ok && parent != "actor" {
				addItem(meta.group, NFOFieldDTO{Key: name, Label: meta.label, Value: textContent, Copyable: meta.copyable})
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			content.Reset()
		}
	}
	groups := make([]NFOGroupDTO, 0, len(groupOrder))
	for _, title := range groupOrder {
		items := grouped[title]
		if len(items) == 0 {
			continue
		}
		groups = append(groups, NFOGroupDTO{Title: title, Items: items})
	}
	return flatFields, groups
}

func cleanNFOValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
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
