CREATE TABLE IF NOT EXISTS tg_blacklist (
  tg_user_id text PRIMARY KEY,
  reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fingerprint_blacklist (
  fingerprint_id text PRIMARY KEY,
  reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE npcs ADD COLUMN IF NOT EXISTS avatar_url text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS npcs_tg_username_lower_idx ON npcs (lower(tg_username)) WHERE tg_username <> '';
