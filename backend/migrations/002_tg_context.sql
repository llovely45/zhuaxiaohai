ALTER TABLE players ADD COLUMN IF NOT EXISTS tg_context jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE npcs ADD COLUMN IF NOT EXISTS public_id bigserial;
ALTER TABLE npcs ADD COLUMN IF NOT EXISTS tg_username text NOT NULL DEFAULT '';

DELETE FROM npcs WHERE name = '泡泡糖';
UPDATE npcs SET public_id = -1 WHERE name = '群规机器人';
UPDATE npcs SET public_id = 9478, name = '小孩哥', tg_username = '@xiaohai' WHERE name IN ('小板凳','小孩哥');
UPDATE npcs SET public_id = 1, tg_username = '@thisisabot' WHERE name = '群规机器人';

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'npcs_name_key') THEN
    ALTER TABLE npcs ADD CONSTRAINT npcs_name_key UNIQUE(name);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'npcs_public_id_key') THEN
    ALTER TABLE npcs ADD CONSTRAINT npcs_public_id_key UNIQUE(public_id);
  END IF;
END $$;

SELECT setval(pg_get_serial_sequence('npcs','public_id'), GREATEST((SELECT MAX(public_id) FROM npcs),1), true);

CREATE TABLE IF NOT EXISTS miniapp_events (
  id bigserial PRIMARY KEY,
  player_id uuid NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  event text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS miniapp_events_player_created_idx ON miniapp_events(player_id, created_at DESC);
