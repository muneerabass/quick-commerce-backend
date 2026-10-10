-- Add the image_url column to products on databases where 0002 ran before it was added.
ALTER TABLE public.products ADD COLUMN IF NOT EXISTS image_url text;
