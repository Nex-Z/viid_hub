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
COMMENT ON TABLE devices IS '接入设备台账';
COMMENT ON COLUMN devices.id IS '设备标识';
COMMENT ON COLUMN devices.name IS '设备名称';
COMMENT ON COLUMN devices.kind IS '设备类别';
COMMENT ON COLUMN devices.ip IS '设备网络地址';
COMMENT ON COLUMN devices.point IS '采集点位';
COMMENT ON COLUMN devices.enabled IS '是否启用';
COMMENT ON COLUMN devices.registered IS '是否已注册';
COMMENT ON COLUMN devices.heartbeat IS '最近心跳时间';
COMMENT ON COLUMN devices.created_at IS '建档时间';

COMMENT ON TABLE device_events IS '设备事件流水';
COMMENT ON COLUMN device_events.seq IS '事件流水号';
COMMENT ON COLUMN device_events.device_id IS '关联设备标识';
COMMENT ON COLUMN device_events.event IS '事件类型';
COMMENT ON COLUMN device_events.at IS '事件发生时间';

COMMENT ON TABLE records IS '设备上报的采集记录';
COMMENT ON COLUMN records.id IS '采集对象标识';
COMMENT ON COLUMN records.kind IS '对象类别，person 为人员，face 为人脸，motor 为机动车，nonmotor 为非机动车';
COMMENT ON COLUMN records.device_id IS '来源设备标识';
COMMENT ON COLUMN records.captured IS '采集时间';
COMMENT ON COLUMN records.title IS '记录展示标题';
COMMENT ON COLUMN records.raw IS '采集对象的原始协议数据';

COMMENT ON TABLE images IS '已保存图片及其二进制内容';
COMMENT ON COLUMN images.id IS '图片标识';
COMMENT ON COLUMN images.filename IS '图片文件名';
COMMENT ON COLUMN images.mime IS '图片媒体类型';
COMMENT ON COLUMN images.content IS '图片二进制内容';
COMMENT ON COLUMN images.record_id IS '关联采集记录标识';
COMMENT ON COLUMN images.device_id IS '来源设备标识';
COMMENT ON COLUMN images.kind IS '图片类别';
COMMENT ON COLUMN images.width IS '图片宽度（像素）';
COMMENT ON COLUMN images.height IS '图片高度（像素）';
COMMENT ON COLUMN images.uploaded IS '历史时间字段，曾混用设备采集时间与手动上传时间';
COMMENT ON COLUMN images.ingested_at IS '平台接收图片的时间，历史数据可能为空';
COMMENT ON COLUMN images.shot_at IS '图片拍摄时间，未知时为空';

COMMENT ON TABLE operation_audit IS '管理操作审计日志';
COMMENT ON COLUMN operation_audit.seq IS '审计流水号';
COMMENT ON COLUMN operation_audit.at IS '操作时间';
COMMENT ON COLUMN operation_audit.actor IS '操作者';
COMMENT ON COLUMN operation_audit.action IS '操作类型';
COMMENT ON COLUMN operation_audit.resource IS '操作涉及的资源类型';
COMMENT ON COLUMN operation_audit.target IS '操作目标';

COMMENT ON TABLE digest_counters IS 'Digest 认证防重放计数';
COMMENT ON COLUMN digest_counters.nonce IS '服务端质询随机值';
COMMENT ON COLUMN digest_counters.identity IS '认证用户名';
COMMENT ON COLUMN digest_counters.cnonce IS '客户端随机值';
COMMENT ON COLUMN digest_counters.nc IS '该随机值组合已接受的最大请求计数';
COMMENT ON COLUMN digest_counters.expires IS '质询过期时间';

COMMENT ON TABLE subscriptions IS '采集记录通知订阅';
COMMENT ON COLUMN subscriptions.id IS '平台内部订阅编号';
COMMENT ON COLUMN subscriptions.target IS '通知投递目标';
COMMENT ON COLUMN subscriptions.kind IS '订阅对象类别，为空表示全部类别';
COMMENT ON COLUMN subscriptions.device_id IS '来源设备筛选条件，为空表示全部设备';
COMMENT ON COLUMN subscriptions.enabled IS '是否启用订阅';
COMMENT ON COLUMN subscriptions.external_id IS '协议订阅标识，手动订阅为空';
COMMENT ON COLUMN subscriptions.owner_id IS '订阅方标识';
COMMENT ON COLUMN subscriptions.details IS '订阅协议数据';
COMMENT ON COLUMN subscriptions.begins IS '订阅开始时间';
COMMENT ON COLUMN subscriptions.ends IS '订阅结束时间';

COMMENT ON TABLE deliveries IS '按订阅生成的采集记录通知投递任务';
COMMENT ON COLUMN deliveries.id IS '投递任务编号';
COMMENT ON COLUMN deliveries.subscription IS '关联订阅的内部编号';
COMMENT ON COLUMN deliveries.record_id IS '待投递的采集记录标识';
COMMENT ON COLUMN deliveries.state IS '投递任务状态';
COMMENT ON COLUMN deliveries.attempts IS '已尝试投递次数';
COMMENT ON COLUMN deliveries.due IS '下次可尝试投递的时间';
COMMENT ON COLUMN deliveries.updated IS '任务最近更新时间';
COMMENT ON COLUMN deliveries.message IS '投递结果或错误说明';
COMMENT ON COLUMN deliveries.created IS '任务创建时间';

COMMENT ON TABLE viid_catalog IS '视图库协议资源目录';
COMMENT ON COLUMN viid_catalog.resource IS '协议资源类型';
COMMENT ON COLUMN viid_catalog.id IS '资源标识';
COMMENT ON COLUMN viid_catalog.owner IS '资源所属上报方标识';
COMMENT ON COLUMN viid_catalog.payload IS '资源协议数据';
COMMENT ON COLUMN viid_catalog.current IS '资源是否仍然有效';

COMMENT ON TABLE viid_image_info IS '协议图片元数据';
COMMENT ON COLUMN viid_image_info.id IS '协议图片标识';
COMMENT ON COLUMN viid_image_info.device IS '来源设备标识';
COMMENT ON COLUMN viid_image_info.info IS '图片协议数据';
COMMENT ON COLUMN viid_image_info.record_id IS '关联采集记录标识';

COMMENT ON TABLE viid_image_links IS '协议图片与采集记录的关联关系';
COMMENT ON COLUMN viid_image_links.image_id IS '协议图片标识';
COMMENT ON COLUMN viid_image_links.record_id IS '采集记录标识';

COMMENT ON TABLE lower_subscriptions IS '本平台向下级平台发起的订阅';
COMMENT ON COLUMN lower_subscriptions.id IS '协议订阅标识';
COMMENT ON COLUMN lower_subscriptions.target IS '目标下级平台名称';
COMMENT ON COLUMN lower_subscriptions.owner IS '订阅方标识';
COMMENT ON COLUMN lower_subscriptions.kind IS '订阅对象类别';
COMMENT ON COLUMN lower_subscriptions.source IS '订阅的来源设备条件';
COMMENT ON COLUMN lower_subscriptions.details IS '订阅协议数据';
COMMENT ON COLUMN lower_subscriptions.ends IS '订阅结束时间';
COMMENT ON COLUMN lower_subscriptions.state IS '订阅状态，取值为 pending、active、cancelling 或 cancelled';

COMMENT ON TABLE lower_notices IS '下级平台订阅通知接收与去重记录';
COMMENT ON COLUMN lower_notices.owner IS '通知来源方标识';
COMMENT ON COLUMN lower_notices.id IS '通知标识';
COMMENT ON COLUMN lower_notices.subscription IS '关联下级订阅标识';
COMMENT ON COLUMN lower_notices.fingerprint IS '用于识别重复通知的内容指纹';
COMMENT ON COLUMN lower_notices.received IS '通知接收时间';
