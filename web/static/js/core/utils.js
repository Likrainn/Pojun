// Utility Functions

/**
 * Class name utility - like clsx/cn
 */
export function cn(...inputs) {
    const classes = [];
    for (const input of inputs) {
        if (!input) continue;
        if (typeof input === 'string') {
            classes.push(input);
        } else if (Array.isArray(input)) {
            classes.push(cn(...input));
        } else if (typeof input === 'object') {
            for (const [key, value] of Object.entries(input)) {
                if (value) classes.push(key);
            }
        }
    }
    return classes.join(' ');
}

/**
 * Format relative time
 */
export function fmtRel(ts) {
    if (!ts) return '—';
    const ms = Date.now() - (typeof ts === 'number' ? ts * 1000 : Date.parse(ts));
    const s = Math.floor(ms / 1000);
    if (s < 60) return `${s}s 前`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m 前`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h 前`;
    return `${Math.floor(h / 24)}d 前`;
}

/**
 * Format absolute date time
 */
export function fmtDateTime(unix) {
    if (!unix || unix <= 0) return '—';
    const d = new Date(unix * 1000);
    const p = (n) => String(n).padStart(2, '0');
    return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/**
 * Format date only
 */
export function fmtDate(unix) {
    if (!unix || unix <= 0) return '—';
    const d = new Date(unix * 1000);
    const p = (n) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/**
 * Format duration in seconds
 */
export function fmtDuration(sec) {
    if (sec <= 0) return '—';
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = sec % 60;
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    if (m > 0) return `${m}m ${s}s`;
    return `${s}s`;
}

/**
 * Format token count
 */
export function fmtTokens(n) {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
    return String(n);
}

/**
 * Format bytes
 */
export function fmtBytes(bytes) {
    if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
    if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${bytes} B`;
}

/**
 * Debounce function
 */
export function debounce(fn, delay) {
    let timeoutId;
    return (...args) => {
        clearTimeout(timeoutId);
        timeoutId = setTimeout(() => fn(...args), delay);
    };
}

/**
 * Throttle function
 */
export function throttle(fn, limit) {
    let inThrottle;
    return (...args) => {
        if (!inThrottle) {
            fn(...args);
            inThrottle = true;
            setTimeout(() => inThrottle = false, limit);
        }
    };
}

/**
 * Deep clone
 */
export function deepClone(obj) {
    return JSON.parse(JSON.stringify(obj));
}

/**
 * Generate unique ID
 */
export function uid(prefix = '') {
    return `${prefix}${Date.now().toString(36)}${Math.random().toString(36).slice(2, 9)}`;
}

/**
 * Get nested object value by path
 */
export function get(obj, path, defaultValue) {
    const keys = path.split('.');
    let result = obj;
    for (const key of keys) {
        if (result == null) return defaultValue;
        result = result[key];
    }
    return result !== undefined ? result : defaultValue;
}

/**
 * Set nested object value by path
 */
export function set(obj, path, value) {
    const keys = path.split('.');
    const last = keys.pop();
    let current = obj;
    for (const key of keys) {
        if (current[key] == null) current[key] = {};
        current = current[key];
    }
    current[last] = value;
}

/**
 * Parse query string to object
 */
export function parseQueryString(str) {
    const params = new URLSearchParams(str);
    const result = {};
    for (const [key, value] of params) {
        if (result[key]) {
            if (!Array.isArray(result[key])) result[key] = [result[key]];
            result[key].push(value);
        } else {
            result[key] = value;
        }
    }
    return result;
}

/**
 * Build query string from object
 */
export function buildQueryString(obj) {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(obj)) {
        if (value == null) continue;
        if (Array.isArray(value)) {
            for (const v of value) params.append(key, v);
        } else {
            params.set(key, value);
        }
    }
    return params.toString();
}

/**
 * Copy text to clipboard
 */
export async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        return true;
    } catch {
        // Fallback for older browsers
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        document.body.removeChild(textarea);
        return true;
    }
}

/**
 * Download blob as file
 */
export function downloadBlob(blob, filename) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/**
 * Sleep utility
 */
export function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Retry with exponential backoff
 */
export async function retry(fn, options = {}) {
    const { retries = 3, delay = 1000, backoff = 2, shouldRetry = () => true } = options;
    let lastError;
    for (let i = 0; i <= retries; i++) {
        try {
            return await fn();
        } catch (error) {
            lastError = error;
            if (i === retries || !shouldRetry(error)) throw error;
            await sleep(delay * Math.pow(backoff, i));
        }
    }
    throw lastError;
}

/**
 * Create element with attributes and children
 */
export function h(tag, attrs = {}, ...children) {
    const el = document.createElement(tag);
    for (const [key, value] of Object.entries(attrs)) {
        if (key.startsWith('on') && typeof value === 'function') {
            el.addEventListener(key.slice(2).toLowerCase(), value);
        } else if (key === 'className') {
            el.className = value;
        } else if (key === 'dataset') {
            Object.assign(el.dataset, value);
        } else if (key === 'style' && typeof value === 'object') {
            Object.assign(el.style, value);
        } else {
            el.setAttribute(key, value);
        }
    }
    for (const child of children) {
        if (child == null) continue;
        if (typeof child === 'string' || typeof child === 'number') {
            el.appendChild(document.createTextNode(child));
        } else if (child instanceof Node) {
            el.appendChild(child);
        }
    }
    return el;
}

/**
 * Get CSRF token from meta tag
 */
export function getCsrfToken() {
    const meta = document.querySelector('meta[name="csrf-token"]');
    return meta?.getAttribute('content') || '';
}

/**
 * Check if element is in viewport
 */
export function isInViewport(el) {
    const rect = el.getBoundingClientRect();
    return (
        rect.top >= 0 &&
        rect.left >= 0 &&
        rect.bottom <= (window.innerHeight || document.documentElement.clientHeight) &&
        rect.right <= (window.innerWidth || document.documentElement.clientWidth)
    );
}

/**
 * Scroll element into view with smooth behavior
 */
export function scrollIntoView(el, options = {}) {
    el.scrollIntoView({ behavior: 'smooth', block: 'nearest', ...options });
}

/**
 * Format number with thousands separator
 */
export function fmtNumber(n) {
    return new Intl.NumberFormat().format(n);
}

/**
 * Truncate string with ellipsis
 */
export function truncate(str, len = 100) {
    if (str.length <= len) return str;
    return str.slice(0, len - 1) + '…';
}

/**
 * Escape HTML
 */
export function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

/**
 * Strip HTML tags
 */
export function stripHtml(str) {
    const div = document.createElement('div');
    div.innerHTML = str;
    return div.textContent || '';
}

/**
 * Generate color from string (consistent)
 */
export function stringToColor(str) {
    let hash = 0;
    for (let i = 0; i < str.length; i++) {
        hash = str.charCodeAt(i) + ((hash << 5) - hash);
    }
    const hue = Math.abs(hash) % 360;
    return `hsl(${hue}, 65%, 50%)`;
}

/**
 * Format status code color
 */
export function statusColor(code) {
    if (code < 300) return 'text-emerald-500';
    if (code < 400) return 'text-blue-400';
    if (code < 500) return 'text-amber-400';
    return 'text-red-400';
}

export function statusBg(code) {
    if (code < 300) return 'bg-emerald-500';
    if (code < 400) return 'bg-blue-400';
    if (code < 500) return 'bg-amber-400';
    return 'bg-red-400';
}