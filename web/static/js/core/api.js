// API Client - Replaces lib/api.ts
import { getRuntimeConfig } from './config.js';
import { getCsrfToken } from './utils.js';

const TOKEN_KEY = 'artex_token';

/**
 * Get stored auth token
 */
export function getToken() {
    if (typeof window === 'undefined') return null;
    return localStorage.getItem(TOKEN_KEY);
}

/**
 * Set auth token
 */
export function setToken(token) {
    if (typeof window === 'undefined') return;
    if (token) {
        localStorage.setItem(TOKEN_KEY, token);
        // Also set cookie for SSE
        document.cookie = `artex_token=${token}; path=/; max-age=2592000; SameSite=Lax`;
    } else {
        localStorage.removeItem(TOKEN_KEY);
        document.cookie = 'artex_token=; path=/; max-age=0';
    }
}

/**
 * Check if user is authenticated
 */
export function isAuthenticated() {
    return !!getToken();
}

/**
 * Build API URL
 */
function apiUrl(path) {
    const base = getRuntimeConfig().apiBase || '';
    return `${base}/api${path}`;
}

/**
 * Build SSE URL
 */
export function sseUrl(path) {
    const base = getRuntimeConfig().sseBase ||
        (typeof window !== 'undefined' && window.location.hostname !== 'localhost'
            ? ''
            : `${window.location.protocol}//${window.location.hostname}:8787`);
    const token = getToken();
    const sep = path.includes('?') ? '&' : '?';
    return token ? `${base}${path}${sep}token=${encodeURIComponent(token)}` : `${base}${path}`;
}

/**
 * Core HTTP request
 */
async function http(path, init = {}) {
    const token = getToken();
    const headers = {
        ...(init.body ? { 'Content-Type': 'application/json' } : {}),
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(init.headers || {}),
    };

    // Add CSRF token for mutating requests
    if (['POST', 'PUT', 'PATCH', 'DELETE'].includes(init.method || 'GET')) {
        const csrf = getCsrfToken();
        if (csrf) headers['X-CSRF-Token'] = csrf;
    }

    const response = await fetch(apiUrl(path), {
        ...init,
        headers,
        credentials: 'same-origin',
    });

    // Handle 401 - unauthorized
    if (response.status === 401) {
        setToken(null);
        if (typeof window !== 'undefined' && !window.location.pathname.startsWith('/login')) {
            window.location.href = '/login';
        }
        throw new Error('未授权，请重新登录');
    }

    if (!response.ok) {
        let message = `${init.method || 'GET'} ${path}: ${response.status}`;
        try {
            const payload = await response.json();
            if (typeof payload.error === 'string' && payload.error.trim()) {
                message = payload.error.trim();
            }
        } catch {
            // Keep status-based fallback
        }
        throw new Error(message);
    }

    if (response.status === 204) return undefined;
    return response.json();
}

/**
 * HTTP method helpers
 */
const get = (path) => http(path);
const post = (path, body) => http(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined });
const put = (path, body) => http(path, { method: 'PUT', body: body ? JSON.stringify(body) : undefined });
const patch = (path, body) => http(path, { method: 'PATCH', body: body ? JSON.stringify(body) : undefined });
const del = (path, body) => http(path, { method: 'DELETE', body: body ? JSON.stringify(body) : undefined });

// Helper for nil slice coercion
const arr = (x) => x ?? [];

/**
 * Task query helpers
 */
function taskQuery(task) {
    return task ? `?task=${encodeURIComponent(task)}` : '';
}

/**
 * Finding filter params
 */
function findingFilterParams(q) {
    const p = new URLSearchParams();
    if (q.severity && q.severity !== 'all') p.set('severity', q.severity);
    if (q.status && q.status !== 'all') p.set('status', q.status);
    if (q.vulnclass && q.vulnclass !== 'all') p.set('vulnclass', q.vulnclass);
    if (q.task && q.task !== 'all') p.set('task_id', q.task);
    if (q.query?.trim()) p.set('q', q.query.trim());
    if (q.sort) p.set('sort', q.sort);
    if (q.assetScope) p.set('asset_scope', q.assetScope);
    return p;
}

// ============================================
// API Exports
// ============================================

export const api = {
    // Health & Version
    health: () => get('/health'),

    // Auth
    authStatus: () => get('/auth/status'),
    login: (username, password) => post('/auth/login', { username, password }),
    initPassword: (password) => post('/auth/init', { password }),
    changePassword: (oldPassword, newPassword) =>
        post('/auth/change-password', { old_password: oldPassword, new_password: newPassword }),

    // Tasks
    tasks: () => get('/tasks').then(r => ({ tasks: arr(r.tasks), active: r.active ?? '' })),
    task: (id) => get(`/tasks/${encodeURIComponent(id)}`),
    createTask: (input) => post('/tasks', {
        name: input.name ?? '',
        category_id: input.categoryId ?? null,
        description: input.description,
        goal: input.goal,
        llm_profile_ids: input.llmProfileIds ?? [],
        source_task_ids: input.sourceTaskIds ?? [],
        company_ids: input.companyIds ?? [],
        timeout_seconds: input.timeoutSeconds ?? 0,
        seed_first_intent: input.seedFirstIntent ?? false,
        plan_heartbeat_seconds: input.planHeartbeatSeconds ?? 0,
        coverage_enabled: input.coverageEnabled ?? true,
        intercept_rules: input.interceptRules ?? [],
    }),
    taskCategories: () => get('/task-categories').then(r => arr(r.categories)),
    updateTask: (id, input) => patch(`/tasks/${id}`, input),
    renameTask: (id, name) => patch(`/tasks/${id}`, { name }),
    pinTask: (id, pinned) => patch(`/tasks/${id}`, { pinned }),
    createTaskCategory: (name) => post('/task-categories', { name }),
    renameTaskCategory: (id, name) => patch(`/task-categories/${id}`, { name }),
    deleteTaskCategory: (id) => del(`/task-categories/${id}`),
    updateTaskCategory: (taskId, categoryId) =>
        patch(`/tasks/${taskId}/category`, { category_id: categoryId ?? null }),
    taskTemplates: () => get('/task-templates').then(r => arr(r.templates)),
    createTaskTemplate: (input) => post('/task-templates', input),
    updateTaskTemplate: (id, input) => patch(`/task-templates/${id}`, input),
    deleteTaskTemplate: (id) => del(`/task-templates/${id}`),
    updateTaskLLMProfiles: (id, llmProfileIds, activeLLMProfileId) =>
        put(`/tasks/${id}/llm`, { llm_profile_ids: llmProfileIds, active_llm_profile_id: activeLLMProfileId ?? null }),
    deleteTask: (id, options) => del(`/tasks/${id}`, options),
    controlTask: (id, action) => post(`/tasks/${id}/control`, { action }),
    controlTasksBatch: (taskIds, action) => post('/tasks/control/batch', { task_ids: taskIds, action }),
    taskArchives: (input = {}) => {
        const query = new URLSearchParams();
        query.set('page', String(input.page ?? 1));
        query.set('size', String(input.size ?? 20));
        if (input.q) query.set('q', input.q);
        if (input.state) query.set('state', input.state);
        return get(`/task-archives?${query.toString()}`).then(r => ({ ...r, items: arr(r.items) }));
    },
    taskArchive: (id) => get(`/task-archives/${id}`),
    archiveTask: (id) => post(`/tasks/${id}/archive`),
    archiveTasks: (taskIds) => post('/tasks/archive/batch', { task_ids: taskIds }),
    restoreTaskArchive: (id) => post(`/task-archives/${id}/restore`),
    restoreTaskArchives: (archiveIds) => post('/task-archives/restore/batch', { archive_ids: archiveIds }),
    deleteTaskArchive: (id) => del(`/task-archives/${id}`),
    deleteTaskArchives: (archiveIds) => post('/task-archives/delete/batch', { archive_ids: archiveIds }),
    controlIntent: (taskId, intentId, action, reason, mode = 'soft') =>
        post(`/tasks/${taskId}/intents/${intentId}/control`, { action, reason: reason ?? '', mode }),
    sendWorkerMessage: (taskId, intentId, message, requestId) =>
        post(`/tasks/${taskId}/intents/${intentId}/messages`, { message, request_id: requestId }),
    taskLLMResolution: (id) => get(`/tasks/${id}/llm/resolution`),
    rerunIntent: (taskId, intentId) => post(`/tasks/${taskId}/intents/${intentId}/rerun`),
    rerunBlocked: (taskId) => post(`/tasks/${taskId}/intents/rerun-blocked`),
    setActive: (id) => post('/active', { id }),

    // Stats
    stats: (task) => get(`/stats${taskQuery(task)}`),
    taskCoverage: (id) => get(`/tasks/${id}/coverage`),
    taskCoverageGraph: (id) => get(`/tasks/${id}/coverage-graph`),
    usageStats: (days = 365) => get(`/tokens/usage?days=${days}`),

    // Goals
    taskGoals: (id) => get(`/tasks/${id}/goals`).then(r => ({ goals: arr(r.goals) })),
    addGoal: (id, text, vulnclass) => post(`/tasks/${id}/goals`, { text, vulnclass: vulnclass ?? '' }),
    updateGoal: (id, goalId, text, vulnclass) =>
        patch(`/tasks/${id}/goals/${goalId}`, { text, vulnclass: vulnclass ?? '' }),
    deleteGoal: (id, goalId) => del(`/tasks/${id}/goals/${goalId}`),

    // Constraints
    taskConstraints: (id) => get(`/tasks/${id}/constraints`).then(r => ({ constraints: arr(r.constraints) })),
    addConstraint: (id, text, kind) => post(`/tasks/${id}/constraints`, { text, kind }),
    updateConstraint: (id, constraintId, text, kind) =>
        patch(`/tasks/${id}/constraints/${constraintId}`, { text, kind }),
    deleteConstraint: (id, constraintId) => del(`/tasks/${id}/constraints/${constraintId}`),

    // Intercept Rules (task-scoped)
    taskInterceptRules: (id) => get(`/tasks/${id}/intercept-rules`).then(r => arr(r.rules)),
    createTaskInterceptRule: (id, rule) => post(`/tasks/${id}/intercept-rules`, rule),
    updateTaskInterceptRule: (id, ruleId, rule) => put(`/tasks/${id}/intercept-rules/${ruleId}`, rule),
    deleteTaskInterceptRule: (id, ruleId) => del(`/tasks/${id}/intercept-rules/${ruleId}`),
    toggleTaskInterceptRule: (id, ruleId, enabled) =>
        post(`/tasks/${id}/intercept-rules/${ruleId}/toggle`, { enabled }),

    // Task Scope
    taskScope: (id) => get(`/tasks/${id}/scope`).then(r => ({ scope: r.scope })),
    addTaskScope: (id, kind, value, reason) => post(`/tasks/${id}/scope`, { kind, value, reason }),
    deleteTaskScope: (id, scopeId) => del(`/tasks/${id}/scope/${scopeId}`),
    taskAssetRefs: (id, assetId) => get(`/tasks/${id}/asset-refs?asset_id=${assetId}`),

    // Workspace
    workspaceList: (path = '') => get(`/workspace/list?path=${encodeURIComponent(path)}`),
    workspaceRead: (path) => get(`/workspace/read?path=${encodeURIComponent(path)}`),
    workspaceWrite: (path, content) => post('/workspace/write', { path, content }),
    workspaceMkdir: (path) => post('/workspace/mkdir', { path }),
    workspaceDelete: (path) => del(`/workspace/delete?path=${encodeURIComponent(path)}`),
    workspaceUpload: async (dir, files) => {
        const fd = new FormData();
        for (const f of files) fd.append('file', f);
        const token = getToken();
        const r = await fetch(apiUrl(`/workspace/upload?path=${encodeURIComponent(dir)}`), {
            method: 'POST',
            headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}) },
            body: fd,
        });
        if (!r.ok) throw new Error(`上传失败: ${r.status}`);
        return r.json();
    },
    workspaceDownload: async (path) => {
        const token = getToken();
        const r = await fetch(apiUrl(`/workspace/download?path=${encodeURIComponent(path)}`), {
            headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}) },
        });
        if (!r.ok) throw new Error(`下载失败: ${r.status}`);
        const blob = await r.blob();
        const objUrl = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = objUrl;
        a.download = path.split('/').pop() || 'download';
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(objUrl);
    },

    // Assets
    assets: (type = '', limit = 50, offset = 0) =>
        get(`/assets?type=${type}&limit=${limit}&offset=${offset}`).then(
            r => ({ assets: arr(r?.assets), total: r?.total ?? r?.count ?? 0 })
        ),
    searchAssets: (dsl, type = '', limit = 50, offset = 0) =>
        get(`/assets?dsl=${encodeURIComponent(dsl)}${type ? `&type=${encodeURIComponent(type)}` : ''}&limit=${limit}&offset=${offset}`).then(
            r => ({ assets: arr(r?.assets), total: r?.total ?? r?.count ?? 0 })
        ),
    assetCounts: (taskId = '') =>
        get(`/assets/counts${taskId ? `?task_id=${encodeURIComponent(taskId)}` : ''}`),
    deleteAssets: (ids) => http('/assets', { method: 'DELETE', body: JSON.stringify({ ids }) }),
    taskAssets: (taskId, type = '', limit = 50, offset = 0) =>
        get(`/assets?task_id=${encodeURIComponent(taskId)}&type=${encodeURIComponent(type)}&limit=${limit}&offset=${offset}`).then(
            r => ({ assets: arr(r?.assets), total: r?.total ?? r?.count ?? 0 })
        ),
    searchTaskAssets: (taskId, dsl, type = '', limit = 50, offset = 0) =>
        get(`/assets?task_id=${encodeURIComponent(taskId)}&dsl=${encodeURIComponent(dsl)}&type=${encodeURIComponent(type)}&limit=${limit}&offset=${offset}`).then(
            r => ({ assets: arr(r?.assets), total: r?.total ?? r?.count ?? 0 })
        ),
    attachTaskAssets: (taskId, assetIds, sourceSummary) =>
        post(`/tasks/${taskId}/assets`, { asset_ids: assetIds, source_summary: sourceSummary }),
    registerTaskAssetScopes: (taskId, scope) =>
        post(`/tasks/${taskId}/assets`, { scope }),
    detachTaskAsset: (taskId, assetId) => del(`/tasks/${taskId}/assets/${assetId}`),
    taskIntentAssets: (taskId) => get(`/tasks/${taskId}/intent-assets`).then(r => arr(r.assets)),

    // Companies
    companies: () => get('/companies').then(arr),
    createCompany: (name, scope) => post('/companies', { name, scope }),
    addCompanyScope: (id, scope, reason = '') => post(`/companies/${id}/scope`, { scope, reason }),
    updateCompanyScope: (id, scope, reason = '') => post(`/companies/${id}/scope`, { scope, reason, reset: true }),
    deleteCompany: (id, deleteAssets = false) =>
        http(`/companies/${id}`, { method: 'DELETE', body: JSON.stringify({ delete_assets: deleteAssets }) }),

    // Exploration
    frontier: (task) => get(`/exploration/frontier${taskQuery(task)}`).then(arr),
    findings: (task) => get(`/exploration/findings${taskQuery(task)}`).then(arr),
    findingsPage: (q) => {
        const p = findingFilterParams(q);
        p.set('page', String(q.page));
        p.set('limit', String(q.pageSize));
        return get(`/exploration/findings?${p.toString()}`);
    },
    findingGroups: (q) => {
        const p = findingFilterParams(q);
        p.set('page', String(q.page));
        p.set('limit', String(q.pageSize));
        return get(`/exploration/findings/groups?${p.toString()}`);
    },
    findingAssetTree: (q) => get(`/exploration/findings/asset-tree?${findingFilterParams(q).toString()}`),
    findingStats: () => get('/exploration/findings/stats'),
    exportFindings: async (opts) => {
        const p = new URLSearchParams({ format: opts.format, scope: opts.scope });
        if (opts.scope === 'selected') {
            p.set('ids', (opts.ids ?? []).join(','));
        } else if (opts.scope === 'filtered' && opts.filters) {
            for (const [k, v] of findingFilterParams(opts.filters)) p.set(k, v);
        }
        const token = getToken();
        const r = await fetch(apiUrl(`/exploration/findings/export?${p.toString()}`), {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!r.ok) throw new Error(`export: ${r.status}`);
        const blob = await r.blob();
        const disp = r.headers.get('Content-Disposition') ?? '';
        const m = disp.match(/filename="?([^"]+)"?/);
        const filename = m?.[1] ?? `findings-export`;
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
    },
    getFinding: (id, contextTaskId) =>
        get(`/exploration/findings/${id}${contextTaskId ? `?context_task=${encodeURIComponent(contextTaskId)}` : ''}`),
    findingTraffic: (id, contextTask) =>
        get(`/exploration/findings/${id}/traffic${contextTask ? `?context_task=${encodeURIComponent(contextTask)}` : ''}`),
    bindFindingTraffic: (id, traffic_refs, contextTask) =>
        post(`/exploration/findings/${id}/traffic${contextTask ? `?context_task=${encodeURIComponent(contextTask)}` : ''}`, { traffic_refs }),
    editFindingTraffic: (id, bindingId, version, fields, contextTask) =>
        patch(`/exploration/findings/${id}/traffic/${bindingId}${contextTask ? `?context_task=${encodeURIComponent(contextTask)}` : ''}`, { version, ...fields }),
    removeFindingTraffic: (id, bindingId, version, contextTask) =>
        del(`/exploration/findings/${id}/traffic/${bindingId}${contextTask ? `?context_task=${encodeURIComponent(contextTask)}` : ''}`, { version }),
    orderFindingTraffic: (id, binding_ids, version, contextTask) =>
        http(`/exploration/findings/${id}/traffic/order${contextTask ? `?context_task=${encodeURIComponent(contextTask)}` : ''}`, {
            method: 'PUT',
            body: JSON.stringify({ binding_ids, version }),
        }),
    findingTrafficDetail: (id, bindingId, contextTask) =>
        get(`/exploration/findings/${id}/traffic/${bindingId}${contextTask ? `?context_task=${encodeURIComponent(contextTask)}` : ''}`),
    findingTrafficBody: (id, bindingId, side, offset, contextTask) =>
        get(`/exploration/findings/${id}/traffic/${bindingId}/body?side=${side}&offset=${offset}${contextTask ? `&context_task=${encodeURIComponent(contextTask)}` : ''}`),
    downloadFindingTrafficBody: async (id, bindingId, side, contextTask) => {
        const token = getToken();
        const response = await fetch(
            apiUrl(`/exploration/findings/${id}/traffic/${bindingId}/body?side=${side}&download=1${contextTask ? `&context_task=${encodeURIComponent(contextTask)}` : ''}`),
            { headers: token ? { Authorization: `Bearer ${token}` } : {} }
        );
        if (!response.ok) {
            const error = await response.json().catch(() => ({ error: '下载失败' }));
            throw new Error(error.error ?? '下载失败');
        }
        const blob = await response.blob();
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `evidence-${bindingId}-${side}.bin`;
        a.click();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
    },
    findingLineage: (id) => get(`/exploration/findings/${id}/lineage`),
    setFindingStatus: (id, status) => patch(`/exploration/findings/${id}`, { status }),
    setFindingSeverity: (id, severity) => patch(`/exploration/findings/${id}`, { severity }),
    updateFinding: (id, fields) => patch(`/exploration/findings/${id}`, fields),
    deleteFinding: (id) => del(`/exploration/findings/${id}`),
    findingRetests: (id) =>
        get(`/exploration/findings/${encodeURIComponent(id)}/retests`).then(r => arr(r.retests)),
    activeFindingRetests: () =>
        get('/exploration/findings/retests/active').then(r => arr(r.retests)),
    startFindingRetest: (id, notes) =>
        post(`/exploration/findings/${encodeURIComponent(id)}/retests`, { notes }),
    deepenFinding: (id, description) =>
        post(`/exploration/findings/${id}/deepen`, { description }),
    intents: (task) => get(`/exploration/intents${taskQuery(task)}`).then(arr),
    tokenStats: (task) =>
        get(`/exploration/tokens${taskQuery(task)}`).then(r => ({
            workers: arr(r.workers),
            sessions: arr(r.sessions),
            total: r.total,
        })),
    tokenDaily: (days = 30) => get(`/tokens/daily?days=${days}`).then(arr),
    conversationTokens: () => get('/tokens/conversations').then(arr).catch(() => []),
    explorationGraph: (task) => get(`/exploration/graph${taskQuery(task)}`),
    explorationNodes: (task, query = {}) => {
        const q = new URLSearchParams();
        if (task) q.set('task', task);
        q.set('page', String(query.page ?? 1));
        q.set('size', String(query.size ?? 20));
        if (query.kinds?.length) q.set('kind', query.kinds.join(','));
        if (query.states?.length) q.set('state', query.states.join(','));
        if (query.q?.trim()) q.set('q', query.q.trim());
        if (query.order === 'asc') q.set('order', 'asc');
        return get(`/exploration/nodes?${q.toString()}`).then(r => ({
            items: arr(r.items),
            total: r.total ?? 0,
            page: r.page ?? 1,
            size: r.size ?? query.size ?? 20,
            edges: arr(r.edges),
            refs: r.refs ?? {},
            assets: r.assets ?? {},
        }));
    },
    activity: (task, opts = {}) => {
        const q = new URLSearchParams();
        if (task) q.set('task', task);
        if (opts.intent) q.set('intent', opts.intent);
        if (opts.since) q.set('since', String(opts.since));
        if (opts.limit) q.set('limit', String(opts.limit));
        return get(`/exploration/activity?${q.toString()}`).then(r => ({
            items: arr(r.items),
            cursor: r.cursor ?? 0,
        }));
    },
    activityDetail: (id, task) => get(`/exploration/activity/${id}${taskQuery(task)}`),
    activityHistory: (task, session, before = 0, limit = 200) => {
        const q = new URLSearchParams({ session, limit: String(limit) });
        if (task) q.set('task', task);
        if (before > 0) q.set('before', String(before));
        return get(`/exploration/activity/history?${q.toString()}`).then(r => ({
            items: arr(r.items),
            snapshotCursor: r.snapshot_cursor ?? 0,
            earliestCursor: r.earliest_cursor ?? 0,
            hasMore: !!r.has_more,
        }));
    },
    intentsPage: (task, before = 0, limit = 300) => {
        const q = new URLSearchParams({ limit: String(limit) });
        if (task) q.set('task', task);
        q.set('before', String(before > 0 ? before : 0));
        q.set('page', '1');
        return get(`/exploration/intents?${q.toString()}`).then(r => ({
            items: arr(r.items),
            hasMore: !!r.has_more,
        }));
    },
    mainSessions: (task) =>
        get(`/exploration/main-sessions${taskQuery(task)}`).then(r => ({
            sessions: arr(r.sessions),
            current: r.current ?? 0,
        })),
    newMainSession: (task) => post(`/exploration/main-session/new${taskQuery(task)}`),

    // Traffic
    traffic: (page = 0, size = 100, host = '', method = '', q = '', opts = {}) => {
        const params = new URLSearchParams({ page: String(page), size: String(size) });
        if (host) params.set('host', host);
        if (method && method !== 'all') params.set('method', method);
        if (q) params.set('q', q);
        if (opts.body) params.set('body', opts.body);
        if (opts.path) params.set('path', opts.path);
        if (opts.status && opts.status !== 'all') params.set('status', opts.status);
        if (opts.respMin) params.set('resp_min', opts.respMin);
        if (opts.respMax) params.set('resp_max', opts.respMax);
        if (opts.sort) params.set('sort', opts.sort);
        if (opts.order) params.set('order', opts.order);
        return get(`/traffic?${params.toString()}`);
    },
    trafficExchange: (id) => get(`/traffic/exchange?id=${encodeURIComponent(id)}`),
    trafficHosts: () => get(`/traffic/hosts`),
    trafficDeleteHost: (host) => del(`/traffic?host=${encodeURIComponent(host)}`),
    trafficDeleteHosts: (hosts) => del('/traffic/hosts', { hosts }),
    trafficDeleteAll: () => del('/traffic/all'),

    // Settings
    settings: () => get('/settings'),
    setSettings: (patch) => put('/settings', patch),
    testWebSearch: (patch) => post('/settings/web-search/test', patch),
    detectPython: () => post('/settings/python/detect', {}),

    // Notify
    notifyMeta: () => get('/notify/meta'),
    notifyChannels: () => get('/notify/channels').then(r => arr(r.channels)),
    notifyCreateChannel: (payload) => post('/notify/channels', payload),
    notifyUpdateChannel: (id, payload) => patch(`/notify/channels/${id}`, payload),
    notifyDeleteChannel: (id) => del(`/notify/channels/${id}`),
    notifyTestChannel: (id) => post(`/notify/channels/${id}/test`),
    notifyDeliveries: (q = {}) => {
        const p = new URLSearchParams();
        if (q.channelId) p.set('channel_id', String(q.channelId));
        if (q.state) p.set('state', q.state);
        p.set('page', String(q.page ?? 1));
        p.set('page_size', String(q.pageSize ?? 50));
        return get(`/notify/deliveries?${p.toString()}`).then(r => ({ ...r, deliveries: arr(r.deliveries) }));
    },
    notifyRetryDelivery: (id) => post(`/notify/deliveries/${id}/retry`),

    // Report
    report: async (task) => {
        const token = getToken();
        const r = await fetch(apiUrl(`/report${taskQuery(task)}`), {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!r.ok) throw new Error(`report: ${r.status}`);
        return r.text();
    },
    downloadTaskDocx: async (task) => {
        const token = getToken();
        const r = await fetch(apiUrl(`/report/export${taskQuery(task)}`), {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!r.ok) throw new Error(`docx: ${r.status}`);
        const blob = await r.blob();
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = `artex-report-${task || 'active'}-${new Date().toISOString().slice(0, 10)}.docx`;
        a.click();
        URL.revokeObjectURL(a.href);
    },
    downloadFindingDocx: async (id) => {
        const token = getToken();
        const r = await fetch(apiUrl(`/findings/${id}/export`), {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!r.ok) throw new Error(`docx: ${r.status}`);
        const blob = await r.blob();
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = `artex-finding-${id}-${new Date().toISOString().slice(0, 10)}.docx`;
        a.click();
        URL.revokeObjectURL(a.href);
    },

    // Chat
    chatMentions: (kind, query, signal, cursor = '') =>
        http(`/chat/mentions?${new URLSearchParams({ kind, q: query, cursor })}`, { signal }),
    chat: (message, task, attachments, seg) => post(`/chat${taskQuery(task)}`, { message, attachments, seg }),
    chatStatus: (taskId) => get(`/tasks/${taskId}/chat/status`),
    chatUpload: async (scope, id, files) => {
        const fd = new FormData();
        for (const f of files) fd.append('file', f);
        const token = getToken();
        const r = await fetch(apiUrl(`/chat/upload?scope=${scope}&id=${encodeURIComponent(id)}`), {
            method: 'POST',
            headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}) },
            body: fd,
        });
        if (!r.ok) throw new Error(`上传失败: ${r.status} ${await r.text()}`);
        return r.json();
    },
    stopChat: (taskId) => post(`/tasks/${taskId}/chat/stop`, {}),

    // LLM
    getLLM: () => get('/llm'),
    setLLM: (provider, model, base_url, api_key, rate_per_second = 0, rate_per_minute = 0, proxy = '', context_window_k = 0) =>
        post('/llm', { provider, model, base_url, proxy, api_key, rate_per_second, rate_per_minute, context_window_k }),
    testLLM: (provider, model, base_url, api_key, proxy = '', thinking_type = '', reasoning_effort = '', profile_id, streaming = true, session_header_key = '') =>
        post('/llm/test', { provider, model, base_url, proxy, api_key, thinking_type, reasoning_effort, profile_id, streaming, session_header_key }),
    llmProfiles: () => get('/llm/profiles').then(r => arr(r.profiles)),
    saveLLMProfile: (p) => post('/llm/profiles', p),
    deleteLLMProfile: (id) => del(`/llm/profiles/${id}`),
    activateLLMProfile: (id) => post('/llm/profiles/active', { id: Number(id) }),
    llmPool: () => get('/llm/pool'),
    resetLLMPool: (id) => post('/llm/pool/reset', { id: id ? Number(id) : 0 }),
    llmRetryPolicy: () => get('/llm/retry-policy'),
    saveLLMRetryPolicy: (p) => post('/llm/retry-policy', p),
    fetchLLMModels: (provider, base_url, api_key, proxy = '', profile_id) =>
        post('/llm/models', { provider, base_url, api_key, proxy, profile_id }),

    // Agents
    agents: () => get('/agents').then(r => arr(r.agents)),
    getAgent: (key) => get(`/agents/${key}`),
    createAgent: (key, name, description = '') => post('/agents', { key, name, description }),
    updateAgent: (key, name, description = '') => patch(`/agents/${key}`, { name, description }),
    deleteAgent: (key) => del(`/agents/${key}`),
    agentTriggers: (key) => get(`/agents/${key}/triggers`).then(r => arr(r.triggers)),
    createTrigger: (key, t) => post(`/agents/${key}/triggers`, t),
    updateTrigger: (id, t) => patch(`/triggers/${id}`, t),
    deleteTrigger: (id) => del(`/triggers/${id}`),
    saveAgentConfig: (key, patch) => put(`/agents/${key}/config`, patch),
    agentPromptVersions: (key) => get(`/agents/${key}/prompts`).then(r => arr(r.versions)),
    agentVariables: (key) => get(`/agents/${key}/variables`).then(r => arr(r.variables)),
    previewAgentPrompt: (key, template, sample) => post(`/agents/${key}/prompt/preview`, { template, sample }),
    getAgentVisibility: (key) => get(`/agents/${key}/visibility`),
    setAgentVisibility: (key, mcp, skill) => put(`/agents/${key}/visibility`, { mcp, skill }),
    saveAgentPrompt: (key, template, note = '') => put(`/agents/${key}/prompt`, { template, note }),
    resetAgentPrompt: (key) => post(`/agents/${key}/prompt/reset`, {}),
    saveAgentWrapup: (key, prompt, maxTurns) => put(`/agents/${key}/wrapup`, { prompt, max_turns: maxTurns }),
    resetAgentWrapup: (key) => post(`/agents/${key}/wrapup/reset`, {}),
    saveAgentTaskTimeoutWrapup: (key, prompt, maxTurns) => put(`/agents/${key}/wrapup/task-timeout`, { prompt, max_turns: maxTurns }),
    resetAgentTaskTimeoutWrapup: (key) => post(`/agents/${key}/wrapup/task-timeout/reset`, {}),

    // Conversations
    conversations: () => get('/conversations').then(r => arr(r.conversations)),
    createConversation: (agent_key, title = '', llm_profile_id) =>
        post('/conversations', { agent_key, title, llm_profile_id: llm_profile_id ?? null }),
    updateConversation: (id, input) => patch(`/conversations/${id}`, input),
    renameConversation: (id, title) => patch(`/conversations/${id}`, { title }),
    pinConversation: (id, pinned) => patch(`/conversations/${id}`, { pinned }),
    updateConversationProfile: (id, llm_profile_id) => patch(`/conversations/${id}/profile`, { llm_profile_id }),
    deleteConversation: (id) => del(`/conversations/${id}`),
    deleteConversations: (ids) => post('/conversations/delete/batch', { ids }),
    conversationMessages: (id, since = 0) =>
        get(`/conversations/${id}/messages?since=${since}`).then(r => ({
            items: arr(r.items),
            cursor: r.cursor ?? 0,
            running: !!r.running,
        })),
    conversationHistory: (id, before = 0, limit = 200) => {
        const q = new URLSearchParams({ limit: String(limit) });
        if (before > 0) q.set('before', String(before));
        return get(`/conversations/${id}/messages?${q.toString()}`).then(r => ({
            items: arr(r.items),
            cursor: r.cursor ?? 0,
            running: !!r.running,
            hasMore: !!r.hasMore,
        }));
    },
    conversationMsgDetail: (id, seq) => get(`/conversations/${id}/messages/${seq}`).then(r => r.detail ?? ''),
    sendConversationMessage: (id, message, attachments) =>
        post(`/conversations/${id}/messages`, { message, attachments }),
    stopConversation: (id) => post(`/conversations/${id}/stop`, {}),

    // Tools
    tools: () => get('/tools').then(r => arr(r.tools)),
    saveTool: (key, patch) => put(`/tools/${key}`, patch),
    resetTool: (key) => post(`/tools/${key}/reset`, {}),
    createCustomTool: (t) => post('/tools/custom', t),
    updateCustomTool: (key, t) => put(`/tools/custom/${key}`, t),
    deleteCustomTool: (key) => del(`/tools/custom/${key}`),
    testCustomTool: (body) => post('/tools/custom/test', body),

    // MCP
    mcpServers: () => get('/mcp').then(r => arr(r.servers)),
    saveMcpServer: (m) => post('/mcp', m),
    deleteMcpServer: (id) => del(`/mcp/${id}`),
    mcpTools: (id) => get(`/mcp/${id}/tools`).then(r => arr(r.tools)),
    refreshMcpServer: (id) => post(`/mcp/${id}/refresh`, {}).then(r => arr(r.tools)),

    // ScopeSentry
    ssStatus: () => get('/sync/scopesentry/status'),
    ssDatasource: (body) => post('/sync/scopesentry/datasource', body),
    ssProjects: (page = 1, size = 50, search = '') =>
        get(`/sync/scopesentry/projects?page=${page}&size=${size}${search ? `&search=${encodeURIComponent(search)}` : ''}`).then(
            r => ({ projects: arr(r.projects), tag: r.tag ?? {} })
        ),
    ssTasks: (page = 1, size = 50, search = '') =>
        get(`/sync/scopesentry/tasks?page=${page}&size=${size}${search ? `&search=${encodeURIComponent(search)}` : ''}`).then(
            r => arr(r.tasks)
        ),
    ssSync: (body) => post('/sync/scopesentry/sync', body),

    // Skills
    skills: () => get('/skills').then(r => arr(r.skills)),
    createSkill: (s) => post('/skills', s),
    uploadSkill: async (file, overwrite = false) => {
        const fd = new FormData();
        fd.append('file', file);
        const token = getToken();
        const r = await fetch(apiUrl(`/skills/upload${overwrite ? '?overwrite=true' : ''}`), {
            method: 'POST',
            headers: token ? { Authorization: `Bearer ${token}` } : {},
            body: fd,
        });
        const body = await r.json().catch(() => ({}));
        if (!r.ok) throw new Error(body?.error || `上传失败(${r.status})`);
        return body;
    },
    deleteSkill: (name) => del(`/skills/${name}`),
    updateSkillMeta: (name, meta) => put(`/skills/${name}/meta`, meta),
    createSkillDir: (skill, path) => post(`/skills/${skill}/dirs`, { path }),
    skillFiles: (name) => get(`/skills/${name}/files`).then(r => r.files),
    readSkillFile: (name, file) => get(`/skills/${name}/files/${file}`).then(r => r.content),
    writeSkillFile: (name, file, content) => put(`/skills/${name}/files/${file}`, { content }),
    deleteSkillPath: (skill, path) => del(`/skills/${skill}/files/${path}`),
    skillUsage: (name, limit = 50) => get(`/skills/${name}/usage?limit=${limit}`).then(r => arr(r.calls)),
    missingSkills: (limit = 20) => get(`/skills/missing?limit=${limit}`).then(r => arr(r.missing)),

    // Visibility
    resourceVisibility: (kind, id) => get(`/visibility/${kind}/${id}`).then(r => arr(r.agents)),
    toggleVisibility: (agentId, kind, resourceId, visible) =>
        post('/visibility/toggle', { agent_id: agentId, kind, resource_id: resourceId, visible }),
    skillVisibility: (name) => get(`/visibility/skill/${name}`).then(r => arr(r.agents)),
    toggleSkillVisibility: (agentId, skillName, visible) =>
        post('/visibility/skill/toggle', { agent_id: agentId, skill_name: skillName, visible }),

    // Intercept
    interceptRules: () => get('/intercept/rules').then(r => arr(r.rules)),
    createInterceptRule: (rule) => post('/intercept/rules', rule),
    updateInterceptRule: (id, rule) => put(`/intercept/rules/${id}`, rule),
    deleteInterceptRule: (id) => del(`/intercept/rules/${id}`),
    toggleInterceptRule: (id, enabled) => post(`/intercept/rules/${id}/toggle`, { enabled }),
    assetInterceptRules: () => get('/asset-intercept/rules').then(r => arr(r.rules)),
    createAssetInterceptRule: (rule) => post('/asset-intercept/rules', rule),
    updateAssetInterceptRule: (id, rule) => put(`/asset-intercept/rules/${id}`, rule),
    deleteAssetInterceptRule: (id) => del(`/asset-intercept/rules/${id}`),
    toggleAssetInterceptRule: (id, enabled) => post(`/asset-intercept/rules/${id}/toggle`, { enabled }),
    interceptPending: () => get('/intercept/pending').then(r => arr(r.pending)),
    interceptGetOne: (id) => get(`/intercept/pending/${id}`),
    interceptDecide: (id, decision) => post(`/intercept/pending/${id}/decide`, { decision }),
    interceptExecution: (id, conversationId) =>
        get(`/intercept/history/${id}/execution${conversationId ? `?conversation=${conversationId}` : ''}`),
    interceptDetail: (id) => get(`/intercept/history/${id}`),
    interceptHistory: () => get('/intercept/history').then(r => arr(r.items)),
    interceptHistoryPage: (page = 1, size = 20, filter = {}) =>
        get(`/intercept/history?${new URLSearchParams({ page: String(page), size: String(size), ...filter }).toString()}`).then(
            r => ({ items: arr(r.items), total: r.total ?? r.items?.length ?? 0 })
        ),
    interceptTask: (taskId) => get(`/intercept/task/${taskId}`).then(r => arr(r.items)),
    interceptTaskPage: (taskId, page = 1, size = 20, filter = {}) =>
        get(`/intercept/task/${encodeURIComponent(taskId)}?${new URLSearchParams({ page: String(page), size: String(size), ...filter }).toString()}`).then(
            r => ({ items: arr(r.items), total: r.total ?? r.items?.length ?? 0 })
        ),
    interceptGetToolConfig: async () => {
        const token = getToken();
        const r = await fetch(apiUrl('/intercept/tool-config'), {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!r.ok) throw new Error(await r.text());
        return r.json();
    },
    interceptSetToolConfig: async (enabledTools) => {
        const token = getToken();
        const r = await fetch(apiUrl('/intercept/tool-config'), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
            body: JSON.stringify({ enabled_tools: enabledTools }),
        });
        if (!r.ok) throw new Error(await r.text());
    },
    interceptGetJudgeConfig: () => get('/intercept/judge'),
    interceptSetJudgeConfig: (cfg) => put('/intercept/judge', cfg),
    interceptJudgeUsage: (days = 30) => get(`/intercept/judge/usage?days=${days}`),

    // Commands
    commands: (params = {}) => {
        const sp = new URLSearchParams();
        if (params.task) sp.set('task', params.task);
        if (params.q) sp.set('q', params.q);
        sp.set('page', String(params.page ?? 0));
        sp.set('size', String(params.size ?? 50));
        return get(`/commands?${sp}`);
    },
    commandStats: (params = {}) => {
        const sp = new URLSearchParams();
        if (params.task) sp.set('task', params.task);
        if (params.q) sp.set('q', params.q);
        return get(`/commands/stats?${sp}`);
    },

    // LLM Records
    llmRecords: (params = {}) => {
        const sp = new URLSearchParams();
        if (params.model) sp.set('model', params.model);
        if (params.session) sp.set('session', params.session);
        if (params.task) sp.set('task', params.task);
        if (params.page !== undefined) sp.set('page', String(params.page));
        sp.set('size', String(params.size ?? 50));
        return get(`/llm/records?${sp}`).then(r => ({ records: arr(r.records), total: r.total }));
    },
    llmRecordDetail: (id) => get(`/llm/records/${id}`),
    llmTasks: () => get('/llm/records/tasks').then(r => arr(r.tasks)),
    llmRecordsDeleteTask: (task) => del(`/llm/records?task=${encodeURIComponent(task)}`),
    tokensByModel: (task) => get(`/llm/records/by-model?task=${encodeURIComponent(task)}`),

    // Self-update
    checkUpdate: (force = false) => get(`/update/check${force ? '?force=1' : ''}`),
    applyUpdate: () => post('/update/apply'),
    rollbackUpdate: () => post('/update/rollback'),

    // Proxy Pool
    proxyPoolGet: () => get('/proxypool'),
    proxyPoolPut: (payload) => put('/proxypool', payload),

    // Vulnintel
    vulnintelStatus: () => get('/vulnintel/status'),

    // Tools Inventory
    toolsInventory: () => get('/tools/inventory'),

    // GC
    gc: (ttl = 86400) => post(`/gc?ttl=${ttl}`, {}),
};

// Re-export token helpers
export { getToken, setToken, isAuthenticated, sseUrl };