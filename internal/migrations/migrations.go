package migrations

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

//go:embed *.sql
var files embed.FS

func Run(db *gorm.DB) error {
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
