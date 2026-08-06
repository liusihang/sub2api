ALTER TABLE composite_model_routes
    ADD COLUMN IF NOT EXISTS billing_rate_multiplier DECIMAL(10,4);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'composite_model_routes_billing_rate_multiplier_nonnegative'
          AND conrelid = 'composite_model_routes'::regclass
    ) THEN
        ALTER TABLE composite_model_routes
            ADD CONSTRAINT composite_model_routes_billing_rate_multiplier_nonnegative
            CHECK (billing_rate_multiplier IS NULL OR billing_rate_multiplier >= 0);
    END IF;
END
$$;
