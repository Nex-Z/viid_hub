CREATE TABLE IF NOT EXISTS devices (
 id text PRIMARY KEY, name text NOT NULL, kind text NOT NULL DEFAULT '采集设备',
 ip text NOT NULL DEFAULT '', point text NOT NULL DEFAULT '', enabled boolean NOT NULL DEFAULT true,
 registered boolean NOT NULL DEFAULT false, heartbeat timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS device_events (
 seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, device_id text NOT NULL REFERENCES devices(id),
 event text NOT NULL, at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_events_recent ON device_events(device_id,at DESC);
CREATE TABLE IF NOT EXISTS records (
 id text PRIMARY KEY, kind text NOT NULL CHECK(kind IN ('person','face','motor','nonmotor')),
 device_id text NOT NULL REFERENCES devices(id), captured timestamptz NOT NULL,
 title text NOT NULL, raw jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS records_by_kind ON records(kind,captured DESC,id);
CREATE INDEX IF NOT EXISTS records_by_device ON records(device_id,captured DESC);
CREATE TABLE IF NOT EXISTS images (
 id text PRIMARY KEY, filename text NOT NULL, mime text NOT NULL, content bytea NOT NULL,
 record_id text REFERENCES records(id) ON DELETE SET NULL, device_id text REFERENCES devices(id),
 kind text NOT NULL, width int NOT NULL,height int NOT NULL, uploaded timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS images_by_record ON images(record_id,uploaded);
CREATE INDEX IF NOT EXISTS images_by_time ON images(uploaded DESC,id);
-- Legacy uploaded values mixed device capture time with manual upload time.
-- Only new writes have a known receipt time; keep historical values intact.
ALTER TABLE images ADD COLUMN IF NOT EXISTS ingested_at timestamptz;
ALTER TABLE images ALTER COLUMN ingested_at SET DEFAULT now();
ALTER TABLE images ADD COLUMN IF NOT EXISTS shot_at timestamptz;
-- Previous device writes stored the parent capture timestamp in uploaded.
UPDATE images i SET shot_at=i.uploaded FROM records r
 WHERE i.ingested_at IS NULL AND i.shot_at IS NULL AND i.record_id=r.id AND i.uploaded=r.captured;
CREATE INDEX IF NOT EXISTS images_by_ingest ON images(ingested_at DESC NULLS LAST,id);
CREATE TABLE IF NOT EXISTS operation_audit (
 seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, at timestamptz NOT NULL DEFAULT now(),
 actor text NOT NULL, action text NOT NULL, resource text NOT NULL, target text NOT NULL
);
CREATE TABLE IF NOT EXISTS digest_counters (
 nonce text NOT NULL, identity text NOT NULL, cnonce text NOT NULL, nc bigint NOT NULL, expires timestamptz NOT NULL,
 PRIMARY KEY(nonce,identity,cnonce)
);
CREATE INDEX IF NOT EXISTS digest_counters_expiry ON digest_counters(expires);
CREATE TABLE IF NOT EXISTS subscriptions (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, target text NOT NULL, kind text NOT NULL DEFAULT '',
 device_id text NOT NULL DEFAULT '', enabled boolean NOT NULL DEFAULT true,
 UNIQUE(target,kind,device_id)
);
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS external_id text;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS owner_id text;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS details jsonb;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS begins timestamptz;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS ends timestamptz;
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_external ON subscriptions(external_id) WHERE external_id IS NOT NULL;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_target_kind_device_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_manual ON subscriptions(target,kind,device_id) WHERE external_id IS NULL;
CREATE TABLE IF NOT EXISTS deliveries (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, subscription bigint NOT NULL REFERENCES subscriptions(id),
 record_id text NOT NULL REFERENCES records(id) ON DELETE CASCADE, state text NOT NULL DEFAULT 'pending',
 attempts int NOT NULL DEFAULT 0, due timestamptz NOT NULL DEFAULT now(), updated timestamptz NOT NULL DEFAULT now(),
 message text NOT NULL DEFAULT '', UNIQUE(subscription,record_id)
);
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS created timestamptz NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS deliveries_due ON deliveries(due,id) WHERE state='pending';
CREATE OR REPLACE VIEW delivery_list AS
 SELECT d.id,d.subscription,d.record_id,s.target,CASE WHEN d.state='pending' AND s.ends<now() THEN 'expired' WHEN d.state='pending' AND NOT s.enabled THEN 'paused' ELSE d.state END AS state,d.attempts,d.updated,d.message FROM deliveries d JOIN subscriptions s ON s.id=d.subscription;
CREATE TABLE IF NOT EXISTS viid_catalog (
 resource text NOT NULL, id text NOT NULL, owner text NOT NULL, payload jsonb NOT NULL,
 current boolean NOT NULL DEFAULT true, PRIMARY KEY(resource,id)
);
CREATE INDEX IF NOT EXISTS viid_catalog_owner ON viid_catalog(owner,resource);
CREATE TABLE IF NOT EXISTS viid_image_info (
 id text PRIMARY KEY, device text NOT NULL REFERENCES devices(id), info jsonb,
 record_id text REFERENCES records(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS viid_image_links (
 image_id text NOT NULL REFERENCES viid_image_info(id) ON DELETE CASCADE,
 record_id text NOT NULL REFERENCES records(id) ON DELETE CASCADE, PRIMARY KEY(image_id,record_id)
);
CREATE INDEX IF NOT EXISTS viid_image_links_by_record ON viid_image_links(record_id,image_id);
CREATE OR REPLACE VIEW device_list AS
 SELECT d.*,CASE WHEN NOT d.enabled THEN '停用' WHEN c.id IS NOT NULL THEN CASE WHEN c.current AND c.payload->>'IsOnline'='1' AND parent.enabled AND parent.registered AND parent.heartbeat>now()-interval '5 minutes' THEN '在线' ELSE '离线' END WHEN d.registered AND d.heartbeat>now()-interval '5 minutes' THEN '在线'
 WHEN d.registered THEN '超时' ELSE '离线' END AS status,
 (SELECT count(*) FROM records r WHERE r.device_id=d.id AND r.captured>=date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai') AS reports
 FROM devices d LEFT JOIN viid_catalog c ON c.resource='APEs' AND c.id=d.id LEFT JOIN devices parent ON parent.id=c.owner;
CREATE OR REPLACE VIEW record_list AS
 SELECT r.*,d.name AS device,d.point,
 COALESCE((SELECT id FROM images i WHERE i.record_id=r.id ORDER BY uploaded,id LIMIT 1), (SELECT image_id FROM viid_image_links WHERE record_id=r.id ORDER BY image_id LIMIT 1)) AS image
 FROM records r JOIN devices d ON d.id=r.device_id;
CREATE OR REPLACE VIEW image_list AS
 SELECT i.id,i.filename AS title,i.record_id,i.device_id,i.kind,i.width,i.height,i.shot_at AS captured,
 i.mime,octet_length(i.content) AS bytes,d.name AS device,r.title AS related,i.id AS image,
 i.ingested_at,i.shot_at AS shot_at
 FROM images i LEFT JOIN devices d ON d.id=i.device_id LEFT JOIN records r ON r.id=i.record_id;

-- Management responses redact IDNumber even inside nested vendor extensions.
CREATE OR REPLACE FUNCTION redact_identity(payload jsonb) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE result jsonb;
BEGIN
 CASE jsonb_typeof(payload)
 WHEN 'object' THEN
  SELECT coalesce(jsonb_object_agg(key, CASE
   WHEN lower(key)='idnumber' AND value <> 'null'::jsonb THEN to_jsonb(CASE
    WHEN length(value #>> '{}') > 10 THEN left(value #>> '{}',3)||'********'||right(value #>> '{}',4)
    ELSE '****' END)
   ELSE redact_identity(value) END),'{}'::jsonb) INTO result FROM jsonb_each(payload);
 WHEN 'array' THEN
  SELECT coalesce(jsonb_agg(redact_identity(value) ORDER BY n),'[]'::jsonb)
  INTO result FROM jsonb_array_elements(payload) WITH ORDINALITY a(value,n);
 ELSE RETURN payload;
 END CASE;
 RETURN result;
END $$;
CREATE OR REPLACE VIEW management_records AS
 SELECT id,kind,device_id,captured,title,redact_identity(raw) AS raw,device,point,image FROM record_list;

CREATE SEQUENCE IF NOT EXISTS viid_sub_seq;
CREATE TABLE IF NOT EXISTS lower_subscriptions (
 id text PRIMARY KEY, target text NOT NULL, owner text NOT NULL, kind text NOT NULL,
 source text NOT NULL, details jsonb NOT NULL, ends timestamptz NOT NULL,
 state text NOT NULL CHECK (state IN ('pending','active','cancelling','cancelled'))
);
CREATE TABLE IF NOT EXISTS lower_notices (
 owner text NOT NULL, id text NOT NULL, subscription text NOT NULL REFERENCES lower_subscriptions(id),
 fingerprint bytea NOT NULL, received timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(owner,id)
);
