package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// BuildRecord represents a persisted Packer build and AMI in SQLite.
type BuildRecord struct {
	BuildID         string    `json:"build_id"`
	AMIID           string    `json:"ami_id"`
	AMIName         string    `json:"ami_name"`
	Region          string    `json:"region"`
	Status          string    `json:"status"` // running, success, failed, cancelled, deleted
	HCLCode         string    `json:"hcl_code"`
	ConfigJSON      string    `json:"config_json"`
	BuildLogs       string    `json:"build_logs"`
	ErrorMessage    string    `json:"error_message"`
	DurationSeconds int       `json:"duration_seconds"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// DB handles persistence for builds and AMIs using modernc.org/sqlite.
type DB struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewDB initializes a SQLite database at the specified path or default (~/.vib/vib.db).
// Use ":memory:" for tests.
func NewDB(dbPath ...string) (*DB, error) {
	path := ""
	if len(dbPath) > 0 && strings.TrimSpace(dbPath[0]) != "" {
		path = strings.TrimSpace(dbPath[0])
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		dir := filepath.Join(home, ".vib")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create directory %q: %w", dir, err)
		}
		path = filepath.Join(dir, "vib.db")
	}

	if path != ":memory:" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create directory %q: %w", dir, err)
		}
	}

	dsn := path
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", path, err)
	}

	// Configure single open connection to eliminate SQLITE_BUSY lock collisions
	db.SetMaxOpenConns(1)

	// Execute WAL mode and concurrency hardening PRAGMAs
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute %s: %w", pragma, err)
		}
	}

	store := &DB{
		db:   db,
		path: path,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return store, nil
}

// migrate creates required tables and indexes.
func (s *DB) migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	CREATE TABLE IF NOT EXISTS builds (
		build_id TEXT PRIMARY KEY,
		ami_id TEXT,
		ami_name TEXT NOT NULL,
		region TEXT NOT NULL,
		status TEXT NOT NULL,
		hcl_code TEXT NOT NULL,
		config_json TEXT NOT NULL,
		build_logs TEXT,
		error_message TEXT,
		duration_seconds INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_builds_created_at ON builds(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_builds_ami_id ON builds(ami_id);
	`
	if _, err := s.db.Exec(query); err != nil {
		return err
	}

	// Reconcile any in-flight builds left from a previous server session as failed
	_, _ = s.db.Exec(`UPDATE builds SET status = 'failed', error_message = 'Build interrupted by server shutdown' WHERE status IN ('running', 'pending')`)
	return nil
}

// Close closes the database connection.
func (s *DB) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Path returns the SQLite file path or :memory:.
func (s *DB) Path() string {
	return s.path
}

// CreateBuild inserts a new build record into SQLite.
func (s *DB) CreateBuild(b *BuildRecord) error {
	if b == nil {
		return errors.New("build record cannot be nil")
	}
	if strings.TrimSpace(b.BuildID) == "" {
		return errors.New("build_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	if b.UpdatedAt.IsZero() {
		b.UpdatedAt = now
	}

	query := `
	INSERT INTO builds (
		build_id, ami_id, ami_name, region, status, hcl_code,
		config_json, build_logs, error_message, duration_seconds,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(build_id) DO UPDATE SET
		ami_id = excluded.ami_id,
		ami_name = excluded.ami_name,
		region = excluded.region,
		status = excluded.status,
		hcl_code = excluded.hcl_code,
		config_json = excluded.config_json,
		build_logs = excluded.build_logs,
		error_message = excluded.error_message,
		duration_seconds = excluded.duration_seconds,
		updated_at = excluded.updated_at;
	`

	_, err := s.db.Exec(query,
		b.BuildID,
		b.AMIID,
		b.AMIName,
		b.Region,
		b.Status,
		b.HCLCode,
		b.ConfigJSON,
		b.BuildLogs,
		b.ErrorMessage,
		b.DurationSeconds,
		b.CreatedAt.Format(time.RFC3339),
		b.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// UpdateBuildStatus updates status, ami_id, logs, error message, duration, and updated_at.
func (s *DB) UpdateBuildStatus(buildID, status, amiID, logs, errMsg string, duration int) error {
	if strings.TrimSpace(buildID) == "" {
		return errors.New("build_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	query := `
	UPDATE builds SET
		status = ?,
		ami_id = CASE WHEN ? != '' THEN ? ELSE ami_id END,
		build_logs = CASE WHEN ? != '' THEN ? ELSE build_logs END,
		error_message = ?,
		duration_seconds = ?,
		updated_at = ?
	WHERE build_id = ?;
	`

	res, err := s.db.Exec(query, status, amiID, amiID, logs, logs, errMsg, duration, now, buildID)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("build with ID %q not found", buildID)
	}

	return nil
}

// GetBuild retrieves a build record by build_id or ami_id.
func (s *DB) GetBuild(id string) (*BuildRecord, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("id is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT
		build_id, ami_id, ami_name, region, status, hcl_code,
		config_json, build_logs, error_message, duration_seconds,
		created_at, updated_at
	FROM builds
	WHERE build_id = ? OR ami_id = ?
	LIMIT 1;
	`

	row := s.db.QueryRow(query, id, id)

	var b BuildRecord
	var amiID, buildLogs, errMsg sql.NullString
	var createdAtStr, updatedAtStr string

	err := row.Scan(
		&b.BuildID,
		&amiID,
		&b.AMIName,
		&b.Region,
		&b.Status,
		&b.HCLCode,
		&b.ConfigJSON,
		&buildLogs,
		&errMsg,
		&b.DurationSeconds,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("build %q not found: %w", id, err)
		}
		return nil, err
	}

	if amiID.Valid {
		b.AMIID = amiID.String
	}
	if buildLogs.Valid {
		b.BuildLogs = buildLogs.String
	}
	if errMsg.Valid {
		b.ErrorMessage = errMsg.String
	}

	if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
		b.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
		b.UpdatedAt = t
	}

	return &b, nil
}

// ListBuilds returns all build records sorted by created_at descending.
func (s *DB) ListBuilds() ([]*BuildRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT
		build_id, ami_id, ami_name, region, status, hcl_code,
		config_json, build_logs, error_message, duration_seconds,
		created_at, updated_at
	FROM builds
	ORDER BY created_at DESC;
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*BuildRecord
	for rows.Next() {
		var b BuildRecord
		var amiID, buildLogs, errMsg sql.NullString
		var createdAtStr, updatedAtStr string

		if err := rows.Scan(
			&b.BuildID,
			&amiID,
			&b.AMIName,
			&b.Region,
			&b.Status,
			&b.HCLCode,
			&b.ConfigJSON,
			&buildLogs,
			&errMsg,
			&b.DurationSeconds,
			&createdAtStr,
			&updatedAtStr,
		); err != nil {
			return nil, err
		}

		if amiID.Valid {
			b.AMIID = amiID.String
		}
		if buildLogs.Valid {
			b.BuildLogs = buildLogs.String
		}
		if errMsg.Valid {
			b.ErrorMessage = errMsg.String
		}

		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			b.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			b.UpdatedAt = t
		}

		records = append(records, &b)
	}

	return records, rows.Err()
}

// DeleteBuild deletes a build record by build_id or ami_id.
func (s *DB) DeleteBuild(buildID string) error {
	if strings.TrimSpace(buildID) == "" {
		return errors.New("build_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	query := `DELETE FROM builds WHERE build_id = ? OR ami_id = ?;`
	res, err := s.db.Exec(query, buildID, buildID)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("build with ID %q not found", buildID)
	}
	return nil
}

// MarkBuildDeleted marks a build record's status as "deleted".
func (s *DB) MarkBuildDeleted(buildID string) error {
	if strings.TrimSpace(buildID) == "" {
		return errors.New("build_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	query := `UPDATE builds SET status = 'deleted', updated_at = ? WHERE build_id = ? OR ami_id = ?;`
	res, err := s.db.Exec(query, now, buildID, buildID)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("build with ID %q not found", buildID)
	}
	return nil
}
