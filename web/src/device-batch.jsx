import React, { useState } from "react";
import { Action, api, Modal } from "./shared";
export function BatchDevices({ mode, devices, close, onFinish }) {
  const importing = mode === "import";
  const [entries, setEntries] = useState(devices),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [completed, setCompleted] = useState(0);
  const labels = {
    import: "批量导入设备",
    enable: "批量启用设备",
    disable: "批量停用设备",
  };
  async function choose(file) {
    if (!file) return;
    setError("");
    setEntries([]);
    setCompleted(0);
    try {
      if (file.size > 1024 * 1024) throw Error("文件须小于 1 MB");
      const arr = JSON.parse((await file.text()).replace(/^\uFEFF/, ""));
      if (!Array.isArray(arr) || !arr.length || arr.length > 500)
        throw Error("请选择含 1–500 台设备的 JSON 数组");
      const mapped = arr.map((r) => ({
        id: String(r.id ?? "").trim(),
        name: String(r.name ?? "").trim(),
        ip: r.ip || "",
        kind: r.kind || "采集设备",
        point: r.point || "",
        enabled: r.enabled !== false,
      }));
      if (
        mapped.some((r) => !r.id || !r.name) ||
        new Set(mapped.map((r) => r.id)).size !== mapped.length
      )
        throw Error("每行都需要唯一的 id 和非空 name");
      setEntries(mapped);
    } catch (e) {
      setError(e.message);
    }
  }
  async function submit() {
    setBusy(true);
    setError("");
    let ok = 0;
    const failed = [];
    let reason = "";
    for (const row of entries) {
      try {
        await api(
          importing ? "devices" : "devices/" + encodeURIComponent(row.id),
          importing ? "POST" : "PUT",
          {
            ID: row.id,
            Name: row.name,
            Kind: row.kind,
            IP: row.ip,
            Point: row.point,
            Enabled: importing ? row.enabled : mode === "enable",
          },
        );
        ok++;
      } catch (e) {
        failed.push(row);
        reason = e.message;
      }
    }
    const total = completed + ok;
    setCompleted(total);
    setEntries(failed);
    setBusy(false);
    if (failed.length)
      setError(
        "成功 " + total + " 台，失败 " + failed.length + " 台。" + reason,
      );
    else onFinish(total);
  }
  return (
    <Modal
      title={labels[mode]}
      close={() => {
        if (!busy) {
          if (completed) onFinish(completed);
          else close();
        }
      }}
    >
      <div className="editor">
        {importing ? (
          <>
            <p>
              导入 JSON 文件，每台设备填写 <code>id</code> 和 <code>name</code>
              。已导出的设备列表可直接使用。
            </p>
            <input
              type="file"
              accept=".json,application/json"
              aria-label="设备 JSON 文件"
              disabled={busy}
              onChange={(e) => choose(e.target.files[0])}
            />
          </>
        ) : (
          <p>
            将{mode === "enable" ? "启用" : "停用"}所选设备。
            {mode === "disable"
              ? "停用后，设备上报和保活会被拒绝。"
              : "设备再次注册后可恢复上报。"}
          </p>
        )}
        {error && <p role="alert">{error}</p>}
        <div className="formactions">
          <small>待处理 {entries.length} 台</small>
          <Action primary disabled={busy || !entries.length} onClick={submit}>
            {busy ? "处理中…" : completed ? "重试失败项" : "确认"}
          </Action>
        </div>
      </div>
    </Modal>
  );
}
