// Application Entry Point
import { initAuth, requireAuth } from './core/auth.js';
import { initRouter } from './core/router.js';
import { initI18n, onLocaleChange } from './core/i18n.js';
import { uiStore } from './core/store.js';
import { api } from './core/api.js';

// Import page modules (they register their routes)
import './pages/login.js';
import './pages/dashboard.js';
import './pages/tasks.js';
import './pages/findings.js';
import './pages/settings.js';

// Global error handler
window.addEventListener('error', (e) => {
    console.error('Global error:', e.error);
});

window.addEventListener('unhandledrejection', (e) => {
    console.error('Unhandled rejection:', e.reason);
});

/**
 * Initialize application
 */
async function initApp() {
    console.log(`🚀 ARTEX v${APP_CONFIG.version} starting...`);

    // 1. Initialize i18n
    const savedLocale = localStorage.getItem('locale') || APP_CONFIG.i18n.defaultLocale;
    await initI18n(savedLocale);
    console.log(`🌐 Locale: ${savedLocale}`);

    // 2. Initialize auth
    const authed = initAuth();
    console.log(`🔐 Auth: ${authed ? 'OK' : 'Not authenticated'}`);

    // 3. Initialize router
    initRouter();
    console.log('🛣️ Router initialized');

    // 4. Load initial settings if authenticated
    if (authed) {
        try {
            const settings = await api.settings();
            // Update runtime config and stores
            const { settingsStore, uiStore, taskStore, findingStore } = await import('./core/store.js');
            settingsStore.setState(settings);
            uiStore.setState((s) => ({
                ...s,
                themeMode: settings.theme_mode || 'system',
                themePreset: settings.theme_preset || 'default',
                contentLayout: settings.content_layout || 'centered',
                navbarStyle: settings.navbar_style || 'default',
                sidebarVariant: settings.sidebar_variant || 'inset',
                sidebarCollapsible: settings.sidebar_collapsible !== false,
                font: settings.font || 'Inter',
            }));
            console.log('⚙️ Settings loaded');
        } catch (e) {
            console.warn('Failed to load settings:', e);
        }
    }

    // 5. Listen for locale changes to update UI
    onLocaleChange((locale) => {
        console.log(`🌐 Locale changed to: ${locale}`);
        document.documentElement.lang = locale;
    });

    // 6. Set up theme listener
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    mediaQuery.addEventListener('change', () => {
        const { uiStore } = require('./core/store.js');
        const mode = uiStore.getState().themeMode;
        if (mode === 'system') {
            document.documentElement.setAttribute('data-theme-mode',
                mediaQuery.matches ? 'dark' : 'light');
        }
    });

    // 7. Apply initial theme
    applyTheme();

    console.log('✅ ARTEX initialized');
}

/**
 * Apply theme based on store
 */
function applyTheme() {
    const { uiStore } = require('./core/store.js');
    const mode = uiStore.getState().themeMode;
    let theme = mode;
    if (mode === 'system') {
        theme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    document.documentElement.setAttribute('data-theme-mode', theme);
}

// Make applyTheme globally available for theme switching
window.__ARTEX_applyTheme = applyTheme;

// Initialize when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initApp);
} else {
    initApp();
}

// Export for debugging
window.__ARTEX__ = {
    api: () => import('./core/api.js'),
    stores: () => import('./core/store.js'),
    auth: () => import('./core/auth.js'),
    router: () => import('./core/router.js'),
    i18n: () => import('./core/i18n.js'),
};