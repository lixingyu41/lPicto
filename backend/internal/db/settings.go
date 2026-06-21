package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type ScanLibrary struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Roots           []string `json:"roots"`
	DiscoveredFiles int      `json:"discoveredFiles"`
	DiscoveredAt    *int64   `json:"discoveredAt"`
}

func (d *DB) GetScanLibraries(ctx context.Context) ([]ScanLibrary, bool, error) {
	rows, err := d.conn.QueryContext(ctx, `
SELECT l.public_id, l.name, COALESCE(l.discovered_files, 0), l.discovered_at, r.rel_path
FROM scan_library l
LEFT JOIN scan_library_root r ON r.scan_library_id = l.id
ORDER BY l.id ASC, r.position ASC, r.rel_path ASC`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	index := map[string]int{}
	var libraries []ScanLibrary
	for rows.Next() {
		var id string
		var name string
		var discoveredFiles int
		var discoveredAt sql.NullInt64
		var root sql.NullString
		if err := rows.Scan(&id, &name, &discoveredFiles, &discoveredAt, &root); err != nil {
			return nil, false, err
		}
		pos, ok := index[id]
		if !ok {
			pos = len(libraries)
			index[id] = pos
			library := ScanLibrary{ID: id, Name: name, DiscoveredFiles: discoveredFiles}
			if discoveredAt.Valid {
				library.DiscoveredAt = &discoveredAt.Int64
			}
			libraries = append(libraries, library)
		}
		if root.Valid {
			libraries[pos].Roots = append(libraries[pos].Roots, root.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(libraries) == 0 {
		return []ScanLibrary{}, false, nil
	}
	normalized, err := NormalizeScanLibraries(libraries)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func (d *DB) SetScanLibraries(ctx context.Context, libraries []ScanLibrary) error {
	normalized, err := NormalizeScanLibraries(libraries)
	if err != nil {
		return err
	}
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM scan_library`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, library := range normalized {
		var libraryID int64
		if err := tx.QueryRowContext(ctx, `
INSERT INTO scan_library (public_id, name, discovered_files, discovered_at, created_at, updated_at)
VALUES (?, ?, ?, ?, now(), now())
RETURNING id`, library.ID, library.Name, library.DiscoveredFiles, nullInt64(library.DiscoveredAt)).Scan(&libraryID); err != nil {
			_ = tx.Rollback()
			return err
		}
		for index, root := range library.Roots {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO scan_library_root (scan_library_id, rel_path, position)
VALUES (?, ?, ?)`, libraryID, root, index); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

func (d *DB) AddScanLibrary(ctx context.Context, name string, roots []string) ([]ScanLibrary, ScanLibrary, error) {
	libraries, configured, err := d.GetScanLibraries(ctx)
	if err != nil {
		return nil, ScanLibrary{}, err
	}
	if !configured {
		libraries = nil
	}
	roots, err = NormalizeScanFolders(roots)
	if err != nil {
		return nil, ScanLibrary{}, err
	}
	library := ScanLibrary{
		ID:    fmt.Sprintf("lib-%d", util.UnixNowNano()),
		Name:  strings.TrimSpace(name),
		Roots: roots,
	}
	libraries = append(libraries, library)
	libraries, err = NormalizeScanLibraries(libraries)
	if err != nil {
		return nil, ScanLibrary{}, err
	}
	for _, item := range libraries {
		if item.ID == library.ID {
			library = item
			break
		}
	}
	return libraries, library, d.SetScanLibraries(ctx, libraries)
}

func (d *DB) UpdateScanLibrary(ctx context.Context, id string, name string, roots []string) ([]ScanLibrary, ScanLibrary, error) {
	libraries, _, err := d.GetScanLibraries(ctx)
	if err != nil {
		return nil, ScanLibrary{}, err
	}
	roots, err = NormalizeScanFolders(roots)
	if err != nil {
		return nil, ScanLibrary{}, err
	}
	id = strings.TrimSpace(id)
	found := false
	updated := ScanLibrary{}
	for index := range libraries {
		if libraries[index].ID != id {
			continue
		}
		if !sameStringSet(libraries[index].Roots, roots) {
			libraries[index].DiscoveredFiles = 0
			libraries[index].DiscoveredAt = nil
		}
		libraries[index].Name = strings.TrimSpace(name)
		libraries[index].Roots = roots
		updated = libraries[index]
		found = true
		break
	}
	if !found {
		return nil, ScanLibrary{}, sql.ErrNoRows
	}
	libraries, err = NormalizeScanLibraries(libraries)
	if err != nil {
		return nil, ScanLibrary{}, err
	}
	for _, item := range libraries {
		if item.ID == id {
			updated = item
			break
		}
	}
	return libraries, updated, d.SetScanLibraries(ctx, libraries)
}

func (d *DB) RemoveScanLibrary(ctx context.Context, id string) ([]ScanLibrary, error) {
	libraries, _, err := d.GetScanLibraries(ctx)
	if err != nil {
		return nil, err
	}
	next := make([]ScanLibrary, 0, len(libraries))
	for _, library := range libraries {
		if library.ID != id {
			next = append(next, library)
		}
	}
	return next, d.SetScanLibraries(ctx, next)
}

func (d *DB) FindScanLibrary(ctx context.Context, id string) (ScanLibrary, error) {
	libraries, _, err := d.GetScanLibraries(ctx)
	if err != nil {
		return ScanLibrary{}, err
	}
	for _, library := range libraries {
		if library.ID == id {
			return library, nil
		}
	}
	return ScanLibrary{}, sql.ErrNoRows
}

func (d *DB) UpdateScanLibraryDiscovered(ctx context.Context, id string, discoveredFiles int, discoveredAt int64) error {
	_, err := d.conn.ExecContext(ctx, `
UPDATE scan_library
SET discovered_files = ?, discovered_at = ?, updated_at = now()
WHERE public_id = ?`, discoveredFiles, discoveredAt, strings.TrimSpace(id))
	return err
}

func (d *DB) GetScanFolders(ctx context.Context) ([]string, bool, error) {
	libraries, configured, err := d.GetScanLibraries(ctx)
	if err != nil {
		return nil, configured, err
	}
	return ScanRoots(libraries), configured, nil
}

func (d *DB) getLegacyScanFolders(ctx context.Context) ([]string, bool, error) {
	return []string{""}, false, nil
}

func (d *DB) SetScanFolders(ctx context.Context, folders []string) error {
	normalized, err := NormalizeScanFolders(folders)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return d.SetScanLibraries(ctx, nil)
	}
	return d.SetScanLibraries(ctx, []ScanLibrary{{ID: "legacy", Name: "默认来源", Roots: normalized}})
}

func (d *DB) setLegacyScanFolders(ctx context.Context, folders []string) error {
	return d.SetScanFolders(ctx, folders)
}

func (d *DB) AddScanFolder(ctx context.Context, rel string) ([]string, error) {
	folders, _, err := d.GetScanFolders(ctx)
	if err != nil {
		return nil, err
	}
	normalized, err := storage.NormalizeRelPath(rel)
	if err != nil {
		return nil, err
	}
	folders = append(folders, normalized)
	folders, err = NormalizeScanFolders(folders)
	if err != nil {
		return nil, err
	}
	return folders, d.SetScanFolders(ctx, folders)
}

func (d *DB) RemoveScanFolder(ctx context.Context, rel string) ([]string, error) {
	folders, _, err := d.GetScanFolders(ctx)
	if err != nil {
		return nil, err
	}
	normalized, err := storage.NormalizeRelPath(rel)
	if err != nil {
		return nil, err
	}
	next := make([]string, 0, len(folders))
	for _, folder := range folders {
		if folder != normalized {
			next = append(next, folder)
		}
	}
	next, err = NormalizeScanFolders(next)
	if err != nil {
		return nil, err
	}
	return next, d.SetScanFolders(ctx, next)
}

func NormalizeScanFolders(folders []string) ([]string, error) {
	seen := make(map[string]struct{}, len(folders))
	var normalized []string
	for _, folder := range folders {
		rel, err := storage.NormalizeRelPath(folder)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		normalized = append(normalized, rel)
	}
	if _, ok := seen[""]; ok {
		return []string{""}, nil
	}
	sort.Slice(normalized, func(i, j int) bool {
		if storage.FolderDepth(normalized[i]) == storage.FolderDepth(normalized[j]) {
			return normalized[i] < normalized[j]
		}
		return storage.FolderDepth(normalized[i]) < storage.FolderDepth(normalized[j])
	})
	reduced := make([]string, 0, len(normalized))
	for _, folder := range normalized {
		if hasAncestor(folder, reduced) {
			continue
		}
		reduced = append(reduced, folder)
	}
	return reduced, nil
}

func NormalizeScanLibraries(libraries []ScanLibrary) ([]ScanLibrary, error) {
	ids := make(map[string]struct{}, len(libraries))
	result := make([]ScanLibrary, 0, len(libraries))
	for index, library := range libraries {
		name := strings.TrimSpace(library.Name)
		if name == "" {
			return nil, fmt.Errorf("scan library name is required")
		}
		roots, err := NormalizeScanFolders(library.Roots)
		if err != nil {
			return nil, err
		}
		if len(roots) == 0 {
			return nil, fmt.Errorf("scan library roots are required")
		}
		id := strings.TrimSpace(library.ID)
		if id == "" {
			id = fmt.Sprintf("lib-%d-%d", util.UnixNowNano(), index)
		}
		for {
			if _, ok := ids[id]; !ok {
				break
			}
			id = fmt.Sprintf("%s-%d", id, index+1)
		}
		ids[id] = struct{}{}
		result = append(result, ScanLibrary{
			ID:              id,
			Name:            name,
			Roots:           roots,
			DiscoveredFiles: library.DiscoveredFiles,
			DiscoveredAt:    library.DiscoveredAt,
		})
	}
	return result, nil
}

func ScanRoots(libraries []ScanLibrary) []string {
	roots := make([]string, 0)
	for _, library := range libraries {
		roots = append(roots, library.Roots...)
	}
	normalized, err := NormalizeScanFolders(roots)
	if err != nil {
		return roots
	}
	return normalized
}

func AssetInScanFolders(rel string, roots []string) bool {
	for _, root := range roots {
		if root == "" || rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func hasAncestor(rel string, roots []string) bool {
	for _, root := range roots {
		if root == "" || rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func sameStringSet(a []string, b []string) bool {
	left, err := NormalizeScanFolders(a)
	if err != nil {
		return false
	}
	right, err := NormalizeScanFolders(b)
	if err != nil {
		return false
	}
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
