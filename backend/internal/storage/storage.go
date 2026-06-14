package storage

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Store struct {
	PhotoRoot string
	DataRoot  string
}

func New(photoRoot, dataRoot string) (Store, error) {
	photoAbs, err := filepath.Abs(photoRoot)
	if err != nil {
		return Store{}, err
	}
	dataAbs, err := filepath.Abs(dataRoot)
	if err != nil {
		return Store{}, err
	}
	return Store{PhotoRoot: filepath.Clean(photoAbs), DataRoot: filepath.Clean(dataAbs)}, nil
}

func NormalizeRelPath(input string) (string, error) {
	rel := strings.ReplaceAll(strings.TrimSpace(input), `\`, "/")
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." {
		return "", nil
	}
	if strings.HasPrefix(rel, "../") || rel == ".." || strings.Contains(rel, "/../") {
		return "", errors.New("relative path contains traversal")
	}
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == "/" {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return "", errors.New("relative path escapes root")
	}
	return cleaned, nil
}

func ParentRelPath(rel string) string {
	rel = filepath.ToSlash(rel)
	parent := path.Dir(rel)
	if parent == "." || parent == "/" {
		return ""
	}
	return parent
}

func FolderName(rel string) string {
	if rel == "" {
		return "照片"
	}
	return path.Base(filepath.ToSlash(rel))
}

func FolderDepth(rel string) int {
	if rel == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

func AncestorFolders(rel string) []string {
	parent := ParentRelPath(rel)
	folders := []string{""}
	if parent == "" {
		return folders
	}
	parts := strings.Split(parent, "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		folders = append(folders, current)
	}
	return folders
}

func (s Store) PhotoPath(rel string) (string, error) {
	normalized, err := NormalizeRelPath(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.PhotoRoot, filepath.FromSlash(normalized))
	if !IsWithinRoot(s.PhotoRoot, full) {
		return "", errors.New("path escapes photo root")
	}
	return full, nil
}

func (s Store) RelPath(absPath string) (string, error) {
	rel, err := filepath.Rel(s.PhotoRoot, absPath)
	if err != nil {
		return "", err
	}
	return NormalizeRelPath(rel)
}

func IsWithinRoot(root, candidate string) bool {
	rootClean, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	candidateClean, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rootClean = filepath.Clean(rootClean)
	candidateClean = filepath.Clean(candidateClean)
	if samePath(rootClean, candidateClean) {
		return true
	}
	rel, err := filepath.Rel(rootClean, candidateClean)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel)
}

func SymlinkTargetWithinRoot(root, linkPath string) (bool, string, error) {
	target, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return false, "", err
	}
	return IsWithinRoot(root, target), target, nil
}

func CacheKey(rel string, size, mtime int64) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s:%d:%d", filepath.ToSlash(rel), size, mtime)))
	return hex.EncodeToString(sum[:])[:20]
}

func (s Store) CachePath(kind, cacheKey, ext string) (string, error) {
	if cacheKey == "" {
		return "", errors.New("empty cache key")
	}
	if ext == "" {
		return "", errors.New("empty cache extension")
	}
	switch kind {
	case "thumbs", "previews", "video-posters", "video-proxies":
	default:
		return "", errors.New("invalid cache kind")
	}
	shard := cacheKey
	if len(shard) > 2 {
		shard = shard[:2]
	}
	dir := filepath.Join(s.DataRoot, "cache", kind, shard)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheKey+"."+strings.TrimPrefix(ext, ".")), nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
