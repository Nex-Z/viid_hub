import React, { useState } from "react";
import { useData, Load } from "./shared";

export function DeviceFilter({ value, onChange }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState(null);
  const { data, error } = useData(
    open
      ? "devices?" + new URLSearchParams({ q: query, page, size: 10 })
      : null,
  );
  function choose(device) {
    onChange(device?.id || "");
    setSelected(device);
    setOpen(false);
  }
  return (
    <div
      className="device-filter"
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false);
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          setOpen(false);
          e.stopPropagation();
        }
      }}
    >
      <button
        className="linkbutton"
        type="button"
        aria-label="筛选设备"
        aria-expanded={open}
        onClick={() => {
          setOpen(!open);
          setQuery("");
          setPage(1);
        }}
      >
        {value ? (selected?.id === value ? selected.name : value) : "全部设备"}{" "}
        ▾
      </button>
      {open && (
        <div className="device-menu">
          <input
            autoFocus
            aria-label="检索设备"
            placeholder="搜索设备名称、编号或点位"
            value={query}
            onKeyDown={(e) => {
              if (e.key === "Enter") e.preventDefault();
            }}
            onChange={(e) => {
              setQuery(e.target.value);
              setPage(1);
            }}
          />
          <button type="button" onClick={() => choose(null)}>
            全部设备
          </button>
          {data ? (
            <>
              {data.items.map((d) => (
                <button key={d.id} type="button" onClick={() => choose(d)}>
                  {d.name}
                  <small>{d.id}</small>
                </button>
              ))}
              {!data.total && <small>没有匹配设备</small>}
              <div>
                <button
                  type="button"
                  disabled={data.page === 1}
                  onClick={() => setPage(data.page - 1)}
                >
                  上一页
                </button>
                <span>
                  {data.page} / {Math.max(1, Math.ceil(data.total / 10))}
                </span>
                <button
                  type="button"
                  disabled={data.page * 10 >= data.total}
                  onClick={() => setPage(data.page + 1)}
                >
                  下一页
                </button>
              </div>
            </>
          ) : (
            <Load error={error} />
          )}
        </div>
      )}
    </div>
  );
}
