// Login Page
import { route } from '../core/router.js';
import { t } from '../core/i18n.js';
import { api } from '../core/api.js';
import { auth } from '../core/auth.js';
import { uiStore } from '../core/store.js';
import { h, cn } from '../core/utils.js';

// Login page state
let state = {
    password: '',
    error: '',
    loading: false,
    checking: true,
    agreed: false,
    termsOpen: false,
    readToEnd: false,
    termsBodyRef: null,
};

const termsContent = `
    <section class="space-y-1.5">
        <h4 class="flex items-center gap-2 font-medium text-foreground">
            <span class="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">1</span>
            第一条 · 定义与开源许可
        </h4>
        <p class="pl-7">
            本软件（ARTEX）是一款基于 GNU Affero General Public License
            v3.0（AGPL-3.0）发布的开源程序。您可依据该协议自由使用、复制、修改和分发本软件；但任何衍生作品（包括通过网络向第三方提供的在线服务）均须同样以
            AGPL-3.0 协议开源并向使用者公开对应的完整源代码。AGPL-3.0 完整条款以随附的 LICENSE 文件为准。
        </p>
    </section>
    <section class="space-y-1.5">
        <h4 class="flex items-center gap-2 font-medium text-foreground">
            <span class="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">2</span>
            第二条 · 授权使用范围
        </h4>
        <p class="pl-7">
            本软件仅供个人学习、代码研究、安全技术原理探讨，以及在您自行搭建的本地隔离环境中进行技术验证之用，适用于学习、学术研究、代码审阅等非攻击性、非破坏性用途。除本条明确许可的情形外，您不得将本软件用于任何其他目的。
        </p>
    </section>
    <section class="space-y-2">
        <h4 class="flex items-center gap-2 font-medium text-destructive">
            <span class="flex size-5 items-center justify-center rounded-md bg-destructive/10 font-semibold text-destructive text-xs">3</span>
            <svg class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
            第三条 · 禁止行为
        </h4>
        <ul class="ml-7 list-decimal space-y-1.5 rounded-lg border border-destructive/20 bg-destructive/5 p-3 pl-8 text-foreground/80 marker:text-destructive/70">
            <li>严禁对任何网站、线上服务、他人或第三方所有的联网系统发起扫描、探测、利用或攻击（无论是否已获得授权、是否为您自有资产）；</li>
            <li>严禁将本软件用于任何实际的渗透测试、攻防对抗、红蓝演练或生产环境；</li>
            <li>严禁将本软件用于非法入侵、数据窃取、勒索、拒绝服务（DoS/DDoS）或任何破坏性、犯罪性活动；</li>
            <li>严禁移除、篡改或规避本软件及其输出中的任何版权、许可或安全提示信息；</li>
            <li>严禁从事任何违反您所在国家或地区法律、法规及监管规定的行为。</li>
        </ul>
    </section>
    <section class="space-y-1.5">
        <h4 class="flex items-center gap-2 font-medium text-foreground">
            <span class="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">4</span>
            第四条 · 知识产权
        </h4>
        <p class="pl-7">
            本软件的著作权及相关知识产权归项目作者及贡献者所有，并在 AGPL-3.0
            协议约定的范围内向您授予相应权利。除该协议明确授予的权利外，本声明未以明示或默示方式授予您任何其他权利。
        </p>
    </section>
    <section class="space-y-1.5">
        <h4 class="flex items-center gap-2 font-medium text-foreground">
            <span class="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">5</span>
            第五条 · 数据与隐私
        </h4>
        <p class="pl-7">
            本软件为可自行部署的开源程序，作者不运营任何集中式服务、亦不会收集或上传您的使用数据。您在使用过程中产生、处理或接触的一切数据，均由您自行掌控并负责其合法性与安全性；因数据处理不当引发的任何后果由您自行承担。
        </p>
    </section>
    <section class="space-y-1.5">
        <h4 class="flex items-center gap-2 font-medium text-foreground">
            <span class="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">6</span>
            第六条 · 合规与法律责任
        </h4>
        <p class="pl-7">
            您应自行遵守所在国家或地区关于网络安全、数据安全与个人信息保护、计算机犯罪等方面的全部法律法规（在中国大陆包括但不限于《网络安全法》《数据安全法》《个人信息保护法》及相关司法解释）。
            <span class="font-medium text-foreground">因您违反上述法律法规或本声明约定而产生的一切法律责任与后果，均由您本人独立承担，与本软件作者及贡献者无关。</span>
        </p>
    </section>
    <section class="space-y-1.5">
        <h4 class="flex items-center gap-2 font-medium text-foreground">
            <span class="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">7</span>
            第七条 · 免责声明与责任限制
        </h4>
        <p class="pl-7">
            本软件按"现状（AS IS）"与"现有（AS AVAILABLE）"状态提供，不附带任何明示或默示的担保，包括但不限于对适销性、特定用途适用性、准确性及不侵权的担保。在适用法律允许的最大范围内，本软件作者及贡献者不对因使用或无法使用本软件（无论使用方式是否得当）而导致的任何直接、间接、偶然、特殊或后果性损失承担责任，包括但不限于数据丢失、系统损坏、业务中断、利润损失或法律纠纷。
        </p>
    </section>
    <section class="space-y-1.5">
        <h4 class="flex items-center gap-2 font-medium text-foreground">
            <span class="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">8</span>
            第八条 · 条款变更与最终解释
        </h4>
        <p class="pl-7">
            作者有权根据法律法规或项目发展需要不时更新本声明，更新后的版本将随项目发布并自公布之日起生效；您继续使用本软件即视为接受修订后的条款。在法律允许的范围内，本声明的最终解释权归项目作者所有。若本声明任一条款被认定为无效，不影响其余条款的效力。
        </p>
    </section>
`;

function render() {
    const app = document.getElementById('app');
    if (!app) return;

    if (state.checking) {
        app.innerHTML = `
            <div role="status" class="flex min-h-dvh items-center justify-center text-muted-foreground">
                正在检查登录状态…
            </div>
        `;
        return;
    }

    app.innerHTML = `
        <div class="flex h-dvh">
            <!-- Left panel -->
            <div class="hidden flex-col items-center justify-center bg-primary p-12 text-center lg:flex lg:w-1/3">
                <div class="relative flex items-center justify-center">
                    <div class="absolute size-80 rounded-full border border-primary-foreground/10"></div>
                    <div class="absolute size-60 rounded-full border border-primary-foreground/15"></div>
                    <div class="absolute size-40 rounded-full border border-primary-foreground/20"></div>
                    <img src="/static/logo.png" alt="ARTEX" width="160" height="160" class="relative brightness-0 invert" />
                </div>
            </div>

            <!-- Right panel -->
            <div class="flex w-full items-center justify-center bg-background p-8 lg:w-2/3">
                <div class="w-full max-w-md space-y-10 py-24 lg:py-32">
                    <div class="space-y-4 text-center">
                        <h2 class="font-medium text-2xl tracking-tight">登录</h2>
                        <p class="mx-auto max-w-xl text-muted-foreground">欢迎回来，请输入密码以继续使用 ARTEX</p>
                    </div>
                    <form id="login-form" class="flex flex-col gap-4">
                        <div class="space-y-1.5">
                            <label for="username" class="label">用户名</label>
                            <input id="username" value="ARTEX" readOnly class="input bg-muted text-muted-foreground" />
                        </div>
                        <div class="space-y-1.5">
                            <label for="password" class="label">密码</label>
                            <input
                                id="password"
                                type="password"
                                value="${state.password}"
                                placeholder="请输入密码"
                                autoFocus
                                autoComplete="current-password"
                                class="input"
                                ${state.loading ? 'disabled' : ''}
                            />
                        </div>
                        <div class="flex items-start gap-2">
                            <input
                                type="checkbox"
                                id="agree-terms"
                                class="checkbox mt-0.5"
                                ${state.agreed ? 'checked' : ''}
                                ${state.loading ? 'disabled' : ''}
                            />
                            <label for="agree-terms" class="font-normal text-muted-foreground text-sm leading-relaxed">
                                我已阅读并同意
                                <button
                                    type="button"
                                    id="open-terms"
                                    class="mx-0.5 font-medium text-primary underline-offset-4 hover:underline"
                                >
                                    《使用须知》
                                </button>
                            </label>
                        </div>
                        ${state.error ? `<p class="text-destructive text-sm">${state.error}</p>` : ''}
                        <button
                            type="submit"
                            class="btn btn-primary w-full"
                            ${state.loading || !state.password || !state.agreed ? 'disabled' : ''}
                        >
                            ${state.loading ? '登录中...' : '登录'}
                        </button>
                    </form>
                </div>
            </div>
        </div>

        <!-- Terms Dialog -->
        <div id="terms-dialog" class="${state.termsOpen ? '' : 'hidden'} fixed inset-0 z-50 flex items-center justify-center">
            <div class="fixed inset-0 bg-black/50 backdrop-blur-sm" id="terms-overlay"></div>
            <div class="relative w-full max-w-2xl bg-card rounded-xl shadow-xl max-h-[90vh] flex flex-col">
                <div class="flex items-center gap-3 border-b px-6 py-4">
                    <div class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <svg class="size-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
                    </div>
                    <div class="space-y-0.5">
                        <h3 class="text-base font-semibold">ARTEX 使用须知与免责声明</h3>
                        <p class="text-muted-foreground text-xs">版本 v1.0 · 生效日期 2026-09-18 · 请在登录前完整阅读以下全部条款</p>
                    </div>
                </div>
                <div
                    id="terms-body"
                    onscroll="window.__ARTEX_handleTermsScroll?.()"
                    class="max-h-[60vh] space-y-5 overflow-y-auto px-6 py-5 text-muted-foreground text-sm leading-relaxed"
                >
                    ${termsContent}
                </div>
                <div class="mx-0 mb-0 flex-col items-stretch gap-2 rounded-b-xl px-6 sm:flex-row sm:items-center sm:justify-between border-t px-6 py-4">
                    <p class="text-muted-foreground text-xs">
                        ${state.readToEnd ? '您已浏览全部条款' : '请将条款滚动至底部后再确认'}
                    </p>
                    <button
                        type="button"
                        id="confirm-terms"
                        class="btn btn-primary"
                        ${!state.readToEnd ? 'disabled' : ''}
                    >
                        我已阅读并同意全部条款
                    </button>
                </div>
            </div>
        </div>
    `;

    // Bind events after render
    bindEvents();
}

function bindEvents() {
    // Login form
    const form = document.getElementById('login-form');
    if (form) {
        form.addEventListener('submit', handleSubmit);
    }

    // Password input
    const passwordInput = document.getElementById('password');
    if (passwordInput) {
        passwordInput.addEventListener('input', (e) => {
            state.password = e.target.value;
            updateSubmitButton();
        });
    }

    // Agree checkbox
    const agreeCheckbox = document.getElementById('agree-terms');
    if (agreeCheckbox) {
        agreeCheckbox.addEventListener('change', (e) => {
            state.agreed = e.target.checked;
            updateSubmitButton();
        });
    }

    // Open terms
    const openTermsBtn = document.getElementById('open-terms');
    if (openTermsBtn) {
        openTermsBtn.addEventListener('click', () => {
            state.termsOpen = true;
            state.readToEnd = false;
            render();
        });
    }

    // Terms overlay close
    const termsOverlay = document.getElementById('terms-overlay');
    if (termsOverlay) {
        termsOverlay.addEventListener('click', () => closeTerms());
    }

    // Terms body scroll
    const termsBody = document.getElementById('terms-body');
    if (termsBody) {
        state.termsBodyRef = termsBody;
        termsBody.addEventListener('scroll', handleTermsScroll);
        // Check if content fits without scrolling
        if (termsBody.scrollHeight <= termsBody.clientHeight + 8) {
            state.readToEnd = true;
            updateConfirmButton();
        }
    }

    // Confirm terms
    const confirmBtn = document.getElementById('confirm-terms');
    if (confirmBtn) {
        confirmBtn.addEventListener('click', () => {
            state.agreed = true;
            state.termsOpen = false;
            const agreeCheckbox = document.getElementById('agree-terms');
            if (agreeCheckbox) agreeCheckbox.checked = true;
            updateSubmitButton();
            render();
        });
    }
}

function handleTermsScroll() {
    const el = state.termsBodyRef;
    if (!el) return;
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - 8) {
        state.readToEnd = true;
        updateConfirmButton();
    }
}

function updateConfirmButton() {
    const confirmBtn = document.getElementById('confirm-terms');
    if (confirmBtn) {
        confirmBtn.disabled = !state.readToEnd;
    }
}

function closeTerms() {
    state.termsOpen = false;
    render();
}

function updateSubmitButton() {
    const submitBtn = document.querySelector('#login-form button[type="submit"]');
    if (submitBtn) {
        submitBtn.disabled = state.loading || !state.password || !state.agreed;
    }
}

async function handleSubmit(e) {
    e.preventDefault();
    if (!state.agreed) {
        state.error = '请先阅读并同意《使用须知》';
        render();
        return;
    }
    if (!state.password) {
        state.error = '请输入密码';
        render();
        return;
    }

    state.loading = true;
    state.error = '';
    updateSubmitButton();

    try {
        const { token } = await api.login('ARTEX', state.password);
        auth.setToken(token);

        // Redirect to tasks page
        window.location.href = '/function/tasks';
    } catch (err) {
        state.loading = false;
        state.error = '用户名或密码错误';
        updateSubmitButton();
    }
}

// Register route
route('/login', async () => {
    // Check if already authenticated
    if (auth.isAuthenticated() && !auth.isTokenExpired()) {
        window.location.href = '/function/tasks';
        return;
    }

    // Check auth status
    try {
        const { initialized } = await api.authStatus();
        if (!initialized) {
            window.location.href = '/setup';
            return;
        }
    } catch {
        // Ignore, will show login
    }

    state = {
        password: '',
        error: '',
        loading: false,
        checking: false,
        agreed: false,
        termsOpen: false,
        readToEnd: false,
        termsBodyRef: null,
    };

    render();
});

// Handle terms scroll globally
window.__ARTEX_handleTermsScroll = handleTermsScroll;

export default { render };