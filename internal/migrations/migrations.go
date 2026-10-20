package migrations

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

//go:embed *.sql
var files embed.FS

// migrationLockID is a stable PostgreSQL advisory-lock key for this
// application's migration runner (L-010). Derived from the ASCII bytes of
// "qcmigra" so it is human-traceable; kept non-zero and fits in int64.
const migrationLockID int64 = 0x71636d69677261 // "qcmigra"

// Run applies all pending SQL migrations in version order.
//
// Before scanning the migration table it acquires a PostgreSQL session-level
// advisory lock on a dedicated connection so that two application instances
// starting simultaneously cannot execute the same migration concurrently
// (L-010). The lock is released when the dedicated connection is returned to
// the pool (i.e. when conn.Close() is called in the deferred statement below).
func Run(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get underlying sql.DB: %w", err)
	}

	ctx := context.Background()

	// Acquire a dedicated connection so the session-level lock lifetime is
	// exactly the scope of this function.
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	// pg_advisory_lock blocks until the lock is available; no competing
	// migration runner can proceed past this point.
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	// Release explicitly before the connection is recycled.
	defer conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version bigint PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read migration files: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid migration filename %q: %w", entry.Name(), err)
		}
		var applied bool
		if err := db.Raw("SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&applied).Error; err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied {
			continue
		}
		content, err := files.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(string(content)).Error; err != nil {
				return fmt.Errorf("apply %s: %w", entry.Name(), err)
			}
			return tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version).Error
		}); err != nil {
			return err
		}
	}
	return nil
}
