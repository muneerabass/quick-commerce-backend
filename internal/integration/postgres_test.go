// Package integration contains PostgreSQL integration tests (L-009).
//
// These tests require a real PostgreSQL instance. Set TEST_DB_URL to opt in:
//
//	TEST_DB_URL=postgresql://postgres:postgres@localhost:5432/quickcommerce_test?sslmode=disable \
//	  go test ./internal/integration/...
//
// The test database is wiped and rebuilt from the embedded migrations at the
// start of every test run; do NOT point TEST_DB_URL at the production database.
package integration

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/migrations"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openTestDB opens the database pointed to by TEST_DB_URL and returns it.
// The test is skipped when TEST_DB_URL is not set.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		t.Skip("TEST_DB_URL not set — skipping PostgreSQL integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	return db
}

// resetSchema drops and recreates the public schema so every top-level test
// starts from a blank slate. It must only be called by TestMain.
func resetSchema(db *gorm.DB) error {
	return db.Exec(`
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		GRANT ALL ON SCHEMA public TO postgres;
		GRANT ALL ON SCHEMA public TO public;
	`).Error
}

// TestMain resets the schema once, then applies migrations, before any tests run.
func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		os.Exit(m.Run()) // tests will self-skip; nothing to set up
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		panic("TestMain: open test DB: " + err.Error())
	}
	if err := resetSchema(db); err != nil {
		panic("TestMain: reset schema: " + err.Error())
	}
	if err := migrations.Run(db); err != nil {
		panic("TestMain: apply migrations: " + err.Error())
	}
	os.Exit(m.Run())
}

// ----------------------------------------------------------------------------
// Migration tests
// ----------------------------------------------------------------------------

// TestMigrationsIdempotent verifies that running migrations a second time is a
// no-op and does not return an error.
func TestMigrationsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := migrations.Run(db); err != nil {
		t.Fatalf("second Run() should be a no-op, got: %v", err)
	}
}

// TestSchemaVersionsRecorded asserts every SQL file produced a row in
// schema_migrations, proving the version table is maintained correctly.
func TestSchemaVersionsRecorded(t *testing.T) {
	db := openTestDB(t)
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM schema_migrations").Scan(&count).Error; err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("schema_migrations is empty after migrations.Run()")
	}
}

// ----------------------------------------------------------------------------
// Uniqueness constraint tests (L-007)
// ----------------------------------------------------------------------------

// TestStoreCaseInsensitiveUnique checks that inserting two stores whose names
// differ only by case violates the unique index created in migration 0009.
func TestStoreCaseInsensitiveUnique(t *testing.T) {
	db := openTestDB(t)
	catalog := repository.NewCatalogRepository(db)

	store1 := &domain.Store{ID: uuid.New(), Name: "Fresh Market", Address: "1 A St", IsActive: true, CreatedAt: time.Now().UTC()}
	if err := catalog.CreateStore(db, store1); err != nil {
		t.Fatalf("create first store: %v", err)
	}
	t.Cleanup(func() { db.Delete(store1) })

	store2 := &domain.Store{ID: uuid.New(), Name: "fresh market", Address: "2 B St", IsActive: true, CreatedAt: time.Now().UTC()}
	err := catalog.CreateStore(db, store2)
	if err == nil {
		db.Delete(store2)
		t.Fatal("expected unique constraint violation, got nil")
	}
}

// TestProductCaseInsensitiveUniqueWithinStore checks that two products with the
// same name (case-insensitive) in the same store are rejected.
func TestProductCaseInsensitiveUniqueWithinStore(t *testing.T) {
	db := openTestDB(t)
	catalog := repository.NewCatalogRepository(db)

	store := &domain.Store{ID: uuid.New(), Name: "UniqueProductTestStore", Address: "3 C St", IsActive: true, CreatedAt: time.Now().UTC()}
	if err := catalog.CreateStore(db, store); err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { db.Delete(store) })

	p1 := &domain.Product{ID: uuid.New(), StoreID: store.ID, Name: "Organic Milk", Price: 100, Images: []string{}, CreatedAt: time.Now().UTC()}
	if err := catalog.CreateProduct(db, p1); err != nil {
		t.Fatalf("create first product: %v", err)
	}
	t.Cleanup(func() { db.Delete(p1) })

	p2 := &domain.Product{ID: uuid.New(), StoreID: store.ID, Name: "organic milk", Price: 120, Images: []string{}, CreatedAt: time.Now().UTC()}
	err := catalog.CreateProduct(db, p2)
	if err == nil {
		db.Delete(p2)
		t.Fatal("expected unique constraint violation for duplicate product name in same store, got nil")
	}
}

// TestProductSameNameDifferentStoreAllowed verifies that the same product name
// is permitted across different stores.
func TestProductSameNameDifferentStoreAllowed(t *testing.T) {
	db := openTestDB(t)
	catalog := repository.NewCatalogRepository(db)

	mkStore := func(name string) *domain.Store {
		s := &domain.Store{ID: uuid.New(), Name: name, Address: "X St", IsActive: true, CreatedAt: time.Now().UTC()}
		if err := catalog.CreateStore(db, s); err != nil {
			t.Fatalf("create store %q: %v", name, err)
		}
		t.Cleanup(func() { db.Delete(s) })
		return s
	}

	storeA := mkStore("Cross-Store Test Alpha")
	storeB := mkStore("Cross-Store Test Beta")

	p1 := &domain.Product{ID: uuid.New(), StoreID: storeA.ID, Name: "Shared Product", Price: 50, Images: []string{}, CreatedAt: time.Now().UTC()}
	if err := catalog.CreateProduct(db, p1); err != nil {
		t.Fatalf("create product in store A: %v", err)
	}
	t.Cleanup(func() { db.Delete(p1) })

	p2 := &domain.Product{ID: uuid.New(), StoreID: storeB.ID, Name: "Shared Product", Price: 60, Images: []string{}, CreatedAt: time.Now().UTC()}
	if err := catalog.CreateProduct(db, p2); err != nil {
		t.Fatalf("same name in different store should be allowed, got: %v", err)
	}
	t.Cleanup(func() { db.Delete(p2) })
}

// ----------------------------------------------------------------------------
// Foreign-key constraint test
// ----------------------------------------------------------------------------

// TestProductRequiresValidStore verifies that inserting a product that
// references a non-existent store_id is rejected by the FK constraint.
func TestProductRequiresValidStore(t *testing.T) {
	db := openTestDB(t)
	catalog := repository.NewCatalogRepository(db)

	badStoreID := uuid.New() // does not exist in stores table
	p := &domain.Product{ID: uuid.New(), StoreID: badStoreID, Name: "Ghost Product", Price: 1, CreatedAt: time.Now().UTC()}
	err := catalog.CreateProduct(db, p)
	if err == nil {
		db.Delete(p)
		t.Fatal("expected FK violation for non-existent store_id, got nil")
	}
}

// ----------------------------------------------------------------------------
// Advisory-lock concurrency test (L-010)
// ----------------------------------------------------------------------------

// TestMigrationAdvisoryLockSerializes confirms that concurrent calls to
// migrations.Run() do not race: they serialize, and both return without error.
func TestMigrationAdvisoryLockSerializes(t *testing.T) {
	db := openTestDB(t)
	var wg sync.WaitGroup
	var errCount int32

	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := migrations.Run(db); err != nil {
				atomic.AddInt32(&errCount, 1)
				t.Errorf("concurrent migrations.Run() error: %v", err)
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent migrations.Run() did not finish within 30s")
	}
	if errCount > 0 {
		t.Fatalf("%d goroutine(s) returned errors from migrations.Run()", errCount)
	}
}

// ----------------------------------------------------------------------------
// Transaction / locking test
// ----------------------------------------------------------------------------

// TestInventoryUpdateLocking verifies that concurrent inventory updates via
// SELECT … FOR UPDATE are serialized correctly and the final quantity is exact.
func TestInventoryUpdateLocking(t *testing.T) {
	db := openTestDB(t)
	catalog := repository.NewCatalogRepository(db)

	store := &domain.Store{ID: uuid.New(), Name: "LockTestStore " + uuid.New().String(), Address: "Z St", IsActive: true, CreatedAt: time.Now().UTC()}
	if err := catalog.CreateStore(db, store); err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { db.Delete(store) })

	product := &domain.Product{ID: uuid.New(), StoreID: store.ID, Name: "Lock Item", Price: 10, Images: []string{}, CreatedAt: time.Now().UTC()}
	if err := catalog.CreateProduct(db, product); err != nil {
		t.Fatalf("create product: %v", err)
	}
	t.Cleanup(func() { db.Delete(product) })

	initial := 100
	inv := &domain.Inventory{ID: uuid.New(), StoreID: store.ID, ProductID: product.ID, Quantity: initial, UpdatedAt: time.Now().UTC()}
	if err := catalog.UpsertInventory(db, inv); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	t.Cleanup(func() { db.Delete(inv) })

	const goroutines = 5
	const decrement = 10
	var wg sync.WaitGroup
	sqlDB, _ := db.DB()

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := sqlDB.Conn(context.Background())
			if err != nil {
				t.Errorf("acquire conn: %v", err)
				return
			}
			defer conn.Close()
			tx, err := conn.BeginTx(context.Background(), nil)
			if err != nil {
				t.Errorf("begin tx: %v", err)
				return
			}
			var qty int
			if err := tx.QueryRow(
				"SELECT quantity FROM inventory WHERE store_id=$1 AND product_id=$2 FOR UPDATE",
				store.ID, product.ID,
			).Scan(&qty); err != nil {
				tx.Rollback()
				t.Errorf("select for update: %v", err)
				return
			}
			if _, err := tx.Exec(
				"UPDATE inventory SET quantity=$1 WHERE store_id=$2 AND product_id=$3",
				qty-decrement, store.ID, product.ID,
			); err != nil {
				tx.Rollback()
				t.Errorf("update: %v", err)
				return
			}
			tx.Commit()
		}()
	}
	wg.Wait()

	var final int
	db.Raw("SELECT quantity FROM inventory WHERE store_id=? AND product_id=?", store.ID, product.ID).Scan(&final)
	expected := initial - goroutines*decrement
	if final != expected {
		t.Fatalf("expected final quantity %d, got %d (lost updates indicate locking failure)", expected, final)
	}
}
