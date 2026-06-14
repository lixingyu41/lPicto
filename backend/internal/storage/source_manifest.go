package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sourceFolderManifest struct {
	Folders []string `json:"folders"`
}

func SourceFoldersManifestPath(dataRoot string) string {
	return filepath.Join(dataRoot, "source-folders.json")
}

func LoadSourceFolderManifest(dataRoot string) ([]string, error) {
	data, err := os.ReadFile(SourceFoldersManifestPath(dataRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var folders []string
	if err := json.Unmarshal(data, &folders); err != nil {
		var wrapped sourceFolderManifest
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil {
			return nil, err
		}
		folders = wrapped.Folders
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(folders))
	for _, folder := range folders {
		rel, err := NormalizeRelPath(folder)
		if err != nil || rel == "" {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		result = append(result, rel)
	}
	sort.Slice(result, func(i, j int) bool {
		if FolderDepth(result[i]) == FolderDepth(result[j]) {
			return result[i] < result[j]
		}
		return FolderDepth(result[i]) < FolderDepth(result[j])
	})
	return result, nil
}

func ManifestChildren(folders []string, parent string) []string {
	seen := map[string]struct{}{}
	var children []string
	for _, folder := range folders {
		child := manifestChild(folder, parent)
		if child == "" {
			continue
		}
		if _, ok := seen[child]; ok {
			continue
		}
		seen[child] = struct{}{}
		children = append(children, child)
	}
	sort.Strings(children)
	return children
}

func ManifestTopLevelFolders(folders []string) []string {
	return ManifestChildren(folders, "")
}

func manifestChild(folder string, parent string) string {
	if parent == "" {
		if !strings.Contains(folder, "/") {
			return folder
		}
		return strings.Split(folder, "/")[0]
	}
	if folder == parent || !strings.HasPrefix(folder, parent+"/") {
		return ""
	}
	rest := strings.TrimPrefix(folder, parent+"/")
	first := strings.Split(rest, "/")[0]
	if first == "" {
		return ""
	}
	return parent + "/" + first
}
