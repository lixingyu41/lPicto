package db

import "context"

func (d *DB) DatabaseSize(ctx context.Context) (int64, error) {
	var sizeBytes int64
	err := d.conn.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&sizeBytes)
	return sizeBytes, err
}
