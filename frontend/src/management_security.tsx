import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { LockKeyhole, Shield, Users } from "lucide-react";
import { Button, ErrorLine, Field, PageHeader, Panel, useResource } from "./ui";

type Session = { authenticated: boolean; username: string; initial: boolean };
export async function managementRequest<T>(path: string, data: Record<string, string>): Promise<T> {
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
    const expired = () => setSession({ authenticated: false, initial: false, username: "" });
    const restored = (event: PageTransitionEvent) => { if (event.persisted) void check(); };
    void check();
    window.addEventListener("foxray:auth-expired", expired);
    window.addEventListener("pageshow", restored);
    return () => { window.removeEventListener("foxray:auth-expired", expired); window.removeEventListener("pageshow", restored); };
  }, []);
  if (session?.authenticated && !session.initial) return children;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const fields = new FormData(form);
    const username = String(fields.get("username") || "");
    const password = String(fields.get("password") || "");
    setBusy(true);
    setError("");
    try {
      if (session?.initial) {
        if (password !== fields.get("confirmation")) throw new Error("两次输入的密码不一致");
        await managementRequest("/api/management/users", { old_name: session.username, username, password });
        setSession({ authenticated: false, initial: false, username });
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
        <PageHeader title={session?.initial ? "设置管理密码" : "登录 FoXray Gateway"} icon={<LockKeyhole size={20} />} />
        {session ? (
          <form key={String(session.initial)} onSubmit={submit}>
            <Field label="用户名"><input name="username" autoComplete="username" defaultValue={session.username || "admin"} required maxLength={32} /></Field>
            <Field label={session.initial ? "新密码" : "密码"}><input name="password" type="password" required maxLength={256} minLength={session.initial ? 8 : undefined} autoComplete={session.initial ? "new-password" : "current-password"} /></Field>
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
  const status = useResource<{ users: string[]; current: string }>("/api/management/users", 15000);
  const [target, setTarget] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    setBusy(true); setError("");
    try {
      const result = await managementRequest<{ relogin?: boolean }>("/api/management/users", {
        old_name: target,
        username: name,
        password: String(new FormData(form).get("password") || ""),
      });
      if (result.relogin) { window.dispatchEvent(new Event("foxray:auth-expired")); return; }
      form.reset(); setName(""); setTarget(""); await status.refresh();
    } catch (failure) { setError(failure instanceof Error ? failure.message : "保存失败"); }
    finally { setBusy(false); }
  }
  return <Panel className="presentation-settings">
    <PageHeader title="管理用户" icon={<Users size={20} />} />
    <form onSubmit={save}>
      <div className="settings-section form-grid">
        <Field label="用户"><select value={target} onChange={event => {setTarget(event.target.value);setName(event.target.value);}}>
          <option value="">新增用户</option>
          {status.data?.users.map(user => <option key={user} value={user}>{user}</option>)}
        </select></Field>
        <Field label="用户名"><input value={name} onChange={event => setName(event.target.value)} required maxLength={32} autoComplete="off" /></Field>
        <Field label="新密码" hint={target ? "留空保留该用户的密码" : "至少 8 个字符"}><input name="password" type="password" autoComplete="new-password" minLength={8} maxLength={256} required={!target} /></Field>
      </div>
      <ErrorLine error={error || status.error} />
      <div className="save-card-footer">
        <Button type="submit" variant="primary" disabled={busy || !status.data}>保存用户</Button>
        <Button disabled={busy} onClick={() => void managementRequest("/api/management/logout", {}).then(() => window.dispatchEvent(new Event("foxray:auth-expired"))).catch(() => setError("退出失败"))}>退出登录</Button>
      </div>
    </form>
  </Panel>;
}

type HTTPSStatus = { enabled: boolean; mode: string; domain: string; expires?: string; port: number; warning?: string; busy?: boolean };
export function ManagementHTTPSSettings() {
  const status = useResource<HTTPSStatus>("/api/management/https", 15000);
  const [mode, setMode] = useState("self");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const fields = new FormData(form);
    setBusy(true); setError("");
    try {
      const body: Record<string, string> = {mode, domain: String(fields.get("domain") || ""), email: String(fields.get("email") || "")};
      if (mode === "import") {
        for (const name of ["certificate", "key"]) {
          const file = fields.get(name);
          if (!(file instanceof File) || !file.size || file.size > 200 * 1024) throw new Error("请选择不超过 200 KB 的 PEM 文件");
          body[name] = await file.text();
        }
      }
      await managementRequest("/api/management/https", body);
      form.reset();
      await status.refresh();
    } catch (failure) { setError(failure instanceof Error ? failure.message : "HTTPS 配置失败"); }
    finally { setBusy(false); }
  }
  return <Panel className="presentation-settings">
    <PageHeader title="HTTPS" icon={<Shield size={20} />} description={status.data?.enabled ? `已启用 · 端口 ${status.data.port}${status.data.expires ? ` · 有效期至 ${new Date(status.data.expires).toLocaleString()}` : ""}` : "通过 18443 端口提供 HTTPS"} />
    <form onSubmit={save}>
      <div className="settings-section form-grid">
        <Field label="证书来源"><select value={mode} onChange={event => setMode(event.target.value)} disabled={busy}>
          <option value="self">自签名证书</option><option value="import">导入第三方证书</option><option value="cloudflare">Cloudflare DNS 验证</option><option value="off">关闭 HTTPS</option>
        </select></Field>
        {(mode === "self" || mode === "cloudflare") && <Field label={mode === "self" ? "域名或 IP 地址" : "证书域名"}><input name="domain" required defaultValue={mode === "self" ? location.hostname : status.data?.domain} /></Field>}
        {mode === "cloudflare" && <Field label="电子邮箱" hint="在服务器配置 Certbot Cloudflare 插件及凭据文件后申请，签发后自动续期"><input type="email" name="email" required /></Field>}
        {mode === "import" && <><Field label="PEM 证书及证书链"><input type="file" name="certificate" required /></Field><Field label="PEM 私钥"><input type="file" name="key" required /></Field></>}
      </div>
      <ErrorLine error={error || status.error || status.data?.warning} />
      <div className="save-card-footer"><Button variant="primary" type="submit" disabled={busy || !!status.data?.busy}>{busy ? "正在处理证书…" : "保存 HTTPS 设置"}</Button></div>
    </form>
  </Panel>;
}
