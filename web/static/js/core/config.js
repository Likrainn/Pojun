// Application Configuration
// Injected by Go template at build time

export const APP_CONFIG = {
    version: "{{.Version}}",
    buildTime: "{{.BuildTime}}",
    commitHash: "{{.CommitHash}}",
    apiBase: "",
    sseBase: "",
    wsBase: "",
    features: {
        trafficCapture: {{.TrafficCapture}},
        webSearch: {{.WebSearch}},
        vulnintel: {{.Vulnintel}},
        proxypool: {{.Proxypool}},
        c2: {{.C2Enabled}},
        webShell: {{.WebShellEnabled}},
    },
    limits: {
        maxUploadSize: 50 * 1024 * 1024,
        maxTaskNameLength: 200,
        maxDescriptionLength: 5000,
        maxFindingNameLength: 200,
        maxExportRecords: 10000,
    },
    i18n: {
        defaultLocale: "zh-CN",
        supportedLocales: ["zh-CN", "en-US"],
    },
    theme: {
        defaultMode: "system",
        defaultPreset: "default",
    }
};

export let runtimeConfig = {
    trafficCapture: false,
    webSearchEnabled: false,
    webSearchBackend: "ddgs",
    webSearchProxy: "",
    globalProxy: "",
    egressHint: "",
    egressPortMap: "",
    pythonInterpreter: "",
    workers: 3,
    injectPlanner: true,
    injectWorker: true,
    noaCompaction: false,
    chatSendMode: "enter",
};

export function updateRuntimeConfig(config) {
    Object.assign(runtimeConfig, config);
}

export function getRuntimeConfig() {
    return { ...runtimeConfig };
}