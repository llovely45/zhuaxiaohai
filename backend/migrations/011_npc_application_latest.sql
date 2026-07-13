WITH ranked AS (
  SELECT id,
         row_number() OVER (PARTITION BY player_id ORDER BY created_at DESC, id DESC) AS rn
  FROM npc_applications
  WHERE status = 'pending'
)
DELETE FROM npc_applications
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

WITH ranked AS (
  SELECT id,
         row_number() OVER (PARTITION BY extracted_data->>'verified_tg_id' ORDER BY created_at DESC, id DESC) AS rn
  FROM npc_applications
  WHERE status = 'pending'
    AND COALESCE(extracted_data->>'verified_tg_id', '') <> ''
)
DELETE FROM npc_applications
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

CREATE UNIQUE INDEX IF NOT EXISTS npc_applications_one_pending_per_player_idx
ON npc_applications(player_id)
WHERE status = 'pending';

CREATE UNIQUE INDEX IF NOT EXISTS npc_applications_one_pending_per_tg_id_idx
ON npc_applications((extracted_data->>'verified_tg_id'))
WHERE status = 'pending'
  AND COALESCE(extracted_data->>'verified_tg_id', '') <> '';
