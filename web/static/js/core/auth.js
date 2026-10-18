// Authentication Module
import { getToken, setToken, isAuthenticated } from './api.js';

const AUTH_EVENTS = {
    LOGIN: 'auth:login',
    LOGOUT: 'auth:logout',
    TOKEN_CHANGE: 'auth:token_change',
};

/**
 * Emit auth event
 */
function emit(event, detail) {
    window.dispatchEvent(new CustomEvent(event, { detail }));
}

/**
 * Login with username/password
 */
export async function login(username, password) {
    const { api } = await import('./api.js');
    const { token } = await api.login(username, password);
    setToken(token);
    emit(AUTH_EVENTS.LOGIN, { username });
    return token;
}

/**
 * Logout current user
 */
export function logout() {
    setToken(null);
    emit(AUTH_EVENTS.LOGOUT);
}

/**
 * Initialize auth state on app start
 */
export function initAuth() {
    const token = getToken();
    if (token) {
        // Validate token with backend
        validateToken().catch(() => {
            // Token invalid, clear it
            setToken(null);
        });
    }
    return isAuthenticated();
}

/**
 * Validate current token with backend
 */
async function validateToken() {
    const { api } = await import('./api.js');
    await api.authStatus();
}

/**
 * Get current user info (from token payload if needed)
 */
export function getCurrentUser() {
    const token = getToken();
    if (!token) return null;
    try {
        // JWT payload is middle part
        const payload = token.split('.')[1];
        if (!payload) return null;
        const decoded = JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/')));
        return {
            username: decoded.username || decoded.sub || 'ARTEX',
            exp: decoded.exp,
            iat: decoded.iat,
        };
    } catch {
        return { username: 'ARTEX' };
    }
}

/**
 * Check if token is expired
 */
export function isTokenExpired() {
    const user = getCurrentUser();
    if (!user || !user.exp) return true;
    return Date.now() / 1000 >= user.exp;
}

/**
 * Refresh token (if backend supports it)
 */
export async function refreshToken() {
    // If backend implements refresh endpoint, call it here
    // For now, just re-validate
    await validateToken();
}

/**
 * Require authentication - redirect to login if not authenticated
 */
export function requireAuth(redirectTo = '/login') {
    if (!isAuthenticated() || isTokenExpired()) {
        if (typeof window !== 'undefined') {
            window.location.href = `${redirectTo}?next=${encodeURIComponent(window.location.pathname)}`;
        }
        return false;
    }
    return true;
}

/**
 * Listen for auth changes
 */
export function onAuthChange(callback) {
    const handler = (e) => callback(e.detail);
    window.addEventListener(AUTH_EVENTS.LOGIN, handler);
    window.addEventListener(AUTH_EVENTS.LOGOUT, handler);
    window.addEventListener(AUTH_EVENTS.TOKEN_CHANGE, handler);
    return () => {
        window.removeEventListener(AUTH_EVENTS.LOGIN, handler);
        window.removeEventListener(AUTH_EVENTS.LOGOUT, handler);
        window.removeEventListener(AUTH_EVENTS.TOKEN_CHANGE, handler);
    };
}

// Re-export
export { getToken, setToken, isAuthenticated };