package db

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type DB struct {
	conn         *sqlConn
	raw          *sql.DB
	testAdminURL string
	testDatabase string
}

const migrationLockKey int64 = 0x4c506963746f

func Open(ctx context.Context, databaseURL string, migrationsDir string) (*DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	testAdminURL := ""
	testDatabase := ""
	if !looksLikePostgresURL(databaseURL) {
		resolved, adminURL, dbName, err := createTestDatabase(ctx, databaseURL)
		if err != nil {
			return nil, err
		}
		databaseURL = resolved
		testAdminURL = adminURL
		testDatabase = dbName
	}
	raw, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(32)
	raw.SetMaxIdleConns(16)
	if err := raw.PingContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	database := &DB{conn: &sqlConn{db: raw}, raw: raw, testAdminURL: testAdminURL, testDatabase: testDatabase}
	dir, err := findMigrationsDir(migrationsDir)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := database.Migrate(ctx, dir); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := database.MarkInterruptedScanRuns(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return database, nil
}

func (d *DB) Close() error {
	if d == nil || d.raw == nil {
		return nil
	}
	err := d.raw.Close()
	if d.testAdminURL != "" && d.testDatabase != "" {
		if dropErr := dropTestDatabase(context.Background(), d.testAdminURL, d.testDatabase); err == nil {
			err = dropErr
		}
	}
	return err
}

func (d *DB) Conn() *sql.DB {
	return d.raw
}

func (d *DB) Checkpoint(ctx context.Context) error {
	_ = ctx
	return nil
}

func (d *DB) Migrate(ctx context.Context, migrationsDir string) (err error) {
	conn, err := d.raw.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	locked := false
	defer func() {
		if !locked {
			return
		}
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, unlockErr := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockKey)
		if err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return err
	}
	locked = true

	if _, err := conn.ExecContext(ctx, rebindPostgres(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now());`)); err != nil {
		return err
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		applied, err := migrationAppliedOnConn(ctx, conn, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, rebindPostgres(string(sqlBytes))); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, rebindPostgres(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, now())`), name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) migrationApplied(ctx context.Context, version string) (bool, error) {
	return migrationAppliedOnConn(ctx, d.raw, version)
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func migrationAppliedOnConn(ctx context.Context, conn queryRower, version string) (bool, error) {
	var count int
	err := conn.QueryRowContext(ctx, rebindPostgres(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`), version).Scan(&count)
	return count > 0, err
}

func findMigrationsDir(preferred string) (string, error) {
	candidates := []string{preferred, "migrations", filepath.Join("backend", "migrations"), filepath.Join("LPicto", "backend", "migrations")}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("migrations directory not found")
}

func looksLikePostgresURL(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.HasPrefix(value, "postgres://") || strings.HasPrefix(value, "postgresql://")
}

func createTestDatabase(ctx context.Context, seed string) (string, string, string, error) {
	base := strings.TrimSpace(os.Getenv("LPIC_TEST_DATABASE_URL"))
	if base == "" {
		return "", "", "", fmt.Errorf("cannot parse `%s`: failed to parse as keyword/value (invalid keyword/value)", seed)
	}
	adminURL, err := databaseURLWithName(base, "postgres")
	if err != nil {
		return "", "", "", err
	}
	hash := sha1.Sum([]byte(seed + time.Now().Format(time.RFC3339Nano)))
	name := "lpicto_test_" + hex.EncodeToString(hash[:])[:20]
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		return "", "", "", err
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		return "", "", "", err
	}
	testURL, err := databaseURLWithName(base, name)
	if err != nil {
		_ = dropTestDatabase(ctx, adminURL, name)
		return "", "", "", err
	}
	return testURL, adminURL, name, nil
}

func dropTestDatabase(ctx context.Context, adminURL string, name string) error {
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		return err
	}
	defer admin.Close()
	_, _ = admin.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
	_, err = admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name)
	return err
}

func databaseURLWithName(rawURL string, name string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

type sqlConn struct {
	db *sql.DB
}

func (c *sqlConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, rebindPostgres(query), args...)
}

func (c *sqlConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, rebindPostgres(query), args...)
}

func (c *sqlConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.db.QueryRowContext(ctx, rebindPostgres(query), args...)
}

func (c *sqlConn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sqlTx, error) {
	tx, err := c.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &sqlTx{tx: tx}, nil
}

type sqlTx struct {
	tx *sql.Tx
}

func (t *sqlTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, rebindPostgres(query), args...)
}

func (t *sqlTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, rebindPostgres(query), args...)
}

func (t *sqlTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, rebindPostgres(query), args...)
}

func (t *sqlTx) Commit() error {
	return t.tx.Commit()
}

func (t *sqlTx) Rollback() error {
	return t.tx.Rollback()
}

func rebindPostgres(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	inString := false
	arg := 1
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			b.WriteByte(ch)
			if inString && i+1 < len(query) && query[i+1] == '\'' {
				i++
				b.WriteByte(query[i])
				continue
			}
			inString = !inString
			continue
		}
		if ch == '?' && !inString {
			b.WriteByte('$')
			b.WriteString(fmt.Sprint(arg))
			arg++
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func nullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func nullInt(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}

func nullFloat(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func int64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func intPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	v := int(value.Int64)
	return &v
}

func floatPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}
