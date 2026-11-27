import { HomeHeaderToggleIcon } from "./home_header";
import type { DDNSItem } from "./management_ddns";
import { createPortal } from "react-dom";
import { ActionMenu } from "./action_menu";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Eye, EyeOff, LockKeyhole, LogOut, Shield, UserCircle, Users } from "lucide-react";
import { HomeMonitorScreen } from "./home_monitor_screen";
import { Button, ErrorLine, Field, Modal, PageHeader, Panel, useResource } from "./ui";

type LoginMode = "account" | "password" | "off";
type Session = { authenticated: boolean; username: string; initial: boolean; mode: LoginMode; home_monitor?: boolean };
export async function managementRequest<T>(path: string, data: Record<string, string | number | boolean>): Promise<T> {
  const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(data) });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error || "请求失败");
  return result as T;
}

export function ManagementGate({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [checking, setChecking] = useState(true);
  const [showLogin, setShowLogin] = useState(false);
  async function check() {
    setChecking(true);
    try {
      const response = await fetch("/api/management/session", { cache: "no-store" });
      if (!response.ok) throw new Error("无法读取登录状态");
      setSession(await response.json());
      setError("");
    } catch { setError("无法连接管理服务，请重试"); }
    finally { setChecking(false); }
  }
  useEffect(() => {
    const expired = () => { setSession(undefined); void check(); };
    const restored = (event: PageTransitionEvent) => { if (event.persisted) void check(); };
    void check();
    window.addEventListener("foxray:auth-expired", expired);
    window.addEventListener("pageshow", restored);
    return () => { window.removeEventListener("foxray:auth-expired", expired); window.removeEventListener("pageshow", restored); };
  }, []);
  if (session?.authenticated && !session.initial) return children;
  if (session?.home_monitor && !session.initial && !showLogin && sessionStorage.getItem("foxray.home-monitor-page") === "yes") {
    return <HomeMonitorScreen onLogin={() => setShowLogin(true)} />;
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const fields = new FormData(form);
    const username = String(fields.get("username") || session?.username || "");
    const password = String(fields.get("password") || "");
    setBusy(true);
    setError("");
    try {
      if (session?.initial) {
        if (password !== fields.get("confirmation")) throw new Error("两次输入的密码不一致");
        await managementRequest("/api/management/users", { old_name: session.username, username, password });
        setSession({ authenticated: false, initial: false, username, mode: session.mode });
      } else {
        setSession(await managementRequest<Session>("/api/management/login", { username, password }));
      }
      form.reset();
    } catch (failure) { setError(failure instanceof Error ? failure.message : "请求失败"); }
    finally { setBusy(false); }
  }
  return (
    <div className="management-login-screen">
      <Panel className="management-login-card">
        <div className="management-login-brand">
          <HomeHeaderToggleIcon />
          <h1>Fo<span className="brand-name-x">X</span>ray Gateway</h1>
        </div>
        {session?.initial && <h2 className="management-login-setup">设置管理密码</h2>}
        {session ? (
          <form key={String(session.initial)} onSubmit={submit}>
            {(session.mode !== "password" || session.initial) && <Field label="用户名"><input name="username" autoComplete="username" defaultValue={session.username || "admin"} required maxLength={32} /></Field>}
            <Field label={session.initial ? "新密码" : "密码"}><input name="password" type="password" required maxLength={256} autoComplete={session.initial ? "new-password" : "current-password"} /></Field>
            {session.initial && <Field label="确认新密码"><input name="confirmation" type="password" autoComplete="new-password" required maxLength={256} /></Field>}
            <ErrorLine error={error} />
            <Button type="submit" variant="primary" disabled={busy}>{session.initial ? "保存并重新登录" : "登录"}</Button>
          </form>
        ) : <><ErrorLine error={error} /><Button disabled={checking} onClick={() => void check()}>{checking ? "正在连接…" : "重试"}</Button></>}
      </Panel>
    </div>
  );
}

export function ManagementUserSettings() {
  const status = useResource<{ users: string[]; current: string; mode: LoginMode; password_account: string; session_hours: number; home_monitor_enabled: boolean }>("/api/management/users", 15000);
  const [target, setTarget] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<{ mode?: LoginMode; account?: string; hours?: string; homeMonitor?: boolean }>({});
  const saved = status.data;
  const mode = draft.mode ?? saved?.mode ?? "account";
  const account = draft.account ?? (saved?.password_account || saved?.current || saved?.users[0] || "");
  const hours = draft.hours ?? String(saved?.session_hours ?? 24);
  const homeMonitor = draft.homeMonitor ?? saved?.home_monitor_enabled ?? false;
  const validHours = /^\d+$/.test(hours) && Number(hours) >= 1 && Number(hours) <= 168;
  const userChanged = name !== target || password !== "";
  const userValid = !userChanged || (/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,31}$/.test(name) && (!!target || !!password));
  const dirty = userChanged || mode !== saved?.mode || Number(hours) !== (saved?.session_hours ?? 24) || homeMonitor !== (saved?.home_monitor_enabled ?? false) || (mode === "password" && account !== saved?.password_account);
  const canSave = !!saved && dirty && validHours && userValid && (mode !== "password" || !!account);
  function resetUser() { setTarget(""); setName(""); setPassword(""); }
  function reset(defaults = false) {
    resetUser();
    setDraft(defaults ? { mode: "account", hours: "24", homeMonitor: false } : {});
    setError("");
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !canSave) return;
    setBusy(true);
    setError("");
    try {
      const result = await managementRequest<{ relogin?: boolean }>("/api/management/account-settings", {
        mode, password_account: account, session_hours: Number(hours), home_monitor_enabled: homeMonitor,
        edit_user: userChanged, old_name: target, username: name, password,
      });
      reset();
      if (result.relogin) window.dispatchEvent(new Event("foxray:auth-expired"));
      else await status.refresh();
    } catch (failure) { setError(failure instanceof Error ? failure.message : "保存账号与登录失败"); }
    finally { setBusy(false); }
  }
  return <form className="account-login-settings" onSubmit={save}>
    <Panel className="presentation-settings account-login-card">
      <PageHeader title="账号与登录" icon={<Users size={20} />} actions={saved?.mode && saved.mode !== "off" && <Button disabled={busy} onClick={() => void managementRequest("/api/management/logout", {}).then(() => window.dispatchEvent(new Event("foxray:auth-expired"))).catch(() => setError("退出失败"))}>退出登录</Button>} />
      <div className="form-grid">
        <Field label="登录方式"><select value={mode} disabled={busy} onChange={e => setDraft(d => ({ ...d, mode: e.target.value as LoginMode }))}>
          <option value="account">用户名和密码</option>
          <option value="password">仅密码登录</option>
          <option value="off">关闭认证登录</option>
        </select></Field>
        {mode === "password" && <Field label="使用该账号的密码"><select value={account} disabled={busy} onChange={e => setDraft(d => ({ ...d, account: e.target.value }))}>
          {saved?.users.map(user => <option key={user} value={user}>{user}</option>)}
        </select></Field>}
      </div>
      {mode === "off" && <p className="hint">关闭后，能访问管理地址的人无需登录即可操作 FoXray。</p>}
      <div className="form-grid">
        <Field label="用户"><select value={target} disabled={busy} onChange={e => { setTarget(e.target.value); setName(e.target.value); setPassword(""); }}>
          <option value="">新增用户</option>
          {saved?.users.map(user => <option key={user} value={user}>{user}</option>)}
        </select></Field>
        <Field label="用户名"><input value={name} disabled={busy} onChange={e => setName(e.target.value)} maxLength={32} autoComplete="off" /></Field>
        <Field label="新密码" hint={target ? "留空保留该用户的密码" : "填写该用户的密码"}><input type="password" autoComplete="new-password" maxLength={256} disabled={busy} value={password} onChange={e => setPassword(e.target.value)} /></Field>
      </div>
      <div className="form-grid">
        <Field label="登录会话保持时间（小时）" hint="1～168 小时。下次登录时生效。"><input type="number" min={1} max={168} step={1} value={hours} disabled={busy} onChange={e => setDraft(d => ({ ...d, hours: e.target.value }))} /></Field>
        <Field label="首页常驻监控" hint="管理会话到期后，已打开的首页继续显示只读指标。"><label className="check"><input type="checkbox" checked={homeMonitor} disabled={busy} onChange={e => setDraft(d => ({ ...d, homeMonitor: e.target.checked }))} />启用首页常驻监控</label></Field>
      </div>
      {userChanged && !userValid && <p className="hint">请填写有效的用户名。新增用户时还须填写密码。</p>}
      <ErrorLine error={error || status.error} />
    </Panel>
    <div className="save-card-footer" aria-label="账号与登录保存工具栏" role="group">
      <Button disabled={busy || !saved} onClick={() => reset(true)}>恢复默认</Button>
      <Button disabled={busy || !saved || !dirty} onClick={() => reset()}>放弃保存</Button>
      <Button type="submit" variant="primary" highlighted={canSave} disabled={busy || !canSave}>保存并应用</Button>
    </div>
  </form>;
}

export function ManagementUserMenu() {
  const session = useResource<Session>("/api/management/session", 15000);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const username = session.data?.username ?? "";
  const signedOut = () => window.dispatchEvent(new Event("foxray:auth-expired"));
  async function change(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const inputs = new FormData(event.currentTarget);
    const password = String(inputs.get("password") || "");
    if (!username || password !== inputs.get("confirm")) { setError("两次输入的密码不一致"); return; }
    setBusy(true); setError("");
    try { await managementRequest("/api/management/users", {old_name: username, username, password}); signedOut(); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "修改密码失败"); }
    finally { setBusy(false); }
  }
  if (session.data?.mode === "off") return null;
  return <div className="management-user-menu">
    <ActionMenu label="" triggerLabel="用户菜单" triggerIcon={<UserCircle size={20} />} items={[
      {label: "修改密码", icon: <LockKeyhole size={16} />, disabled: !username, onClick: () => {setError(""); setOpen(true);}},
      {label: "退出登录", icon: <LogOut size={16} />, onClick: () => void managementRequest("/api/management/logout", {}).then(signedOut).catch(() => {setError("退出登录失败，请重试"); setOpen(true);})},
    ]} />
    {open && createPortal(<Modal className="management-password-dialog" title="修改密码" onClose={() => {if (!busy) setOpen(false);}}>
      <form onSubmit={change}>
        <Field label="新密码"><input type="password" name="password" maxLength={256} autoComplete="new-password" required /></Field>
        <Field label="确认新密码"><input type="password" name="confirm" maxLength={256} autoComplete="new-password" required /></Field>
        <ErrorLine error={error} />
        <div className="modal-actions"><Button disabled={busy} onClick={() => setOpen(false)}>取消</Button><Button type="submit" variant="primary" disabled={busy}>保存并重新登录</Button></div>
      </form>
    </Modal>, document.body)}
  </div>;
}

type HTTPSStatus = { enabled: boolean; mode: string; domain: string; expires?: string; port: number; redirect_port: number; description?: string; warning?: string; busy?: boolean; email?: string; issuer?: string; subject?: string; certificate_names?: string[]; auto_renew?: boolean; renew_hours?: number; last_renew_check?: string; next_renew_check?: string; cloudflare_token_saved?: boolean };
export function ManagementHTTPSSettings() {
  const ddns = useResource<{ items: DDNSItem[] }>("/api/management/ddns", 15000);
  const status = useResource<HTTPSStatus>("/api/management/https", 15000);
  const [draftMode, setMode] = useState<string>();
  const [editedDomain, setEditedDomain] = useState<string>();
  const [token, setToken] = useState("");
  const [savedToken, setSavedToken] = useState("");
  const [showToken, setShowToken] = useState(false);
  const [tokenLoading, setTokenLoading] = useState(false);
  const tokenRequest = useRef<AbortController | null>(null);
  const current = status.data;
  const mode = draftMode ?? (current?.enabled ? current.mode : "self");
  const acme = mode === "cloudflare" || mode === "acme-http";
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const savedForDomain = mode === "cloudflare" && !!current?.cloudflare_token_saved &&
    (editedDomain ?? current.domain).trim().toLowerCase() === current.domain.toLowerCase();
  const hideToken = () => {
    tokenRequest.current?.abort();
    tokenRequest.current = null;
    setTokenLoading(false);
    setShowToken(false);
    setSavedToken("");
  };
  useEffect(() => {
    const hideOnBlur = () => hideToken();
    window.addEventListener("blur", hideOnBlur);
    return () => {
      window.removeEventListener("blur", hideOnBlur);
      tokenRequest.current?.abort();
    };
  }, []);
  useEffect(() => {
    hideToken();
  }, [savedForDomain, current?.domain, mode]);
  useEffect(() => {
    if (!showToken) return;
    const timer = window.setTimeout(hideToken, 30000);
    return () => window.clearTimeout(timer);
  }, [showToken]);
  async function toggleToken() {
    if (showToken) { hideToken(); return; }
    if (token || !savedForDomain) { setShowToken(true); return; }
    const controller = new AbortController();
    tokenRequest.current = controller;
    setTokenLoading(true);
    setError("");
    try {
      const response = await fetch("/api/management/https/cloudflare-token", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ domain: current?.domain }), cache: "no-store", signal: controller.signal,
      });
      if (!response.ok) throw new Error("请通过 HTTPS 登录后查看已保存的令牌");
      const result = await response.json() as {token: string};
      if (controller.signal.aborted) return;
      setSavedToken(result.token);
      setShowToken(true);
    } catch (failure) {
      if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : "无法查看令牌");
    } finally {
      if (tokenRequest.current === controller) {
        tokenRequest.current = null;
        setTokenLoading(false);
      }
    }
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const fields = new FormData(form);
    setBusy(true); setError("");
    try {
      const body: Record<string, string> = {mode};
      for (const key of ["domain", "email", "description", "port", "redirect_port", "renew_hours"]) body[key] = String(fields.get(key) || "");
      if (mode === "cloudflare") body.cloudflare_token = token;
      const certificate = fields.get("certificate");
      const keyFile = fields.get("key");
      const replacing = (certificate instanceof File && certificate.size > 0) || (keyFile instanceof File && keyFile.size > 0);
      if (current?.enabled && mode === current.mode && (mode === "import" ? !replacing : body.domain.trim() === current.domain)) body.mode = "keep";
      if (mode === "import" && body.mode !== "keep") {
        for (const name of ["certificate", "key"]) {
          const file = fields.get(name);
          if (!(file instanceof File) || !file.size || file.size > 200 * 1024) throw new Error("请选择不超过 200 KB 的 PEM 文件");
          body[name] = await file.text();
        }
      }
      const result = await managementRequest<{enabled: boolean; port: number; http_port?: number}>("/api/management/https", body);
      const next = new URL(location.href);
      if (result.enabled) {
        next.protocol = "https:";
        next.port = String(result.port || 18443);
        if (acme && body.mode !== "keep" && body.domain.trim()) next.hostname = body.domain.trim();
      }
      else if (result.http_port) { next.protocol = "http:"; next.port = String(result.http_port); }
      if (next.origin !== location.origin) { location.assign(next.toString()); return; }
      form.reset(); setMode(undefined); setEditedDomain(undefined); setToken(""); hideToken();
      await status.refresh();
    } catch (failure) { setError(failure instanceof Error ? failure.message : "HTTPS 配置失败"); }
    finally { setBusy(false); }
  }
  return <Panel className="presentation-settings">
    <PageHeader title="HTTPS" icon={<Shield size={20} />} description={status.data?.enabled ? `已启用 · 端口 ${status.data.port}${status.data.expires ? ` · 有效期至 ${new Date(status.data.expires).toLocaleString()}` : ""}` : "启用后关闭 HTTP 管理访问"} />
    {current?.enabled && <dl className="settings-section https-current-certificate">
      <div><dt>当前证书</dt><dd>{current.subject || current.domain || "—"}</dd></div>
      <div><dt>签发机构</dt><dd>{current.issuer || "—"}</dd></div>
      <div><dt>备用域名</dt><dd>{current.certificate_names?.join("、") || "—"}</dd></div>
      <div><dt>有效期至</dt><dd>{current.expires ? new Date(current.expires).toLocaleString() : "—"}</dd></div>
      <div><dt>自动续期</dt><dd>{current.auto_renew ? `每 ${current.renew_hours ?? 12} 小时检查` : "不适用"}</dd></div>
      {current.last_renew_check && <div><dt>上次续期检查</dt><dd>{new Date(current.last_renew_check).toLocaleString()}</dd></div>}
      {current.next_renew_check && <div><dt>下次续期检查</dt><dd>{new Date(current.next_renew_check).toLocaleString()}</dd></div>}
    </dl>}
    <form onSubmit={save}>
      <div className="settings-section form-grid">
        <Field label="证书来源"><select value={mode} onChange={event => { setMode(event.target.value); setEditedDomain(undefined); setToken(""); }} disabled={busy}>
          <option value="self">自签名证书</option><option value="import">导入第三方证书</option>
          <option value="acme-http">Let's Encrypt · HTTP</option><option value="cloudflare">Let's Encrypt · Cloudflare DNS</option><option value="off">关闭 HTTPS</option>
        </select></Field>
        {mode !== "off" && <>
          <Field label="HTTPS 端口"><input name="port" type="number" min={1} max={65535} key={`port-${status.data?.port}`} defaultValue={status.data?.port || 18443} required /></Field>
          <Field label="HTTP 跳转端口" hint="留空关闭 HTTP。填写后该端口仅跳转到 HTTPS。"><input name="redirect_port" type="number" min={1} max={65535} key={`redirect-${status.data?.redirect_port}`} defaultValue={status.data?.redirect_port || ""} /></Field>
        </>}
        {(mode === "self" || acme) && <Field label={mode === "self" ? "域名或 IP 地址" : "证书域名"}><input key={`${mode}-${current?.domain}`} name="domain" onChange={event => setEditedDomain(event.target.value)} required defaultValue={current?.mode === mode && current.domain ? current.domain : mode === "self" ? location.hostname : ""} list={acme ? "management-acme-domains" : undefined} placeholder={acme ? "填写证书域名或选择已配置的 DDNS" : undefined} /></Field>}
        {acme && <datalist id="management-acme-domains">{ddns.data?.items.filter(item => mode !== "cloudflare" || item.provider === "cloudflare").map(item => <option key={item.domain} value={item.domain} />)}</datalist>}
        {acme && <>
          <Field label="描述（可选）"><input name="description" maxLength={120} defaultValue={status.data?.description || ""} /></Field>
          <Field label="电子邮箱" hint={mode === "acme-http" ? "公网 80 端口须转发到网关，签发后自动续期" : "签发后自动检查续期"}><input type="email" name="email" key={`email-${current?.email}`} defaultValue={current?.email} required /></Field>
          {mode === "cloudflare" && <Field label="Cloudflare API 令牌" hint={savedForDomain ? "已保存独立令牌，留空可继续使用" : "可独立填写。留空使用同域名的 Cloudflare DDNS 令牌或服务器凭据。"}>
            <div className="https-token-control">
              {showToken && savedToken && !token ?
                <input aria-label="已保存的 API 令牌" type="text" value={savedToken} readOnly autoComplete="off" /> :
                <input aria-label="Cloudflare API 令牌" type={showToken ? "text" : "password"} name="cloudflare_token"
                  value={token} onChange={event => setToken(event.target.value)} autoComplete="new-password" maxLength={256}
                  placeholder={savedForDomain ? "••••••••••••" : "输入 API 令牌"} />}
              {(savedForDomain || token) && <Button type="button" variant="ghost" disabled={tokenLoading}
                title={showToken ? "隐藏令牌" : "显示令牌"} onClick={() => void toggleToken()}>
                {showToken ? <EyeOff size={16} /> : <Eye size={16} />}
              </Button>}
            </div>
          </Field>}
          <Field label="续期检查间隔（小时）"><input name="renew_hours" type="number" min={1} max={168} key={`renew-${current?.renew_hours}`} defaultValue={current?.renew_hours ?? 12} required /></Field>
        </>}
        {mode === "import" && <><Field label="PEM 证书及证书链"><input type="file" name="certificate" required={current?.mode !== "import"} /></Field><Field label="PEM 私钥"><input type="file" name="key" required={current?.mode !== "import"} /></Field></>}
      </div>
      <ErrorLine error={error || status.error || status.data?.warning} />
      <div className="save-card-footer"><Button variant="primary" type="submit" disabled={busy || !!status.data?.busy}>{busy ? "正在处理证书…" : "保存 HTTPS 设置"}</Button></div>
    </form>
  </Panel>;
}
