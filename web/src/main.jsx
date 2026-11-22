import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  Bell,
  Bike,
  Boxes,
  CarFront,
  ChartColumn,
  ChevronDown,
  ChevronRight,
  House,
  Images,
  Layers3,
  LogOut,
  Monitor,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  ShieldCheck,
  UserRound,
  UsersRound,
  ScanFace,
} from "lucide-react";
import {
  Action,
  api,
  Empty,
  IconBtn,
  Input,
  Load,
  Modal,
  names,
  useData,
} from "./shared";
import { ExchangePage } from "./exchange";
import { AuditPage } from "./audit";
import { Dashboard, MonitorPage, Recent } from "./overview";
import { Catalog, ImageWorkspace } from "./records";
import "./styles.css";
const groups = [
  [
    "工作台",
    [
      ["home", "工作台", House],
      ["overview", "平台概览", ChartColumn],
      ["monitor", "运行监控", Activity],
      ["feed", "接入流水", Layers3],
      ["audit", "操作审计", Activity],
    ],
  ],
  [
    "资源管理",
    [
      ["devices", "设备管理", Monitor],
      ["images", "图片资源", Images],
      ["workspace", "图像数据管理", Layers3],
    ],
  ],
  ["数据交换", [["exchange", "订阅与级联", Layers3]]],
  [
    "结构化数据",
    [
      ["person", "人员记录", UsersRound],
      ["face", "人脸记录", ScanFace],
      ["motor", "机动车记录", CarFront],
      ["nonmotor", "非机动车记录", Bike],
      ["vehicles", "车辆综合查询", Boxes],
    ],
  ],
];
const pages = Object.fromEntries(
  groups.flatMap(([group, items]) =>
    items.map(([id, label]) => [id, { group, label }]),
  ),
);
function route() {
  const [page, ref] = location.hash.slice(1).split("?");
  return {
    page: pages[page] ? page : "home",
    ref: new URLSearchParams(ref).get("id") || "",
  };
}
export function go(page, id) {
  location.hash = page + (id ? "?id=" + encodeURIComponent(id) : "");
}
function Brand() {
  return (
    <div className="brand">
      <span className="brandmark">
        <Boxes size={30} strokeWidth={2.3} />
      </span>
      <div>
        <strong>VIID 智能视图库</strong>
        <small>GA/T 1400 数据管理平台</small>
      </div>
    </div>
  );
}
function Login({ onLogin }) {
  const [err, setErr] = useState(""),
    [busy, setBusy] = useState(false);
  return (
    <main className="login">
      <form
        className="card loginbox"
        onSubmit={async (e) => {
          e.preventDefault();
          const f = new FormData(e.currentTarget);
          setBusy(true);
          try {
            const r = await api("login", "POST", Object.fromEntries(f));
            onLogin(r.user);
          } catch (e) {
            setErr(e.message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Brand />
        <h1>登录管理后台</h1>
        <label>
          用户名
          <Input
            name="User"
            autoComplete="username"
            defaultValue="admin"
            required
          />
        </label>
        <label>
          密码
          <Input
            name="Password"
            type="password"
            autoComplete="current-password"
            required
            autoFocus
          />
        </label>
        {err && <p role="alert">{err}</p>}
        <Action primary disabled={busy}>
          {busy ? "正在登录" : "登录"}
        </Action>
      </form>
    </main>
  );
}
function Finder({ close }) {
  const [q, set] = useState("");
  const search = q.trim(),
    qs = encodeURIComponent(search);
  const a = useData(search ? "records?size=6&q=" + qs : ""),
    b = useData(search ? "devices?size=4&q=" + qs : ""),
    c = useData(search ? "images?size=4&q=" + qs : "");
  const results = [
    ...Object.entries(pages)
      .filter(([, p]) => p.label.includes(search))
      .map(([page, p]) => ({ page, title: p.label, group: p.group })),
    ...(a.data?.items || []).map((r) => ({
      page: r.kind,
      title: r.title,
      group: names[r.kind] + "记录",
      id: r.id,
    })),
    ...(b.data?.items || []).map((r) => ({
      page: "devices",
      title: r.name,
      group: "设备",
      id: r.id,
    })),
    ...(c.data?.items || []).map((r) => ({
      page: "images",
      title: r.title,
      group: "图片",
      id: r.id,
    })),
  ];
  return (
    <Modal title="全局搜索" close={close}>
      <Input
        autoFocus
        value={q}
        onChange={(e) => set(e.target.value)}
        placeholder="搜索页面、设备、人员、车牌或图片编号"
      />
      <div className="findresults">
        {results.map((r) => (
          <button
            key={r.page + (r.id || "")}
            onClick={() => {
              go(r.page, r.id);
              close();
            }}
          >
            <Search size={15} />
            <span>{r.title}</span>
            <small>{r.group}</small>
            <ChevronRight size={14} />
          </button>
        ))}
        {!results.length &&
          (search && (!a.data || !b.data || !c.data) ? (
            <Load error={a.error || b.error || c.error} />
          ) : (
            <Empty />
          ))}
      </div>
    </Modal>
  );
}
function App() {
  const [user, setUser] = useState(undefined),
    [bootError, setBootError] = useState(""),
    [loc, setLoc] = useState(route),
    [collapsed, setCollapsed] = useState(false),
    [mobile, setMobile] = useState(false),
    [narrow, setNarrow] = useState(
      () => window.matchMedia("(max-width: 780px)").matches,
    ),
    [find, setFind] = useState(false),
    [account, setAccount] = useState(false),
    [rev, refresh] = useState(0),
    [msg, setMsg] = useState("");
  const notify = (s) => setMsg(s);
  useEffect(() => {
    const viewport = window.matchMedia("(max-width: 780px)");
    const resize = () => {
      setNarrow(viewport.matches);
      setMobile(false);
    };
    viewport.addEventListener("change", resize);
    fetch("/api/session")
      .then(async (r) => {
        if (r.status === 401) setUser(null);
        else if (r.ok) setUser((await r.json()).user);
        else throw Error("后台连接失败");
      })
      .catch((e) => setBootError(e.message));
    const update = () => {
        setLoc(route());
        setMobile(false);
        setAccount(false);
      },
      expired = () => setUser(null),
      key = (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
          e.preventDefault();
          setFind((v) => !v);
        }
      };
    window.addEventListener("hashchange", update);
    window.addEventListener("sessionexpired", expired);
    window.addEventListener("keydown", key);
    return () => {
      viewport.removeEventListener("change", resize);
      window.removeEventListener("hashchange", update);
      window.removeEventListener("sessionexpired", expired);
      window.removeEventListener("keydown", key);
    };
  }, []);
  useEffect(() => {
    if (msg) {
      const t = setTimeout(() => setMsg(""), 3200);
      return () => clearTimeout(t);
    }
  }, [msg]);
  if (user === undefined) return <Load error={bootError} />;
  if (!user) return <Login onLogin={setUser} />;
  const navExpanded = narrow ? mobile : !collapsed;
  const props = {
    page: loc.page,
    initialId: loc.ref,
    rev,
    refresh: () => refresh((x) => x + 1),
    notify,
    go,
  };
  return (
    <div
      className={
        "shell " +
        (!narrow && collapsed ? "slim " : "") +
        (mobile ? "mobile-open" : "")
      }
    >
      {mobile && (
        <button
          className="scrim"
          aria-label="关闭导航"
          onClick={() => setMobile(false)}
        />
      )}
      <aside className="rail" id="sidebar">
        <Brand />
        <nav>
          {groups.map(([group, items]) => (
            <section key={group}>
              <h4>{group}</h4>
              {items.map(([id, label, Icon]) => (
                <a
                  href={"#" + id}
                  key={id}
                  aria-current={id === loc.page ? "page" : undefined}
                  className={id === loc.page ? "here" : ""}
                  title={label}
                >
                  <Icon size={18} />
                  <span>{label}</span>
                </a>
              ))}
            </section>
          ))}
        </nav>
      </aside>
      <button
        type="button"
        className="iconbtn nav-toggle"
        aria-label={navExpanded ? "收起导航" : "展开导航"}
        title={navExpanded ? "收起导航" : "展开导航"}
        aria-controls="sidebar"
        aria-expanded={navExpanded}
        onClick={() => (narrow ? setMobile(!mobile) : setCollapsed(!collapsed))}
      >
        {navExpanded ? (
          <PanelLeftClose size={18} />
        ) : (
          <PanelLeftOpen size={18} />
        )}
        {navExpanded && <span>收起导航</span>}
      </button>
      <div className="body">
        <header className="topbar">
          <div className="crumb">
            {pages[loc.page].group}
            <ChevronRight size={13} />
            <b>{pages[loc.page].label}</b>
          </div>
          <button className="find" onClick={() => setFind(true)}>
            <Search size={16} />
            <span>搜索设备、人员、车辆、图片…</span>
            <kbd>Ctrl K</kbd>
          </button>
          <IconBtn
            icon={Bell}
            label="查看设备告警"
            onClick={() => go("monitor")}
          />
          <div className="account">
            <button
              onClick={() => setAccount((v) => !v)}
              aria-expanded={account}
            >
              <span className="avatar">
                <UserRound size={17} />
              </span>
              管理员
              <ChevronDown size={13} />
            </button>
            {account && (
              <div className="accountmenu">
                <small>当前账号 · {user}</small>
                <button
                  onClick={async () => {
                    await api("logout", "POST");
                    setUser(null);
                    setAccount(false);
                  }}
                >
                  <LogOut size={15} />
                  退出登录
                </button>
              </div>
            )}
          </div>
        </header>
        <main className="canvas" key={loc.page}>
          {["home", "overview"].includes(loc.page) ? (
            <Dashboard {...props} />
          ) : loc.page === "feed" ? (
            <Recent full {...props} />
          ) : loc.page === "exchange" ? (
            <ExchangePage {...props} />
          ) : loc.page === "audit" ? (
            <AuditPage {...props} />
          ) : loc.page === "monitor" ? (
            <MonitorPage {...props} />
          ) : loc.page === "workspace" ? (
            <ImageWorkspace {...props} />
          ) : (
            <Catalog {...props} />
          )}
        </main>
      </div>
      {find && <Finder close={() => setFind(false)} />}
      {msg && (
        <div role="status" className="toast">
          <ShieldCheck size={17} />
          {msg}
        </div>
      )}
    </div>
  );
}
createRoot(document.getElementById("root")).render(<App />);
import "./catalog.css";
