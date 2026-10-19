"use client";

import * as React from "react";
import { DownloadIcon, Loader2Icon, MonitorCog, RefreshCwIcon, TerminalIcon, XIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api } from "@/lib/api";
import type { HostTool, HostenvStatus, InstallJob } from "@/lib/types";

const TERMINAL_STATES = new Set(["done", "failed", "cancelled"]);

const KIND_LABEL: Record<HostTool["kind"], string> = {
  apt: "系统源",
  github: "GitHub Release",
  pip: "pip",
  manual: "手工安装",
};

// 宿主环境:工具体检。二进制部署形态下系统里装了什么,agent 就能经 Bash
// 直接调什么;此页把家底显式化,缺失项可一键安装(由特权助手 artex-hostd 执行)。
export default function HostEnvPage() {
  const [inv, setInv] = React.useState<HostenvStatus | null>(null);
  const [loaded, setLoaded] = React.useState(false);
  const [proxyInput, setProxyInput] = React.useState("");
  const [savingProxy, setSavingProxy] = React.useState(false);
  const [job, setJob] = React.useState<InstallJob | null>(null);
  const [busyTool, setBusyTool] = React.useState<string | null>(null);
  const logRef = React.useRef<HTMLPreElement | null>(null);

  const refresh = React.useCallback(() => {
    api
      .hostenvStatus()
      .then((s) => {
        setInv(s);
        setProxyInput(s.installer.proxy ?? "");
      })
      .catch(() => undefined)
      .finally(() => setLoaded(true));
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  // 安装任务轮询：进行中每 1.2s 拉一次；到终态后刷新体检结果。
  React.useEffect(() => {
    if (!job || TERMINAL_STATES.has(job.state)) return;
    const timer = setInterval(async () => {
      try {
        const next = await api.hostenvJob(job.id);
        setJob(next);
        if (TERMINAL_STATES.has(next.state)) {
          setBusyTool(null);
          if (next.state === "done") toast.success(`${next.tool_name} 安装完成`);
          else if (next.state === "failed") toast.error(`${next.tool_name} 安装失败`);
          else toast.info(`${next.tool_name} 安装已取消`);
          refresh();
        }
      } catch {
        // 轮询失败不打断，下一轮再试
      }
    }, 1200);
    return () => clearInterval(timer);
  }, [job, refresh]);

  React.useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [job?.log]);

  const saveProxy = async () => {
    setSavingProxy(true);
    try {
      await api.setSettings({ install_proxy: proxyInput.trim() });
      toast.success("装机代理已保存");
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSavingProxy(false);
    }
  };

  const install = async (tool: HostTool) => {
    setBusyTool(tool.id);
    try {
      const j = await api.hostenvInstall(tool.id);
      setJob(j);
    } catch (e) {
      setBusyTool(null);
      toast.error(e instanceof Error ? e.message : "发起安装失败");
    }
  };

  const cancel = async () => {
    if (!job) return;
    try {
      await api.hostenvCancel(job.id);
    } catch {
      // 忽略：任务可能刚好自己结束了
    }
  };

  const available = inv?.tools.filter((t) => t.available) ?? [];
  const missing = inv?.tools.filter((t) => !t.available) ?? [];
  const installerReady = inv?.installer.available ?? false;
  const installing = job !== null && !TERMINAL_STATES.has(job.state);

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2 text-sm">
              <MonitorCog className="size-4" /> 宿主工具体检
            </CardTitle>
            <CardDescription className="mt-1">
              系统里有这些工具,Agent 就能直接调用;派任务选手段时会以这份清单为准。
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            {inv && (
              <Badge variant={inv.deploy_mode === "docker" ? "secondary" : "outline"}>
                {inv.deploy_mode === "docker" ? "Docker 容器" : "宿主二进制"}
              </Badge>
            )}
            <Button size="sm" variant="outline" onClick={refresh}>
              <RefreshCwIcon /> 刷新
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {!loaded ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : (
            <>
              {/* 装机代理：只作用于安装动作,与目标流量/搜索/LLM 代理无关 */}
              <div className="flex flex-col gap-2 rounded-lg border bg-muted/30 p-3">
                <div className="flex items-center justify-between gap-2">
                  <Label htmlFor="install-proxy" className="text-sm">
                    装机代理
                  </Label>
                  <span className="text-muted-foreground text-xs">
                    {installerReady ? "装东西时走它（apt / GitHub 下载）" : "宿主助手未运行，暂不可用"}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    id="install-proxy"
                    autoComplete="off"
                    className="font-mono text-xs"
                    placeholder="http://10.0.0.14:7897 或 socks5://host:1080（留空=直连）"
                    value={proxyInput}
                    disabled={!loaded || savingProxy}
                    onChange={(e) => setProxyInput(e.target.value)}
                  />
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={saveProxy}
                    disabled={!loaded || savingProxy}
                  >
                    保存
                  </Button>
                </div>
                <p className="text-muted-foreground text-xs">
                  国内环境装 nuclei 这类走 GitHub Release 的工具通常需要它；与「全局代理」（Agent
                  目标流量）互不影响。
                </p>
              </div>

              {!installerReady && inv?.installer.reason && (
                <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
                  {inv.installer.reason}
                  {inv.deploy_mode === "docker" && (
                    <span className="text-muted-foreground">
                      {" "}
                      容器镜像里的工具是构建时固化的，需要额外工具请基于官方镜像自建。
                    </span>
                  )}
                </div>
              )}

              {installerReady && inv?.installer.upgrade_required && (
                <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
                  助手版本较旧，不具备自我换装能力，因此无法随主程序自动升级。
                  在服务器上重跑一次 <code className="font-mono">install.sh</code> 完成引导后，
                  之后每次发版助手都会自动跟随。
                </div>
              )}

              <p className="text-muted-foreground text-sm">
                可用 <span className="font-medium text-foreground">{available.length}</span> /{" "}
                {inv?.tools.length ?? 0}
                {missing.length > 0 && (
                  <span className="ml-3">
                    缺失 {missing.length} 项
                    {installerReady && "，可一键安装"}
                  </span>
                )}
              </p>

              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-32">工具</TableHead>
                    <TableHead>用途</TableHead>
                    <TableHead className="w-24">状态</TableHead>
                    <TableHead>安装方式 / 提示</TableHead>
                    <TableHead className="w-28 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(inv?.tools ?? []).map((t) => (
                    <TableRow key={t.id}>
                      <TableCell className="font-mono text-sm">{t.bin}</TableCell>
                      <TableCell className="text-muted-foreground text-sm">{t.purpose}</TableCell>
                      <TableCell>
                        {t.available ? (
                          <Badge>可用</Badge>
                        ) : (
                          <Badge variant="outline">缺失</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">
                        {t.available ? (
                          "—"
                        ) : (
                          // break-all：缺失项的提示里可能带长 URL（如 msfconsole 的官方文档），
                          // 不换行会把表格撑到横向溢出，需要左右拖动才看得到第一列。
                          <span className="break-all">
                            <span className="mr-1 text-foreground/70">{KIND_LABEL[t.kind]}</span>
                            {t.install}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        {!t.available && t.installable && installerReady && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={installing || busyTool === t.id}
                            onClick={() => install(t)}
                          >
                            {busyTool === t.id ? (
                              <Loader2Icon className="animate-spin" />
                            ) : (
                              <DownloadIcon />
                            )}
                            安装
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </>
          )}
        </CardContent>
      </Card>

      {/* 安装进度：进行中显示日志，实时滚动 */}
      {job && (
        <Card>
          <CardHeader className="flex-row items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2 text-sm">
                <TerminalIcon className="size-4" /> 安装 {job.tool_name}
              </CardTitle>
              <CardDescription className="mt-1 flex items-center gap-2">
                <Badge
                  variant={
                    job.state === "done"
                      ? "default"
                      : job.state === "failed"
                        ? "destructive"
                        : "secondary"
                  }
                >
                  {job.state}
                </Badge>
                {job.error && <span className="text-destructive">{job.error}</span>}
              </CardDescription>
            </div>
            <div className="flex items-center gap-2">
              {installing && (
                <Button size="sm" variant="outline" onClick={cancel}>
                  <XIcon /> 取消
                </Button>
              )}
              {!installing && (
                <Button size="sm" variant="ghost" onClick={() => setJob(null)}>
                  关闭
                </Button>
              )}
            </div>
          </CardHeader>
          <CardContent>
            <pre
              ref={logRef}
              className="max-h-80 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed"
            >
              {job.log || "等待输出…"}
            </pre>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
