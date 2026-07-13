CREATE TABLE IF NOT EXISTS fingerprint_labels (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  label_name text NOT NULL,
  fingerprint_id text NOT NULL,
  fingerprint_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  rules jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(label_name, fingerprint_id)
);

CREATE INDEX IF NOT EXISTS fingerprint_labels_fingerprint_id_idx ON fingerprint_labels(fingerprint_id);
