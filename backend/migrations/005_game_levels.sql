CREATE TABLE IF NOT EXISTS game_levels (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id text NOT NULL,
  level_no integer NOT NULL,
  npc_ids integer[] NOT NULL DEFAULT '{}',
  npc_photos jsonb NOT NULL DEFAULT '{}'::jsonb,
  messages jsonb NOT NULL DEFAULT '[]'::jsonb,
  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(group_id, level_no)
);

CREATE INDEX IF NOT EXISTS game_levels_group_active_idx ON game_levels(group_id, is_active, level_no);

INSERT INTO game_levels(group_id, level_no, npc_ids, npc_photos, messages, is_active)
VALUES
  (
    'night-watch',
    10001,
    ARRAY[1,9478],
    '{"1":"","9478":""}'::jsonb,
    '[
      {"send_id":1,"text":"大家好，群规提醒：不索要代理节点、不刷屏，也不要攻击群友。"},
      {"send_id":9478,"text":"有没有腾讯云节点？快点发一个我急用。","reportable":true},
      {"send_id":9478,"text":"没人回？一个个装高手是吧。","reportable":true}
    ]'::jsonb,
    true
  ),
  (
    'station',
    30001,
    ARRAY[0,9478],
    '{"0":"","9478":""}'::jsonb,
    '[
      {"send_id":0,"text":"大家好，今天继续讲胡说哥传奇第一回。"},
      {"send_id":9478,"text":"有没有腾讯云节点，给我一个能用的。"},
      {"send_id":0,"text":"先别刷屏，按群规来。"}
    ]'::jsonb,
    true
  )
ON CONFLICT(group_id, level_no) DO UPDATE SET
  npc_ids = EXCLUDED.npc_ids,
  npc_photos = EXCLUDED.npc_photos,
  messages = EXCLUDED.messages,
  is_active = EXCLUDED.is_active,
  updated_at = now();
