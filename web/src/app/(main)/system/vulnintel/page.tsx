"use client";

import * as React from "react";

import { DatabaseIcon, RefreshCwIcon, ShieldAlertIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";

type Source = {
  source: string;
  last_sync_at?: string;
  cursor: string;
  total_synced: number;
  last_error: string;
  enabled: boolean;
};

type Status = {
  enabled: boolean;
  nuclei_templates_dir: string;
  sources: Source[];
  counts: { cves: number; kev: number; pocs: number };
};

const SOURCE_LABEL: Record<string, string> = {
  nvd: "NVD(CVE 全量/增量)",
  kev: "CISA KEV(在野利用)",
  poc: "GitHub PoC 聚合",
};

function fmtTime(v?: string): string {
  if (!v) return "—";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleString();
}

// 系统 → 漏洞情报:同步水位与库内条数总览。只读;开关在同步
// 设置里(settings),这里展示现状让"情报是否在更新"一眼可见。
export default function VulnintelPage() {
  const [status, setStatus] = React.useState<Status | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");

  const load = React.useCallback(() => {
    setLoading(true);
    api
      .vulnintelStatus()
      .then(setStatus)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(load, [load]);

  return (
    <div className="flex h-full flex-col gap-4 overflow-y-auto p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <ShieldAlertIcon className="size-5" />
          <h1 className="text-lg font-semibold">漏洞情报</h1>
          {status && (
            <Badge variant={status.enabled ? "default" : "secondary"}>
              {status.enabled ? "同步开启" : "同步关闭"}
            </Badge>
          )}
        </div>
        <Button variant="outline" size="sm" onClick={load} disabled={loading}>
          <RefreshCwIcon className={"mr-1 size-4" + (loading ? " animate-spin" : "")} />
          刷新
        </Button>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}

      {status && (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
                  <DatabaseIcon className="size-4" /> CVE 记录
                </CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-semibold tabular-nums">{status.counts.cves.toLocaleString()}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">KEV 在野利用</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-semibold tabular-nums text-red-500">{status.counts.kev.toLocaleString()}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">PoC 仓库</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-semibold tabular-nums">{status.counts.pocs.toLocaleString()}</p>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">同步状态</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b text-left text-muted-foreground">
                      <th className="py-2 pr-4 font-medium">数据源</th>
                      <th className="py-2 pr-4 font-medium">最近同步</th>
                      <th className="py-2 pr-4 font-medium">本轮处理</th>
                      <th className="py-2 pr-4 font-medium">状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {status.sources.length === 0 && (
                      <tr>
                        <td colSpan={4} className="py-3 text-muted-foreground">
                          尚未开始同步(首次启动约 90 秒后开始;全量回填需数轮)
                        </td>
                      </tr>
                    )}
                    {status.sources.map((s) => (
                      <tr key={s.source} className="border-b last:border-0">
                        <td className="py-2 pr-4">{SOURCE_LABEL[s.source] ?? s.source}</td>
                        <td className="py-2 pr-4 tabular-nums">{fmtTime(s.last_sync_at)}</td>
                        <td className="py-2 pr-4 tabular-nums">{s.total_synced.toLocaleString()}</td>
                        <td className="py-2 pr-4">
                          {s.last_error ? (
                            <Badge variant="destructive" title={s.last_error}>
                              有错误
                            </Badge>
                          ) : (
                            <Badge variant="default">正常</Badge>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="mt-3 text-xs text-muted-foreground">
                模板库目录:{status.nuclei_templates_dir || "未配置"}(nuclei 模板随同步自动更新)
              </p>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
