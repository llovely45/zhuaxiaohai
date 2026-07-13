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

INSERT INTO fingerprint_labels(label_name, fingerprint_id, fingerprint_payload, rules)
VALUES
  (
    '小孩',
    '1d8b97beeeb5c9b5fefffa10',
    '{
      "publicIpInfo": {
        "ip": "152.32.254.68",
        "asn": "135377",
        "organization": "UCLOUD-HK-AS-AP - UCLOUD INFORMATION TECHNOLOGY (HK) LIMITED, HK"
      },
      "webrtcIpInfos": [
        {
          "ip": "152.32.254.68",
          "asn": "135377",
          "organization": "UCLOUD-HK-AS-AP - UCLOUD INFORMATION TECHNOLOGY (HK) LIMITED, HK"
        }
      ],
      "details": {
        "os": "Android",
        "cpu": {"hardwareConcurrency": 8, "deviceMemory": 8, "maxTouchPoints": 5},
        "screen": {"width": 360, "height": 800, "availWidth": 360, "availHeight": 800, "colorDepth": 24, "pixelDepth": 24, "pixelRatio": 2},
        "fonts": ["Arial", "Helvetica", "Times New Roman", "Courier New", "Verdana", "Georgia"],
        "canvas": "2183eaad044c07bad8f42bf2",
        "webgl": {"hash": "67a2bd09fa91a217ff5b972a"},
        "audio": "7982d8f12778bdcea03c6b84"
      }
    }'::jsonb,
    '["ip", "asn", "isp", "webrtc_ip", "webrtc_asn", "webrtc_isp", "canvas", "webgl", "audio", "system", "cpu", "screen", "fonts"]'::jsonb
  ),
  (
    '小孩',
    '0e9e1a53d73e4ae10be96c3e',
    '{
      "publicIpInfo": {
        "ip": "2001:b030:8111:ff0b:cc04:bfe8:3b5a:72b2",
        "asn": "3462",
        "organization": "HINET - Data Communication Business Group, TW"
      },
      "webrtcIpInfos": [],
      "details": {
        "os": "Android",
        "cpu": {"hardwareConcurrency": 8, "deviceMemory": 8, "maxTouchPoints": 5},
        "screen": {"width": 361, "height": 801, "availWidth": 361, "availHeight": 801, "colorDepth": 24, "pixelDepth": 24, "pixelRatio": 3.5},
        "fonts": ["Arial", "Helvetica", "Times New Roman", "Courier New", "Verdana", "Georgia"],
        "canvas": "8a16fff05eb2cb9cb9bcb98a",
        "webgl": {"hash": "acf2ac9c7663cd84f7139eb7"},
        "audio": "7982d8f12778bdcea03c6b84"
      }
    }'::jsonb,
    '["ip", "asn", "isp", "canvas", "webgl", "audio", "system", "cpu", "screen", "fonts"]'::jsonb
  ),
  (
    '小孩',
    '84368c261150ffd4db1f1bd6',
    '{
      "publicIpInfo": {
        "ip": "36.227.198.64",
        "asn": "3462",
        "organization": "HINET - Data Communication Business Group, TW"
      },
      "webrtcIpInfos": [],
      "details": {
        "os": "Android",
        "cpu": {"hardwareConcurrency": 8, "deviceMemory": 8, "maxTouchPoints": 5},
        "screen": {"width": 360, "height": 800, "availWidth": 360, "availHeight": 800, "colorDepth": 24, "pixelDepth": 24, "pixelRatio": 2},
        "fonts": ["Arial", "Helvetica", "Times New Roman", "Courier New", "Verdana", "Georgia"],
        "canvas": "2183eaad044c07bad8f42bf2",
        "webgl": {"hash": "67a2bd09fa91a217ff5b972a"},
        "audio": "7982d8f12778bdcea03c6b84"
      }
    }'::jsonb,
    '["ip", "asn", "isp", "canvas", "webgl", "audio", "system", "cpu", "screen", "fonts"]'::jsonb
  )
ON CONFLICT(label_name, fingerprint_id) DO UPDATE SET
  fingerprint_payload = EXCLUDED.fingerprint_payload,
  rules = EXCLUDED.rules,
  updated_at = now();
