// State Management - Lightweight Zustand-like Store
import { cn } from './utils.js';

const stores = new Map();
const subscribers = new Map();

/**
 * Create a store
 */
export function createStore(name, initialState, middleware = []) {
    let state = { ...initialState };
    const listeners = new Set();

    const store = {
        getState: () => state,
        setState: (partial, replace = false) => {
            const nextState = typeof partial === 'function' ? partial(state) : partial;
            state = replace ? nextState : { ...state, ...nextState };
            // Notify listeners
            for (const listener of listeners) {
                listener(state);
            }
            // Persist if configured
            if (store.options?.persist) {
                try {
                    localStorage.setItem(`store:${name}`, JSON.stringify(state));
                } catch (e) {
                    console.warn('Failed to persist store:', e);
                }
            }
        },
        subscribe: (listener) => {
            listeners.add(listener);
            return () => listeners.delete(listener);
        },
        destroy: () => {
            listeners.clear();
            stores.delete(name);
        },
        options: { persist: false },
    };

    // Apply middleware
    for (const mw of middleware) {
        const originalSetState = store.setState;
        store.setState = (partial, replace) => mw(store)(originalSetState)(partial, replace);
    }

    // Load persisted state
    if (store.options?.persist) {
        try {
            const saved = localStorage.getItem(`store:${name}`);
            if (saved) {
                state = { ...state, ...JSON.parse(saved) };
            }
        } catch (e) {
            console.warn('Failed to load persisted store:', e);
        }
    }

    stores.set(name, store);
    return store;
}

/**
 * Get existing store
 */
export function getStore(name) {
    return stores.get(name);
}

/**
 * Persist middleware
 */
export function persist(options = {}) {
    return (store) => (setState) => (partial, replace) => {
        setState(partial, replace);
        if (options.name) {
            try {
                localStorage.setItem(`store:${options.name}`, JSON.stringify(store.getState()));
            } catch (e) {
                console.warn('Persist failed:', e);
            }
        }
    };
}

/**
 * Devtools middleware (logs state changes)
 */
export function devtools(name) {
    return (store) => (setState) => (partial, replace) => {
        const prevState = store.getState();
        setState(partial, replace);
        console.group(`📦 ${name}`);
        console.log('Prev:', prevState);
        console.log('Next:', store.getState());
        console.groupEnd();
    };
}

/**
 * Combine multiple stores
 */
export function combineStores(...storeNames) {
    return {
        getState: () => {
            const combined = {};
            for (const name of storeNames) {
                const store = stores.get(name);
                if (store) combined[name] = store.getState();
            }
            return combined;
        },
        subscribe: (listener) => {
            const unsubscribes = storeNames.map(name => {
                const store = stores.get(name);
                return store?.subscribe(listener);
            }).filter(Boolean);
            return () => unsubscribes.forEach(unsub => unsub());
        },
    };
}

// ============================================
// Pre-defined Stores
// ============================================

// UI State Store
export const uiStore = createStore('ui', {
    sidebarOpen: true,
    sidebarCollapsed: false,
    themeMode: 'system',
    themePreset: 'default',
    contentLayout: 'centered',
    navbarStyle: 'default',
    sidebarVariant: 'inset',
    sidebarCollapsible: true,
    font: 'Inter',
    toasts: [],
    modals: [],
    loading: {},
}, { persist: true });

// Task Store
export const taskStore = createStore('task', {
    tasks: [],
    activeTaskId: null,
    categories: [],
    templates: [],
    filters: {
        query: '',
        status: 'all',
        category: 'all',
    },
    sort: { field: 'id', direction: 'desc' },
    pagination: { page: 1, pageSize: 20 },
    selectedIds: new Set(),
    batchControlling: null,
}, { persist: { name: 'task' } });

// Finding Store
export const findingStore = createStore('finding', {
    view: 'flat',
    findings: [],
    groups: [],
    assetTree: null,
    assetScope: null,
    stats: null,
    filters: {
        severity: 'all',
        status: 'all',
        vulnclass: 'all',
        task: 'all',
        sort: 'severity',
        query: '',
    },
    pagination: {
        flat: { page: 1, pageSize: 20 },
        grouped: { page: 1, pageSize: 10 },
    },
    expanded: null,
    expandedGroups: new Set(),
    selectedIds: new Set(),
}, { persist: { name: 'finding' } });

// Settings Store
export const settingsStore = createStore('settings', {
    trafficCapture: false,
    agentTrafficBinding: false,
    webSearchEnabled: false,
    webSearchBackend: 'ddgs',
    webSearchProxy: '',
    globalProxy: '',
    egressHint: '',
    egressPortMap: '',
    pythonInterpreter: '',
    workers: 3,
    injectPlanner: true,
    injectWorker: true,
    noaCompaction: false,
    chatSendMode: 'enter',
    braveKeySet: false,
    tavilyKeySet: false,
}, { persist: true });

// Auth Store
export const authStore = createStore('auth', {
    token: null,
    user: null,
    initialized: false,
});

// LLM Store
export const llmStore = createStore('llm', {
    configured: false,
    provider: '',
    model: '',
    baseUrl: '',
    keySet: false,
    profiles: [],
    activeProfileId: null,
    pool: null,
    retryPolicy: null,
});

// Notification Store
export const notifyStore = createStore('notify', {
    channels: [],
    deliveries: [],
    meta: null,
});

// Proxy Pool Store
export const proxyPoolStore = createStore('proxyPool', {
    entries: [],
    strategy: 'round_robin',
    running: [],
    capturing: false,
});

// Vulnintel Store
export const vulnintelStore = createStore('vulnintel', {
    enabled: false,
    nucleiTemplatesDir: '',
    sources: [],
    counts: { cves: 0, kev: 0, pocs: 0 },
});

// Tools Inventory Store
export const toolsInventoryStore = createStore('toolsInventory', {
    tools: [],
    summary: { available: [], missing: [], hint: '' },
    deployMode: 'docker',
});

// Intercept Store
export const interceptStore = createStore('intercept', {
    rules: [],
    assetInterceptRules: [],
    pending: [],
    history: [],
    toolConfig: { enabled_tools: [] },
    judgeConfig: null,
});

// Conversation Store
export const conversationStore = createStore('conversation', {
    conversations: [],
    currentConversationId: null,
    messages: [],
    running: false,
});

// Agent Store
export const agentStore = createStore('agent', {
    agents: [],
    currentAgentKey: null,
    triggers: [],
    visibility: { mcp: [], skill: [] },
});

// Asset Store
export const assetStore = createStore('asset', {
    assets: [],
    total: 0,
    counts: {},
    taskAssets: [],
    taskAssetCounts: {},
});

// Traffic Store
export const trafficStore = createStore('traffic', {
    exchanges: [],
    hosts: [],
    total: 0,
    filters: { host: '', method: '', q: '', status: 'all' },
    pagination: { page: 0, size: 100 },
});

// ============================================
// React-like Hook Helpers (for vanilla JS)
// ============================================

/**
 * Use store in component (returns state + setState)
 */
export function useStore(store, selector) {
    const state = store.getState();
    const selected = selector ? selector(state) : state;
    return [selected, store.setState.bind(store)];
}

/**
 * Subscribe to store changes in component
 */
export function useStoreSubscribe(store, callback, selector) {
    return store.subscribe((state) => {
        const selected = selector ? selector(state) : state;
        callback(selected);
    });
}

/**
 * Create derived state
 */
export function derive(store, selector) {
    let current = selector(store.getState());
    return {
        get value() {
            const next = selector(store.getState());
            if (next !== current) current = next;
            return current;
        },
        subscribe: (callback) => store.subscribe((state) => callback(selector(state))),
    };
}

// Export all stores
export const stores = {
    ui: uiStore,
    task: taskStore,
    finding: findingStore,
    settings: settingsStore,
    auth: authStore,
    llm: llmStore,
    notify: notifyStore,
    proxyPool: proxyPoolStore,
    vulnintel: vulnintelStore,
    toolsInventory: toolsInventoryStore,
    intercept: interceptStore,
    conversation: conversationStore,
    agent: agentStore,
    asset: assetStore,
    traffic: trafficStore,
};