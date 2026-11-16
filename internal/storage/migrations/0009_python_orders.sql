-- Preserve the Python state for audit; fulfillment uses the Go state machine.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS legacy_status text;
UPDATE orders SET legacy_status=COALESCE(legacy_status,status), status='provisioned'
 WHERE status IN ('paid','manual_granted','trial_granted') AND provisioned_at IS NOT NULL;
UPDATE orders SET legacy_status=COALESCE(legacy_status,status), status='failed'
 WHERE status='declined';
