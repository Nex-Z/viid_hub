import React, { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Camera,
  ChevronLeft,
  ChevronRight,
  Inbox,
  LoaderCircle,
  X,
} from "lucide-react";
export { Button, Input };
export const names = {
  person: "人员",
  face: "人脸",
  motor: "机动车",
  nonmotor: "非机动车",
  scene: "场景",
};
export const fileURL = (id) =>
  "/api/images/" + encodeURIComponent(id) + "/file";
const dateFormat = new Intl.DateTimeFormat("sv-SE", {
  timeZone: "Asia/Shanghai",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});
export function date(value) {
  if (!value) return "—";
  return dateFormat.format(new Date(value));
}
export const num = (n) => (n ?? 0).toLocaleString("en-US");
export const bytes = (n) =>
  n >= 1048576
    ? (n / 1048576).toFixed(1) + " MB"
    : (n / 1024).toFixed(1) + " KB";
export async function api(path, method = "GET", body, signal) {
  const form = body instanceof FormData;
  const res = await fetch("/api/" + path, {
    method,
    headers: body && !form ? { "Content-Type": "application/json" } : undefined,
    body: body ? (form ? body : JSON.stringify(body)) : undefined,
    signal,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    if (res.status === 401 && !["login", "session"].includes(path))
      window.dispatchEvent(new Event("sessionexpired"));
    throw new Error(err.error ?? "服务请求失败（" + res.status + "）");
  }
  return res.json();
}
export function useData(path, revision = 0) {
  const [state, set] = useState({ data: null, error: "", key: "" });
  useEffect(() => {
    if (!path) return;
    const c = new AbortController();
    api(path, "GET", undefined, c.signal)
      .then((data) => set({ data, error: "", key: path }))
      .catch((e) => {
        if (!c.signal.aborted) set({ data: null, error: e.message, key: path });
      });
    return () => c.abort();
  }, [path, revision]);
  return state.key === path ? state : { data: null, error: "" };
}
export function Action({
  icon: Icon,
  children,
  primary = false,
  small = false,
  ...props
}) {
  return (
    <Button
      variant={primary ? "default" : "outline"}
      size={small ? "sm" : "default"}
      {...props}
    >
      {Icon && <Icon size={16} />}
      {children}
    </Button>
  );
}
export function IconBtn({ icon: Icon, label, ...props }) {
  return (
    <button
      type="button"
      className="iconbtn"
      title={label}
      aria-label={label}
      {...props}
    >
      <Icon size={17} />
    </button>
  );
}
export function Tag({ kind }) {
  return (
    <Badge variant="secondary" className={"tag " + kind}>
      {names[kind] || kind}
    </Badge>
  );
}
export function Dot({ value }) {
  return (
    <span className="dotlabel">
      <i
        className={
          "dot " +
          (["在线", "已关联", "已入库", "正常"].includes(value)
            ? "green"
            : value === "超时"
              ? "orange"
              : "gray")
        }
      />
      {value}
    </span>
  );
}
export function Photo({ id, ...props }) {
  return id ? (
    <img
      className="thumb"
      src={fileURL(id)}
      alt="抓拍图片"
      loading="lazy"
      {...props}
    />
  ) : (
    <span className="noimg">
      <Camera size={18} />
    </span>
  );
}
export function Load({ error }) {
  return (
    <div className="empty">
      {error ? (
        <>
          <Inbox />
          {error}
        </>
      ) : (
        <>
          <LoaderCircle className="spin" />
          正在加载
        </>
      )}
    </div>
  );
}
export function Empty({ text = "没有符合条件的记录" }) {
  return (
    <div className="empty">
      <Inbox size={29} />
      {text}
    </div>
  );
}
const pageSizes = [10, 15, 20, 50, 100];
const pageSizeKey = "viid-hub.pageSize";
const defaultPageSize = 15;

export function usePageSize() {
  const [size, setSize] = useState(() => {
    try {
      const saved = Number(localStorage.getItem(pageSizeKey));
      return pageSizes.includes(saved) ? saved : defaultPageSize;
    } catch {
      return defaultPageSize;
    }
  });
  function chooseSize(next) {
    setSize(next);
    try {
      localStorage.setItem(pageSizeKey, String(next));
    } catch {
      // The current list still uses the selection when storage is unavailable.
    }
  }
  return [size, chooseSize];
}

export function Pager({ page, size, total, onPage, onSize }) {
  const last = Math.max(1, Math.ceil(total / size)),
    first = Math.max(1, Math.min(page - 2, last - 4));
  return (
    <footer className="pager">
      <span>
        共 <b>{num(total)}</b> 条
      </span>
      <select
        aria-label="每页条数"
        value={size}
        onChange={(e) => onSize(+e.target.value)}
      >
        {pageSizes.map((n) => (
          <option key={n} value={n}>
            {n} 条/页
          </option>
        ))}
      </select>
      <span className="flexfill" />
      <IconBtn
        icon={ChevronLeft}
        label="上一页"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      />
      {Array.from({ length: Math.min(5, last) }, (_, i) => first + i).map(
        (n) => (
          <button
            key={n}
            className={"page " + (page === n ? "active" : "")}
            onClick={() => onPage(n)}
          >
            {n}
          </button>
        ),
      )}
      <IconBtn
        icon={ChevronRight}
        label="下一页"
        disabled={page >= last}
        onClick={() => onPage(page + 1)}
      />
    </footer>
  );
}
export function Head({ title, description, children }) {
  return (
    <div className="pagehead">
      <div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      <div className="actions">{children}</div>
    </div>
  );
}
export function Tabs({ items, value, onChange }) {
  return (
    <div className="tabs">
      {items.map(([id, label]) => (
        <button
          type="button"
          key={id}
          className={value === id ? "chosen" : ""}
          onClick={() => onChange(id)}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
export function Details({ title, rows }) {
  return (
    <section className="detailgroup">
      <h3>{title}</h3>
      <dl>
        {rows.map(([k, v]) => (
          <React.Fragment key={k}>
            <dt>{k}</dt>
            <dd>{v ?? "—"}</dd>
          </React.Fragment>
        ))}
      </dl>
    </section>
  );
}
export function Modal({ title, close, children, wide = false }) {
  const ref = React.useRef(null);
  useEffect(() => {
    const d = ref.current;
    d.showModal();
    return () => d.close();
  }, []);
  return (
    <dialog
      className={"modal " + (wide ? "wide" : "")}
      ref={ref}
      onCancel={close}
      onClick={(e) => {
        if (e.target === ref.current) close();
      }}
    >
      <header>
        <h2>{title}</h2>
        <IconBtn icon={X} label="关闭" onClick={close} />
      </header>
      {children}
    </dialog>
  );
}
export async function exportItems(resource, ids, name, raw = false) {
  const { items } = await api(resource + "/export", "POST", { ids });
  downloadJSON(raw ? items[0].raw : items, name);
}
function downloadJSON(rows, name) {
  const blob = new Blob([JSON.stringify(rows, null, 2)], {
      type: "application/json",
    }),
    url = URL.createObjectURL(blob),
    a = document.createElement("a");
  a.href = url;
  a.download = name + ".json";
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
