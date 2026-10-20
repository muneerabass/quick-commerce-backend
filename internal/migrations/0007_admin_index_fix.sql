-- H-003: The partial unique index that enforces one admin per database did not
-- exclude soft-deleted (status = 'deleted') rows. A deleted admin's row kept
-- role = 'admin', which blocked the index from allowing a new admin to be
-- created. Recreate the index to exclude deleted users.
--
-- This migration is non-destructive: no existing row is modified.

DROP INDEX IF EXISTS public.users_one_admin_idx;

CREATE UNIQUE INDEX IF NOT EXISTS users_one_admin_idx
    ON public.users (role)
    WHERE role = 'admin' AND status <> 'deleted';
