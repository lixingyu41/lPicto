package db

import (
	"context"
	"time"

	"lpicto/backend/internal/model"
)

func (d *DB) TimelineGroups(ctx context.Context, unit string, page int, pageSize int) (model.Page[model.TimelineGroup], error) {
	groupExpr, labelLayout := timelineFormat(unit)
	limit := pageSize + 1
	offset := (page - 1) * pageSize
	query := `
WITH grouped AS (
  SELECT
    ` + groupExpr + ` AS group_key,
    COUNT(*) AS count,
    MIN(timeline_at) AS min_time,
    MAX(timeline_at) AS max_time
  FROM assets
  WHERE deleted_at IS NULL
  GROUP BY group_key
)
SELECT
  group_key,
  min_time,
  max_time,
  count,
  (
    SELECT id FROM assets
    WHERE deleted_at IS NULL AND ` + groupExpr + ` = grouped.group_key
    ORDER BY timeline_at DESC, id DESC
    LIMIT 1
  ) AS cover_asset_id
FROM grouped
ORDER BY min_time DESC
LIMIT ? OFFSET ?`
	rows, err := d.conn.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return model.Page[model.TimelineGroup]{}, err
	}
	defer rows.Close()
	var groups []model.TimelineGroup
	for rows.Next() {
		var group model.TimelineGroup
		var cover sqlNullInt64
		if err := rows.Scan(&group.Key, &group.Start, &group.End, &group.Count, &cover); err != nil {
			return model.Page[model.TimelineGroup]{}, err
		}
		group.Start = groupStartUnix(group.Key, unit)
		group.End = groupEndUnix(group.Start, unit)
		group.Label = groupLabel(group.Start, labelLayout)
		group.CoverAssetID = cover.ptr()
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return model.Page[model.TimelineGroup]{}, err
	}
	hasMore := len(groups) > pageSize
	if hasMore {
		groups = groups[:pageSize]
	}
	return model.Page[model.TimelineGroup]{Items: groups, Page: page, PageSize: pageSize, HasMore: hasMore}, nil
}

func timelineFormat(unit string) (groupExpr string, labelLayout string) {
	switch unit {
	case "year":
		return "to_char(to_timestamp(timeline_at), 'YYYY')", "2006"
	case "day":
		return "to_char(to_timestamp(timeline_at), 'YYYY-MM-DD')", "2006-01-02"
	default:
		return "to_char(to_timestamp(timeline_at), 'YYYY-MM')", "2006-01"
	}
}

func groupStartUnix(key string, unit string) int64 {
	layout := "2006-01"
	value := key
	switch unit {
	case "year":
		layout = "2006"
	case "day":
		layout = "2006-01-02"
	default:
		layout = "2006-01"
	}
	parsed, err := time.ParseInLocation(layout, value, time.Local)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}

func groupLabel(start int64, layout string) string {
	if start == 0 {
		return "未知时间"
	}
	return time.Unix(start, 0).Format(layout)
}

func groupEndUnix(start int64, unit string) int64 {
	if start == 0 {
		return 0
	}
	t := time.Unix(start, 0)
	switch unit {
	case "year":
		return t.AddDate(1, 0, 0).Unix() - 1
	case "day":
		return t.AddDate(0, 0, 1).Unix() - 1
	default:
		return t.AddDate(0, 1, 0).Unix() - 1
	}
}

type sqlNullInt64 struct {
	Int64 int64
	Valid bool
}

func (n *sqlNullInt64) Scan(value any) error {
	if value == nil {
		n.Valid = false
		return nil
	}
	switch typed := value.(type) {
	case int64:
		n.Int64 = typed
	case int:
		n.Int64 = int64(typed)
	default:
		n.Valid = false
		return nil
	}
	n.Valid = true
	return nil
}

func (n sqlNullInt64) ptr() *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
