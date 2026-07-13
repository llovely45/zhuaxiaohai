DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM npcs WHERE public_id = 9479 AND name <> '胡说哥') THEN
    UPDATE npcs SET public_id = (SELECT GREATEST(COALESCE(MAX(public_id), 9479), 9479) + 1 FROM npcs)
    WHERE public_id = 9479 AND name <> '胡说哥';
  END IF;
END $$;

INSERT INTO npcs(public_id, name, tg_username, description, rarity, sort_order, is_active)
VALUES(9479, '胡说哥', '@Raymond520666', '喜欢胡说', '普通', 15, true)
ON CONFLICT(name) DO UPDATE SET
  public_id = 9479,
  tg_username = EXCLUDED.tg_username,
  description = EXCLUDED.description,
  is_active = true,
  sort_order = EXCLUDED.sort_order;

SELECT setval(pg_get_serial_sequence('npcs','public_id'), GREATEST((SELECT MAX(public_id) FROM npcs), 9479), true);
