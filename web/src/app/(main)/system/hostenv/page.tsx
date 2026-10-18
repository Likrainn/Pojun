"use client";

import * as React from "react";
import { MonitorCog, RefreshCwIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
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

type ToolRow = {
  name: string;
  bin: string;
  available: boolean;
  purpose: string;
  install?: string;
};

type Inventory = {
  tools: ToolRow[];
  summary: { available: string[]; missing: { name: string; install: string }[]; hint: string };
  deploy_mode: "docker" | "host";
};

// 宿主环境:工具体检。二进制部署形态下系统里装了什么,agent 就能经 Bash
// 直接调什么;此页把家底显式化,缺失项带安装提示。
export default function HostEnvPage() {
  const [inv, setInv] = React.useState<Inventory | null>(null);
  const [loaded, setLoaded] = React.useState(false);

  const refresh = React.useCallback(() => {
    api
      .toolsInventory()
      .then(setInv)
      .catch(() => undefined)
      .finally(() => setLoaded(true));
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const available = inv?.tools.filter((t) => t.available) ?? [];
  const missing = inv?.tools.filter((t) => !t.available) ?? [];

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
        <CardContent>
          {!loaded ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : (
            <>
              <p className="mb-3 text-muted-foreground text-sm">
                可用 <span className="font-medium text-foreground">{available.length}</span> /{" "}
                {inv?.tools.length ?? 0}
                {missing.length > 0 && (
                  <span className="ml-3">
                    缺失 {missing.length} 项,按需安装后点「刷新」
                  </span>
                )}
              </p>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-32">工具</TableHead>
                    <TableHead>用途</TableHead>
                    <TableHead className="w-24">状态</TableHead>
                    <TableHead>安装提示</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(inv?.tools ?? []).map((t) => (
                    <TableRow key={t.bin}>
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
                        {t.available ? "—" : t.install}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
