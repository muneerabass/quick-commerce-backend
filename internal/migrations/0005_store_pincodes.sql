-- Serviceability is decided by pincode instead of lat/long distance.
-- A store serves every pincode listed here; a customer sees the union of
-- in-stock products of all active stores serving their pincode.
CREATE TABLE IF NOT EXISTS public.store_pincodes (
    store_id uuid NOT NULL REFERENCES public.stores(id) ON DELETE CASCADE,
    pincode varchar(10) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (store_id, pincode)
);

CREATE INDEX IF NOT EXISTS store_pincodes_pincode_idx ON public.store_pincodes (pincode);
