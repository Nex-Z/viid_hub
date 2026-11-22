import React, { useEffect, useState } from "react";
import {
  Camera,
  ChevronLeft,
  ChevronRight,
  Download,
  Expand,
  Link2,
  Pencil,
  ShieldCheck,
  X,
} from "lucide-react";
import {
  Action,
  bytes,
  date,
  Details,
  Dot,
  api,
  exportItems,
  Empty,
  fileURL,
  IconBtn,
  Load,
  Modal,
  names,
  Photo,
  Tabs,
  Tag,
  useData,
} from "./shared";
export function Inspector({
  resource,
  notify,
  id,
  close,
  edit,
  go,
  rev,
  step,
  canPrev,
  canNext,
}) {
  const { data, error } = useData(resource + "/" + encodeURIComponent(id), rev);
  const [tab, setTab] = useState("basic"),
    [index, setIndex] = useState(0),
    [zoom, setZoom] = useState(false),
    [identity, setIdentity] = useState("");
  useEffect(() => {
    setTab("basic");
    setIndex(0);
    setZoom(false);
  }, [id, resource]);
  const r = data?.item,
    dev = resource === "devices",
    img = resource === "images",
    title = dev
      ? "设备详情"
      : img
        ? "图片预览"
        : (names[r?.kind] || "") + "记录详情";
  const pics = dev ? [] : img ? [r].filter(Boolean) : data?.images || [],
    pic = pics[Math.min(index, pics.length - 1)],
    raw = r?.raw || {};
  const imageBox = pic ? (
    <div className="preview">
      <Photo id={pic.id} />
      <IconBtn label="放大图片" icon={Expand} onClick={() => setZoom(true)} />
      {pics.length > 1 && (
        <div className="slidebuttons">
          <IconBtn
            icon={ChevronLeft}
            label="上一张"
            onClick={() => setIndex((index + pics.length - 1) % pics.length)}
          />
          <small>
            {index + 1} / {pics.length}
          </small>
          <IconBtn
            icon={ChevronRight}
            label="下一张"
            onClick={() => setIndex((index + 1) % pics.length)}
          />
        </div>
      )}
    </div>
  ) : (
    <Empty text="暂无关联图片" />
  );
  const capture = r
    ? [
        ["记录编号", r.id],
        ["抓拍时间", date(r.captured)],
        [
          "设备名称",
          <button
            className="textbtn"
            onClick={() => go("devices", r.device_id)}
          >
            {r.device}
          </button>,
        ],
        ["点位位置", r.point],
        ["数据来源", raw.Source || "设备上报"],
      ]
    : [];
  return (
    <aside className="inspector">
      <header>
        <h2>{title}</h2>
        <IconBtn icon={X} label="关闭详情" onClick={close} />
      </header>
      {!r ? (
        <Load error={error} />
      ) : (
        <>
          <div className="inscroll">
            {dev ? (
              <>
                <div className="profile">
                  <span className="deviceavatar">
                    <Camera size={40} />
                  </span>
                  <div>
                    <h2>{r.name}</h2>
                    <Dot value={r.status} />
                    <small>{r.id}</small>
                  </div>
                </div>
                <Tabs
                  items={[
                    ["basic", "运行状态"],
                    ["info", "基础信息"],
                    ["logs", "心跳日志"],
                  ]}
                  value={tab}
                  onChange={setTab}
                />
                {tab === "basic" && (
                  <div className="healthbox">
                    <ShieldCheck size={28} />
                    <div>
                      <b>{r.status === "在线" ? "运行正常" : r.status}</b>
                      <small>最近心跳　{date(r.heartbeat)}</small>
                    </div>
                  </div>
                )}
                {tab !== "logs" && (
                  <Details
                    title={tab === "basic" ? "接入状态" : "基础信息"}
                    rows={[
                      ["设备编号", r.id],
                      ["设备类型", r.kind],
                      ["IP 地址", r.ip || "—"],
                      ["所属点位", r.point || "—"],
                      ["今日上报", r.reports + " 条"],
                      ["启用状态", r.enabled ? "启用" : "停用"],
                    ]}
                  />
                )}
                {tab !== "info" && (
                  <section className="detailgroup">
                    <h3>最近心跳与事件</h3>
                    {data.events.length ? (
                      data.events
                        .slice(0, tab === "logs" ? 30 : 5)
                        .map((e, i) => (
                          <div className="event" key={i}>
                            <i className="dot green" />
                            <time>{date(e.at)}</time>
                            {e.event}
                          </div>
                        ))
                    ) : (
                      <Empty text="暂无设备事件" />
                    )}
                  </section>
                )}
              </>
            ) : img ? (
              <>
                {imageBox}
                <Details
                  title="基础信息"
                  rows={[
                    ["文件名", r.title],
                    ["图片类型", names[r.kind]],
                    ["文件大小", bytes(r.bytes)],
                    ["分辨率", r.width + " × " + r.height],
                    ["入库时间", date(r.ingested_at)],
                    ["抓拍时间", date(r.shot_at)],
                    ["来源设备", r.device || "手动上传"],
                    ["图片编号", r.id],
                  ]}
                />
                <Details
                  title="关联信息"
                  rows={[
                    [
                      "关联状态",
                      <Dot value={r.record_id ? "已关联" : "未关联"} />,
                    ],
                    ["关联记录", r.related],
                    ["记录编号", r.record_id],
                  ]}
                />
              </>
            ) : (
              <>
                <Tabs
                  value={tab}
                  onChange={setTab}
                  items={[
                    ["basic", "基本信息"],
                    ["photos", "关联图片 (" + pics.length + ")"],
                    ["json", "原始 JSON"],
                  ]}
                />
                {tab === "json" ? (
                  <pre>{JSON.stringify(raw, null, 2)}</pre>
                ) : tab === "photos" ? (
                  <>
                    {imageBox}
                    <div className="film">
                      {pics.map((p, i) => (
                        <button
                          key={p.id}
                          className={i === index ? "on" : ""}
                          onClick={() => setIndex(i)}
                        >
                          <Photo id={p.id} />
                        </button>
                      ))}
                    </div>
                  </>
                ) : (
                  <>
                    {["person", "face"].includes(r.kind) ? (
                      <div className="profile">
                        <button onClick={() => pic && setZoom(true)}>
                          <Photo id={r.image} />
                        </button>
                        <div>
                          <h2>{r.title}</h2>
                          <Tag kind={r.kind} />
                          <small>{date(r.captured)}</small>
                        </div>
                      </div>
                    ) : (
                      imageBox
                    )}
                    <Details
                      title="结构化信息"
                      rows={
                        ["person", "face"].includes(r.kind)
                          ? [
                              ["姓名", raw.Name],
                              [
                                "证件号码",
                                <>
                                  {identity || raw.IDNumber || "—"}{" "}
                                  {data.canReveal && raw.IDNumber && (
                                    <button
                                      className="textbtn"
                                      onClick={async () => {
                                        if (identity) {
                                          setIdentity("");
                                          return;
                                        }
                                        try {
                                          const result = await api(
                                            "records/" +
                                              encodeURIComponent(r.id) +
                                              "/identity",
                                            "POST",
                                          );
                                          setIdentity(result.value);
                                        } catch (e) {
                                          notify(e.message);
                                        }
                                      }}
                                    >
                                      {identity ? "隐藏" : "查看完整号码"}
                                    </button>
                                  )}
                                </>,
                              ],
                              ["性别", raw.GenderCode],
                              ["年龄", raw.Age ?? raw.AgeUpLimit],
                            ]
                          : [
                              ["车辆类型", raw.VehicleClass],
                              ...(r.kind === "motor"
                                ? [
                                    ["号牌号码", raw.PlateNo],
                                    [
                                      "车速",
                                      raw.Speed != null
                                        ? raw.Speed + " km/h"
                                        : "—",
                                    ],
                                  ]
                                : [
                                    [
                                      "骑行人数",
                                      raw.RiderCount ?? raw.CyclingPersonNumber,
                                    ],
                                  ]),
                              ["车身颜色", raw.VehicleColor],
                              ["行驶方向", raw.Direction],
                            ]
                      }
                    />
                    <Details title="抓拍信息" rows={capture} />
                  </>
                )}
              </>
            )}
          </div>
          <footer className="insfoot">
            {dev ? (
              <Action primary icon={Pencil} onClick={() => edit("device", r)}>
                编辑设备
              </Action>
            ) : img ? (
              <>
                <Action
                  icon={Link2}
                  onClick={() =>
                    r.record_id ? go(r.kind, r.record_id) : edit("link", r)
                  }
                >
                  {r.record_id ? "查看关联记录" : "关联记录"}
                </Action>
                <a className="linkbutton" href={fileURL(r.id) + "?download=1"}>
                  <Download size={15} />
                  下载图片
                </a>
              </>
            ) : (
              <>
                <Action
                  icon={Download}
                  onClick={() =>
                    exportItems("records", [r.id], r.id, true).catch((e) =>
                      notify(e.message),
                    )
                  }
                >
                  导出记录
                </Action>
                <span className="flexfill" />
                <IconBtn
                  icon={ChevronLeft}
                  label="上一条"
                  disabled={!canPrev}
                  onClick={() => step(-1)}
                />
                <IconBtn
                  icon={ChevronRight}
                  label="下一条"
                  disabled={!canNext}
                  onClick={() => step(1)}
                />
              </>
            )}
          </footer>
        </>
      )}
      {zoom && pic && (
        <Modal
          title={pic.title || "抓拍原图"}
          wide
          close={() => setZoom(false)}
        >
          <img className="fullphoto" src={fileURL(pic.id)} alt="抓拍原图" />
        </Modal>
      )}
    </aside>
  );
}
