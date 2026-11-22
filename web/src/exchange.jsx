import React, { useState } from "react";
import {
  Action,
  api,
  date,
  Empty,
  Head,
  Load,
  names,
  Pager,
  useData,
  usePageSize,
} from "./shared";
import { DeviceFilter } from "./device-filter";

const states = {
  paused: "已暂停",
  expired: "订阅已到期",
  pending: "待投递",
  sent: "已确认",
  failed: "重试已停止",
};
export function ExchangePage({ rev, refresh, notify }) {
  const [target, setTarget] = useState(""),
    [kind, setKind] = useState(""),
    [device, setDevice] = useState("");
  const [page, setPage] = useState(1),
    [size, setSize] = usePageSize(),
    [busy, setBusy] = useState(false);
  const settings = useData("exchange/settings", rev),
    jobs = useData("exchange/deliveries?page=" + page + "&size=" + size, rev);
  async function change(path, method, body) {
    setBusy(true);
    try {
      const result = await api("exchange/" + path, method, body);
      if (path.endsWith("/sync"))
        notify("目录已同步，采集设备 " + result.APEs + " 台");
      refresh();
      return true;
    } catch (e) {
      refresh();
      notify(e.message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Head
        title="订阅与级联"
        description="同步下级目录、接收订阅通知，并向上级投递记录"
      >
        <Action onClick={refresh}>刷新</Action>
      </Head>
      {settings.data && (
        <LowerSubscriptions
          settings={settings.data}
          busy={busy}
          change={change}
        />
      )}
      <section className="card">
        <div className="cardhead">
          <h2>订阅规则</h2>
        </div>
        {!settings.data ? (
          <Load error={settings.error} />
        ) : (
          <>
            {Object.keys(settings.data.peers).length ? (
              <div className="feedfilters">
                <label>
                  交换目标
                  <select
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                  >
                    <option value="">请选择目标</option>
                    {Object.entries(settings.data.peers).map(([name, mode]) => (
                      <option key={name} value={name}>
                        {name} · {mode === "viid" ? "上级视图库" : "业务回调"}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  订阅类型
                  <select
                    value={kind}
                    onChange={(e) => setKind(e.target.value)}
                  >
                    <option value="">全部记录</option>
                    {Object.entries(names)
                      .slice(0, 4)
                      .map(([k, n]) => (
                        <option key={k} value={k}>
                          {n}
                        </option>
                      ))}
                  </select>
                </label>
                <DeviceFilter value={device} onChange={setDevice} />
                <Action
                  primary
                  disabled={busy || !target}
                  onClick={() =>
                    change("subscriptions", "POST", { target, kind, device })
                  }
                >
                  新增订阅
                </Action>
              </div>
            ) : (
              <Empty text="尚未配置交换目标。请按 README 设置 VIID_PEERS_FILE 后重启服务。" />
            )}
            {!!settings.data.subscriptions.length && (
              <div className="tablewrap">
                <table>
                  <thead>
                    <tr>
                      {["目标", "类型", "来源设备", "状态", "操作"].map((t) => (
                        <th key={t}>{t}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {settings.data.subscriptions.map((s) => (
                      <tr key={s.id}>
                        <td>{s.target}</td>
                        <td>{names[s.kind] || "全部"}</td>
                        <td>{s.device_id || "全部"}</td>
                        <td>{s.enabled ? "启用" : "暂停"}</td>
                        <td>
                          {s.external_id ? (
                            "协议订阅"
                          ) : (
                            <button
                              className="textbtn"
                              disabled={busy}
                              onClick={() =>
                                change("subscriptions/" + s.id, "PATCH", {
                                  enabled: !s.enabled,
                                })
                              }
                            >
                              {s.enabled ? "暂停" : "恢复"}
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </section>
      <section className="card">
        <div className="cardhead">
          <h2>投递记录</h2>
        </div>
        {!jobs.data ? (
          <Load error={jobs.error} />
        ) : (
          <>
            <div className="tablewrap">
              <table>
                <thead>
                  <tr>
                    {[
                      "记录编号",
                      "交换目标",
                      "状态",
                      "尝试次数",
                      "最近更新",
                      "结果说明",
                      "操作",
                    ].map((t) => (
                      <th key={t}>{t}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {jobs.data.items.map((j) => (
                    <tr key={j.id}>
                      <td>{j.record_id}</td>
                      <td>{j.target}</td>
                      <td>{states[j.state]}</td>
                      <td>{j.attempts}</td>
                      <td>{date(j.updated)}</td>
                      <td>{j.message || "—"}</td>
                      <td>
                        {j.state === "failed" && (
                          <button
                            disabled={busy}
                            className="textbtn"
                            onClick={() =>
                              change("deliveries/" + j.id + "/retry", "POST")
                            }
                          >
                            重新投递
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!jobs.data.total && <Empty text="尚无投递记录" />}
            <Pager
              page={jobs.data.page}
              size={size}
              total={jobs.data.total}
              onPage={setPage}
              onSize={(n) => {
                setSize(n);
                setPage(1);
              }}
            />
          </>
        )}
      </section>
    </>
  );
}

function LowerSubscriptions({ settings, busy, change }) {
  const [peer, setPeer] = useState("");
  const [kind, setKind] = useState("person");
  const [device, setDevice] = useState("");
  const [show, setShow] = useState(false);
  const path = "lower/" + encodeURIComponent(peer);
  const labels = {
    pending: "待确认",
    active: "订阅中",
    cancelling: "取消待确认",
    cancelled: "已取消",
  };
  return (
    <section className="card">
      <div className="cardhead">
        <h2>下级平台接入</h2>
      </div>
      {settings.lowers.length ? (
        <>
          <div className="feedfilters">
            <label>
              下级平台
              <select
                value={peer}
                onChange={(e) => {
                  setPeer(e.target.value);
                  setDevice("");
                  setShow(false);
                }}
              >
                <option value="">请选择平台</option>
                {settings.lowers.map((p) => (
                  <option key={p}>{p}</option>
                ))}
              </select>
            </label>
            <Action
              disabled={!peer || busy}
              onClick={() => change(path + "/sync", "POST", {})}
            >
              同步设备目录
            </Action>
            <Action
              primary
              disabled={!peer || busy}
              onClick={() => setShow(!show)}
            >
              订阅下级
            </Action>
          </div>
          {show && (
            <form
              className="feedfilters"
              onSubmit={async (e) => {
                e.preventDefault();
                const input = Object.fromEntries(new FormData(e.currentTarget));
                if (
                  await change(path + "/subscribe", "POST", {
                    ...input,
                    kind,
                    device,
                  })
                )
                  setShow(false);
              }}
            >
              <label>
                标题
                <input name="Title" required />
              </label>
              <label>
                对象类别
                <select value={kind} onChange={(e) => setKind(e.target.value)}>
                  {["person", "face", "motor", "nonmotor"].map((k) => (
                    <option key={k} value={k}>
                      {names[k]}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                设备编号
                <input
                  value={device}
                  onChange={(e) => setDevice(e.target.value)}
                  placeholder="留空订阅整个平台"
                />
              </label>
              <label>
                申请人
                <input name="ApplicantName" required />
              </label>
              <label>
                申请单位
                <input name="ApplicantOrg" required />
              </label>
              <label>
                截止时间
                <input type="datetime-local" name="EndTime" required />
              </label>
              <Action type="submit" primary disabled={busy}>
                提交订阅
              </Action>
            </form>
          )}
        </>
      ) : (
        <Empty text="尚未配置下级平台。在 VIID_PEERS_FILE 中添加 lower 目标后重启服务。" />
      )}
      {!!settings.lowerTasks.length && (
        <div className="tablewrap">
          <table>
            <thead>
              <tr>
                {[
                  "下级平台",
                  "订阅编号",
                  "对象类别",
                  "资源范围",
                  "截止时间",
                  "状态",
                  "已收通知",
                  "操作",
                ].map((x) => (
                  <th key={x}>{x}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {settings.lowerTasks.map((task) => (
                <tr key={task.id}>
                  <td>{task.target}</td>
                  <td>{task.id}</td>
                  <td>{names[task.kind]}</td>
                  <td>{task.source}</td>
                  <td>{date(task.ends)}</td>
                  <td>
                    {task.state === "active" && new Date(task.ends) < new Date()
                      ? "已到期"
                      : labels[task.state]}
                  </td>
                  <td>{task.received}</td>
                  <td>
                    {["pending", "cancelling"].includes(task.state) && (
                      <button
                        className="textbtn"
                        disabled={busy}
                        onClick={() =>
                          change(
                            "lower-tasks/" + task.id + "/retry",
                            "POST",
                            {},
                          )
                        }
                      >
                        重试
                      </button>
                    )}
                    {["active", "pending"].includes(task.state) && (
                      <button
                        className="textbtn"
                        disabled={busy}
                        onClick={() =>
                          change(
                            "lower-tasks/" + task.id + "/cancel",
                            "POST",
                            {},
                          )
                        }
                      >
                        取消订阅
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
