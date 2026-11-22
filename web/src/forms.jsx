import React, { useState } from "react";
import { Action, api, Input, Modal, names, useData } from "./shared";
function RecordField({ value, onChange }) {
  const { data } = useData("records?size=12&q=" + encodeURIComponent(value));
  return (
    <label>
      关联记录编号
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        list="record-options"
        placeholder="输入名称或编号，选择记录"
      />
      <datalist id="record-options">
        {data?.items.map((r) => (
          <option key={r.id} value={r.id}>
            {r.title} · {names[r.kind]}
          </option>
        ))}
      </datalist>
    </label>
  );
}
export function Editor({ action, item, close, done }) {
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [record, setRecord] = useState(item?.record_id || "");
  const isDevice = action === "device";
  async function save(e) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    setBusy(true);
    setError("");
    try {
      let out;
      if (isDevice) {
        const body = Object.fromEntries(f);
        body.Enabled = f.has("Enabled");
        out = await api(
          item ? "devices/" + encodeURIComponent(item.id) : "devices",
          item ? "PUT" : "POST",
          body,
        );
      } else if (action === "link") {
        out = await api("images/" + encodeURIComponent(item.id), "PATCH", {
          Record: record,
        });
      } else {
        f.set("record_id", record);
        out = await api("images", "POST", f);
      }
      done(out.id);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={
        isDevice
          ? item
            ? "编辑设备"
            : "新增设备"
          : action === "link"
            ? "关联结构化记录"
            : "上传图片"
      }
      close={busy ? () => {} : close}
    >
      <form className="editor" onSubmit={save}>
        {isDevice ? (
          <>
            <label>
              设备编号
              <Input
                name="ID"
                defaultValue={item?.id}
                readOnly={!!item}
                maxLength={128}
                required
              />
            </label>
            <label>
              设备名称
              <Input name="Name" defaultValue={item?.name} required />
            </label>
            <label>
              设备类型
              <select name="Kind" defaultValue={item?.kind || "采集设备"}>
                {[
                  "采集设备",
                  "人脸抓拍机",
                  "人员采集机",
                  "机动车卡口",
                  "非机动车卡口",
                  "全景相机",
                  ...(item?.kind ? [item.kind] : []),
                ]
                  .filter((s, i, a) => a.indexOf(s) === i)
                  .map((s) => (
                    <option key={s}>{s}</option>
                  ))}
              </select>
            </label>
            <label>
              IP 地址
              <Input name="IP" defaultValue={item?.ip} />
            </label>
            <label>
              所属点位
              <Input name="Point" defaultValue={item?.point} />
            </label>
            <label className="check">
              <input
                type="checkbox"
                name="Enabled"
                defaultChecked={item?.enabled ?? true}
              />
              启用设备
            </label>
          </>
        ) : (
          <>
            {action === "upload" && (
              <>
                <label>
                  图片文件
                  <input
                    type="file"
                    name="file"
                    accept="image/jpeg,image/png"
                    required
                  />
                </label>
                <label>
                  图片类型
                  <select name="kind" defaultValue="scene">
                    {Object.entries(names).map(([id, n]) => (
                      <option key={id} value={id}>
                        {n}
                      </option>
                    ))}
                  </select>
                </label>
              </>
            )}
            <RecordField value={record} onChange={setRecord} />
          </>
        )}
        {error && <p role="alert">{error}</p>}
        <div className="formactions">
          <Action type="button" onClick={close} disabled={busy}>
            取消
          </Action>
          <Action primary disabled={busy || (action === "link" && !record)}>
            {busy ? "保存中…" : "保存"}
          </Action>
        </div>
      </form>
    </Modal>
  );
}
