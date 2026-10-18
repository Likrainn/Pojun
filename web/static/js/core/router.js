// Client-side Router
import { cn } from './utils.js';

const ROUTES = new Map();
let currentRoute = null;
let beforeEachGuards = [];
let afterEachHooks = [];

/**
 * Route definition
 */
export function route(path, handler, options = {}) {
    // Convert path pattern to regex
    const pattern = path
        .replace(/\{([^}]+)\}/g, '(?<$1>[^/]+)')
        .replace(/\*/g, '.*');
    const regex = new RegExp(`^${pattern}$`);
    ROUTES.set(path, { path, pattern, regex, handler, options });
    return { path, pattern, regex, handler, options };
}

/**
 * Add beforeEach guard
 */
export function beforeEach(guard) {
    beforeEachGuards.push(guard);
}

/**
 * Add afterEach hook
 */
export function afterEach(hook) {
    afterEachHooks.push(hook);
}

/**
 * Match current URL to route
 */
function matchRoute(pathname) {
    for (const [path, route] of ROUTES) {
        const match = pathname.match(route.regex);
        if (match) {
            const params = match.groups || {};
            return { route, params };
        }
    }
    return null;
}

/**
 * Navigate to path
 */
export function navigate(path, options = {}) {
    const { replace = false, state = {} } = options;
    if (replace) {
        window.history.replaceState(state, '', path);
    } else {
        window.history.pushState(state, '', path);
    }
    handleRouteChange(path);
}

/**
 * Go back in history
 */
export function goBack() {
    window.history.back();
}

/**
 * Handle route change
 */
async function handleRouteChange(pathname) {
    const match = matchRoute(pathname);
    if (!match) {
        // 404 - redirect to dashboard or show 404 page
        console.warn(`No route matched: ${pathname}`);
        navigate('/function/tasks', { replace: true });
        return;
    }

    const { route, params } = match;

    // Run beforeEach guards
    for (const guard of beforeEachGuards) {
        const result = await guard(route, params);
        if (result === false) {
            return; // Guard blocked navigation
        }
        if (typeof result === 'string') {
            navigate(result, { replace: true });
            return;
        }
    }

    // Update current route
    currentRoute = { ...route, params };

    // Render route handler
    try {
        await route.handler(params);
    } catch (error) {
        console.error('Route handler error:', error);
        // Could show error boundary here
    }

    // Run afterEach hooks
    for (const hook of afterEachHooks) {
        await hook(route, params);
    }

    // Update document title
    updateTitle(route);
}

/**
 * Update document title based on route
 */
function updateTitle(route) {
    const titles = {
        '/login': '登录 - ARTEX',
        '/function/tasks': '任务 - ARTEX',
        '/function/tasks/detail': '任务详情 - ARTEX',
        '/function/findings': '发现 - ARTEX',
        '/function/assets': '资产 - ARTEX',
        '/function/traffic': '流量 - ARTEX',
        '/function/workspace': '工作空间 - ARTEX',
        '/function/commands': '命令 - ARTEX',
        '/function/llm-records': 'LLM 记录 - ARTEX',
        '/system/settings': '系统配置 - ARTEX',
        '/system/agents': 'Agent - ARTEX',
        '/system/mcp': 'MCP - ARTEX',
        '/system/tools': '工具 - ARTEX',
        '/system/skills': '技能 - ARTEX',
        '/system/notify': '通知 - ARTEX',
        '/system/intercept': '拦截 - ARTEX',
        '/system/vulnintel': '漏洞情报 - ARTEX',
        '/system/proxypool': '代理池 - ARTEX',
        '/system/llm': 'LLM 配置 - ARTEX',
        '/system/logs': '日志 - ARTEX',
        '/system/hostenv': '宿主环境 - ARTEX',
    };
    const title = titles[route.path] || 'ARTEX';
    document.title = title;
}

/**
 * Initialize router
 */
export function initRouter() {
    // Handle popstate (browser back/forward)
    window.addEventListener('popstate', () => {
        handleRouteChange(window.location.pathname);
    });

    // Handle initial load
    handleRouteChange(window.location.pathname);

    // Intercept link clicks
    document.addEventListener('click', (e) => {
        const link = e.target.closest('a[data-link]');
        if (!link) return;
        e.preventDefault();
        const href = link.getAttribute('href');
        if (href) navigate(href);
    });
}

/**
 * Get current route info
 */
export function getCurrentRoute() {
    return currentRoute;
}

/**
 * Get current route params
 */
export function getRouteParams() {
    return currentRoute?.params || {};
}

/**
 * Build URL with query params
 */
export function buildUrl(path, params = {}) {
    const url = new URL(path, window.location.origin);
    for (const [key, value] of Object.entries(params)) {
        if (value != null && value !== '') {
            url.searchParams.set(key, value);
        }
    }
    return url.pathname + url.search;
}

/**
 * Parse current URL query params
 */
export function getQueryParams() {
    return Object.fromEntries(new URLSearchParams(window.location.search));
}

/**
 * Programmatic navigation helpers
 */
export const router = {
    navigate,
    goBack,
    buildUrl,
    getQueryParams,
    getCurrentRoute,
    getRouteParams,
    onBeforeEach: beforeEach,
    onAfterEach: afterEach,
};

// Default auth guard
beforeEach(async (route) => {
    const { requireAuth } = await import('./auth.js');
    const publicRoutes = ['/login', '/setup'];
    if (!publicRoutes.includes(route.path)) {
        return requireAuth();
    }
    return true;
});

export default router;