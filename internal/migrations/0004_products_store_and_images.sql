-- Make products strictly per-store and support multiple images.

-- 1. Each product now belongs to exactly one store.
ALTER TABLE public.products ADD COLUMN IF NOT EXISTS store_id uuid;

-- Backfill: adopt the store from the product's existing inventory row (if any),
-- otherwise fall back to the oldest store so no row is left orphaned.
UPDATE public.products p
SET store_id = COALESCE(
    (SELECT i.store_id FROM public.inventory i WHERE i.product_id = p.id ORDER BY i.updated_at ASC LIMIT 1),
    (SELECT s.id FROM public.stores s ORDER BY s.created_at ASC LIMIT 1)
)
WHERE p.store_id IS NULL;

-- Drop any products that still have no store (no stores exist to attach them to).
DELETE FROM public.products WHERE store_id IS NULL;

ALTER TABLE public.products
    ALTER COLUMN store_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'products_store_id_fkey'
    ) THEN
        ALTER TABLE public.products
            ADD CONSTRAINT products_store_id_fkey
            FOREIGN KEY (store_id) REFERENCES public.stores(id) ON DELETE CASCADE;
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS idx_products_store_id ON public.products (store_id);

-- 2. Multiple images per product. Stored as a JSON array of URLs in a text
-- column (GORM `serializer:json`); image_url stays as the cover (first) image.
ALTER TABLE public.products ADD COLUMN IF NOT EXISTS images text NOT NULL DEFAULT '[]';

-- Seed the images array from the existing single image_url where present.
UPDATE public.products
SET images = json_build_array(image_url)::text
WHERE image_url IS NOT NULL AND image_url <> '' AND (images IS NULL OR images = '[]');
