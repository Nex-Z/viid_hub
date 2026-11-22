// Launched by TestIndependent2017. Both peer listeners are loopback-only.
import assert from "node:assert/strict";
import http from "node:http";
import { createHash, randomBytes } from "node:crypto";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { setTimeout as sleep } from "node:timers/promises";

const hub = process.argv[2],
  local = "50010300002000000001",
  lower = "50010400002000000001",
  upper = "50010500002000000001";
const cameras = ["50010400001320000001", "50010400001320000002"],
  outsider = "50010600001320000001";
const gate = "50010400001310000001",
  aps = "50010400002150000001";
const now = (delta = 0) =>
  new Date(Date.now() + delta + 8 * 3600000)
    .toISOString()
    .replace(/\D/g, "")
    .slice(0, 14);
const time = now(),
  img = cameras[0] + "02" + time + "00001",
  remoteImg = cameras[1] + "02" + time + "00002";
const photo = await readFile(
  new URL("../demo/media/camera.jpg", import.meta.url),
);
const b64 = photo.toString("base64");
const md5 = (s) => createHash("md5").update(s).digest("hex");
const fields = (s) =>
  Object.fromEntries(
    [...s.matchAll(/(\w+)=(?:"([^"]*)"|([^, ]+))/g)].map((m) => [
      m[1],
      m[2] ?? m[3],
    ]),
  );
function signature(
  { username, realm, nonce, nc, cnonce, qop, uri },
  method,
  pw,
) {
  return md5(
    [
      md5([username, realm, pw].join(":")),
      nonce,
      nc,
      cnonce,
      qop,
      md5(method + ":" + uri),
    ].join(":"),
  );
}
const lowerSubs = new Map(),
  forwarded = new Map(),
  pushed = new Map(),
  challenges = new Set();
let breakDirectory = false;
let imageFetches = 0,
  lostReceipt = true;
const registered = new Set();
const catalog = {
  APEs: [
    "APE",
    cameras.map((id, i) => ({
      ApeID: id,
      Name: "camera " + i,
      OwnerApsID: aps,
      Password: "directory-secret",
    })),
  ],
  APSs: ["APS", [{ ApsID: aps, Name: "collector" }]],
  Tollgates: ["Tollgate", [{ TollgateID: gate, Name: "gate" }]],
  Lanes: ["Lane", [{ LaneId: 1, TollgateID: gate, LaneNo: 1, Name: "east" }]],
};
const receipt = (Id) => ({
  Id,
  StatusCode: 0,
  StatusString: "OK",
  LocalTime: now(),
});
const ack = (id) => ({ ResponseStatusObject: receipt(id) });
const listAck = (id) => ({
  ResponseStatusListObject: { ResponseStatusObject: [receipt(id)] },
});
const listeners = [];
async function peer(role) {
  const realm = role,
    nonce = randomBytes(16).toString("hex");
  const server = http.createServer(async (req, res) => {
    try {
      const auth = fields(req.headers.authorization || "");
      if (!auth.username) {
        challenges.add(role);
        res.writeHead(401, {
          "WWW-Authenticate":
            'Digest realm="' +
            realm +
            '", nonce="' +
            nonce +
            '", algorithm=MD5, qop="auth"',
        });
        res.end();
        return;
      }
      assert.equal(req.headers["user-identify"], local);
      assert.equal(auth.realm, realm);
      assert.equal(auth.username, local);
      assert.equal(auth.uri, req.url);
      assert.equal(auth.nonce, nonce);
      assert.equal(auth.response, signature(auth, req.method, "to-peer"));
      const url = new URL(req.url, "http://localhost"),
        route = url.pathname.slice(6),
        chunks = [];
      for await (const c of req) chunks.push(c);
      const body = chunks.length ? JSON.parse(Buffer.concat(chunks)) : null;
      let result;
      if (route.startsWith("System/")) {
        const op = route.slice(7);
        if (op === "Time") {
          result = { SystemTimeObject: { LocalTime: now() } };
        } else {
          assert.equal(body[op + "Object"].DeviceID, local);
          if (op === "Register") {
            assert.ok(!registered.has(role), "duplicate registration");
            registered.add(role);
          }
          result = ack(local);
        }
      } else if (role === "lower" && catalog[route]) {
        if (breakDirectory && route === "Lanes") {
          res.writeHead(503);
          res.end();
          return;
        }
        const [type, rows] = catalog[route],
          start = Number(url.searchParams.get("RecordStartNo")) - 1;
        assert.ok(start >= 0);
        assert.equal(req.method, "GET");
        // Deliberately return at most one entry per page.
        result = {
          [type + "ListObject"]: {
            [type + "Object"]: rows.slice(start, start + 1),
          },
          TotalNum: rows.length,
        };
      } else if (role === "lower" && route === "Subscribes") {
        const [s] = body.SubscribeListObject.SubscribeObject;
        assert.match(s.SubscribeID, /^\d{12}03\d{19}$/);
        assert.equal(s.ResourceURI, lower);
        assert.equal(s.SubscribeDetail, "11");
        assert.equal(s.OperateType, 0);
        assert.equal(s.ReceiveAddr, hub + "/VIID/SubscribeNotifications");
        assert.ok(s.ApplicantName && s.ApplicantOrg && s.BeginTime < s.EndTime);
        if (lowerSubs.has(s.SubscribeID))
          assert.deepEqual(s, lowerSubs.get(s.SubscribeID));
        lowerSubs.set(s.SubscribeID, s);
        if (lostReceipt) {
          lostReceipt = false;
          res.writeHead(503);
          res.end();
          return;
        }
        result = listAck(s.SubscribeID);
      } else if (role === "lower" && route.startsWith("Subscribes/")) {
        const s = body.SubscribeObject;
        assert.equal(req.method, "PUT");
        assert.equal(s.OperateType, 1);
        assert.ok(lowerSubs.delete(s.SubscribeID));
        result = ack(s.SubscribeID);
      } else if (
        role === "lower" &&
        route === "Images/" + remoteImg + "/Data"
      ) {
        imageFetches++;
        res.end(b64);
        return;
      } else if (role === "upper" && route === "Persons") {
        const [p] = body.PersonListObject.PersonObject;
        assert.ok(cameras.includes(p.DeviceID));
        forwarded.set(p.PersonID, p);
        result = listAck(p.PersonID);
      } else if (role === "upper" && route === "SubscribeNotifications") {
        const [n] =
          body.SubscribeNotificationListObject.SubscribeNotificationObject;
        assert.equal(n.SubscribeID, upperTask);
        const [p] = n.PersonObjectList.PersonObject;
        assert.equal(n.InfoIDs, p.PersonID);
        pushed.set(p.PersonID, p);
        result = listAck(n.NotificationID);
      } else {
        throw Error("unexpected " + role + " " + req.url);
      }
      res.setHeader("Content-Type", "application/VIID+JSON");
      res.end(JSON.stringify(result));
    } catch (e) {
      console.error(e);
      res.writeHead(400);
      res.end();
    }
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  listeners.push(server);
  return "http://127.0.0.1:" + server.address().port;
}
const urls = { Lower: await peer("lower"), Upper: await peer("upper") };
console.log(JSON.stringify(urls));
await once(process.stdin, "data");
process.stdin.pause();

const secrets = {
  [lower]: "from-lower",
  [upper]: "from-upper",
  [outsider]: process.env.SIM_DEVICE,
};
let cookie;
async function call(
  path,
  { method = "GET", body, id, expected = 200, raw = false } = {},
) {
  const uri = new URL(path, hub),
    headers = {
      "Content-Type": raw
        ? "application/octet-stream"
        : "application/VIID+JSON",
    };
  if (cookie && !id) headers.Cookie = cookie;
  const options = {
    method,
    headers,
    body: body === undefined ? undefined : raw ? body : JSON.stringify(body),
    signal: AbortSignal.timeout(12000),
  };
  let response;
  if (id) {
    headers["User-Identify"] = id;
    response = await fetch(uri, options);
    assert.equal(response.status, 401);
    const h = fields(response.headers.get("www-authenticate"));
    await response.arrayBuffer();
    const auth = {
      username: id,
      realm: h.realm,
      nonce: h.nonce,
      uri: uri.pathname + uri.search,
      qop: "auth",
      nc: "00000001",
      cnonce: randomBytes(12).toString("hex"),
    };
    headers.Authorization =
      "Digest " +
      Object.entries({
        ...auth,
        response: signature(auth, method, secrets[id]),
      })
        .map(([k, v]) =>
          ["qop", "nc"].includes(k) ? k + "=" + v : k + '="' + v + '"',
        )
        .join(", ");
  }
  response = await fetch(uri, options);
  const txt = await response.text();
  assert.equal(
    response.status,
    expected,
    method + " " + path + " " + txt.slice(0, 350),
  );
  if (path === "/api/login")
    cookie = response.headers.get("set-cookie").split(";")[0];
  if (raw && method === "GET") return Buffer.from(txt, "base64");
  const json = JSON.parse(txt);
  if (expected >= 400 && path.startsWith("/VIID/")) {
    const trace = response.headers.get("X-Request-ID");
    assert.match(trace ?? "", /^[A-Z2-7]{26}$/);
    console.log("caller_rejection http=" + expected + " request_id=" + trace);
  }
  if (path.startsWith("/VIID/")) {
    const statuses =
      json.ResponseStatusListObject?.ResponseStatusObject ??
      (json.ResponseStatusObject ? [json.ResponseStatusObject] : []);
    if (expected >= 400 || method !== "GET")
      assert.ok(statuses.length, "missing business status");
    for (const s of statuses) {
      assert.equal(typeof s.StatusCode, "number");
      assert.equal(s.StatusCode === 0, expected < 400);
    }
  }
  return json;
}
const upperTask = upper.slice(0, 12) + "03" + time + "00001";
const reg = (id) =>
  call("/VIID/System/Register", {
    method: "POST",
    id,
    body: { RegisterObject: { DeviceID: id } },
  });
const admin = (path, body, expected = 200) =>
  call("/api/exchange/" + path, { method: "POST", body, expected });
const read = (path, id = upper) => call("/VIID/" + path, { id });
const send = (path, body, expected = 200, id = lower) =>
  call("/VIID/" + path, { id, method: "POST", body, expected });
const codes = {
  Person: "01",
  Face: "06",
  MotorVehicle: "02",
  NonMotorVehicle: "03",
};
function capture(type, cam, seq, photoID) {
  const src = cam + "02" + time + String(seq).padStart(5, "0");
  const o = {
    [type + "ID"]: src + codes[type] + "00001",
    DeviceID: cam,
    SourceID: src,
    InfoKind: 1,
    LeftTopX: 1,
    LeftTopY: 1,
    RightBtmX: 25,
    RightBtmY: 30,
  };
  if (type === "Face" || type === "Person") {
    o[type + "AppearTime"] = time;
    o[type + "DisAppearTime"] = time;
    o.LocationMarkTime = time;
  } else {
    Object.assign(o, {
      AppearTime: time,
      DisappearTime: time,
      MarkTime: time,
      HasPlate: 1,
      PlateClass: "02",
      PlateColor: "5",
      PlateNo: "TEST",
      VehicleClass: "K33",
      VehicleColor: "1",
    });
  }
  if (type === "Face")
    Object.assign(o, {
      IsSuspectedTerrorist: 2,
      IsCriminalInvolved: 2,
      IsVictim: 2,
      IsSuspiciousPerson: 2,
    });
  if (type === "MotorVehicle") o.StorageUrl1 = "/VIID/Images/" + src + "/Data";
  if (photoID)
    o.SubImageList = {
      SubImageInfoObject: [
        {
          ImageID: photoID,
          DeviceID: cam,
          StoragePath: "/VIID/Images/" + photoID + "/Data",
          FileFormat: "Jpeg",
          ShotTime: time,
        },
      ],
    };
  return o;
}
const envelope = (type, objects) => ({
  [type + "ListObject"]: { [type + "Object"]: objects },
});

try {
  await call("/api/login", {
    method: "POST",
    body: { User: "sim", Password: process.env.SIM_ADMIN },
  });
  const basic = await fetch(hub + "/VIID/System/Time", {
    headers: {
      Authorization:
        "Basic " +
        Buffer.from(outsider + ":" + secrets[outsider]).toString("base64"),
    },
  });
  assert.equal(basic.status, 401);
  await basic.arrayBuffer();
  await reg(lower);
  await reg(upper);
  await reg(outsider);
  const counts = await admin("lower/lower/sync", {});
  assert.deepEqual(counts, { APEs: 2, APSs: 1, Tollgates: 1, Lanes: 1 });
  assert.deepEqual(await admin("lower/lower/sync", {}), counts);
  breakDirectory = true;
  await admin("lower/lower/sync", {}, 502);
  breakDirectory = false;
  for (const [route, [type, rows]] of Object.entries(catalog)) {
    const result = await read(route + "?PageRecordNum=100");
    for (const row of rows) {
      assert.ok(
        result[type + "ListObject"][type + "Object"].some(
          (r) => r.Name === row.Name,
        ),
      );
    }
    assert.ok(!JSON.stringify(result).includes("directory-secret"));
  }
  assert.equal(
    (await read("APEs?RecordStartNo=1&PageRecordNum=1&ApeID=" + cameras[1]))
      .APEListObject.APEObject[0].ApeID,
    cameras[1],
  );
  assert.equal(
    (await read("APEs?ApeID=" + cameras[1], outsider)).APEListObject.APEObject
      .length,
    0,
  );
  console.log(
    "Digest, four catalogs, multipage synchronization and device ownership OK",
  );
  const info = {
    ImageID: img,
    DeviceID: cameras[0],
    InfoKind: 1,
    ImageSource: "01",
    FileFormat: "Jpeg",
    ShotTime: time,
    StoragePath: "/VIID/Images/" + img + "/Data",
  };
  await call("/VIID/Images/" + img + "/Data", {
    method: "POST",
    id: lower,
    body: b64,
    raw: true,
  });
  await send("Images", {
    ImageListObject: { ImageObject: [{ ImageInfoObject: info }] },
  });
  await call("/VIID/Images/" + img + "/Data", {
    method: "POST",
    id: lower,
    body: b64,
    raw: true,
  });
  assert.deepEqual(
    await call("/VIID/Images/" + img + "/Data", { id: upper, raw: true }),
    photo,
  );
  await call("/VIID/Images/" + img + "/Info", { id: outsider, expected: 404 });
  await call("/VIID/Images/" + img + "/Data", {
    method: "POST",
    id: outsider,
    body: b64,
    raw: true,
    expected: 400,
  });
  assert.equal(
    (await read("Images?DeviceID=" + cameras[0])).ImageListObject.ImageObject[0]
      .ImageInfoObject.ImageID,
    img,
  );
  assert.equal(
    (await read("Images/" + img + "/Info")).ImageInfoObject.ImageID,
    img,
  );
  await admin("subscriptions", { target: "upper", kind: "person", device: "" });
  await send(
    "Subscribes",
    envelope("Subscribe", [
      {
        SubscribeID: upperTask,
        Title: "upper feed",
        SubscribeDetail: "11",
        ResourceURI: local,
        ApplicantName: "sim",
        ApplicantOrg: "lab",
        BeginTime: now(-5000),
        EndTime: now(3600000),
        ReceiveAddr: urls.Upper + "/VIID/SubscribeNotifications",
        OperateType: 0,
      },
    ]),
    200,
    upper,
  );
  await admin(
    "lower/lower/subscribe",
    {
      Kind: "person",
      Title: "two cameras",
      ApplicantName: "sim",
      ApplicantOrg: "lab",
      EndTime: now(3600000),
    },
    502,
  );
  let settings = await call("/api/exchange/settings");
  const task = settings.lowerTasks[0];
  assert.equal(task.state, "pending");
  await admin("lower-tasks/" + task.id + "/retry", {});
  assert.equal(lowerSubs.size, 1);
  const persons = [
    capture("Person", cameras[0], 1, img),
    capture("Person", cameras[1], 2, remoteImg),
  ];
  const notice = {
    SubscribeID: task.id,
    NotificationID: lower.slice(0, 12) + "04" + now() + "00001",
    Title: "two cameras",
    TriggerTime: now(),
    InfoIDs: persons.map((p) => p.PersonID).join(";"),
    PersonObjectList: { PersonObject: persons },
  };
  const notify = (n, expected = 200, id = lower) =>
    send(
      "SubscribeNotifications",
      envelope("SubscribeNotification", [n]),
      expected,
      id,
    );
  const other = capture("Person", outsider, 77);
  await send(
    "SubscribeNotifications",
    envelope("SubscribeNotification", [
      notice,
      {
        ...notice,
        NotificationID: notice.NotificationID.slice(0, -1) + "8",
        InfoIDs: other.PersonID,
        PersonObjectList: { PersonObject: [other] },
      },
    ]),
    403,
  );
  assert.equal((await read("Persons")).TotalNum, 0);
  const resumed = once(process.stdin, "data");
  process.stdin.resume();
  console.log("CHECKPOINT_REJECTED_NOTICE");
  await resumed;
  process.stdin.pause();
  await notify(notice);
  await notify(notice);
  await notify({ ...notice, Title: "conflict" }, 409);
  await notify(notice, 403, outsider);
  const foreign = capture("Person", outsider, 5);
  await notify(
    {
      ...notice,
      NotificationID: notice.NotificationID.slice(0, -1) + "2",
      InfoIDs: foreign.PersonID,
      PersonObjectList: { PersonObject: [foreign] },
    },
    403,
  );
  assert.equal((await read("Persons")).TotalNum, 2);
  assert.equal(
    (await read("Persons?DeviceID=" + cameras[1], lower)).PersonListObject
      .PersonObject[0].DeviceID,
    cameras[1],
  );
  for (const p of persons) {
    assert.equal(
      (await read("Persons/" + p.PersonID)).PersonObject.DeviceID,
      p.DeviceID,
    );
  }
  assert.deepEqual(
    await call("/VIID/Images/" + remoteImg + "/Data", { id: upper, raw: true }),
    photo,
  );
  assert.deepEqual(
    await call("/VIID/Images/" + remoteImg + "/Data", { id: upper, raw: true }),
    photo,
  );
  assert.equal(imageFetches, 1);
  console.log(
    "Independent upload /Info /Data, remote image fetch and cached bytes OK",
  );
  for (const type of Object.keys(codes)) {
    const cam = cameras[1],
      obj = capture(type, cam, 3);
    await send(
      type + "s",
      envelope(type, [{ ...obj, [type + "ID"]: obj.SourceID + "9900001" }]),
      400,
    );
    await send(type + "s", envelope(type, [{ ...obj, InfoKind: 7 }]), 400);
    if (type !== "Person") {
      delete obj.AppearTime;
      await send(type + "s", envelope(type, [obj]));
    }
  }
  await send(
    "Persons",
    envelope("Person", [capture("Person", outsider, 9)]),
    403,
  );
  await admin("lower-tasks/" + task.id + "/cancel", {});
  await notify(
    { ...notice, NotificationID: notice.NotificationID.slice(0, -1) + "3" },
    403,
  );
  assert.equal(lowerSubs.size, 0);
  settings = await call("/api/exchange/settings");
  assert.equal(settings.lowerTasks[0].received, 1);
  assert.equal(settings.lowerTasks[0].state, "cancelled");
  for (let i = 0; i < 100 && (forwarded.size !== 2 || pushed.size !== 2); i++)
    await sleep(100);
  assert.deepEqual(
    [...forwarded.keys()].sort(),
    persons.map((p) => p.PersonID).sort(),
  );
  assert.equal(pushed.size, 2);
  assert.deepEqual(
    new Set([...pushed.values()].map((p) => p.DeviceID)),
    new Set(cameras),
  );
  assert.deepEqual(challenges, new Set(["lower", "upper"]));
  console.log(
    "Active subscription, lost receipt/retry, replay/conflict, cancellation, four object types and upper forwarding/notification OK",
  );
  console.log("Verified on " + hub);
} finally {
  for (const server of listeners) {
    server.closeAllConnections();
    server.close();
  }
  process.stdin.destroy();
}
