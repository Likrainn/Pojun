// Dashboard Page
import { route } from '../core/router.js';
import { t } from '../core/i18n.js';
import { api } from '../core/api.js';
import { uiStore, taskStore, findingStore } from '../core/store.js';
import { h, cn, fmtRel, fmtTokens, fmtDuration, fmtNumber } from '../core/utils.js';

let state = {
    tasks: [],
    findings: [],
    stats: null,
    settings: null,
    pending: [],
    activity: [],
    tokens: null,
    convTokens: [],
    usageStats: null,
    assetCounts: {},
    traffic: [],
    agents: [],
    mcpServers: [],
    skills: [],
    tools: [],
    llmProfiles: [],
    tasksByStatus: {},
    findingsBySev: {},
    sortedTasks: [],
    recentFindings: [],
    recentActivity: [],
    totalAssets: 0,
    assetByType: [],
    assetMax: 1,
    trafficByCodes: [],
    trafficMax: 1,
    recentTraffic: [],
    activeProfile: null,
    enabledTools: [],
    pendingCount: 0,
    dailyTokenData: [],
    tokenVersion: 'old',
    tokenTab: 'all',
    tokenDays: 30,
    llmProfiles: [],
    defaultProfileId: null,
    profileIdByName: new Map(),
    tokenByProfileOld: new Map(),
    tokenByProfileNew: new Map(),
    dailyTokenDataOld: [],
    dailyTokenDataNew: [],
};

function render() {
    const app = document.getElementById('app');
    if (!app) return;

    // Build the dashboard HTML
    app.innerHTML = buildDashboardHTML();
    bindEvents();
}

function buildDashboardHTML() {
    const t = (key) => key; // Simplified for now

    return `
        <div class="flex flex-1 flex-col gap-4 pb-6">
            <!-- Header -->
            <div>
                <h1 class="font-semibold text-lg tracking-tight">总览</h1>
                <p class="text-muted-foreground text-xs">系统全局状态 · 实时刷新</p>
            </div>

            <!-- Row 1: 5 stat cards -->
            <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
                ${buildStatCard('活跃任务', 'Target', state.tasksByStatus.running ?? 0, `/function/tasks`, [
                    { count: state.tasksByStatus.running ?? 0, label: '探索', color: 'text-blue-400' },
                    { count: state.tasksByStatus.paused ?? 0, label: '暂停', color: 'text-amber-400' },
                    { count: state.tasksByStatus.done ?? 0, label: '完成', color: 'text-emerald-400' },
                ], state.tasks.length)}
                ${buildStatCard('确认发现', 'Bug', state.findings.length, `/function/findings`, [
                    { count: state.findingsBySev.critical, label: '严重', color: 'text-rose-500' },
                    { count: state.findingsBySev.high, label: '高危', color: 'text-red-400' },
                    { count: state.findingsBySev.medium, label: '中危', color: 'text-amber-400' },
                    { count: state.findingsBySev.low, label: '低危', color: 'text-slate-400' },
                ])}
                ${buildStatCard('资产节点', 'Network', state.totalAssets, null, null, '跨任务共享')}
                ${buildStatCard('流量交互', 'Activity', state.traffic.length, null, 
                    state.settings?.traffic_capture ? '<span class="flex items-center gap-1.5 text-[10px] text-muted-foreground"><span class="inline-block size-1.5 shrink-0 animate-pulse rounded-full bg-blue-400"></span><span>录制中</span></span>' : '<span class="text-[10px] text-muted-foreground">捕获未开启</span>'
                )}
                ${buildStatCard('Token 用量', 'Zap', fmtTokens((state.displayedTokens?.input || 0) + (state.displayedTokens?.output || 0)) || '—', null, 
                    `入 ${fmtTokens(state.displayedTokens?.input || 0)}（含缓存 ${fmtTokens(state.displayedTokens?.cacheRead || 0)}） · 出 ${fmtTokens(state.displayedTokens?.output || 0)}`
                )}
            </div>

            <!-- Row 2: LLM Token 消耗 -->
            <div class="card p-4">
                <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
                    <div class="flex items-center gap-1.5 font-semibold text-xs">
                        <svg class="size-3.5 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
                        LLM Token 消耗
                        <div class="ml-1 flex gap-0.5 rounded-md border bg-muted/30 p-0.5">
                            <button type="button" data-token-version="old" class="rounded px-2 py-0.5 font-medium text-[9px] transition-colors ${state.tokenVersion === 'old' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}">旧版</button>
                            <button type="button" data-token-version="new" class="rounded px-2 py-0.5 font-medium text-[9px] transition-colors ${state.tokenVersion === 'new' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}">新版</button>
                        </div>
                        <span class="font-normal text-[10px] text-muted-foreground">${state.tokenVersion === 'new' ? 'llm_usage' : 'activity'}</span>
                    </div>
                    <div class="flex flex-wrap items-center gap-1">
                        <button type="button" data-token-tab="all" class="rounded-md px-2.5 py-1 font-medium text-[10px] transition-colors ${state.tokenTab === 'all' ? 'bg-foreground text-background' : 'bg-muted/30 text-muted-foreground hover:text-foreground'}">全部</button>
                        ${state.llmProfiles.map(p => {
                            const key = Number(p.id);
                            const hasData = state.tokenByProfile?.has(key) || (p.is_default && state.tokenByProfile?.has(state.defaultProfileId));
                            const resolvedKey = state.tokenByProfile?.has(key) ? key : p.is_default ? state.defaultProfileId : key;
                            return `<button type="button" data-token-tab="${resolvedKey}" class="flex items-center gap-1 rounded-md px-2.5 py-1 font-medium text-[10px] transition-colors ${state.tokenTab === resolvedKey ? 'bg-foreground text-background' : 'bg-muted/30 text-muted-foreground hover:text-foreground'} ${!hasData ? 'opacity-40' : ''}">${p.name}${p.is_default ? '<span class="rounded px-1 py-0 text-[8px]">默认</span>' : ''}</button>`;
                        }).join('')}
                    </div>
                </div>
                <div class="grid grid-cols-1 gap-6 lg:grid-cols-[220px_1fr]">
                    <div class="flex flex-col gap-4">
                        <div>
                            <div class="text-[10px] text-muted-foreground">合计 (输入+输出)</div>
                            <div class="mt-0.5 font-bold text-3xl tabular-nums tracking-tight">${fmtTokens((state.displayedTokens?.input || 0) + (state.displayedTokens?.output || 0)) || '—'}</div>
                            <div class="mt-0.5 text-[10px] text-muted-foreground">${state.displayedTokens?.taskCount || 0} 个任务</div>
                        </div>
                        <div class="space-y-3">
                            ${buildTokenBar('输入(未命中)', (state.displayedTokens?.input || 0) - (state.displayedTokens?.cacheRead || 0), state.displayedTokens, 'text-blue-400', 'hsl(217 91% 60%)')}
                            ${buildTokenBar('缓存命中', state.displayedTokens?.cacheRead || 0, state.displayedTokens, 'text-emerald-400', 'hsl(160 60% 45%)')}
                            ${buildTokenBar('输出', state.displayedTokens?.output || 0, state.displayedTokens, 'text-violet-400', 'hsl(263 70% 60%)')}
                        </div>
                        ${buildCacheHitRate(state.displayedTokens)}
                    </div>
                    <div class="flex min-h-0 flex-col">
                        <div class="mb-2 flex items-center justify-between">
                            <div class="flex gap-3 text-[9px] text-muted-foreground">
                                <span class="flex items-center gap-1"><span class="inline-block size-2 rounded-sm" style="background: hsl(217 91% 60%)"></span>输入</span>
                                <span class="flex items-center gap-1"><span class="inline-block size-2 rounded-sm" style="background: hsl(263 70% 60%)"></span>输出</span>
                                <span class="flex items-center gap-1"><span class="inline-block size-2 rounded-sm" style="background: hsl(160 60% 45%)"></span>缓存命中</span>
                            </div>
                            <div class="flex gap-0.5 rounded-md border bg-muted/30 p-0.5">
                                ${[7, 30, 90, 180, 365].map(d => `<button type="button" data-token-days="${d}" class="rounded px-2.5 py-0.5 font-medium text-[9px] transition-colors ${state.tokenDays === d ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}">${d === 7 ? '7天' : d === 30 ? '30天' : d === 90 ? '3月' : d === 180 ? '6月' : '一年'}</button>`).join('')}
                            </div>
                        </div>
                        ${state.dailyTokenData.length === 0 ? `
                            <div class="flex flex-1 items-center justify-center rounded-lg border bg-muted/10 text-muted-foreground text-xs" style="min-height: 180px">暂无数据</div>
                        ` : `
                            <div class="chart-container" style="height: 200px;">
                                <canvas id="daily-token-chart"></canvas>
                            </div>
                        `}
                    </div>
                </div>
            </div>

            <!-- Row 3: 活动流 | 发现 -->
            <div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
                ${buildActivityCard()}
                ${buildFindingsCard()}
            </div>

            <!-- Row 4: 任务表格 -->
            ${buildTasksTable()}

            <!-- Row 5: 资产分布 | 流量状态码 | 系统状态 & 待审批 -->
            <div class="grid grid-cols-1 gap-3 lg:grid-cols-3">
                ${buildAssetDistributionCard()}
                ${buildTrafficStatusCard()}
                ${buildSystemStatusCard()}
            </div>
        </div>
    `;
}

function buildStatCard(title, icon, value, linkHref, badges, subtitle) {
    const iconSvg = getIconSvg(icon);
    const badgeHtml = badges ? `
        <div class="flex flex-wrap gap-2.5 text-[10px]">
            ${badges.map(b => `<span class="${b.color}">${b.label} ${b.count}</span>`).join('')}
            ${badges.length === 0 && value === 0 ? '<span class="text-muted-foreground">暂无任务</span>' : ''}
        </div>
    ` : '';
    const linkHtml = linkHref ? `
        <a href="${linkHref}" class="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground">
            全部 <svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/></svg>
        </a>
    ` : '';
    const subtitleHtml = subtitle ? `<div class="text-[10px] text-muted-foreground">${subtitle}</div>` : '';

    return `
        <div class="card gap-1">
            <div class="card-header pb-0">
                <div class="flex items-center gap-1.5 text-[10px] text-muted-foreground">${iconSvg} ${title}</div>
                <div class="flex items-baseline gap-1.5">
                    <span class="font-semibold text-2xl tabular-nums">${value}</span>
                    ${linkHtml ? `<span class="text-muted-foreground text-xs">/ ${linkHtml.replace('全部 ', '')}</span>` : ''}
                </div>
            </div>
            <div class="card-content">${badgeHtml}${subtitleHtml}</div>
        </div>
    `;
}

function getIconSvg(name) {
    const icons = {
        Target: '<svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg>',
        Bug: '<svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 16.84A5 5 0 0 1 16.63 8"/><path d="M22 12h-4"/><path d="M2 12h16"/><path d="M21 16.84A5 5 0 0 1 7.37 8"/><path d="M12 8V2"/><path d="M12 16v6"/></svg>',
        Network: '<svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="1" y="1" width="22" height="22" rx="2"/><path d="M12 1v1"/><path d="M12 22v1"/><path d="M1 12h1"/><path d="M22 12h1"/><path d="M4.93 4.93l.7.7"/><path d="M18.36 18.36l.7.7"/><path d="M4.93 19.07l.7-.7"/><path d="M18.36 5.64l.7-.7"/></svg>',
        Activity: '<svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>',
        Zap: '<svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>',
        ShieldCheck: '<svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
        Clock: '<svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>',
    };
    return icons[name] || icons.Target;
}

function buildTokenBar(label, value, displayedTokens, textColor, barColor) {
    const total = (displayedTokens?.input || 0) + (displayedTokens?.output || 0);
    const pct = total > 0 ? (value / total) * 100 : 0;
    return `
        <div>
            <div class="mb-1 flex items-center justify-between text-[10px]">
                <span class="text-muted-foreground">${label}</span>
                <span class="${textColor} font-mono font-semibold tabular-nums">${fmtTokens(value)}</span>
            </div>
            <div class="h-1.5 overflow-hidden rounded-full bg-muted">
                <div class="h-full rounded-full transition-all duration-500" style="width: ${pct}%; background: ${barColor}"></div>
            </div>
        </div>
    `;
}

function buildCacheHitRate(displayedTokens) {
    const denominator = displayedTokens?.input || 0;
    const hitPct = denominator > 0 ? Math.round(((displayedTokens?.cacheRead || 0) / denominator) * 100) : 0;
    return `
        <div class="flex items-center justify-between rounded-lg border bg-muted/20 px-3 py-2 text-[10px]">
            <span class="text-muted-foreground">缓存命中率</span>
            <span class="${hitPct > 50 ? 'text-emerald-400' : 'text-amber-400'} font-semibold tabular-nums">${hitPct}%</span>
        </div>
    `;
}

function buildActivityCard() {
    return `
        <div class="card p-4">
            <div class="mb-3 flex items-center justify-between">
                <div class="flex items-center gap-1.5 font-semibold text-xs">
                    <svg class="size-3.5 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
                    活动流
                </div>
                <div class="flex items-center gap-2">
                    <span class="text-[10px] text-muted-foreground">${state.recentActivity.length} 条事件</span>
                    <a href="/function/tasks" class="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground">查看任务 <svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/></svg></a>
                </div>
            </div>
            <div class="mb-2.5 flex flex-wrap gap-1.5">
                ${['tool_use', 'tool_result', 'text', 'thinking', 'result'].map(kind => {
                    const count = state.activity.filter(a => a.kind === kind).length;
                    if (count === 0) return '';
                    return `<span class="rounded border bg-muted/20 px-1.5 py-0.5 text-[9px] text-muted-foreground">${kind} <strong class="text-foreground/70">${count}</strong></span>`;
                }).join('')}
            </div>
            <div class="divide-y">
                ${state.recentActivity.length === 0 ? `
                    <div class="py-4 text-center text-muted-foreground text-xs">暂无活动记录</div>
                ` : state.recentActivity.map(a => `
                    <div class="flex gap-2.5 py-2">
                        <span class="shrink-0 self-start rounded border px-1.5 py-0.5 font-mono text-[9px] ${getWorkerBg(a.worker)}">${a.worker}</span>
                        <div class="min-w-0 flex-1">
                            <div class="font-medium text-[11px]">${getKindLabel(a)}</div>
                            <div class="mt-0.5 truncate text-[10px] text-muted-foreground">${a.summary}</div>
                        </div>
                        <div class="shrink-0 text-right">
                            <span class="badge badge-outline px-1.5 py-0 text-[9px] text-muted-foreground">${a.kind}</span>
                            <div class="mt-0.5 text-[9px] text-muted-foreground tabular-nums">${fmtRel(a.ts)}</div>
                        </div>
                    </div>
                `).join('')}
            </div>
        </div>
    `;
}

function getWorkerBg(worker) {
    if (worker === 'planner') return 'bg-violet-500/10 text-violet-400 border-violet-500/20';
    if (worker === 'mainagent') return 'bg-cyan-500/10 text-cyan-400 border-cyan-500/20';
    return 'bg-blue-500/10 text-blue-400 border-blue-500/20';
}

function getKindLabel(activity) {
    if (activity.kind === 'tool_use') return activity.tool ?? 'tool_use';
    if (activity.kind === 'tool_result') return 'tool_result';
    return activity.kind;
}

function buildFindingsCard() {
    return `
        <div class="card p-4">
            <div class="mb-3 flex items-center justify-between">
                <div class="flex items-center gap-1.5 font-semibold text-xs">
                    <svg class="size-3.5 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 16.84A5 5 0 0 1 16.63 8"/><path d="M22 12h-4"/><path d="M2 12h16"/><path d="M21 16.84A5 5 0 0 1 7.37 8"/><path d="M12 8V2"/><path d="M12 16v6"/></svg>
                    发现
                </div>
                <a href="/function/findings" class="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground">全部 <svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/></svg></a>
            </div>
            <div class="divide-y">
                ${state.recentFindings.length === 0 ? `
                    <div class="py-4 text-center text-muted-foreground text-xs">暂无发现</div>
                ` : state.recentFindings.map(f => `
                    <div class="flex items-start gap-2 py-2">
                        <span class="badge mt-0.5 shrink-0 px-1.5 py-0 text-[10px]">${getSeverityBadge(f.severity)}</span>
                        <div class="min-w-0 flex-1">
                            <div class="flex flex-wrap items-center gap-1.5">
                                <span class="font-semibold text-[11px]">${f.vulnclass}</span>
                                ${f.task_description ? `<span class="truncate text-[10px] text-muted-foreground">${f.task_description}</span>` : ''}
                            </div>
                            <div class="mt-0.5 line-clamp-2 text-[10px] text-muted-foreground leading-snug">${f.summary}</div>
                        </div>
                        <span class="shrink-0 text-[9px] text-muted-foreground tabular-nums">${fmtRel(f.ts)}</span>
                    </div>
                `).join('')}
            </div>
        </div>
    `;
}

function getSeverityBadge(severity) {
    const colors = {
        critical: 'badge-destructive',
        high: 'text-red-400',
        medium: 'text-amber-400',
        low: 'text-slate-400',
    };
    return `<span class="${colors[severity] || ''}">${severity}</span>`;
}

function buildTasksTable() {
    return `
        <div class="card overflow-hidden p-0">
            <div class="flex items-center justify-between border-b px-4 py-3">
                <div class="flex items-center gap-1.5 font-semibold text-xs">
                    <svg class="size-3.5 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                    任务
                </div>
                <div class="flex items-center gap-2">
                    <span class="text-[10px] text-muted-foreground">${state.tasks.length} 个任务</span>
                    <a href="/function/tasks" class="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground">全部 <svg class="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/></svg></a>
                </div>
            </div>
            <table class="w-full border-collapse text-xs">
                <thead>
                    <tr class="border-b">
                        ${['任务', '状态', '引擎', '目标进度', '在途', '最近活动'].map(h => `<th class="px-4 py-2 text-left font-semibold text-[9px] text-muted-foreground uppercase tracking-widest first:pl-4">${h}</th>`).join('')}
                    </tr>
                </thead>
                <tbody>
                    ${state.sortedTasks.length === 0 ? `
                        <tr><td colSpan="6" class="px-4 py-6 text-center text-muted-foreground text-xs">暂无任务</td></tr>
                    ` : state.sortedTasks.map(t => {
                        const goalsPct = t.goals_total ? Math.round(((t.goals_met ?? 0) / t.goals_total) * 100) : null;
                        return `
                            <tr class="border-b transition-colors last:border-0 hover:bg-muted/30">
                                <td class="max-w-xs px-4 py-3">
                                    <a href="/function/tasks/detail?id=${t.id}" class="group flex flex-col">
                                        <span class="truncate font-medium group-hover:underline">${t.description}</span>
                                        <span class="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">${t.id}</span>
                                    </a>
                                </td>
                                <td class="px-4 py-3"><span class="badge px-1.5 py-0 text-[10px]">${getTaskStatusBadge(t.status)}</span></td>
                                <td class="px-4 py-3"><span class="badge px-1.5 py-0 text-[10px]">${getEngineBadge(t.engine_mode ?? 'idle')}</span></td>
                                <td class="px-4 py-3">${goalsPct !== null ? `<div class="flex items-center gap-2"><div class="progress w-14"><div class="progress-bar" style="width: ${goalsPct}%"></div></div><span class="text-muted-foreground tabular-nums">${t.goals_met ?? 0}/${t.goals_total}</span></div>` : '<span class="text-muted-foreground">—</span>'}</td>
                                <td class="px-4 py-3 tabular-nums">${(t.in_flight ?? 0) > 0 ? `<span class="font-semibold">${t.in_flight}</span>` : '<span class="text-muted-foreground">—</span>'}</td>
                                <td class="px-4 py-3 text-muted-foreground tabular-nums">${fmtRel(t.last_activity_unix ?? t.created_unix)}</td>
                            </tr>
                        `;
                    }).join('')}
                </tbody>
            </table>
        </div>
    `;
}

function getTaskStatusBadge(status) {
    const badges = {
        running: 'badge-primary',
        paused: 'bg-amber-500/10 text-amber-400',
        done: 'badge-success',
        failed: 'badge-destructive',
        timeout: 'bg-amber-500/10 text-amber-400',
        queued: 'bg-blue-500/10 text-blue-400',
        created: 'badge-outline',
    };
    return `<span class="${badges[status] || 'badge-outline'}">${status}</span>`;
}

function getEngineBadge(engine) {
    const badges = {
        idle: 'bg-slate-500/10 text-slate-400',
        exploring: 'bg-violet-500/10 text-violet-400',
        attacking: 'bg-red-500/10 text-red-400',
        reporting: 'bg-emerald-500/10 text-emerald-400',
    };
    return `<span class="${badges[engine] || 'bg-slate-500/10 text-slate-400'}">${engine}</span>`;
}

function buildAssetDistributionCard() {
    return `
        <div class="card p-4">
            <div class="mb-3 flex items-center justify-between">
                <div class="flex items-center gap-1.5 font-semibold text-xs">
                    <svg class="size-3.5 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="1" y="1" width="22" height="22" rx="2"/><path d="M12 1v1"/><path d="M12 22v1"/><path d="M1 12h1"/><path d="M22 12h1"/></svg>
                    资产分布
                </div>
                <span class="text-[10px] text-muted-foreground">按类型</span>
            </div>
            ${state.assetByType.length === 0 ? `
                <div class="py-6 text-center text-muted-foreground text-xs">暂无资产数据</div>
            ` : `
                <div class="flex flex-col gap-2">
                    ${state.assetByType.map(([type, count]) => `
                        <div class="flex items-center gap-2 text-xs">
                            <span class="w-8 shrink-0 text-right text-[10px] text-muted-foreground">${getAssetTypeLabel(type)}</span>
                            <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                                <div class="h-full rounded-full ${getAssetColor(type)}" style="width: ${(count / state.assetMax) * 100}%"></div>
                            </div>
                            <span class="w-4 text-right tabular-nums">${count}</span>
                        </div>
                    `).join('')}
                </div>
            `}
            <div class="mt-3 border-t pt-3 text-[10px] text-muted-foreground">共 ${state.totalAssets} 节点</div>
        </div>
    `;
}

function getAssetTypeLabel(type) {
    const labels = {
        root_domain: '根域名', ip: 'IP', subdomain: '子域名',
        app: '应用', service: '服务', endpoint: '端点',
    };
    return labels[type] || type;
}

function getAssetColor(type) {
    const colors = {
        root_domain: 'bg-blue-500', ip: 'bg-indigo-400', subdomain: 'bg-violet-500',
        app: 'bg-orange-400', service: 'bg-cyan-500', endpoint: 'bg-emerald-500',
    };
    return colors[type] || 'bg-slate-400';
}

function buildTrafficStatusCard() {
    return `
        <div class="card p-4">
            <div class="mb-3 flex items-center justify-between">
                <div class="flex items-center gap-1.5 font-semibold text-xs">
                    <svg class="size-3.5 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
                    流量状态码
                </div>
                <span class="text-[10px] text-muted-foreground">${state.traffic.length} 次请求</span>
            </div>
            ${state.trafficByCodes.length === 0 ? `
                <div class="py-6 text-center text-muted-foreground text-xs">暂无流量数据</div>
            ` : `
                <div class="mb-3 flex items-end gap-2" style="height: 52px;">
                    ${state.trafficByCodes.map(({code, n}) => `
                        <div class="flex flex-1 flex-col items-center gap-1">
                            <span class="text-[9px] text-muted-foreground tabular-nums">${n}</span>
                            <div class="${getStatusBg(code)} min-h-1 w-full rounded-sm opacity-80" style="height: ${Math.max(4, (n / state.trafficMax) * 36)}px"></div>
                            <span class="${getStatusColor(code)} font-mono text-[9px]">${code}</span>
                        </div>
                    `).join('')}
                </div>
                <div class="border-t pt-2.5">
                    <div class="mb-1.5 text-[10px] text-muted-foreground">最近请求</div>
                    <div class="flex flex-col gap-1.5">
                        ${state.recentTraffic.map(e => `
                            <div class="flex items-center gap-1.5 text-[10px]">
                                <span class="${e.method === 'GET' ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400' : 'border-blue-500/30 bg-blue-500/10 text-blue-400'} shrink-0 rounded border px-1 py-0 font-mono text-[9px]">${e.method}</span>
                                <span class="min-w-0 flex-1 truncate text-muted-foreground">${e.host}${e.url.replace(/^https?:\/\/[^/]+/, '').substring(0, 30)}</span>
                                <span class="${getStatusColor(e.status)} shrink-0 font-mono text-[9px]">${e.status}</span>
                            </div>
                        `).join('')}
                    </div>
                </div>
            `}
        </div>
    `;
}

function getStatusColor(code) {
    if (code < 300) return 'text-emerald-500';
    if (code < 400) return 'text-blue-400';
    if (code < 500) return 'text-amber-400';
    return 'text-red-400';
}

function getStatusBg(code) {
    if (code < 300) return 'bg-emerald-500';
    if (code < 400) return 'bg-blue-400';
    if (code < 500) return 'bg-amber-400';
    return 'bg-red-400';
}

function buildSystemStatusCard() {
    return `
        <div class="card p-4">
            <div class="mb-3 font-semibold text-xs">
                <svg class="size-3.5 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
                系统状态
            </div>
            <div class="flex flex-col gap-2">
                <div class="flex items-center justify-between rounded-lg border bg-muted/20 px-3 py-2">
                    <div class="text-[10px] text-muted-foreground">LLM 配置</div>
                    <span class="badge ${state.stats?.llm_configured ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400' : 'border-red-500/30 bg-red-500/10 text-red-400'} text-[10px]">${state.stats?.llm_configured ? '已配置' : '未配置'}</span>
                </div>
                <div class="flex items-center justify-between rounded-lg border bg-muted/20 px-3 py-2">
                    <div class="text-[10px] text-muted-foreground">流量捕获</div>
                    <span class="badge ${state.settings?.traffic_capture ? 'border-violet-500/30 bg-violet-500/10 text-violet-400' : 'text-muted-foreground'} text-[10px]">${state.settings?.traffic_capture ? '开启' : '关闭'}</span>
                </div>
                ${state.activeProfile ? `
                    <div class="flex items-center justify-between rounded-lg border bg-muted/20 px-3 py-2">
                        <div class="text-[10px] text-muted-foreground">激活模型</div>
                        <span class="font-mono text-[10px]">${state.activeProfile.model}</span>
                    </div>
                ` : ''}
            </div>
            ${state.pendingCount > 0 ? `
                <div class="mt-3">
                    <div class="mb-1.5 font-medium text-[10px] text-amber-400">待审批 (${state.pendingCount})</div>
                    <div class="flex flex-col gap-1.5">
                        ${state.pending.slice(0, 3).map(p => `
                            <a href="/system/intercept/approvals" class="flex items-center justify-between rounded-lg border border-amber-500/20 bg-amber-500/5 px-2.5 py-2 hover:bg-amber-500/10">
                                <div class="min-w-0"><div class="font-medium text-[10px]">${p.tool_name}</div><div class="text-[9px] text-muted-foreground">${p.agent_name}</div></div>
                                <svg class="size-3 shrink-0 text-amber-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/></svg>
                            </a>
                        `).join('')}
                        ${state.pendingCount > 3 ? `<a href="/system/intercept/approvals" class="text-center text-[10px] text-muted-foreground hover:text-foreground">还有 ${state.pendingCount - 3} 条…</a>` : ''}
                    </div>
                </div>
            ` : `
                <div class="mt-3 rounded-lg border bg-muted/10 px-3 py-3 text-center text-[10px] text-muted-foreground">
                    <svg class="mx-auto mb-1 size-4 text-emerald-500/50" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
                    无待审批拦截
                </div>
            `}
        </div>
    `;
}

async function loadData() {
    try {
        const [tasks, findings, stats, settings, pending, activity, tokens, conversationTokens] = await Promise.all([
            api.tasks(),
            api.findings(),
            api.stats(),
            api.settings(),
            api.interceptPending(),
            api.activity(undefined, { limit: 30 }),
            api.tokenStats(),
            api.conversationTokens(),
        ]);

        state.tasks = tasks.tasks || [];
        state.findings = findings;
        state.stats = stats;
        state.settings = settings;
        state.pending = pending;
        state.activity = activity.items || [];
        state.tokens = tokens.total ?? null;
        state.convTokens = conversationTokens;
        state.pendingCount = pending.length;

        // Derived data
        state.tasksByStatus = {};
        for (const t of state.tasks) state.tasksByStatus[t.status] = (state.tasksByStatus[t.status] ?? 0) + 1;

        state.findingsBySev = { critical: 0, high: 0, medium: 0, low: 0 };
        for (const f of state.findings) {
            if (f.severity in state.findingsBySev) state.findingsBySev[f.severity]++;
        }

        state.sortedTasks = [...state.tasks].sort((a, b) => (b.last_activity_unix ?? b.created_unix ?? 0) - (a.last_activity_unix ?? a.created_unix ?? 0)).slice(0, 5);
        state.recentFindings = [...state.findings].sort((a, b) => Date.parse(b.ts) - Date.parse(a.ts)).slice(0, 8);
        state.recentActivity = [...state.activity].filter(a => a.kind !== 'usage').sort((a, b) => b.seq - a.seq).slice(0, 6);

        // Load slow data
        loadSlowData();

        // Start polling
        startPolling();
    } catch (e) {
        console.error('Failed to load dashboard data:', e);
    }
}

async function loadSlowData() {
    try {
        const [traf, counts, agentList, mcpList, skillList, toolList, profileList, usage] = await Promise.all([
            api.traffic(0, 50),
            api.assetCounts(),
            api.agents(),
            api.mcpServers(),
            api.skills(),
            api.tools(),
            api.llmProfiles(),
            api.usageStats(365),
        ]);

        state.traffic = traf.exchanges ?? [];
        state.assetCounts = counts ?? {};
        state.agents = agentList;
        state.mcpServers = mcpList;
        state.skills = skillList;
        state.tools = toolList;
        state.llmProfiles = profileList;
        state.usageStats = usage;

        state.totalAssets = Object.values(state.assetCounts).reduce((a, b) => a + b, 0);
        state.assetByType = Object.entries(state.assetCounts)
            .filter(([, n]) => n > 0)
            .sort(([, a], [, b]) => b - a)
            .slice(0, 8);
        state.assetMax = state.assetByType[0]?.[1] ?? 1;

        state.trafficByCodes = Object.entries(
            state.traffic.reduce((m, e) => {
                const bucket = Math.floor(e.status / 100) * 100;
                m[bucket] = (m[bucket] ?? 0) + 1;
                return m;
            }, {})
        ).sort(([a], [b]) => Number(a) - Number(b)).map(([code, n]) => ({ code: Number(code), n }));
        state.trafficMax = Math.max(...state.trafficByCodes.map(x => x.n), 1);
        state.recentTraffic = [...state.traffic].sort((a, b) => Date.parse(b.ts) - Date.parse(a.ts)).slice(0, 5);

        state.activeProfile = state.llmProfiles.find(p => p.is_default);
        state.enabledTools = state.tools.filter(t => t.enabled);

        // Token data
        buildTokenData();

        render();
    } catch (e) {
        console.error('Failed to load slow data:', e);
    }
}

function buildTokenData() {
    state.defaultProfileId = state.activeProfile ? Number(state.activeProfile.id) : null;
    state.profileIdByName = new Map();
    for (const p of state.llmProfiles) state.profileIdByName.set(p.name, Number(p.id));

    // Old version
    state.tokenByProfileOld = new Map();
    const foldOld = (key, inp, out, cr, cw, addTask) => {
        const prev = state.tokenByProfileOld.get(key) ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, taskCount: 0 };
        state.tokenByProfileOld.set(key, {
            input: prev.input + inp, output: prev.output + out,
            cacheRead: prev.cacheRead + cr, cacheWrite: prev.cacheWrite + cw,
            taskCount: prev.taskCount + (addTask ? 1 : 0),
        });
    };
    for (const t of state.tasks) {
        foldOld(t.llm_profile_id ?? state.defaultProfileId,
            t.tokens?.input_tokens ?? 0, t.tokens?.output_tokens ?? 0,
            t.tokens?.cache_read_tokens ?? 0, t.tokens?.cache_write_tokens ?? 0, true);
    }
    for (const c of state.convTokens) {
        foldOld(c.llm_profile_id ?? state.defaultProfileId,
            c.input_tokens, c.output_tokens, c.cache_read_tokens, c.cache_write_tokens, false);
    }

    // New version
    state.tokenByProfileNew = new Map();
    for (const p of state.usageStats?.by_profile ?? []) {
        const key = state.profileIdByName.get(p.profile_name) ?? state.defaultProfileId;
        const prev = state.tokenByProfileNew.get(key) ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, taskCount: 0 };
        state.tokenByProfileNew.set(key, {
            input: prev.input + p.input_tokens, output: prev.output + p.output_tokens,
            cacheRead: prev.cacheRead + p.cache_read_tokens, cacheWrite: prev.cacheWrite + p.cache_write_tokens,
            taskCount: prev.taskCount + p.tasks,
        });
    }

    state.tokenByProfile = state.tokenVersion === 'new' ? state.tokenByProfileNew : state.tokenByProfileOld;

    // Daily data
    buildDailyTokenData();
}

function buildDailyTokenData() {
    const now = new Date();
    now.setDate(now.getDate() - state.tokenDays);
    const cutoff = now.toISOString().slice(0, 10);

    // Old daily
    state.dailyTokenDataOld = [];
    const mOld = new Map();
    const foldDaily = (date, inp, out, cr) => {
        if (date < cutoff) return;
        const prev = mOld.get(date) ?? { input: 0, output: 0, cacheRead: 0 };
        mOld.set(date, { input: prev.input + inp, output: prev.output + out, cacheRead: prev.cacheRead + cr });
    };
    for (const t of state.tasks) {
        if (state.tokenTab !== 'all' && (t.llm_profile_id ?? state.defaultProfileId) !== state.tokenTab) continue;
        foldDaily(t.created_at.slice(0, 10), t.tokens?.input_tokens ?? 0, t.tokens?.output_tokens ?? 0, t.tokens?.cache_read_tokens ?? 0);
    }
    for (const c of state.convTokens) {
        if (state.tokenTab !== 'all' && (c.llm_profile_id ?? state.defaultProfileId) !== state.tokenTab) continue;
        foldDaily(c.created_at.slice(0, 10), c.input_tokens, c.output_tokens, c.cache_read_tokens);
    }
    state.dailyTokenDataOld = [...mOld.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([date, v]) => ({ date: date.slice(5), ...v }));

    // New daily
    state.dailyTokenDataNew = [];
    const mNew = new Map();
    for (const d of state.usageStats?.daily ?? []) {
        if (d.date < cutoff) continue;
        if (state.tokenTab !== 'all' && (state.profileIdByName.get(d.profile_name) ?? state.defaultProfileId) !== state.tokenTab) continue;
        const prev = mNew.get(d.date) ?? { input: 0, output: 0, cacheRead: 0 };
        mNew.set(d.date, {
            input: prev.input + d.input_tokens,
            output: prev.output + d.output_tokens,
            cacheRead: prev.cacheRead + d.cache_read_tokens,
        });
    }
    state.dailyTokenDataNew = [...mNew.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([date, v]) => ({ date: date.slice(5), ...v }));

    state.dailyTokenData = state.tokenVersion === 'new' ? state.dailyTokenDataNew : state.dailyTokenDataOld;
}

function startPolling() {
    // Fast poll (5s)
    setInterval(async () => {
        try {
            const [findings, stats, settings, pending, activity, tokens, conversationTokens] = await Promise.all([
                api.findings(),
                api.stats(),
                api.settings(),
                api.interceptPending(),
                api.activity(undefined, { limit: 30 }),
                api.tokenStats(),
                api.conversationTokens(),
            ]);
            state.findings = findings;
            state.stats = stats;
            state.settings = settings;
            state.pending = pending;
            state.activity = activity.items || [];
            state.tokens = tokens.total ?? null;
            state.convTokens = conversationTokens;
            state.pendingCount = pending.length;
            buildTokenData();
            render();
        } catch (e) {
            // Retry on next interval
        }
    }, 5000);

    // Slow poll (15s)
    setInterval(async () => {
        try {
            const [traf, counts, agentList, mcpList, skillList, toolList, profileList, usage] = await Promise.all([
                api.traffic(0, 50),
                api.assetCounts(),
                api.agents(),
                api.mcpServers(),
                api.skills(),
                api.tools(),
                api.llmProfiles(),
                api.usageStats(365),
            ]);
            state.traffic = traf.exchanges ?? [];
            state.assetCounts = counts ?? {};
            state.agents = agentList;
            state.mcpServers = mcpList;
            state.skills = skillList;
            state.tools = toolList;
            state.llmProfiles = profileList;
            state.usageStats = usage;
            buildTokenData();
            render();
        } catch (e) {
            // Retry on next interval
        }
    }, 15000);
}

function bindEvents() {
    // Token version switch
    document.querySelectorAll('[data-token-version]').forEach(btn => {
        btn.addEventListener('click', () => {
            state.tokenVersion = btn.dataset.tokenVersion;
            buildTokenData();
            render();
        });
    });

    // Token tab switch
    document.querySelectorAll('[data-token-tab]').forEach(btn => {
        btn.addEventListener('click', () => {
            state.tokenTab = btn.dataset.tokenTab;
            buildTokenData();
            render();
        });
    });

    // Token days switch
    document.querySelectorAll('[data-token-days]').forEach(btn => {
        btn.addEventListener('click', () => {
            state.tokenDays = parseInt(btn.dataset.tokenDays);
            buildTokenData();
            render();
        });
    });
}

function render() {
    const app = document.getElementById('app');
    if (!app) return;
    app.innerHTML = buildDashboardHTML();
    bindEvents();

    // Render chart if data exists
    if (state.dailyTokenData.length > 0) {
        renderChart();
    }
}

function renderChart() {
    const canvas = document.getElementById('daily-token-chart');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const data = state.dailyTokenData;
    const labels = data.map(d => d.date);
    const inputData = data.map(d => d.input);
    const outputData = data.map(d => d.output);
    const cacheData = data.map(d => d.cacheRead);

    // Simple canvas chart
    const width = canvas.width = canvas.clientWidth * 2;
    const height = canvas.height = canvas.clientHeight * 2;
    const padding = 40;
    const chartWidth = width - padding * 2;
    const chartHeight = height - padding * 2;

    ctx.clearRect(0, 0, width, height);
    ctx.scale(2, 2);

    // Find max value
    const maxValue = Math.max(...inputData, ...outputData, ...cacheData, 1);

    // Draw axes
    ctx.strokeStyle = 'hsl(var(--border))';
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(padding, padding);
    ctx.lineTo(padding, height - padding);
    ctx.lineTo(width - padding, height - padding);
    ctx.stroke();

    // Draw bars
    const barWidth = chartWidth / labels.length * 0.8;
    const xStep = chartWidth / labels.length;

    labels.forEach((label, i) => {
        const x = padding + i * xStep + (xStep - barWidth) / 2;
        const yBase = height - padding;

        // Stack: input (bottom) + output + cacheRead (top)
        let currentY = yBase;
        const drawBar = (value, color) => {
            const barHeight = (value / maxValue) * chartHeight;
            ctx.fillStyle = color;
            ctx.fillRect(x, currentY - barHeight, barWidth, barHeight);
            currentY -= barHeight;
        };

        drawBar(inputData[i], 'hsl(217 91% 60%)');
        drawBar(outputData[i], 'hsl(263 70% 60%)');
        drawBar(cacheData[i], 'hsl(160 60% 45%)');

        // X-axis label
        ctx.fillStyle = 'hsl(var(--muted-foreground))';
        ctx.font = '10px Inter';
        ctx.textAlign = 'center';
        ctx.fillText(label, x + barWidth / 2, yBase + 20);
    });
}

// Register route
route('/function/dashboard', async () => {
    const { requireAuth } = await import('../core/auth.js');
    if (!requireAuth()) return;

    state = {
        tasks: [], findings: [], stats: null, settings: null, pending: [],
        activity: [], tokens: null, convTokens: [], usageStats: null,
        assetCounts: {}, traffic: [], agents: [], mcpServers: [], skills: [],
        tools: [], llmProfiles: [], tasksByStatus: {}, findingsBySev: {},
        sortedTasks: [], recentFindings: [], recentActivity: [], totalAssets: 0,
        assetByType: [], assetMax: 1, trafficByCodes: [], trafficMax: 1,
        recentTraffic: [], activeProfile: null, enabledTools: [], pendingCount: 0,
        dailyTokenData: [], tokenVersion: 'old', tokenTab: 'all', tokenDays: 30,
        llmProfiles: [], defaultProfileId: null, profileIdByName: new Map(),
        tokenByProfileOld: new Map(), tokenByProfileNew: new Map(),
        dailyTokenDataOld: [], dailyTokenDataNew: [],
    };

    render();
    await loadData();
});

export default { render };