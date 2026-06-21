package storage

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type RootConfig struct {
	ID   string
	Path string
}

type Root struct {
	ID   string
	Path string
}

type Store struct {
	PhotoRoot string
	DataRoot  string
	CacheRoot string
	Roots     []Root
	rootByID  map[string]Root
	virtual   bool
}

func New(photoRoot, dataRoot string) (Store, error) {
	return NewWithRoots([]RootConfig{{Path: photoRoot}}, dataRoot)
}

func NewWithRoots(roots []RootConfig, dataRoot string) (Store, error) {
	return NewWithRootsAndCache(roots, dataRoot, filepath.Join(dataRoot, "cache"))
}

func NewWithRootsAndCache(roots []RootConfig, dataRoot string, cacheRoot string) (Store, error) {
	if len(roots) == 0 {
		return Store{}, errors.New("at least one photo root is required")
	}
	normalizedRoots := make([]Root, 0, len(roots))
	seen := map[string]struct{}{}
	hasNamedRoot := false
	for _, root := range roots {
		id, err := NormalizeRootID(root.ID)
		if err != nil {
			return Store{}, err
		}
		if id != "" {
			hasNamedRoot = true
		}
		if _, ok := seen[id]; ok {
			return Store{}, fmt.Errorf("duplicate photo root id %q", id)
		}
		seen[id] = struct{}{}
		abs, err := filepath.Abs(root.Path)
		if err != nil {
			return Store{}, err
		}
		normalizedRoots = append(normalizedRoots, Root{ID: id, Path: filepath.Clean(abs)})
	}
	dataAbs, err := filepath.Abs(dataRoot)
	if err != nil {
		return Store{}, err
	}
	cacheAbs, err := filepath.Abs(cacheRoot)
	if err != nil {
		return Store{}, err
	}
	rootByID := make(map[string]Root, len(normalizedRoots))
	for _, root := range normalizedRoots {
		rootByID[root.ID] = root
	}
	return Store{
		PhotoRoot: normalizedRoots[0].Path,
		DataRoot:  filepath.Clean(dataAbs),
		CacheRoot: filepath.Clean(cacheAbs),
		Roots:     normalizedRoots,
		rootByID:  rootByID,
		virtual:   hasNamedRoot,
	}, nil
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

func NormalizeRootID(input string) (string, error) {
	id, err := NormalizeRelPath(input)
	if err != nil {
		return "", err
	}
	if strings.Contains(id, "/") {
		return "", errors.New("photo root id must be one path segment")
	}
	return id, nil
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
	root, childRel, err := s.rootForRel(normalized)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root.Path, filepath.FromSlash(childRel))
	if !IsWithinRoot(root.Path, full) {
		return "", errors.New("path escapes photo root")
	}
	return full, nil
}

func (s Store) RootForRel(rel string) (Root, string, error) {
	normalized, err := NormalizeRelPath(rel)
	if err != nil {
		return Root{}, "", err
	}
	return s.rootForRel(normalized)
}

func (s Store) RelPath(absPath string) (string, error) {
	root, err := s.RootForPath(absPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root.Path, absPath)
	if err != nil {
		return "", err
	}
	normalized, err := NormalizeRelPath(rel)
	if err != nil {
		return "", err
	}
	if s.virtual {
		if normalized == "" {
			return root.ID, nil
		}
		return root.ID + "/" + normalized, nil
	}
	return normalized, nil
}

func (s Store) RootForPath(absPath string) (Root, error) {
	candidates := append([]Root(nil), s.Roots...)
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i].Path) > len(candidates[j].Path)
	})
	for _, root := range candidates {
		if IsWithinRoot(root.Path, absPath) {
			return root, nil
		}
	}
	return Root{}, errors.New("path is outside photo roots")
}

func (s Store) SymlinkTargetWithinRoot(linkPath string) (bool, string, error) {
	root, err := s.RootForPath(linkPath)
	if err != nil {
		return false, "", err
	}
	target, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return false, "", err
	}
	return IsWithinRoot(root.Path, target), target, nil
}

func (s Store) HasVirtualRoot() bool {
	return s.virtual
}

func (s Store) RootRelPaths() []string {
	if !s.virtual {
		return []string{""}
	}
	relPaths := make([]string, 0, len(s.Roots))
	for _, root := range s.Roots {
		relPaths = append(relPaths, root.ID)
	}
	return relPaths
}

func (s Store) rootForRel(rel string) (Root, string, error) {
	if !s.virtual {
		return s.Roots[0], rel, nil
	}
	if rel == "" {
		return Root{}, "", errors.New("virtual photo root has no single filesystem path")
	}
	rootID := rel
	childRel := ""
	if strings.Contains(rel, "/") {
		parts := strings.SplitN(rel, "/", 2)
		rootID = parts[0]
		childRel = parts[1]
	}
	root, ok := s.rootByID[rootID]
	if !ok {
		return Root{}, "", errors.New("unknown photo root")
	}
	return root, childRel, nil
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
	path, err := s.CacheFilePath(kind, cacheKey, ext)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func (s Store) CacheFilePath(kind, cacheKey, ext string) (string, error) {
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
	return filepath.Join(s.CacheRoot, kind, shard, cacheKey+"."+strings.TrimPrefix(ext, ".")), nil
}

func (s Store) RemoveCache(cacheKey string) error {
	if cacheKey == "" || strings.ContainsAny(cacheKey, `/\`) || strings.Contains(cacheKey, "..") {
		return errors.New("invalid cache key")
	}
	type cacheVariant struct {
		kind string
		ext  string
	}
	var firstErr error
	for _, variant := range []cacheVariant{
		{kind: "thumbs", ext: "webp"},
		{kind: "previews", ext: "webp"},
		{kind: "video-posters", ext: "jpg"},
		{kind: "video-proxies", ext: "mp4"},
	} {
		for _, path := range s.cacheVariantPaths(variant.kind, cacheKey, variant.ext) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
		}
		shardDir := filepath.Dir(s.cacheFilePath(variant.kind, cacheKey, variant.ext))
		_ = os.Remove(shardDir)
	}
	return firstErr
}

func (s Store) RemoveCacheVariant(cacheKey string, kind string, ext string) error {
	if cacheKey == "" || strings.ContainsAny(cacheKey, `/\`) || strings.Contains(cacheKey, "..") {
		return errors.New("invalid cache key")
	}
	switch kind {
	case "thumbs", "previews", "video-posters", "video-proxies":
	default:
		return errors.New("invalid cache kind")
	}
	var firstErr error
	for _, path := range s.cacheVariantPaths(kind, cacheKey, ext) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	_ = os.Remove(filepath.Dir(s.cacheFilePath(kind, cacheKey, ext)))
	return firstErr
}

func (s Store) cacheVariantPaths(kind string, cacheKey string, ext string) []string {
	path := s.cacheFilePath(kind, cacheKey, ext)
	return []string{path, path + ".tmp." + strings.TrimPrefix(ext, ".")}
}

func (s Store) cacheFilePath(kind string, cacheKey string, ext string) string {
	shard := cacheKey
	if len(shard) > 2 {
		shard = shard[:2]
	}
	return filepath.Join(s.CacheRoot, kind, shard, cacheKey+"."+strings.TrimPrefix(ext, "."))
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
