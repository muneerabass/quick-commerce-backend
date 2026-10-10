-- Catalog tables (created first so FKs below resolve on a fresh database).
CREATE TABLE IF NOT EXISTS public.categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.stores (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    address text NOT NULL,
    latitude double precision,
    longitude double precision,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180)
);

CREATE TABLE IF NOT EXISTS public.products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    description text,
    price integer NOT NULL CHECK (price >= 0),
    image_url text,
    category_id uuid REFERENCES public.categories(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.inventory (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id uuid NOT NULL REFERENCES public.stores(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES public.products(id) ON DELETE CASCADE,
    quantity integer NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (store_id, product_id)
);

CREATE INDEX IF NOT EXISTS inventory_store_idx ON public.inventory (store_id);
CREATE INDEX IF NOT EXISTS inventory_product_idx ON public.inventory (product_id);

ALTER TABLE public.users ADD COLUMN IF NOT EXISTS role text;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'user_type'
    ) THEN
        UPDATE public.users
        SET role = CASE lower(COALESCE(user_type, ''))
            WHEN 'admin' THEN 'admin'
            WHEN 'delivery' THEN 'delivery'
            ELSE 'customer'
        END
        WHERE role IS NULL OR role = '';
    ELSE
        UPDATE public.users SET role = 'customer' WHERE role IS NULL OR role = '';
    END IF;
END $$;

ALTER TABLE public.users ALTER COLUMN role SET DEFAULT 'customer';
ALTER TABLE public.users ALTER COLUMN role SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'users_role_check' AND conrelid = 'public.users'::regclass
    ) THEN
        ALTER TABLE public.users ADD CONSTRAINT users_role_check CHECK (role IN ('customer', 'admin', 'delivery'));
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS users_one_admin_idx ON public.users (role) WHERE role = 'admin';

CREATE TABLE IF NOT EXISTS public.carts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL UNIQUE REFERENCES public.users(id) ON DELETE RESTRICT,
    store_id uuid REFERENCES public.stores(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.cart_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_id uuid NOT NULL REFERENCES public.carts(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES public.products(id) ON DELETE RESTRICT,
    quantity integer NOT NULL CHECK (quantity > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (cart_id, product_id)
);

CREATE TABLE IF NOT EXISTS public.orders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    store_id uuid NOT NULL REFERENCES public.stores(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'completed')),
    payment_method text NOT NULL DEFAULT 'cod' CHECK (payment_method = 'cod'),
    payment_status text NOT NULL DEFAULT 'pending' CHECK (payment_status IN ('pending', 'paid')),
    total_amount integer NOT NULL CHECK (total_amount >= 0),
    recipient_name text NOT NULL,
    delivery_phone varchar(20) NOT NULL,
    address_line1 text NOT NULL,
    address_line2 text,
    landmark text,
    city text NOT NULL,
    state text NOT NULL,
    pincode varchar(10) NOT NULL,
    latitude double precision,
    longitude double precision,
    delivery_instructions text,
    accepted_by uuid REFERENCES public.users(id) ON DELETE RESTRICT,
    accepted_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180)
);

CREATE INDEX IF NOT EXISTS orders_user_created_idx ON public.orders (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS orders_store_status_idx ON public.orders (store_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS public.order_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL REFERENCES public.orders(id) ON DELETE RESTRICT,
    product_id uuid NOT NULL REFERENCES public.products(id) ON DELETE RESTRICT,
    product_name text NOT NULL,
    unit_price integer NOT NULL CHECK (unit_price >= 0),
    quantity integer NOT NULL CHECK (quantity > 0),
    line_total integer NOT NULL CHECK (line_total >= 0)
);

CREATE INDEX IF NOT EXISTS order_items_order_idx ON public.order_items (order_id);

CREATE TABLE IF NOT EXISTS public.delivery_store_assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    store_id uuid NOT NULL REFERENCES public.stores(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (delivery_id, store_id)
);

CREATE INDEX IF NOT EXISTS delivery_store_assignments_delivery_idx
    ON public.delivery_store_assignments (delivery_id, store_id);
