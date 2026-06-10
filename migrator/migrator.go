package migrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultDirectory = "migrations"
	upMarker         = "----------UP----------"
	downMarker       = "----------DOWN----------"
	lockKey          = int64(880081317)
)

var migrationFilePattern = regexp.MustCompile(`^(\d{14})_([a-z0-9_]+)\.sql$`)

type Migrator struct {
	pool   *pgxpool.Pool
	dir    string
	schema string
}

// Option, Migrator yapilandirma secenegi
type Option func(*Migrator)

// WithSchema, migration tablosunun olusturulacagi ve sorgulanacagi sema adi belirtir.
// Ensure() cagrildigi zaman sema mevcut degilse otomatik olusturulur.
func WithSchema(schema string) Option {
	return func(m *Migrator) {
		m.schema = schema
	}
}

type Migration struct {
	Version string
	Name    string
	Path    string
	UpSQL   string
	DownSQL string
}

type StatusItem struct {
	Version   string     `json:"version"`
	Name      string     `json:"name"`
	Applied   bool       `json:"applied"`
	Missing   bool       `json:"missing"`
	Dirty     bool       `json:"dirty"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

type Run struct {
	Version string `json:"version"`
	Name    string `json:"name"`
}

type ValidationIssue struct {
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Message string `json:"message"`
}

type appliedRecord struct {
	appliedAt time.Time
	dirty     bool
}

func New(pool *pgxpool.Pool, dir string, opts ...Option) *Migrator {
	m := &Migrator{pool: pool, dir: dir}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Migrator) Ensure(ctx context.Context) error {
	if m.pool == nil {
		return errors.New("postgres pool gerekli")
	}
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return fmt.Errorf("migrations dizini olusturulamadi: %w", err)
	}
	if m.schema != "" {
		if _, err := m.pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{m.schema}.Sanitize()); err != nil {
			return fmt.Errorf("schema olusturulamadi: %w", err)
		}
	}
	_, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			name text NOT NULL,
			batch integer NOT NULL DEFAULT 1,
			dirty boolean NOT NULL DEFAULT false,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return err
	}
	_, err = m.pool.Exec(ctx, "ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS dirty boolean NOT NULL DEFAULT false")
	return err
}

func (m *Migrator) Create(name string) (string, error) {
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return "", fmt.Errorf("migrations dizini olusturulamadi: %w", err)
	}

	slug := slugify(name)
	if slug == "" {
		return "", fmt.Errorf("migration adi bos olamaz")
	}

	version := time.Now().Format("20060102150405")
	path := filepath.Join(m.dir, version+"_"+slug+".sql")
	body := upMarker + "\n\n" + downMarker + "\n"
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", fmt.Errorf("migration dosyasi yazilamadi: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(body); err != nil {
		return "", fmt.Errorf("migration dosyasi yazilamadi: %w", err)
	}
	return path, nil
}

func (m *Migrator) Delete(ctx context.Context, query string) (string, error) {
	migrations, err := m.loadFiles()
	if err != nil {
		return "", err
	}
	match, err := findMigration(migrations, query)
	if err != nil {
		return "", err
	}
	applied, err := m.appliedMap(ctx)
	if err != nil {
		return "", err
	}
	if applied[match.Version] != nil {
		return "", fmt.Errorf("%s uygulanmis; once down/reset ile geri alin", match.Version)
	}
	if err := os.Remove(match.Path); err != nil {
		return "", fmt.Errorf("migration silinemedi: %w", err)
	}
	return match.Path, nil
}

func (m *Migrator) Up(ctx context.Context, steps int) ([]Run, error) {
	if steps < 0 {
		return nil, fmt.Errorf("steps negatif olamaz")
	}
	var runs []Run
	err := m.withLock(ctx, func(tx pgx.Tx) error {
		files, err := m.loadFiles()
		if err != nil {
			return err
		}
		applied, err := appliedMapTx(ctx, tx)
		if err != nil {
			return err
		}
		batch, err := nextBatch(ctx, tx)
		if err != nil {
			return err
		}

		for _, migration := range files {
			if applied[migration.Version] != nil {
				continue
			}
			if steps > 0 && len(runs) >= steps {
				break
			}
			if strings.TrimSpace(migration.UpSQL) != "" {
				if _, err := tx.Exec(ctx, migration.UpSQL); err != nil {
					return fmt.Errorf("%s up calismadi: %w", migration.Version, err)
				}
			}
			if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name, batch, dirty) VALUES ($1, $2, $3, false)", migration.Version, migration.Name, batch); err != nil {
				return err
			}
			runs = append(runs, Run{Version: migration.Version, Name: migration.Name})
		}
		return nil
	})
	return runs, err
}

func (m *Migrator) Pending(ctx context.Context) ([]StatusItem, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	var pending []StatusItem
	for _, item := range status {
		if !item.Applied && !item.Missing {
			pending = append(pending, item)
		}
	}
	return pending, nil
}

func (m *Migrator) Applied(ctx context.Context) ([]StatusItem, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	var applied []StatusItem
	for _, item := range status {
		if item.Applied {
			applied = append(applied, item)
		}
	}
	return applied, nil
}

func (m *Migrator) Down(ctx context.Context, steps int) ([]Run, error) {
	if steps <= 0 {
		return nil, fmt.Errorf("steps 1 veya daha buyuk olmali")
	}
	var runs []Run
	err := m.withLock(ctx, func(tx pgx.Tx) error {
		files, err := m.loadFiles()
		if err != nil {
			return err
		}
		byVersion := make(map[string]Migration, len(files))
		for _, migration := range files {
			byVersion[migration.Version] = migration
		}

		rows, err := tx.Query(ctx, "SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT $1", steps)
		if err != nil {
			return err
		}

		var applied []Run
		for rows.Next() {
			var version, name string
			if err := rows.Scan(&version, &name); err != nil {
				rows.Close()
				return err
			}
			applied = append(applied, Run{Version: version, Name: name})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		for _, item := range applied {
			version := item.Version
			migration, ok := byVersion[version]
			if !ok {
				return fmt.Errorf("%s icin migration dosyasi bulunamadi", version)
			}
			if strings.TrimSpace(migration.DownSQL) != "" {
				if _, err := tx.Exec(ctx, migration.DownSQL); err != nil {
					return fmt.Errorf("%s down calismadi: %w", version, err)
				}
			}
			if _, err := tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", version); err != nil {
				return err
			}
			runs = append(runs, item)
		}
		return nil
	})
	return runs, err
}

func (m *Migrator) Rollback(ctx context.Context) ([]Run, error) {
	var batch int
	err := m.pool.QueryRow(ctx, "SELECT COALESCE(MAX(batch), 0) FROM schema_migrations").Scan(&batch)
	if err != nil {
		return nil, err
	}
	if batch == 0 {
		return nil, nil
	}

	var count int
	err = m.pool.QueryRow(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE batch = $1", batch).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	return m.Down(ctx, count)
}

func (m *Migrator) Redo(ctx context.Context) ([]Run, []Run, error) {
	rolledBack, err := m.Down(ctx, 1)
	if err != nil || len(rolledBack) == 0 {
		return rolledBack, nil, err
	}
	applied, err := m.Up(ctx, 1)
	return rolledBack, applied, err
}

func (m *Migrator) Force(ctx context.Context, version string) ([]Run, error) {
	files, err := m.loadFiles()
	if err != nil {
		return nil, err
	}

	index := -1
	for i, migration := range files {
		if migration.Version == version {
			index = i
			break
		}
	}
	if version == "0" || version == "none" {
		index = -1
	} else if index == -1 {
		return nil, fmt.Errorf("%s migration bulunamadi", version)
	}

	var runs []Run
	err = m.withLock(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM schema_migrations"); err != nil {
			return err
		}
		if index == -1 {
			return nil
		}
		batch, err := nextBatch(ctx, tx)
		if err != nil {
			return err
		}
		for _, migration := range files[:index+1] {
			if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name, batch, dirty) VALUES ($1, $2, $3, false)", migration.Version, migration.Name, batch); err != nil {
				return err
			}
			runs = append(runs, Run{Version: migration.Version, Name: migration.Name})
		}
		return nil
	})
	return runs, err
}

func (m *Migrator) Reset(ctx context.Context) ([]Run, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, item := range status {
		if item.Applied {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return m.Down(ctx, count)
}

func (m *Migrator) To(ctx context.Context, version string) ([]Run, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	targetIndex := -1
	appliedCount := 0
	for i, item := range status {
		if item.Version == version {
			targetIndex = i
		}
		if item.Applied {
			appliedCount++
		}
	}
	if targetIndex == -1 {
		return nil, fmt.Errorf("%s migration bulunamadi", version)
	}
	targetAppliedCount := targetIndex + 1
	if appliedCount == targetAppliedCount {
		return nil, nil
	}
	if appliedCount < targetAppliedCount {
		return m.Up(ctx, targetAppliedCount-appliedCount)
	}
	return m.Down(ctx, appliedCount-targetAppliedCount)
}

func (m *Migrator) Status(ctx context.Context) ([]StatusItem, error) {
	files, err := m.loadFiles()
	if err != nil {
		return nil, err
	}
	applied, err := m.appliedMap(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]StatusItem, 0, len(files))
	for _, migration := range files {
		item := StatusItem{
			Version: migration.Version,
			Name:    migration.Name,
		}
		if record := applied[migration.Version]; record != nil {
			item.Applied = true
			item.Dirty = record.dirty
			item.AppliedAt = &record.appliedAt
		}
		items = append(items, item)
	}
	known := make(map[string]struct{}, len(files))
	for _, migration := range files {
		known[migration.Version] = struct{}{}
	}
	for version, record := range applied {
		if _, ok := known[version]; ok {
			continue
		}
		items = append(items, StatusItem{
			Version:   version,
			Name:      "missing_file",
			Applied:   true,
			Missing:   true,
			Dirty:     record.dirty,
			AppliedAt: &record.appliedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Version < items[j].Version
	})
	return items, nil
}

func (m *Migrator) Validate(ctx context.Context) ([]ValidationIssue, error) {
	issues, err := m.validateFiles()
	if err != nil {
		return nil, err
	}
	if m.pool == nil {
		return issues, nil
	}
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range status {
		if item.Missing {
			issues = append(issues, ValidationIssue{
				Version: item.Version,
				Message: "applied migration dosyasi bulunamadi",
			})
		}
	}
	return issues, nil
}

func (m *Migrator) Current(ctx context.Context) (string, error) {
	var version string
	err := m.pool.QueryRow(ctx, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return version, err
}

func (m *Migrator) withLock(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (m *Migrator) appliedMap(ctx context.Context) (map[string]*appliedRecord, error) {
	rows, err := m.pool.Query(ctx, "SELECT version, applied_at, dirty FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]*appliedRecord)
	for rows.Next() {
		var version string
		var appliedAt time.Time
		var dirty bool
		if err := rows.Scan(&version, &appliedAt, &dirty); err != nil {
			return nil, err
		}
		applied[version] = &appliedRecord{appliedAt: appliedAt, dirty: dirty}
	}
	return applied, rows.Err()
}

func appliedMapTx(ctx context.Context, tx pgx.Tx) (map[string]*appliedRecord, error) {
	rows, err := tx.Query(ctx, "SELECT version, applied_at, dirty FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]*appliedRecord)
	for rows.Next() {
		var version string
		var appliedAt time.Time
		var dirty bool
		if err := rows.Scan(&version, &appliedAt, &dirty); err != nil {
			return nil, err
		}
		applied[version] = &appliedRecord{appliedAt: appliedAt, dirty: dirty}
	}
	return applied, rows.Err()
}

func nextBatch(ctx context.Context, tx pgx.Tx) (int, error) {
	var batch int
	err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(batch), 0) + 1 FROM schema_migrations").Scan(&batch)
	return batch, err
}

func (m *Migrator) loadFiles() ([]Migration, error) {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("migrations dizini okunamadi: %w", err)
	}

	var migrations []Migration
	seenVersions := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		if previous := seenVersions[matches[1]]; previous != "" {
			return nil, fmt.Errorf("%s ve %s ayni versiyonu kullaniyor", previous, entry.Name())
		}
		seenVersions[matches[1]] = entry.Name()
		path := filepath.Join(m.dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s okunamadi: %w", path, err)
		}
		upSQL, downSQL, err := splitMigration(string(content))
		if err != nil {
			return nil, fmt.Errorf("%s gecersiz: %w", path, err)
		}
		migrations = append(migrations, Migration{
			Version: matches[1],
			Name:    matches[2],
			Path:    path,
			UpSQL:   upSQL,
			DownSQL: downSQL,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func (m *Migrator) validateFiles() ([]ValidationIssue, error) {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("migrations dizini okunamadi: %w", err)
	}

	var issues []ValidationIssue
	seenVersions := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := filepath.Join(m.dir, entry.Name())
		matches := migrationFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			issues = append(issues, ValidationIssue{Path: path, Message: "dosya adi YYYYMMDDHHMMSS_name.sql formatinda olmali"})
			continue
		}
		if previous := seenVersions[matches[1]]; previous != "" {
			issues = append(issues, ValidationIssue{Path: path, Version: matches[1], Message: "ayni versiyonda birden fazla migration var: " + previous})
		}
		seenVersions[matches[1]] = entry.Name()

		content, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, ValidationIssue{Path: path, Version: matches[1], Message: err.Error()})
			continue
		}
		upSQL, downSQL, err := splitMigration(string(content))
		if err != nil {
			issues = append(issues, ValidationIssue{Path: path, Version: matches[1], Message: err.Error()})
			continue
		}
		if strings.TrimSpace(upSQL) == "" {
			issues = append(issues, ValidationIssue{Path: path, Version: matches[1], Message: "UP bolumu bos"})
		}
		if strings.TrimSpace(downSQL) == "" {
			issues = append(issues, ValidationIssue{Path: path, Version: matches[1], Message: "DOWN bolumu bos"})
		}
	}
	return issues, nil
}

func splitMigration(content string) (string, string, error) {
	upIndex := strings.Index(content, upMarker)
	downIndex := strings.Index(content, downMarker)
	if upIndex == -1 || downIndex == -1 || downIndex < upIndex {
		return "", "", fmt.Errorf("%s ve %s bolumleri gerekli", upMarker, downMarker)
	}
	upStart := upIndex + len(upMarker)
	return strings.TrimSpace(content[upStart:downIndex]), strings.TrimSpace(content[downIndex+len(downMarker):]), nil
}

func findMigration(migrations []Migration, query string) (Migration, error) {
	for _, migration := range migrations {
		if migration.Version == query || migration.Name == query || strings.HasPrefix(migration.Version+"_"+migration.Name, query) {
			return migration, nil
		}
	}
	return Migration{}, fmt.Errorf("%s migration bulunamadi", query)
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteRune('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}
