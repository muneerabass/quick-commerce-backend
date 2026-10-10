CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
BEGIN
    IF to_regclass('public.users') IS NULL THEN
        CREATE TABLE public.users (
            id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
            phone varchar(20),
            name text,
            phone_verified_at timestamptz,
            status text NOT NULL DEFAULT 'active',
            created_at timestamptz NOT NULL DEFAULT now(),
            updated_at timestamptz NOT NULL DEFAULT now()
        );
    ELSE
        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = 'public' AND table_name = 'users'
              AND column_name = 'id' AND data_type <> 'uuid'
        ) THEN
            IF EXISTS (SELECT 1 FROM public.users WHERE id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$') THEN
                RAISE EXCEPTION 'users.id contains non-UUID values; migrate these values before applying user module migration';
            END IF;
            ALTER TABLE public.users ALTER COLUMN id TYPE uuid USING id::uuid;
        END IF;
        ALTER TABLE public.users ADD COLUMN IF NOT EXISTS phone varchar(20);
        ALTER TABLE public.users ADD COLUMN IF NOT EXISTS phone_verified_at timestamptz;
        ALTER TABLE public.users ADD COLUMN IF NOT EXISTS status text;
        ALTER TABLE public.users ADD COLUMN IF NOT EXISTS updated_at timestamptz;
        ALTER TABLE public.users ALTER COLUMN name DROP NOT NULL;
        ALTER TABLE public.users ALTER COLUMN email DROP NOT NULL;
        ALTER TABLE public.users ALTER COLUMN password DROP NOT NULL;
        ALTER TABLE public.users ALTER COLUMN code DROP NOT NULL;
        ALTER TABLE public.users ALTER COLUMN user_type DROP NOT NULL;
        UPDATE public.users SET status = 'active' WHERE status IS NULL;
        UPDATE public.users SET created_at = now() WHERE created_at IS NULL;
        UPDATE public.users SET updated_at = COALESCE(created_at, now()) WHERE updated_at IS NULL;
        ALTER TABLE public.users ALTER COLUMN status SET DEFAULT 'active';
        ALTER TABLE public.users ALTER COLUMN status SET NOT NULL;
        ALTER TABLE public.users ALTER COLUMN updated_at SET DEFAULT now();
        ALTER TABLE public.users ALTER COLUMN updated_at SET NOT NULL;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS users_phone_unique_idx ON public.users (phone) WHERE phone IS NOT NULL;

DO $$
BEGIN
    IF to_regclass('public.otp_verifications') IS NULL THEN
        CREATE TABLE public.otp_verifications (
            id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
            phone varchar(20) NOT NULL,
            otp_hash text NOT NULL,
            expires_at timestamptz NOT NULL,
            attempts integer NOT NULL DEFAULT 0,
            verified boolean NOT NULL DEFAULT false,
            verified_at timestamptz,
            consumed_at timestamptz,
            created_at timestamptz NOT NULL DEFAULT now()
        );
    ELSE
        ALTER TABLE public.otp_verifications ADD COLUMN IF NOT EXISTS verified_at timestamptz;
        ALTER TABLE public.otp_verifications ADD COLUMN IF NOT EXISTS consumed_at timestamptz;
        UPDATE public.otp_verifications SET verified_at = created_at, consumed_at = created_at WHERE verified = true AND consumed_at IS NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS otp_verifications_phone_created_idx ON public.otp_verifications (phone, created_at DESC);

CREATE TABLE IF NOT EXISTS public.user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    refresh_token_hash char(64) NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS user_sessions_user_id_idx ON public.user_sessions (user_id);
CREATE INDEX IF NOT EXISTS user_sessions_active_idx ON public.user_sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS public.user_addresses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    label text NOT NULL CHECK (label IN ('Home', 'Work', 'Other')),
    recipient_name text NOT NULL,
    phone varchar(20) NOT NULL,
    address_line1 text NOT NULL,
    address_line2 text,
    landmark text,
    city text NOT NULL,
    state text NOT NULL,
    pincode varchar(10) NOT NULL,
    latitude double precision,
    longitude double precision,
    delivery_instructions text,
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180)
);

CREATE INDEX IF NOT EXISTS user_addresses_user_id_idx ON public.user_addresses (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS user_addresses_one_default_idx ON public.user_addresses (user_id) WHERE is_default;
