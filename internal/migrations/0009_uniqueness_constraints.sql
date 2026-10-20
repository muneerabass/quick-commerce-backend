-- L-007: Add case-insensitive uniqueness indexes for category names, store
-- names, and product names within a store.
--
-- Business rules confirmed:
--   • Category names are globally unique (they are shared across stores).
--   • Store names are globally unique.
--   • Product names are unique within a store (same product can appear in
--     different stores).
--
-- Indexes use lower() for case-insensitive uniqueness, matching how the
-- service layer already normalises names on input. The database constraint
-- is the authoritative last line of defence; service-layer validation returns
-- a human-readable conflict error first.

CREATE UNIQUE INDEX IF NOT EXISTS categories_name_unique_idx
    ON public.categories (lower(name));

CREATE UNIQUE INDEX IF NOT EXISTS stores_name_unique_idx
    ON public.stores (lower(name));

-- Products: unique name per store (case-insensitive).
CREATE UNIQUE INDEX IF NOT EXISTS products_store_name_unique_idx
    ON public.products (store_id, lower(name));
