UPDATE game_levels
SET messages = (
  SELECT jsonb_agg(
    CASE
      WHEN (item->>'send_id')::int = 9478 THEN item || '{"reportable": true}'::jsonb
      ELSE item - 'reportable'
    END
    ORDER BY ord
  )
  FROM jsonb_array_elements(messages) WITH ORDINALITY AS rows(item, ord)
),
updated_at = now()
WHERE group_id = 'night-watch';
