package media

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lpicto/backend/internal/storage"
)

const MaxNFOBytes int64 = 256 * 1024

type NFOInfo struct {
	Filename string            `json:"filename"`
	Fields   map[string]string `json:"fields"`
	Groups   []NFOGroup        `json:"groups"`
	Text     string            `json:"text,omitempty"`
}

type NFOGroup struct {
	Title string     `json:"title"`
	Items []NFOField `json:"items"`
}

type NFOField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	Copyable bool   `json:"copyable"`
}

func ReadNFOForAsset(assetPath string, rootPath string, limit int64) (*NFOInfo, error) {
	dir := filepath.Dir(assetPath)
	base := strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	path, ok := FindSidecarPath(rootPath, dir, base, []string{".nfo"})
	if !ok {
		return nil, nil
	}
	data, err := ReadLimitedFile(path, limit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		data = data[:limit]
	}
	info := ParseNFO(filepath.Base(path), strings.TrimSpace(string(data)))
	return &info, nil
}

func ParseNFO(filename string, text string) NFOInfo {
	fields, groups := parseNFOFields(text)
	return NFOInfo{Filename: filename, Fields: fields, Groups: groups, Text: text}
}

func NFOJSON(info NFOInfo) (string, error) {
	info.Text = ""
	data, err := json.Marshal(info)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func NFOSearchText(info NFOInfo) string {
	parts := []string{info.Filename}
	seen := map[string]struct{}{}
	add := func(value string) {
		value = cleanNFOValue(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		parts = append(parts, value)
	}
	for key, value := range info.Fields {
		add(key)
		add(value)
	}
	for _, group := range info.Groups {
		add(group.Title)
		for _, item := range group.Items {
			add(item.Key)
			add(item.Label)
			add(item.Value)
		}
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func FindSidecarPath(root string, dir string, base string, extensions []string) (string, bool) {
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
		if SafeReadableSidecar(root, path) {
			return path, true
		}
	}
	return "", false
}

func SafeReadableSidecar(root string, path string) bool {
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

func ReadLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit+1))
}

func SidecarID(assetID int64, filename string) string {
	sum := sha1.Sum([]byte(strconv.FormatInt(assetID, 10) + ":" + filename))
	return hex.EncodeToString(sum[:])[:12]
}

func parseNFOFields(text string) (map[string]string, []NFOGroup) {
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
		"premiered":     {label: "首映时间", group: "基本"},
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
	grouped := make(map[string][]NFOField, len(groupOrder))
	addItem := func(group string, item NFOField) {
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
				addItem("演员", NFOField{Key: "actor", Label: "演员", Value: actorValue, Copyable: true})
				actor = nil
			} else if name == "uniqueid" {
				label := "ID"
				key := "uniqueid"
				if uniqueIDType != "" {
					label = strings.ToUpper(uniqueIDType)
					key += ":" + strings.ToLower(uniqueIDType)
				}
				addItem("ID", NFOField{Key: key, Label: label, Value: textContent, Copyable: true})
				uniqueIDType = ""
			} else if meta, ok := labels[name]; ok && parent != "actor" {
				addItem(meta.group, NFOField{Key: name, Label: meta.label, Value: textContent, Copyable: meta.copyable})
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			content.Reset()
		}
	}
	groups := make([]NFOGroup, 0, len(groupOrder))
	for _, title := range groupOrder {
		items := grouped[title]
		if len(items) == 0 {
			continue
		}
		groups = append(groups, NFOGroup{Title: title, Items: items})
	}
	return flatFields, groups
}

func cleanNFOValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
