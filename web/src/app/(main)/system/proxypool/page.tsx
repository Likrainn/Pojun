"use client";

import * as React from "react";

import { PlusIcon, RefreshCwIcon, SaveIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";

type Entry = {
  url: string;
  name: string;
  enabled: boolean;
  weight: number;
};

type Running = {
  url: string;
  name: string;
  failures: number;
};

// 系统 → 代理池:多上游出口配置(轮换/粘性/随机),条目启停与权重。
// 保存即热应用(upstream 钩子按请求选代理);连败 5 次自动熔断 10 分钟。
export default function ProxyPoolPage() {
  const [entries, setEntries] = React.useState<Entry[]>([]);
  const [strategy, setStrategy] = React.useState("round_robin");
  const [running, setRunning] = React.useState<Running[]>([]);
  const [capturing, setCapturing] = React.useState(false);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(() => {
    setLoading(true);
    api
      .proxyPoolGet()
      .then((r) => {
        setEntries(r.entries ?? []);
        setStrategy(r.strategy || "round_robin");
        setRunning(r.running ?? []);
        setCapturing(r.capturing);
      })
      .catch((e) => toast.error("加载失败: " + e))
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(load, [load]);

  const save = async () => {
    setSaving(true);
    try {
      await api.proxyPoolPut({ entries, strategy });
      toast.success("代理池已保存并热应用");
      load();
    } catch (e) {
      toast.error("保存失败: " + e);
    } finally {
      setSaving(false);
    }
  };

  const update = (i: number, patch: Partial<Entry>) => {
    setEntries((prev) => prev.map((e, j) => (j === i ? { ...e, ...patch } : e)));
  };

  const failuresByURL = React.useMemo(() => {
    const m: Record<string, number> = {};
    for (const r of running) m[r.url] = r.failures;
    return m;
  }, [running]);

  return (
    <div className="flex h-full flex-col gap-4 overflow-y-auto p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h1 className="text-lg font-semibold">代理池</h1>
          <Badge variant={entries.length > 0 ? "default" : "secondary"}>
            {entries.length > 0 ? `${entries.length} 个上游` : "未启用(直连/全局代理)"}
          </Badge>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCwIcon className={"mr-1 size-4" + (loading ? " animate-spin" : "")} />
            刷新
          </Button>
          <Button size="sm" onClick={save} disabled={saving}>
            <SaveIcon className="mr-1 size-4" />
            保存并热应用
          </Button>
        </div>
      </div>

      {!capturing && (
        <p className="text-sm text-amber-500">
          提示:流量捕获未开启时代理池不生效(worker 流量需经捕获代理 :8788 才走出口轮换)。
        </p>
      )}

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">轮换策略</CardTitle>
        </CardHeader>
        <CardContent className="flex items-center gap-3">
          <Select value={strategy} onValueChange={setStrategy}>
            <SelectTrigger className="w-64">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="round_robin">轮询(按权重)</SelectItem>
              <SelectItem value="sticky_host">按目标粘性(同目标固定出口)</SelectItem>
              <SelectItem value="random">随机</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            连败 5 次自动熔断 10 分钟;池非空时优先于「全局出口代理」。
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2 flex-row flex items-center justify-between space-y-0">
          <CardTitle className="text-sm font-medium">上游代理</CardTitle>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setEntries((p) => [...p, { url: "", name: "", enabled: true, weight: 1 }])}
          >
            <PlusIcon className="mr-1 size-4" />
            添加
          </Button>
        </CardHeader>
        <CardContent className="space-y-2">
          {entries.length === 0 && (
            <p className="py-3 text-sm text-muted-foreground">暂无条目。添加 http/https/socks5 代理(支持 user:pass)。</p>
          )}
          {entries.map((e, i) => (
            <div key={i} className="flex items-center gap-2">
              <Input
                className="flex-1 font-mono text-xs"
                placeholder="socks5://user:pass@1.2.3.4:1080"
                value={e.url}
                onChange={(ev) => update(i, { url: ev.target.value })}
              />
              <Input
                className="w-36"
                placeholder="名称(可选)"
                value={e.name}
                onChange={(ev) => update(i, { name: ev.target.value })}
              />
              <div className="flex items-center gap-1">
                <span className="text-xs text-muted-foreground">权重</span>
                <Input
                  className="w-16"
                  type="number"
                  min={1}
                  value={e.weight}
                  onChange={(ev) => update(i, { weight: Math.max(1, Number(ev.target.value) || 1) })}
                />
              </div>
              <Switch checked={e.enabled} onCheckedChange={(v) => update(i, { enabled: v })} />
              {failuresByURL[e.url] !== undefined && failuresByURL[e.url] > 0 && (
                <Badge variant="destructive">连败 {failuresByURL[e.url]}</Badge>
              )}
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setEntries((p) => p.filter((_, j) => j !== i))}
              >
                <Trash2Icon className="size-4" />
              </Button>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
