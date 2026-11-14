ALTER TABLE recurring_agreements ADD COLUMN IF NOT EXISTS stop_reason text NOT NULL DEFAULT '';
ALTER TABLE recurring_agreements ADD COLUMN IF NOT EXISTS stopped_at timestamptz;
UPDATE recurring_agreements SET stop_reason='review', stopped_at=updated_at WHERE NOT enabled AND stop_reason='';
CREATE INDEX IF NOT EXISTS recurring_subject_history ON recurring_agreements(subject_id, consent_at DESC);
CREATE INDEX IF NOT EXISTS orders_operations_paid ON orders(created_at) WHERE status='paid';
CREATE INDEX IF NOT EXISTS orders_operations_pending ON orders(created_at) WHERE status='pending';

ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS owner_read_message_id bigint NOT NULL DEFAULT 0;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS last_staff_message_id bigint NOT NULL DEFAULT 0;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS context text NOT NULL DEFAULT '';
UPDATE support_tickets t SET last_staff_message_id=COALESCE((SELECT max(id) FROM support_messages m WHERE m.ticket_id=t.id AND m.is_from_admin AND NOT m.is_internal),0);
CREATE OR REPLACE FUNCTION support_track_reply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.is_from_admin AND NOT NEW.is_internal THEN
    UPDATE support_tickets SET last_staff_message_id=GREATEST(last_staff_message_id,NEW.id) WHERE id=NEW.ticket_id;
  END IF;
  RETURN NEW;
END;
$$;
CREATE OR REPLACE TRIGGER support_track_reply AFTER INSERT ON support_messages FOR EACH ROW EXECUTE FUNCTION support_track_reply();
