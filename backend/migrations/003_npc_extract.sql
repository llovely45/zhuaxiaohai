ALTER TABLE npc_applications ADD COLUMN IF NOT EXISTS tg_username text NOT NULL DEFAULT '';
ALTER TABLE npc_applications ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '';
ALTER TABLE npc_applications ADD COLUMN IF NOT EXISTS extracted_data jsonb NOT NULL DEFAULT '{}'::jsonb;

INSERT INTO npcs(public_id,name,tg_username,description,rarity,sort_order)
SELECT 0,'顶尖哥','@anlianxiaoliu','他很顶尖。','普通',5
WHERE NOT EXISTS (SELECT 1 FROM npcs WHERE name='顶尖哥');

UPDATE npcs SET public_id=0,name='顶尖哥',tg_username='@anlianxiaoliu',description='他很顶尖。',sort_order=5
WHERE name='顶尖哥';
