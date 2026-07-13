ALTER TABLE npc_applications ADD COLUMN IF NOT EXISTS fingerprint_hash text NOT NULL DEFAULT '';
ALTER TABLE npc_applications ADD COLUMN IF NOT EXISTS fingerprint_payload jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE npc_applications ADD COLUMN IF NOT EXISTS match_label text NOT NULL DEFAULT '';
ALTER TABLE npc_applications ADD COLUMN IF NOT EXISTS match_score numeric NOT NULL DEFAULT 0;

ALTER TABLE level_submissions ADD COLUMN IF NOT EXISTS group_id text NOT NULL DEFAULT '';
ALTER TABLE level_submissions ADD COLUMN IF NOT EXISTS fingerprint_hash text NOT NULL DEFAULT '';
ALTER TABLE level_submissions ADD COLUMN IF NOT EXISTS fingerprint_payload jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE level_submissions ADD COLUMN IF NOT EXISTS match_label text NOT NULL DEFAULT '';
ALTER TABLE level_submissions ADD COLUMN IF NOT EXISTS match_score numeric NOT NULL DEFAULT 0;

UPDATE level_submissions
SET group_id = CASE
  WHEN group_id <> '' THEN group_id
  WHEN name = '抓小孩' THEN 'night-watch'
  WHEN name = '胡说哥传奇' THEN 'station'
  ELSE group_id
END;
