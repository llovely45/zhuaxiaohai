ALTER TABLE level_submissions ADD COLUMN IF NOT EXISTS approved_level_no integer NOT NULL DEFAULT 0;

INSERT INTO npcs(name, tg_username, description, rarity, sort_order, is_active)
VALUES('胡说哥', '@Raymond520666', '喜欢胡说', '普通', 15, true)
ON CONFLICT(name) DO UPDATE SET
  tg_username = EXCLUDED.tg_username,
  description = EXCLUDED.description,
  is_active = true,
  sort_order = EXCLUDED.sort_order;
