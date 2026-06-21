DELETE FROM scan_library
WHERE public_id = 'default'
  AND name = '默认来源'
  AND NOT EXISTS (
    SELECT 1
    FROM scan_library_root
    WHERE scan_library_root.scan_library_id = scan_library.id
      AND scan_library_root.rel_path <> ''
  );
