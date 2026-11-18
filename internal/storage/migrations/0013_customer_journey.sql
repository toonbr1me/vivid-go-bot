-- UI intents are separate from panel-confirmed first connections.
CREATE TABLE IF NOT EXISTS customer_journey (
  subject_id BIGINT NOT NULL REFERENCES accounts(id),
  step TEXT NOT NULL,
  channel TEXT NOT NULL,
  platform TEXT NOT NULL DEFAULT '',
  day DATE NOT NULL DEFAULT current_date,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(subject_id, step, channel, platform, day)
);
CREATE INDEX IF NOT EXISTS customer_journey_period_idx ON customer_journey(day, step);
