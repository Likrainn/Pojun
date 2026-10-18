// Internationalization Module
import { APP_CONFIG } from './config.js';

const LOCALES = {
    'zh-CN': {},
    'en-US': {},
};

let currentLocale = APP_CONFIG.i18n.defaultLocale;
let localeData = {};

/**
 * Load locale messages
 */
async function loadLocale(locale) {
    if (LOCALES[locale]) {
        localeData = LOCALES[locale];
        return localeData;
    }
    try {
        const response = await fetch(`/static/i18n/${locale}.json`);
        if (response.ok) {
            localeData = await response.json();
            LOCALES[locale] = localeData;
            return localeData;
        }
    } catch (e) {
        console.warn(`Failed to load locale: ${locale}`, e);
    }
    // Fallback to default
    if (locale !== APP_CONFIG.i18n.defaultLocale) {
        return loadLocale(APP_CONFIG.i18n.defaultLocale);
    }
    localeData = {};
    return localeData;
}

/**
 * Initialize i18n
 */
export async function initI18n(locale) {
    currentLocale = locale || localStorage.getItem('locale') || APP_CONFIG.i18n.defaultLocale;
    await loadLocale(currentLocale);
    document.documentElement.lang = currentLocale;
    return currentLocale;
}

/**
 * Get current locale
 */
export function getLocale() {
    return currentLocale;
}

/**
 * Set locale
 */
export async function setLocale(locale) {
    if (!APP_CONFIG.i18n.supportedLocales.includes(locale)) {
        console.warn(`Unsupported locale: ${locale}`);
        return false;
    }
    currentLocale = locale;
    localStorage.setItem('locale', locale);
    await loadLocale(locale);
    document.documentElement.lang = locale;
    // Emit locale change event
    window.dispatchEvent(new CustomEvent('i18n:change', { detail: { locale } }));
    return true;
}

/**
 * Get supported locales
 */
export function getSupportedLocales() {
    return [...APP_CONFIG.i18n.supportedLocales];
}

/**
 * Translate function
 */
export function t(key, params = {}) {
    const keys = key.split('.');
    let value = localeData;
    for (const k of keys) {
        if (value && typeof value === 'object' && k in value) {
            value = value[k];
        } else {
            return key; // Return key if not found
        }
    }
    if (typeof value !== 'string') return key;
    // Replace params
    return value.replace(/\{(\w+)\}/g, (match, param) => params[param] ?? match);
}

/**
 * Translate with pluralization
 */
export function tc(key, count, params = {}) {
    const pluralKey = count === 1 ? `${key}.one` : `${key}.other`;
    let value = t(pluralKey, { count, ...params });
    if (value === pluralKey) {
        value = t(key, { count, ...params });
    }
    return value;
}

/**
 * Get localized date format
 */
export function formatDate(date, options = {}) {
    const locales = {
        'zh-CN': 'zh-CN',
        'en-US': 'en-US',
    };
    return new Intl.DateTimeFormat(locales[currentLocale] || 'zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        ...options,
    }).format(date);
}

/**
 * Get localized relative time
 */
export function formatRelativeTime(date) {
    const rtf = new Intl.RelativeTimeFormat(
        { 'zh-CN': 'zh-CN', 'en-US': 'en-US' }[currentLocale] || 'zh-CN',
        { numeric: 'auto' }
    );
    const diff = date.getTime() - Date.now();
    const seconds = Math.round(diff / 1000);
    const minutes = Math.round(seconds / 60);
    const hours = Math.round(minutes / 60);
    const days = Math.round(hours / 24);
    if (Math.abs(seconds) < 60) return rtf.format(seconds, 'second');
    if (Math.abs(minutes) < 60) return rtf.format(minutes, 'minute');
    if (Math.abs(hours) < 24) return rtf.format(hours, 'hour');
    return rtf.format(days, 'day');
}

/**
 * Get localized number format
 */
export function formatNumber(num, options = {}) {
    return new Intl.NumberFormat(
        { 'zh-CN': 'zh-CN', 'en-US': 'en-US' }[currentLocale] || 'zh-CN',
        options
    ).format(num);
}

/**
 * Listen for locale changes
 */
export function onLocaleChange(callback) {
    const handler = (e) => callback(e.detail.locale);
    window.addEventListener('i18n:change', handler);
    return () => window.removeEventListener('i18n:change', handler);
}

// Pre-defined translations (can be overridden by JSON files)
LOCALES['zh-CN'] = {
    common: {
        loading: '加载中...',
        saving: '保存中...',
        saved: '已保存',
        error: '错误',
        success: '成功',
        warning: '警告',
        info: '提示',
        confirm: '确认',
        cancel: '取消',
        delete: '删除',
        edit: '编辑',
        create: '新建',
        search: '搜索',
        filter: '筛选',
        export: '导出',
        import: '导入',
        refresh: '刷新',
        settings: '设置',
        more: '更多',
        yes: '是',
        no: '否',
        ok: '确定',
        close: '关闭',
        back: '返回',
        next: '下一步',
        previous: '上一步',
        submit: '提交',
        reset: '重置',
        clear: '清空',
        copy: '复制',
        copied: '已复制',
        required: '必填',
        optional: '可选',
        all: '全部',
        none: '无',
        select: '请选择',
        selectAll: '全选',
        deselectAll: '取消全选',
        selected: '已选 {count} 项',
        noData: '暂无数据',
        loadingFailed: '加载失败',
        retry: '重试',
        tryAgain: '请重试',
    },
    auth: {
        login: '登录',
        logout: '登出',
        username: '用户名',
        password: '密码',
        rememberMe: '记住我',
        forgotPassword: '忘记密码',
        loginSuccess: '登录成功',
        loginFailed: '登录失败',
        invalidCredentials: '用户名或密码错误',
        sessionExpired: '会话已过期，请重新登录',
        terms: '使用须知',
        agreeTerms: '我已阅读并同意《使用须知》',
        readToEnd: '请将条款滚动至底部后再确认',
    },
    nav: {
        dashboard: '总览',
        tasks: '任务',
        findings: '发现',
        assets: '资产',
        traffic: '流量',
        workspace: '工作空间',
        commands: '命令',
        llmRecords: 'LLM 记录',
        settings: '系统配置',
        agents: 'Agent',
        mcp: 'MCP',
        tools: '工具',
        skills: '技能',
        notify: '通知',
        intercept: '拦截',
        vulnintel: '漏洞情报',
        proxypool: '代理池',
        llm: 'LLM 配置',
        logs: '日志',
        hostenv: '宿主环境',
    },
    task: {
        task: '任务',
        tasks: '任务列表',
        createTask: '新建任务',
        editTask: '编辑任务',
        deleteTask: '删除任务',
        archiveTask: '归档任务',
        restoreTask: '还原任务',
        taskDetail: '任务详情',
        taskName: '任务名称',
        taskDescription: '任务描述',
        taskGoal: '任务目标',
        taskStatus: '任务状态',
        taskCategory: '任务分类',
        taskLLM: 'LLM 配置',
        taskConstraints: '操作约束',
        taskInterceptRules: '资产拦截规则',
        taskScope: '测试范围',
        taskCoverage: '覆盖度',
        taskGraph: '探索图谱',
        taskActivity: '活动流',
        taskTokens: 'Token 用量',
        taskLogs: '日志',
        taskFiles: '文件',
        taskChat: '会话',
        running: '运行中',
        paused: '已暂停',
        done: '已完成',
        failed: '失败',
        timeout: '已超时',
        queued: '排队中',
        created: '已创建',
        pinned: '已置顶',
        active: '当前任务',
        goalsMet: '目标达成',
        inFlight: '在途',
        lastActivity: '最近活动',
        duration: '运行时长',
        tokens: 'Token',
        inputTokens: '输入 Token',
        outputTokens: '输出 Token',
        cacheTokens: '缓存 Token',
    },
    finding: {
        finding: '发现',
        findings: '发现列表',
        createFinding: '新建发现',
        editFinding: '编辑发现',
        deleteFinding: '删除发现',
        findingDetail: '发现详情',
        findingName: '漏洞名称',
        findingSummary: '漏洞摘要',
        findingSeverity: '严重等级',
        findingStatus: '处置状态',
        findingVulnclass: '漏洞类型',
        findingTask: '所属任务',
        findingAsset: '关联资产',
        findingTraffic: '关联流量',
        findingRetest: '复测',
        findingDeepen: '深入利用',
        critical: '严重',
        high: '高危',
        medium: '中危',
        low: '低危',
        pending: '待处理',
        confirmed: '已确认',
        falsePositive: '误报',
        fixed: '已修复',
        ignored: '已忽略',
        severity: '严重等级',
        status: '处置状态',
        vulnclass: '漏洞类型',
        task: '任务',
        asset: '资产',
        count: '数量',
        total: '总计',
        bySeverity: '按严重度',
        byStatus: '按状态',
        byVulnclass: '按类型',
        byTask: '按任务',
        byAsset: '按资产',
        flat: '全部发现',
        grouped: '按任务分组',
        asset: '按资产',
        export: '导出发现',
        exportFiltered: '导出当前筛选',
        exportAll: '导出全部',
        exportSelected: '导出勾选',
        exportFormat: '导出格式',
        mdSingle: 'Markdown 汇总报告',
        mdZip: 'Markdown 分文件',
        csv: 'CSV 表格',
        json: 'JSON',
    },
    settings: {
        settings: '系统配置',
        trafficCapture: '流量捕获',
        trafficCaptureDesc: '开启后，所有 Agent 的 HTTP 流量经记录代理全量落库',
        agentTrafficBinding: 'Agent 自动绑定流量',
        globalProxy: '全局代理',
        egressConfig: '回连配置',
        constraints: '操作约束注入',
        injectPlanner: '注入规划者',
        injectWorker: '注入执行者',
        experimental: '实验功能',
        noaCompaction: 'noa 上下文压缩',
        webSearch: '网络搜索',
        webSearchBackend: '搜索来源',
        webSearchProxy: '出口代理',
        pythonInterpreter: 'Python 解释器',
        workers: '工作并发',
        chatSendMode: '发送键位',
        update: '版本更新',
        checkUpdate: '检查更新',
        applyUpdate: '应用更新',
        rollbackUpdate: '回滚更新',
    },
    severity: {
        critical: '严重',
        high: '高危',
        medium: '中危',
        low: '低危',
    },
    status: {
        pending: '待处理',
        confirmed: '已确认',
        inProgress: '处理中',
        resolved: '已解决',
        fixed: '已修复',
        falsePositive: '误报',
        ignored: '已忽略',
        created: '已创建',
        queued: '排队中',
        running: '运行中',
        paused: '已暂停',
        done: '已完成',
        failed: '失败',
        timeout: '已超时',
    },
    time: {
        justNow: '刚刚',
        secondsAgo: '{count}s 前',
        minutesAgo: '{count}m 前',
        hoursAgo: '{count}h 前',
        daysAgo: '{count}d 前',
    },
    toast: {
        success: '操作成功',
        error: '操作失败',
        warning: '警告',
        info: '提示',
        saved: '已保存',
        deleted: '已删除',
        copied: '已复制',
        exported: '已导出',
    },
};

LOCALES['en-US'] = {
    common: {
        loading: 'Loading...',
        saving: 'Saving...',
        saved: 'Saved',
        error: 'Error',
        success: 'Success',
        warning: 'Warning',
        info: 'Info',
        confirm: 'Confirm',
        cancel: 'Cancel',
        delete: 'Delete',
        edit: 'Edit',
        create: 'Create',
        search: 'Search',
        filter: 'Filter',
        export: 'Export',
        import: 'Import',
        refresh: 'Refresh',
        settings: 'Settings',
        more: 'More',
        yes: 'Yes',
        no: 'No',
        ok: 'OK',
        close: 'Close',
        back: 'Back',
        next: 'Next',
        previous: 'Previous',
        submit: 'Submit',
        reset: 'Reset',
        clear: 'Clear',
        copy: 'Copy',
        copied: 'Copied',
        required: 'Required',
        optional: 'Optional',
        all: 'All',
        none: 'None',
        select: 'Select',
        selectAll: 'Select All',
        deselectAll: 'Deselect All',
        selected: '{count} selected',
        noData: 'No data',
        loadingFailed: 'Failed to load',
        retry: 'Retry',
        tryAgain: 'Try again',
    },
    auth: {
        login: 'Login',
        logout: 'Logout',
        username: 'Username',
        password: 'Password',
        rememberMe: 'Remember me',
        forgotPassword: 'Forgot password',
        loginSuccess: 'Login successful',
        loginFailed: 'Login failed',
        invalidCredentials: 'Invalid username or password',
        sessionExpired: 'Session expired, please login again',
        terms: 'Terms of Use',
        agreeTerms: 'I have read and agree to the Terms of Use',
        readToEnd: 'Please scroll to the end before confirming',
    },
    nav: {
        dashboard: 'Dashboard',
        tasks: 'Tasks',
        findings: 'Findings',
        assets: 'Assets',
        traffic: 'Traffic',
        workspace: 'Workspace',
        commands: 'Commands',
        llmRecords: 'LLM Records',
        settings: 'Settings',
        agents: 'Agents',
        mcp: 'MCP',
        tools: 'Tools',
        skills: 'Skills',
        notify: 'Notifications',
        intercept: 'Intercept',
        vulnintel: 'Vuln Intel',
        proxypool: 'Proxy Pool',
        llm: 'LLM Config',
        logs: 'Logs',
        hostenv: 'Host Env',
    },
    task: {
        task: 'Task',
        tasks: 'Tasks',
        createTask: 'Create Task',
        editTask: 'Edit Task',
        deleteTask: 'Delete Task',
        archiveTask: 'Archive Task',
        restoreTask: 'Restore Task',
        taskDetail: 'Task Detail',
        taskName: 'Task Name',
        taskDescription: 'Description',
        taskGoal: 'Goal',
        taskStatus: 'Status',
        taskCategory: 'Category',
        taskLLM: 'LLM Config',
        taskConstraints: 'Constraints',
        taskInterceptRules: 'Intercept Rules',
        taskScope: 'Scope',
        taskCoverage: 'Coverage',
        taskGraph: 'Graph',
        taskActivity: 'Activity',
        taskTokens: 'Tokens',
        taskLogs: 'Logs',
        taskFiles: 'Files',
        taskChat: 'Chat',
        running: 'Running',
        paused: 'Paused',
        done: 'Done',
        failed: 'Failed',
        timeout: 'Timeout',
        queued: 'Queued',
        created: 'Created',
        pinned: 'Pinned',
        active: 'Active',
        goalsMet: 'Goals Met',
        inFlight: 'In Flight',
        lastActivity: 'Last Activity',
        duration: 'Duration',
        tokens: 'Tokens',
        inputTokens: 'Input Tokens',
        outputTokens: 'Output Tokens',
        cacheTokens: 'Cache Tokens',
    },
    finding: {
        finding: 'Finding',
        findings: 'Findings',
        createFinding: 'Create Finding',
        editFinding: 'Edit Finding',
        deleteFinding: 'Delete Finding',
        findingDetail: 'Finding Detail',
        findingName: 'Name',
        findingSummary: 'Summary',
        findingSeverity: 'Severity',
        findingStatus: 'Status',
        findingVulnclass: 'Type',
        findingTask: 'Task',
        findingAsset: 'Asset',
        findingTraffic: 'Traffic',
        findingRetest: 'Retest',
        findingDeepen: 'Deepen',
        critical: 'Critical',
        high: 'High',
        medium: 'Medium',
        low: 'Low',
        pending: 'Pending',
        confirmed: 'Confirmed',
        falsePositive: 'False Positive',
        fixed: 'Fixed',
        ignored: 'Ignored',
        severity: 'Severity',
        status: 'Status',
        vulnclass: 'Type',
        task: 'Task',
        asset: 'Asset',
        count: 'Count',
        total: 'Total',
        bySeverity: 'By Severity',
        byStatus: 'By Status',
        byVulnclass: 'By Type',
        byTask: 'By Task',
        byAsset: 'By Asset',
        flat: 'All Findings',
        grouped: 'By Task',
        asset: 'By Asset',
        export: 'Export Findings',
        exportFiltered: 'Export Filtered',
        exportAll: 'Export All',
        exportSelected: 'Export Selected',
        exportFormat: 'Export Format',
        mdSingle: 'Markdown Summary',
        mdZip: 'Markdown Split Files',
        csv: 'CSV',
        json: 'JSON',
    },
    settings: {
        settings: 'Settings',
        trafficCapture: 'Traffic Capture',
        trafficCaptureDesc: 'Enable to record all Agent HTTP traffic via proxy',
        agentTrafficBinding: 'Agent Auto Bind Traffic',
        globalProxy: 'Global Proxy',
        egressConfig: 'Egress Config',
        constraints: 'Constraints Injection',
        injectPlanner: 'Inject Planner',
        injectWorker: 'Inject Worker',
        experimental: 'Experimental',
        noaCompaction: 'noa Context Compaction',
        webSearch: 'Web Search',
        webSearchBackend: 'Search Backend',
        webSearchProxy: 'Egress Proxy',
        pythonInterpreter: 'Python Interpreter',
        workers: 'Workers',
        chatSendMode: 'Send Key',
        update: 'Update',
        checkUpdate: 'Check Update',
        applyUpdate: 'Apply Update',
        rollbackUpdate: 'Rollback Update',
    },
    severity: {
        critical: 'Critical',
        high: 'High',
        medium: 'Medium',
        low: 'Low',
    },
    status: {
        pending: 'Pending',
        confirmed: 'Confirmed',
        inProgress: 'In Progress',
        resolved: 'Resolved',
        fixed: 'Fixed',
        falsePositive: 'False Positive',
        ignored: 'Ignored',
        created: 'Created',
        queued: 'Queued',
        running: 'Running',
        paused: 'Paused',
        done: 'Done',
        failed: 'Failed',
        timeout: 'Timeout',
    },
    time: {
        justNow: 'Just now',
        secondsAgo: '{count}s ago',
        minutesAgo: '{count}m ago',
        hoursAgo: '{count}h ago',
        daysAgo: '{count}d ago',
    },
    toast: {
        success: 'Success',
        error: 'Error',
        warning: 'Warning',
        info: 'Info',
        saved: 'Saved',
        deleted: 'Deleted',
        copied: 'Copied',
        exported: 'Exported',
    },
};

export default {
    initI18n,
    getLocale,
    setLocale,
    getSupportedLocales,
    t,
    tc,
    formatDate,
    formatRelativeTime,
    formatNumber,
    onLocaleChange,
};