-- Order lifecycle: cancellation, stored fees, delivery code, idempotency and an
-- audit trail of status changes.

ALTER TABLE public.orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE public.orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN ('pending', 'accepted', 'completed', 'cancelled'));

-- total_amount stays the grand total the rider collects. Older orders had no
-- fees, so their subtotal equals their total.
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS subtotal_amount integer NOT NULL DEFAULT 0;
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS delivery_fee integer NOT NULL DEFAULT 0;
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS handling_fee integer NOT NULL DEFAULT 0;
UPDATE public.orders SET subtotal_amount = total_amount WHERE subtotal_amount = 0 AND total_amount > 0;

ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS cancelled_at timestamptz;
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS cancelled_by uuid REFERENCES public.users(id) ON DELETE SET NULL;
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS cancelled_by_role text;
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS cancel_reason text;

-- The customer reads this code to the rider on delivery; empty on legacy orders.
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS delivery_code varchar(6) NOT NULL DEFAULT '';
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS delivery_code_attempts integer NOT NULL DEFAULT 0;

-- A client retry (timeout, double tap) with the same key returns the same order.
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS idempotency_key text;
CREATE UNIQUE INDEX IF NOT EXISTS orders_user_idempotency_idx
    ON public.orders (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS orders_status_created_idx ON public.orders (status, created_at);

CREATE TABLE IF NOT EXISTS public.order_status_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL REFERENCES public.orders(id) ON DELETE CASCADE,
    status text NOT NULL,
    actor_id uuid,
    actor_role text NOT NULL DEFAULT 'system',
    note text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS order_status_events_order_idx
    ON public.order_status_events (order_id, created_at);

-- Give existing orders a one-line history.
INSERT INTO public.order_status_events (order_id, status, actor_role, note, created_at)
SELECT o.id, 'pending', 'customer', 'Order placed', o.created_at
FROM public.orders o
WHERE NOT EXISTS (SELECT 1 FROM public.order_status_events e WHERE e.order_id = o.id);
