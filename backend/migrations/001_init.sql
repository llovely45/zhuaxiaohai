CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS players (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tg_user_id text,
  tg_init_data text,
  tg_verified boolean NOT NULL DEFAULT false,
  tg_context jsonb NOT NULL DEFAULT '{}'::jsonb,
  fingerprint_hash text NOT NULL UNIQUE,
  fingerprint jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS players_tg_user_id_idx ON players(tg_user_id);

CREATE TABLE IF NOT EXISTS npcs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  public_id bigserial NOT NULL UNIQUE,
  name text NOT NULL UNIQUE,
  title text NOT NULL DEFAULT '',
  tg_username text NOT NULL DEFAULT '',
  description text NOT NULL,
  avatar_url text NOT NULL DEFAULT '',
  rarity text NOT NULL DEFAULT '普通',
  is_active boolean NOT NULL DEFAULT true,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE npcs ADD COLUMN IF NOT EXISTS public_id bigserial;
ALTER TABLE npcs ADD COLUMN IF NOT EXISTS tg_username text NOT NULL DEFAULT '';
ALTER TABLE npcs ALTER COLUMN title SET DEFAULT '';

CREATE TABLE IF NOT EXISTS achievements (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code text NOT NULL UNIQUE,
  name text NOT NULL,
  description text NOT NULL,
  sort_order integer NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS player_achievements (
  player_id uuid NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  achievement_id uuid NOT NULL REFERENCES achievements(id) ON DELETE CASCADE,
  unlocked_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(player_id, achievement_id)
);

CREATE TABLE IF NOT EXISTS npc_applications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  player_id uuid NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  name text NOT NULL,
  persona text NOT NULL,
  dialogue_sample text NOT NULL DEFAULT '',
  tg_username text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  extracted_data jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS level_submissions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  player_id uuid NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  name text NOT NULL,
  description text NOT NULL,
  payload text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS miniapp_events (
  id bigserial PRIMARY KEY,
  player_id uuid NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  event text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS miniapp_events_player_created_idx ON miniapp_events(player_id, created_at DESC);

INSERT INTO npcs(public_id,name,tg_username,description,rarity,sort_order)
SELECT seed.public_id,seed.name,seed.tg_username,seed.description,seed.rarity,seed.sort_order
FROM (VALUES
  (9478,'小孩哥','@xiaohai','到处索要代理节点，要不到就开始嘴硬。','稀有',10),
  (1,'群规机器人','@thisisabot','负责审核举报、封禁违规账号并发放成就。','传说',20),
  (0,'顶尖哥','@anlianxiaoliu','他很顶尖。','普通',5)
) AS seed(public_id,name,tg_username,description,rarity,sort_order)
WHERE NOT EXISTS (
  SELECT 1 FROM npcs
  WHERE npcs.name=seed.name OR (seed.name='小孩哥' AND npcs.name='小板凳')
);

INSERT INTO achievements(code,name,description,sort_order) VALUES
('first-catch','第一次抓小孩','成功完成一次正确举报。',10),
('fast-catch','闪电执法','在 30 秒内完成举报。',20),
('npc-creator','纸片造物主','提交第一份 NPC 申请。',30)
ON CONFLICT(code) DO NOTHING;
