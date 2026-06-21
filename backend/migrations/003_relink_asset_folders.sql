WITH RECURSIVE active_parents(library_id, rel_path) AS (
  SELECT DISTINCT
    fi.library_id,
    CASE
      WHEN POSITION('/' IN REVERSE(fi.rel_path)) = 0 THEN ''
      ELSE LEFT(fi.rel_path, LENGTH(fi.rel_path) - POSITION('/' IN REVERSE(fi.rel_path)))
    END
  FROM file_instance fi
  JOIN media_asset ma ON ma.id = fi.asset_id
  WHERE fi.missing = false AND ma.deleted_at IS NULL
),
ancestors(library_id, rel_path) AS (
  SELECT library_id, rel_path FROM active_parents
  UNION
  SELECT
    library_id,
    CASE
      WHEN POSITION('/' IN REVERSE(rel_path)) = 0 THEN ''
      ELSE LEFT(rel_path, LENGTH(rel_path) - POSITION('/' IN REVERSE(rel_path)))
    END
  FROM ancestors
  WHERE rel_path <> ''
)
INSERT INTO folder (library_id, rel_path, name, parent_id, depth, updated_at)
SELECT
  library_id,
  rel_path,
  CASE WHEN rel_path = '' THEN '照片' ELSE regexp_replace(rel_path, '^.*/', '') END,
  NULL,
  CASE WHEN rel_path = '' THEN 0 ELSE LENGTH(rel_path) - LENGTH(REPLACE(rel_path, '/', '')) + 1 END,
  now()
FROM ancestors
ON CONFLICT (library_id, rel_path) DO UPDATE SET
  name = excluded.name,
  depth = excluded.depth,
  updated_at = excluded.updated_at;

UPDATE folder f
SET parent_id = p.id
FROM folder p
WHERE f.library_id = p.library_id
  AND f.rel_path <> ''
  AND p.rel_path = CASE
    WHEN POSITION('/' IN REVERSE(f.rel_path)) = 0 THEN ''
    ELSE LEFT(f.rel_path, LENGTH(f.rel_path) - POSITION('/' IN REVERSE(f.rel_path)))
  END
  AND f.parent_id IS DISTINCT FROM p.id;

UPDATE folder
SET parent_id = NULL
WHERE rel_path = ''
  AND parent_id IS NOT NULL;

WITH asset_folders AS (
  SELECT DISTINCT ON (fi.asset_id)
    fi.asset_id,
    f.id AS folder_id
  FROM file_instance fi
  JOIN media_asset ma ON ma.id = fi.asset_id
  JOIN folder f ON f.library_id = fi.library_id AND f.rel_path = CASE
    WHEN POSITION('/' IN REVERSE(fi.rel_path)) = 0 THEN ''
    ELSE LEFT(fi.rel_path, LENGTH(fi.rel_path) - POSITION('/' IN REVERSE(fi.rel_path)))
  END
  WHERE fi.missing = false AND ma.deleted_at IS NULL
  ORDER BY fi.asset_id, fi.library_id, fi.id
)
UPDATE media_asset ma
SET folder_id = asset_folders.folder_id,
    updated_at = now()
FROM asset_folders
WHERE ma.id = asset_folders.asset_id
  AND ma.folder_id IS DISTINCT FROM asset_folders.folder_id;

UPDATE folder
SET
  asset_count = (
    SELECT COUNT(*) FROM assets
    WHERE assets.deleted_at IS NULL AND assets.parent_rel_path = folder.rel_path
  ),
  recursive_asset_count = (
    SELECT COUNT(*) FROM assets
    WHERE assets.deleted_at IS NULL AND (
      folder.rel_path = '' OR
      assets.parent_rel_path = folder.rel_path OR
      assets.parent_rel_path LIKE replace(replace(replace(folder.rel_path, '\', '\\'), '%', '\%'), '_', '\_') || '/%' ESCAPE '\'
    )
  ),
  cover_asset_id = (
    SELECT id FROM assets
    WHERE assets.deleted_at IS NULL AND assets.thumb_status = 'ready' AND (
      folder.rel_path = '' OR
      assets.parent_rel_path = folder.rel_path OR
      assets.parent_rel_path LIKE replace(replace(replace(folder.rel_path, '\', '\\'), '%', '\%'), '_', '\_') || '/%' ESCAPE '\'
    )
    ORDER BY timeline_at DESC, id DESC
    LIMIT 1
  ),
  updated_at = now();

DELETE FROM folder
WHERE rel_path <> ''
  AND recursive_asset_count = 0;
