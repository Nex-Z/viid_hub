import React, { useEffect, useState } from "react";
import {
  ArrowDownUp,
  Bike,
  Camera,
  CarFront,
  Database,
  Download,
  Filter,
  Grid2X2,
  HardDrive,
  Image,
  List,
  Monitor,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  Upload,
  UsersRound,
  Wifi,
  WifiOff,
} from "lucide-react";
import {
  Action,
  api,
  bytes,
  date,
  Dot,
  exportItems,
  Empty,
  Head,
  IconBtn,
  Input,
  Load,
  Modal,
  names,
  num,
  Pager,
  Photo,
  Tabs,
  Tag,
  useData,
  usePageSize,
} from "./shared";
import { Metric } from "./overview";
import { Inspector } from "./inspector";
import { Editor } from "./forms";
import { DeviceFilter } from "./device-filter";
import { BatchDevices } from "./device-batch";
const pageInfo = {
  devices: ["设备管理", "接入设备的注册、状态监控与运维管理"],
  images: ["图片资源管理", "查询、预览与管理设备采集和手动上传的图片资源"],
  vehicles: ["车辆记录", "统一查询机动车与非机动车结构化抓拍记录"],
  person: ["人员记录", "接收、查询和管理人员结构化数据"],
  face: ["人脸记录", "查询人脸抓拍记录、采集设备与关联图片"],
  motor: ["机动车记录", "按设备、车牌和抓拍时间检索机动车结构化数据"],
  nonmotor: ["非机动车记录", "按时间、设备、车辆类型等条件筛选非机动车记录"],
};
function Metrics({ page, summary: s }) {
  if (!s) return null;
  let cards = [];
  const total = s.devices.reduce((a, r) => a + r.n, 0),
    count = (k) => s.devices.find((r) => r.status === k)?.n || 0,
    kind = (k) => s.totals.find((r) => r.kind === k) || {};
  if (page === "devices")
    cards = [
      [Monitor, "接入设备总数", total, "已登记设备"],
      [Wifi, "在线设备", count("在线"), "最近 5 分钟内心跳"],
      [WifiOff, "离线设备", count("离线"), "已注销或未注册"],
      [RefreshCw, "超时设备", count("超时"), "等待设备保活"],
    ];
  if (["motor", "vehicles"].includes(page))
    cards = [
      [CarFront, "机动车抓拍", kind("motor").recent || 0, "最近 24 小时"],
      [Bike, "非机动车抓拍", kind("nonmotor").recent || 0, "最近 24 小时"],
      [Monitor, "接入设备", total, "已登记设备"],
      [
        Database,
        "累计车辆记录",
        (kind("motor").n || 0) + (kind("nonmotor").n || 0),
        "机动车与非机动车",
      ],
    ];
  return cards.length ? (
    <div className="metrics compact">
      {cards.map(([I, n, v, f]) => (
        <Metric key={n} icon={I} label={n} value={num(v)} foot={f} />
      ))}
    </div>
  ) : null;
}
function FilterBox({
  resource,
  kind,
  values,
  set,
  apply,
  clear,
  expanded,
  appliedCount,
  expand,
}) {
  const dev = resource === "devices",
    img = resource === "images";
  const field = (k, v) => set({ ...values, [k]: v });
  const select = (key, label, choices) => (
    <label>
      {label}
      <select
        value={values[key] || ""}
        onChange={(e) => field(key, e.target.value)}
      >
        <option value="">
          {key === "timeField" ? "入库时间" : "全部" + label}
        </option>
        {choices.map(([k, s]) => (
          <option value={k} key={k}>
            {s}
          </option>
        ))}
      </select>
    </label>
  );
  return (
    <form
      className="filterbox card"
      onSubmit={(e) => {
        e.preventDefault();
        apply();
      }}
    >
      <div className="filterrow">
        <label>
          {dev
            ? "设备名称"
            : img
              ? "图片检索"
              : kind === "motor"
                ? "车牌 / 编号"
                : "关键词"}
          <Input
            placeholder={
              dev
                ? "请输入名称或点位"
                : img
                  ? "文件名、记录编号或车牌"
                  : "请输入名称或编号"
            }
            value={values.q || ""}
            onChange={(e) => field("q", e.target.value)}
          />
        </label>
        {dev ? (
          <>
            <label>
              设备编号
              <Input
                placeholder="请输入设备编号"
                value={values.id || ""}
                onChange={(e) => field("id", e.target.value)}
              />
            </label>
            {select(
              "status",
              "状态",
              ["在线", "离线", "超时", "停用"].map((x) => [x, x]),
            )}
          </>
        ) : (
          <>
            <DeviceFilter
              value={values.device || ""}
              onChange={(id) => field("device", id)}
            />
            {img
              ? select("linked", "关联状态", [
                  ["yes", "已关联"],
                  ["no", "未关联"],
                ])
              : null}
          </>
        )}
        <div className="filteractions">
          <Action primary icon={Search}>
            查询
          </Action>
          <Action type="button" onClick={clear}>
            重置
          </Action>
          {!dev && (
            <button
              type="button"
              className="textbtn"
              aria-expanded={expanded}
              onClick={expand}
            >
              <Filter size={14} />
              {expanded ? "收起" : "更多筛选"}
              {!expanded && appliedCount > 0 && `（${appliedCount}）`}
            </button>
          )}
        </div>
      </div>
      {expanded && !dev && (
        <div className="filterrow extra">
          {img && select("timeField", "时间字段", [["shot", "抓拍时间"]])}
          <label>
            {img && values.timeField !== "shot"
              ? "入库开始时间"
              : "抓拍开始时间"}
            <Input
              type="datetime-local"
              step="1"
              value={values.start || ""}
              onChange={(e) => field("start", e.target.value)}
            />
          </label>
          <label>
            {img && values.timeField !== "shot"
              ? "入库结束时间"
              : "抓拍结束时间"}
            <Input
              type="datetime-local"
              step="1"
              value={values.end || ""}
              onChange={(e) => field("end", e.target.value)}
            />
          </label>
          {!img && ["motor", "nonmotor"].includes(kind) && (
            <>
              <label>
                车辆类型
                <Input
                  placeholder="全部类型"
                  value={values.type || ""}
                  onChange={(e) => field("type", e.target.value)}
                />
              </label>
              <label>
                车身颜色
                <Input
                  placeholder="全部颜色"
                  value={values.color || ""}
                  onChange={(e) => field("color", e.target.value)}
                />
              </label>
            </>
          )}
        </div>
      )}
    </form>
  );
}
function columns(resource, kind) {
  if (resource === "devices")
    return [
      ["设备名称", (r) => <b>{r.name}</b>],
      ["设备编号", (r) => r.id],
      ["在线状态", (r) => <Dot value={r.status} />],
      ["设备类型", (r) => r.kind],
      ["IP 地址", (r) => r.ip || "—"],
      ["最后心跳", (r) => date(r.heartbeat)],
      ["今日上报", (r) => num(r.reports)],
    ];
  if (resource === "images")
    return [
      ["缩略图", (r) => <Photo id={r.id} />],
      ["关联记录", (r) => r.related || "—"],
      ["文件名", (r) => r.title],
      ["图片类型", (r) => <Tag kind={r.kind} />],
      ["状态", (r) => <Dot value={r.record_id ? "已关联" : "未关联"} />],
      ["入库时间", (r) => date(r.ingested_at)],
      ["抓拍时间", (r) => date(r.shot_at)],
      ["来源设备", (r) => r.device || "手动上传"],
      ["大小", (r) => bytes(r.bytes)],
    ];
  const captureInfo = [
    ["抓拍时间", (r) => date(r.captured)],
    ["设备名称", (r) => r.device],
  ];
  if (["person", "face"].includes(kind))
    return [
      ["抓拍图", (r) => <Photo id={r.image} />],
      ["姓名", (r) => r.raw.Name || "—"],
      ["证件号码", (r) => r.raw.IDNumber || "—"],
      ["性别", (r) => r.raw.GenderCode || "—"],
      ["年龄", (r) => r.raw.Age ?? r.raw.AgeUpLimit ?? "—"],
      ...captureInfo,
    ];
  return [
    ["抓拍图", (r) => <Photo id={r.image} />],
    ...(kind === "motor"
      ? [
          [
            "车牌号码",
            (r) => <span className="plate">{r.raw.PlateNo || "—"}</span>,
          ],
        ]
      : []),
    ["车辆类型", (r) => r.raw.VehicleClass || "—"],
    ["颜色", (r) => r.raw.VehicleColor || "—"],
    [
      kind === "motor" ? "车速" : "骑行人数",
      (r) =>
        kind === "motor"
          ? r.raw.Speed != null
            ? r.raw.Speed + " km/h"
            : "—"
          : (r.raw.RiderCount ?? r.raw.CyclingPersonNumber ?? "—"),
    ],
    ["行驶方向", (r) => r.raw.Direction || "—"],
    ...captureInfo,
  ];
}
function Grid({ rows, focused, open, checked, toggle }) {
  return (
    <div className="photogrid">
      {rows.map((r) => (
        <div
          key={r.id}
          className={"tile " + (focused === r.id ? "selected" : "")}
        >
          <button onClick={() => open(r.id)}>
            <Photo id={r.image} />
            <div>
              <b>{r.title}</b>
              <small>
                {names[r.kind]}{" "}
                <time>
                  {Object.hasOwn(r, "ingested_at")
                    ? "入库 " + date(r.ingested_at)
                    : date(r.captured)}
                </time>
              </small>
            </div>
          </button>
          {toggle && (
            <input
              type="checkbox"
              aria-label={"选择 " + r.title}
              checked={checked.includes(r.id)}
              onChange={() => toggle(r.id)}
            />
          )}
        </div>
      ))}
    </div>
  );
}
export function Catalog({
  page,
  initialId,
  rev,
  refresh,
  notify,
  go,
  embedded = false,
}) {
  const resource =
    page === "devices" ? "devices" : page === "images" ? "images" : "records";
  const [sub, setSub] = useState(page === "vehicles" ? "motor" : ""),
    [draft, setDraft] = useState({}),
    [filters, setFilters] = useState({}),
    [expanded, setExpanded] = useState(false),
    [pg, setPg] = useState(1),
    [size, setSize] = usePageSize(),
    [asc, setAsc] = useState(false),
    [grid, setGrid] = useState(false),
    [checked, setChecked] = useState([]),
    [id, setId] = useState(initialId || ""),
    [editor, setEditor] = useState(null),
    [confirm, setConfirm] = useState(false),
    [busy, setBusy] = useState(false);
  const kind = page === "vehicles" || page === "images" ? sub : page;
  const qs = new URLSearchParams({
    ...filters,
    page: String(pg),
    size: String(size),
    ...(resource !== "devices" && kind ? { kind } : {}),
    sort: asc ? "asc" : "desc",
  });
  const { data, error } = useData(resource + "?" + qs, rev),
    stats = useData("overview", rev);
  const [batch, setBatch] = useState(""),
    [showColumns, setShowColumns] = useState(false),
    [hidden, setHidden] = useState([]);
  const allCols = columns(resource, kind);
  const rows = data?.items || [],
    cols = allCols.filter(([label]) => !hidden.includes(label)),
    idx = rows.findIndex((r) => r.id === id);
  useEffect(() => {
    setId(initialId || "");
  }, [initialId]);
  useEffect(() => {
    setChecked([]);
  }, [qs.toString()]);
  const resetPage = () => {
    setPg(1);
    setId("");
  };
  const toggle = (x) =>
    setChecked((v) => (v.includes(x) ? v.filter((i) => i !== x) : [...v, x]));
  const step = (d) => setId(rows[idx + d].id);
  const apply = () => {
    if (draft.start && draft.end && draft.start > draft.end) {
      notify("结束时间须晚于开始时间");
      return;
    }
    setFilters(draft);
    resetPage();
  };
  const changeSub = (v) => {
    if (v !== sub) setExpanded(false);
    setSub(v);
    resetPage();
  };
  const edit = (action, item) => setEditor({ action, item });
  async function remove() {
    setBusy(true);
    try {
      for (const item of checked)
        await api(resource + "/" + encodeURIComponent(item), "DELETE");
      notify("已删除 " + checked.length + " 项");
    } catch (e) {
      notify(e.message);
    } finally {
      setId("");
      setChecked([]);
      setConfirm(false);
      setBusy(false);
      refresh();
    }
  }
  return (
    <div
      className={
        "catalog " + (id ? "inspecting " : "") + (embedded ? "embedded" : "")
      }
    >
      <div className="listpane">
        {!embedded && (
          <Head title={pageInfo[page][0]} description={pageInfo[page][1]} />
        )}
        {(page === "images" || page === "vehicles") && (
          <Tabs
            value={sub}
            onChange={changeSub}
            items={
              page === "images"
                ? [
                    ["", "全部图片"],
                    ...Object.entries(names).map(([k, n]) => [k, n + "图片"]),
                  ]
                : [
                    ["motor", "机动车"],
                    ["nonmotor", "非机动车"],
                  ]
            }
          />
        )}
        {!embedded && <Metrics page={page} summary={stats.data} />}
        <FilterBox
          resource={resource}
          kind={kind}
          values={draft}
          set={setDraft}
          expanded={expanded}
          appliedCount={
            ["start", "end", "type", "color", "timeField"].filter(
              (key) => filters[key],
            ).length
          }
          expand={() => setExpanded(!expanded)}
          apply={apply}
          clear={() => {
            setDraft({});
            setFilters({});
            resetPage();
          }}
        />
        <section className="card results">
          <div className="toolbar">
            {resource === "devices" ? (
              <Action icon={Plus} primary onClick={() => edit("device")}>
                新增设备
              </Action>
            ) : resource === "images" ? (
              <Action icon={Upload} primary onClick={() => edit("upload")}>
                上传图片
              </Action>
            ) : (
              <b>共 {num(data?.total)} 条记录</b>
            )}
            {resource === "devices" && (
              <>
                <Action small icon={Upload} onClick={() => setBatch("import")}>
                  批量导入
                </Action>
                {checked.length > 0 && (
                  <>
                    <Action small onClick={() => setBatch("enable")}>
                      批量启用
                    </Action>
                    <Action small onClick={() => setBatch("disable")}>
                      批量停用
                    </Action>
                  </>
                )}
              </>
            )}
            <Action
              small
              icon={Download}
              disabled={!rows.length}
              onClick={() =>
                exportItems(
                  resource,
                  checked.length ? checked : rows.map((r) => r.id),
                  pageInfo[page][0],
                ).catch((e) => notify(e.message))
              }
            >
              {checked.length ? "导出所选" : "导出本页"}
            </Action>
            {resource !== "devices" && (
              <IconBtn
                label="删除所选"
                icon={Trash2}
                disabled={!checked.length}
                onClick={() => setConfirm(true)}
              />
            )}
            <span className="flexfill" />
            {!!checked.length && <small>已选 {checked.length} 项</small>}
            {resource !== "devices" && (
              <div className="switcher">
                <IconBtn
                  label="表格视图"
                  icon={List}
                  className={!grid ? "current" : ""}
                  onClick={() => setGrid(false)}
                />
                <IconBtn
                  label="图片视图"
                  icon={Grid2X2}
                  className={grid ? "current" : ""}
                  onClick={() => setGrid(true)}
                />
              </div>
            )}
            <IconBtn
              icon={Filter}
              label="设置表格列"
              onClick={() => setShowColumns(true)}
            />
            <IconBtn icon={RefreshCw} label="刷新列表" onClick={refresh} />
          </div>
          {!data ? (
            <Load error={error} />
          ) : !rows.length ? (
            <Empty />
          ) : grid ? (
            <Grid
              rows={rows}
              focused={id}
              open={setId}
              checked={checked}
              toggle={toggle}
            />
          ) : (
            <div className="tablewrap">
              <table>
                <thead>
                  <tr>
                    <th>
                      <input
                        type="checkbox"
                        aria-label="全选本页"
                        checked={
                          rows.length > 0 && checked.length === rows.length
                        }
                        onChange={(e) =>
                          setChecked(
                            e.target.checked ? rows.map((r) => r.id) : [],
                          )
                        }
                      />
                    </th>
                    {cols.map(([label]) => (
                      <th key={label}>
                        {label ===
                        (resource === "images" && filters.timeField !== "shot"
                          ? "入库时间"
                          : "抓拍时间") ? (
                          <button
                            onClick={() => {
                              setAsc(!asc);
                              resetPage();
                            }}
                          >
                            {label} <ArrowDownUp size={12} />
                          </button>
                        ) : (
                          label
                        )}
                      </th>
                    ))}
                    <th className="lastcol">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => (
                    <tr
                      key={r.id}
                      className={r.id === id ? "selected" : ""}
                      onClick={() => setId(r.id)}
                    >
                      <td onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={checked.includes(r.id)}
                          aria-label={"选择 " + (r.title || r.name)}
                          onChange={() => toggle(r.id)}
                        />
                      </td>
                      {cols.map(([k, render]) => (
                        <td key={k}>{render(r)}</td>
                      ))}
                      <td className="lastcol">
                        <button className="textbtn" onClick={() => setId(r.id)}>
                          查看
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {data && (
            <Pager
              page={data.page}
              size={size}
              total={data.total}
              onPage={(n) => {
                setPg(n);
                setId("");
              }}
              onSize={(n) => {
                setSize(n);
                resetPage();
              }}
            />
          )}
        </section>
      </div>
      {id && (
        <Inspector
          key={id}
          notify={notify}
          id={id}
          resource={resource}
          close={() => setId("")}
          rev={rev}
          edit={edit}
          go={go}
          canPrev={idx > 0}
          canNext={idx >= 0 && idx < rows.length - 1}
          step={step}
        />
      )}
      {editor && (
        <Editor
          {...editor}
          close={() => setEditor(null)}
          done={(newId) => {
            setEditor(null);
            refresh();
            if (newId) setId(newId);
            notify("已保存");
          }}
        />
      )}
      {batch && (
        <BatchDevices
          mode={batch}
          devices={
            batch === "import" ? [] : rows.filter((r) => checked.includes(r.id))
          }
          close={() => setBatch("")}
          onFinish={(n) => {
            setBatch("");
            setChecked([]);
            refresh();
            notify("已处理 " + n + " 台设备");
          }}
        />
      )}
      {showColumns && (
        <Modal title="显示列" close={() => setShowColumns(false)}>
          <div className="columnchoices">
            {allCols.map(([k]) => (
              <label key={k}>
                <input
                  type="checkbox"
                  checked={!hidden.includes(k)}
                  disabled={cols.length === 1 && !hidden.includes(k)}
                  onChange={() =>
                    setHidden((v) =>
                      v.includes(k) ? v.filter((x) => x !== k) : [...v, k],
                    )
                  }
                />
                {k}
              </label>
            ))}
          </div>
        </Modal>
      )}
      {confirm && (
        <Modal title="删除所选数据" close={() => !busy && setConfirm(false)}>
          <p>
            确认永久删除所选的 {checked.length} 项？
            {resource === "records" ? "关联图片会保留在图片资源库。" : ""}
          </p>
          <div className="formactions">
            <Action disabled={busy} onClick={() => setConfirm(false)}>
              取消
            </Action>
            <Action disabled={busy} primary onClick={remove}>
              确认删除
            </Action>
          </div>
        </Modal>
      )}
    </div>
  );
}
export function ImageWorkspace(props) {
  const [pg, setPg] = useState(1),
    [kind, setKind] = useState(""),
    [tab, setTab] = useState("images"),
    [editor, setEditor] = useState(false);
  const { data, error } = useData(
      "images?size=9&page=" + pg + "&kind=" + kind,
      props.rev,
    ),
    stats = useData("overview", props.rev);
  const s = stats.data;
  return (
    <>
      <Head
        title="图像数据管理"
        description="集中管理抓拍图片与人员结构化数据，支持检索、预览与关联查询"
      />
      <Tabs
        value={tab}
        onChange={setTab}
        items={[
          ["images", "图片资源"],
          ["people", "人员记录"],
        ]}
      />
      {s && (
        <div className="metrics compact">
          {[
            [Image, "图片总量", num(s.storage[0].n)],
            [
              UsersRound,
              "人员记录",
              num(s.totals.find((r) => r.kind === "person")?.n),
            ],
            [HardDrive, "存储占用", bytes(s.storage[0].bytes)],
            [Camera, "近 24 小时新增", num(s.storage[0].recent)],
          ].map(([I, l, v]) => (
            <Metric key={l} label={l} icon={I} value={v} />
          ))}
        </div>
      )}
      <div className={tab === "images" ? "workspacegrid" : "workspacepeople"}>
        {tab === "images" && (
          <section className="card gallerypane">
            <div className="toolbar">
              <Action icon={Upload} primary onClick={() => setEditor(true)}>
                上传图片
              </Action>
              <span className="flexfill" />
              <select
                aria-label="图片类型"
                value={kind}
                onChange={(e) => {
                  setKind(e.target.value);
                  setPg(1);
                }}
              >
                <option value="">全部类型</option>
                {Object.entries(names).map(([k, n]) => (
                  <option key={k} value={k}>
                    {n}
                  </option>
                ))}
              </select>
            </div>
            {data ? (
              <>
                <Grid rows={data.items} open={(id) => props.go("images", id)} />
                {!data.items.length && <Empty />}
                <div className="gallerypager">
                  <span>共 {num(data.total)} 张</span>
                  <button disabled={pg <= 1} onClick={() => setPg(pg - 1)}>
                    上一页
                  </button>
                  {pg}
                  <button
                    disabled={pg * 9 >= data.total}
                    onClick={() => setPg(pg + 1)}
                  >
                    下一页
                  </button>
                </div>
              </>
            ) : (
              <Load error={error} />
            )}
          </section>
        )}
        <section>
          <h2 className="workspaceheading">人员记录</h2>
          <Catalog {...props} page="person" initialId="" embedded />
        </section>
      </div>
      {editor && (
        <Editor
          action="upload"
          close={() => setEditor(false)}
          done={() => {
            setEditor(false);
            props.refresh();
            props.notify("图片已上传");
          }}
        />
      )}
    </>
  );
}
