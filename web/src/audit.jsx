import React, { useState } from "react";
import { date, Head, Load, Empty, Pager, useData, usePageSize } from "./shared";

const actions = {
  view: "查看详情",
  export: "导出",
  delete: "删除",
  download: "下载原图",
  reveal: "查看完整证件号",
  reveal_denied: "完整证件号访问被拒绝",
};
const resources = { images: "图片", records: "记录", devices: "设备" };

export function AuditPage({ rev }) {
  const [page, setPage] = useState(1),
    [size, setSize] = usePageSize();
  const { data, error } = useData("audit?page=" + page + "&size=" + size, rev);
  return (
    <>
      <Head
        title="操作审计"
        description="查看管理账号访问、导出和删除记录的时间与对象"
      />
      <section className="card">
        {!data ? (
          <Load error={error} />
        ) : (
          <>
            <div className="tablewrap">
              <table>
                <thead>
                  <tr>
                    {["操作时间", "账号", "操作", "资源", "对象编号"].map(
                      (t) => (
                        <th key={t}>{t}</th>
                      ),
                    )}
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((r) => (
                    <tr key={r.seq}>
                      <td>{date(r.at)}</td>
                      <td>{r.actor}</td>
                      <td>{actions[r.action] || r.action}</td>
                      <td>{resources[r.resource]}</td>
                      <td>{r.target}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!data.total && <Empty />}
            <Pager
              page={data.page}
              size={size}
              total={data.total}
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
