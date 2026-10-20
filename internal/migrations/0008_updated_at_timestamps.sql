-- L-015: Add updated_at to products, categories, and stores.
-- These columns were absent; GORM's AutoUpdate convention now maintains them.

ALTER TABLE public.products
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE public.categories
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE public.stores
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill existing rows so updated_at equals created_at.
UPDATE public.products  SET updated_at = created_at WHERE updated_at = '2000-01-01';
UPDATE public.categories SET updated_at = created_at WHERE updated_at = '2000-01-01';
UPDATE public.stores    SET updated_at = created_at WHERE updated_at = '2000-01-01';
