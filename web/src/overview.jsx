import React, { useState } from "react";
import { DeviceFilter } from "./device-filter";
import {
  ArrowDownRight,
  ArrowUpRight,
  Bike,
  Camera,
  CarFront,
  Check,
  ChevronRight,
  Database,
  HardDrive,
  Image,
  Monitor,
  RefreshCw,
  TriangleAlert,
  UsersRound,
} from "lucide-react";
import {
  Action,
  bytes,
  date,
  Dot,
  Empty,
  Head,
  Load,
  names,
  num,
  Photo,
  Pager,
  usePageSize,
  Tag,
  useData,
} from "./shared";
const colors = {
  person: "#2563eb",
  motor: "#16a36a",
  nonmotor: "#f59e0b",
  face: "#8960e5",
};
function series(data, kind) {
  const arr = Array(24).fill(0);
  data.trend
    .filter((r) => !kind || r.kind === kind)
    .forEach((r) => (arr[Math.min(23, Math.max(0, r.bucket))] += r.n));
  return arr;
}
function pathFor(arr, w, h, max, pad = 0) {
  return arr
    .map(
      (n, i) =>
        (i ? "L" : "M") +
        ((i * w) / Math.max(1, arr.length - 1)).toFixed(1) +
        " " +
        (pad + h - (n / Math.max(max, 1)) * h).toFixed(1),
    )
    .join(" ");
}
function Spark({ arr, id }) {
  const d = pathFor(arr, 95, 29, Math.max(...arr), 2);
  return (
    <svg className="spark" viewBox="0 0 95 34" aria-hidden="true">
      <defs>
        <linearGradient id={"fill" + id} x2="0" y2="1">
          <stop stopColor="#2563eb" stopOpacity=".2" />
          <stop offset="1" stopColor="#2563eb" stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={d + " L95 34 L0 34Z"} fill={"url(#fill" + id + ")"} />
      <path
        d={d}
        fill="none"
        stroke="#3775ef"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
    </svg>
  );
}
export function Metric({ icon: Icon, label, value, foot, trend, children }) {
  return (
    <article className="metric card">
      <div className="metricname">
        <span>
          <Icon size={18} />
        </span>
        {label}
      </div>
      <div className="metnum">{value}</div>
      <div className="metfoot">{foot}</div>
      {trend && <Spark arr={trend} id={label} />}
      {children}
    </article>
  );
}
function Change({ n, p }) {
  if (!p) return <span>上一周期 {num(p)} 条</span>;
  const pct = ((n - p) * 100) / p;
  return (
    <>
      <span className={pct >= 0 ? "up" : "down"}>
        {pct >= 0 ? <ArrowUpRight size={13} /> : <ArrowDownRight size={13} />}
        {Math.abs(pct).toFixed(1)}%
      </span>{" "}
      较上一周期
    </>
  );
}
function Trend({ data, home, hours }) {
  const [visible, setVisible] = useState(["person", "motor", "nonmotor"]),
    [single, setSingle] = useState(""),
    [hover, setHover] = useState(null);
  const kinds = home ? visible : [single],
    all = kinds.map((k) => series(data, k)),
    max = Math.max(4, Math.ceil(Math.max(...all.flat(), 1) / 4) * 4);
  const label = (i) =>
    date(new Date(data.at).getTime() - (((24 - i) * hours) / 24) * 3600000);
  return (
    <section className="card chartcard">
      <div className="cardhead">
        <h2>{home ? "数据接入趋势" : "抓拍数据趋势"}</h2>
        <div className="legends">
          {(home
            ? ["person", "motor", "nonmotor"]
            : ["", "person", "motor", "nonmotor"]
          ).map((k) => (
            <button
              key={k}
              className={!home && k === single ? "sel" : ""}
              aria-pressed={home ? visible.includes(k) : single === k}
              onClick={() =>
                home
                  ? setVisible((v) =>
                      v.includes(k) ? v.filter((x) => x !== k) : [...v, k],
                    )
                  : setSingle(k)
              }
            >
              {home && (
                <i
                  style={{
                    background: visible.includes(k) ? colors[k] : "#cbd5e1",
                  }}
                >
                  <Check size={10} />
                </i>
              )}
              {names[k] || "总量"}
            </button>
          ))}
        </div>
      </div>
      <div className="plot">
        <div className="yaxis">
          {[4, 3, 2, 1, 0].map((n) => (
            <span key={n}>{Math.round((max * n) / 4)}</span>
          ))}
        </div>
        <div
          className="plotmain"
          onMouseMove={(e) => {
            const r = e.currentTarget.getBoundingClientRect();
            setHover(
              Math.max(
                0,
                Math.min(23, Math.round((23 * (e.clientX - r.left)) / r.width)),
              ),
            );
          }}
          onMouseLeave={() => setHover(null)}
        >
          <svg
            viewBox="0 0 700 136"
            preserveAspectRatio="none"
            role="img"
            aria-label="按时间段统计的抓拍数量"
          >
            <defs>
              <linearGradient id="area" x2="0" y2="1">
                <stop stopColor="#3b82f6" stopOpacity=".24" />
                <stop offset="1" stopColor="#3b82f6" stopOpacity=".025" />
              </linearGradient>
            </defs>
            {all.map((arr, i) => {
              const d = pathFor(arr, 700, 132, max, 2);
              return (
                <g key={kinds[i]}>
                  {i === 0 && <path d={d + "L700 136H0Z"} fill="url(#area)" />}
                  <path
                    d={d}
                    fill="none"
                    stroke={colors[kinds[i]] || colors.person}
                    strokeWidth="2.4"
                    strokeLinejoin="round"
                    vectorEffect="non-scaling-stroke"
                  />
                  {home &&
                    arr.map((n, j) => (
                      <circle
                        key={j}
                        cx={(j * 700) / 23}
                        cy={134 - (n / max) * 132}
                        r="2.5"
                        fill="white"
                        stroke={colors[kinds[i]]}
                      />
                    ))}
                </g>
              );
            })}
          </svg>
          {hover !== null && (
            <div
              className="plotTip"
              style={{ left: Math.min(72, (hover / 23) * 100) + "%" }}
            >
              {label(hover)}
              {all.map((v, i) => (
                <small key={i}>
                  {names[kinds[i]] || "总量"}　{v[hover]} 条
                </small>
              ))}
            </div>
          )}
          <div className="xaxis">
            {[0, 4, 8, 12, 16, 20, 23].map((i) => {
              const timestamp = label(i);
              return (
                <time key={i} title={timestamp}>
                  {hours === 24
                    ? timestamp.slice(11, 16)
                    : timestamp.slice(5, 10)}
                </time>
              );
            })}
          </div>
        </div>
      </div>
    </section>
  );
}
function Health({ data, go }) {
  const count = Object.fromEntries(data.devices.map((r) => [r.status, r.n])),
    all = data.devices.reduce((s, r) => s + r.n, 0),
    online = count["在线"] || 0,
    off = (count["离线"] || 0) + (count["停用"] || 0),
    late = count["超时"] || 0;
  return (
    <section className="card health">
      <div className="cardhead">
        <h2>设备健康状态</h2>
        <button className="textbtn" onClick={() => go("devices")}>
          全部设备 <ChevronRight size={14} />
        </button>
      </div>
      <div className="healthbody">
        <div
          className="ring"
          style={{
            background: all
              ? "conic-gradient(#16ad66 0 " +
                (online / all) * 100 +
                "%,#a7b4c7 0 " +
                ((online + off) / all) * 100 +
                "%,#f5a524 0)"
              : "#e5eaf1",
          }}
        >
          <div>
            <strong>{num(all)}</strong>
            <small>设备总数</small>
          </div>
        </div>
        <div className="healthlegend">
          {[
            ["在线", online],
            ["离线 / 停用", off],
            ["超时", late],
          ].map(([k, n]) => (
            <p key={k}>
              <Dot value={k} />
              <b>{n}</b>
              <small>{all ? ((100 * n) / all).toFixed(1) : "0"}%</small>
            </p>
          ))}
        </div>
      </div>
    </section>
  );
}
function Alerts({ data, go, full = false, rev }) {
  const [page, setPage] = useState(1),
    [size, setSize] = usePageSize();
  const result = useData(
    full ? "devices?status=alerts&page=" + page + "&size=" + size : null,
    rev,
  );
  const alerts = full ? result.data?.items : data.alerts.slice(0, 5);
  return (
    <section className="card">
      <div className="cardhead">
        <h2>{full ? "设备告警" : "最近告警"}</h2>
        {!full && (
          <button className="textbtn" onClick={() => go("monitor")}>
            全部告警 <ChevronRight size={14} />
          </button>
        )}
      </div>
      <div className="alertlist">
        {alerts?.map((r) => (
          <button key={r.id} onClick={() => go("devices", r.id)}>
            <span
              className={"severity " + (r.status === "超时" ? "amber" : "red")}
            >
              {r.status === "超时" ? "中" : "高"}
            </span>
            <span>
              <b>{r.status === "超时" ? "设备心跳超时" : "设备离线"}</b>
              <small>{r.name}</small>
            </span>
            <time>{date(r.heartbeat)}</time>
          </button>
        ))}
        {alerts?.length === 0 && <Empty text="当前没有设备告警" />}
        {!alerts && <Load error={result.error} />}
        {full && result.data && (
          <Pager
            page={result.data.page}
            size={size}
            total={result.data.total}
            onPage={setPage}
            onSize={(n) => {
              setSize(n);
              setPage(1);
            }}
          />
        )}
      </div>
    </section>
  );
}
export function Recent({ data, go, home, full = false, hours, rev }) {
  const [initial] = useState(
    () => new URLSearchParams(full ? location.hash.split("?")[1] : ""),
  );
  const [kind, setKind] = useState(initial.get("kind") || ""),
    [device, setDevice] = useState(initial.get("device") || "");
  const [start, setStart] = useState(initial.get("start") || ""),
    [end, setEnd] = useState(initial.get("end") || "");
  const [advanced, setAdvanced] = useState(false),
    [page, setPage] = useState(1),
    [size, setSize] = usePageSize();
  const boundary = (offset) =>
    date(new Date(data.at).getTime() - offset).replace(" ", "T");
  const filters = new URLSearchParams({
    kind,
    device,
    start: start || (full ? "" : boundary(hours * 3600000)),
    end: end || (full ? "" : boundary(0)),
  });
  const invalid =
    filters.get("start") &&
    filters.get("end") &&
    filters.get("start") > filters.get("end");
  const result = useData(
    invalid
      ? null
      : "records?" +
          filters +
          "&page=" +
          page +
          "&size=" +
          (full ? size : home ? 6 : 10),
    rev,
  );
  const records = result.data?.items || [];
  const change = (setter, value) => {
    setter(value);
    setPage(1);
  };
  const count = [start, end].filter(Boolean).length;
  return (
    <section className="card recent">
      <div className="cardhead">
        <h2>
          {full ? "全部接入记录" : home ? "实时接入记录" : "近期抓拍记录"}
        </h2>
        {!full && (
          <button
            className="textbtn"
            disabled={!!invalid}
            onClick={() => {
              location.hash = "feed?" + filters;
            }}
          >
            查看更多 <ChevronRight size={14} />
          </button>
        )}
      </div>
      <div className="feedfilters">
        <select
          aria-label="抓拍类型"
          value={kind}
          onChange={(e) => change(setKind, e.target.value)}
        >
          <option value="">全部类型</option>
          {Object.entries(names)
            .slice(0, 4)
            .map(([k, n]) => (
              <option key={k} value={k}>
                {n}
              </option>
            ))}
        </select>
        <DeviceFilter value={device} onChange={(id) => change(setDevice, id)} />
        <button
          className="textbtn"
          aria-expanded={advanced}
          onClick={() => setAdvanced(!advanced)}
        >
          {advanced ? "收起" : "更多筛选" + (count ? "（" + count + "）" : "")}
        </button>
        {advanced && (
          <>
            <label>
              抓拍开始
              <input
                type="datetime-local"
                step="1"
                value={start}
                onChange={(e) => change(setStart, e.target.value)}
              />
            </label>
            <label>
              抓拍结束
              <input
                type="datetime-local"
                step="1"
                value={end}
                onChange={(e) => change(setEnd, e.target.value)}
              />
            </label>
          </>
        )}
        {(kind || device || count > 0) && (
          <button
            className="textbtn"
            onClick={() => {
              setKind("");
              setDevice("");
              setStart("");
              setEnd("");
              setPage(1);
            }}
          >
            重置
          </button>
        )}
      </div>
      <div className="tablewrap">
        <table>
          <thead>
            <tr>
              {[
                "抓拍图",
                "对象信息",
                "类型",
                "抓拍时间",
                "设备名称",
                ...(home ? [] : ["点位位置"]),
              ].map((x) => (
                <th key={x}>{x}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {records.map((r) => (
              <tr key={r.id} onClick={() => go(r.kind, r.id)}>
                <td>
                  <Photo id={r.image} />
                </td>
                <td>
                  <button className="textbtn" onClick={() => go(r.kind, r.id)}>
                    {r.title}
                  </button>
                </td>
                <td>
                  <Tag kind={r.kind} />
                </td>
                <td className="digits">{date(r.captured)}</td>
                <td>{r.device}</td>
                {!home && <td>{r.point}</td>}
              </tr>
            ))}
          </tbody>
        </table>
        {invalid ? (
          <Empty text="结束时间须晚于开始时间" />
        ) : !result.data ? (
          <Load error={result.error} />
        ) : (
          !records.length && <Empty />
        )}
      </div>
      {full && result.data && !invalid && (
        <Pager
          page={result.data.page}
          size={size}
          total={result.data.total}
          onPage={setPage}
          onSize={(n) => {
            setSize(n);
            setPage(1);
          }}
        />
      )}
    </section>
  );
}
export function Dashboard({ page, go, rev, refresh }) {
  const home = page === "home",
    [hours, setHours] = useState(24);
  const { data, error } = useData("overview?hours=" + hours, rev);
  const head = (
    <Head
      title={home ? "工作台" : "平台概览"}
      description={
        home
          ? "智能交通数据接入、管理与交换平台"
          : "查看设备接入、数据采集、告警与存储使用情况"
      }
    >
      <select
        aria-label="统计时间范围"
        value={hours}
        onChange={(e) => setHours(+e.target.value)}
      >
        <option value="24">最近 24 小时</option>
        <option value="168">最近 7 天</option>
        <option value="720">最近 30 天</option>
      </select>
      <Action primary icon={RefreshCw} onClick={refresh}>
        刷新数据
      </Action>
    </Head>
  );
  if (!data)
    return (
      <>
        {head}
        <Load error={error} />
      </>
    );
  const totals = Object.fromEntries(data.totals.map((r) => [r.kind, r])),
    store = data.storage[0],
    total = data.devices.reduce((s, r) => s + r.n, 0),
    online = data.devices.find((r) => r.status === "在线")?.n || 0;
  return (
    <>
      {head}
      <div className="metrics">
        {home ? (
          [
            ["person", UsersRound],
            ["motor", CarFront],
            ["nonmotor", Bike],
            ["image", Image],
          ].map(([k, Icon]) => {
            const v = k === "image" ? store : totals[k] || {};
            return (
              <Metric
                key={k}
                icon={Icon}
                label={names[k] || "图片"}
                value={num(v.recent)}
                foot={<Change n={v.recent || 0} p={v.previous || 0} />}
                trend={k === "image" ? undefined : series(data, k)}
              />
            );
          })
        ) : (
          <>
            <Metric
              icon={Monitor}
              label="在线设备"
              value={
                <>
                  {online}
                  <em> / {total}</em>
                </>
              }
              foot="当前设备连接"
            />
            <Metric
              icon={Camera}
              label="周期抓拍"
              value={num(data.totals.reduce((s, r) => s + r.recent, 0))}
              foot="条结构化记录"
              trend={series(data)}
            />
            <Metric
              icon={TriangleAlert}
              label="设备告警"
              value={data.devices
                .filter((r) => ["离线", "超时"].includes(r.status))
                .reduce((s, r) => s + r.n, 0)}
              foot="离线与心跳超时"
            />
            <Metric
              icon={Database}
              label="图片存储"
              value={bytes(store.bytes)}
              foot={num(store.n) + " 张图片"}
            />
          </>
        )}
      </div>
      <div className="dashgrid">
        <Trend data={data} home={home} hours={hours} />
        <Health data={data} go={go} />
        <Recent data={data} home={home} go={go} hours={hours} rev={rev} />
        {home && <Alerts data={data} go={go} />}
      </div>
      <div className="servicebar card">
        <span>
          <i className="dot green" />
          平台服务 <b>正常</b>
        </span>
        <span>
          <i className="dot green" />
          PostgreSQL <b>已连接</b>
        </span>
        <span>
          <HardDrive size={15} /> 图片存储 <b>{bytes(store.bytes)}</b>
        </span>
        <small title={date(data.at)}>更新于 {date(data.at).slice(11)}</small>
      </div>
    </>
  );
}
export function MonitorPage({ rev, refresh, go }) {
  const { data, error } = useData("overview", rev);
  return (
    <>
      <Head title="运行监控" description="查看设备在线状态、心跳超时与离线告警">
        <Action icon={RefreshCw} onClick={refresh}>
          刷新
        </Action>
      </Head>
      {data ? (
        <div className="monitorgrid">
          <Health data={data} go={go} />
          <Alerts data={data} go={go} rev={rev} full />
        </div>
      ) : (
        <Load error={error} />
      )}
    </>
  );
}
