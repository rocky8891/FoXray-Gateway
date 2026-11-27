# FoXray Gateway v0.9.75 管理登录与 HTTPS

适用于 `foxray_v0.9.75`。安装或升级后，使用初始账号登录。初始登录要求设置新密码。之后可从「设置 → FoXray → 管理用户」新增账号、修改用户名或密码，以及退出登录。当前版本的管理账号具有相同权限。管理用户卡片支持用户名和密码、仅密码、关闭认证三种登录方式。仅密码方式使用所选账号的密码。关闭认证后，能访问管理地址的人可以直接操作应用。

「设置 → FoXray → HTTPS」可指定 HTTPS 端口，默认 `18443`。启用后关闭 HTTP 管理访问。可选的 HTTP 跳转端口只接收页面跳转。升级会保留已有证书和 HTTPS 端口，原 HTTP 管理地址不再提供界面。已有证书可选择「保留现有证书」单独修改端口。

- **自签名**填写实际使用的域名或 IP。浏览器需要信任该证书。
- **第三方证书**导入完整 PEM 链和对应的 PEM 私钥。建议在 HTTPS 页面导入，或通过 SSH 隧道打开本机管理入口导入。
- **Let's Encrypt HTTP 验证**安装器会自动安装 Certbot。填入公共域名或 DDNS 域名和电子邮箱，确保公网 TCP 80 可访问网关的验证路径。系统会在验证时监听 TCP 80，或者复用 FoXray 自己在 TCP 80 的跳转入口。签发成功后会定期检查续期并载入新证书。
- **Cloudflare DNS 验证**在「设置 → FoXray → DDNS」新增 Cloudflare 记录，填入自己的域名和该区域的 API 令牌。令牌需要区域读取和 DNS 编辑权限。随后在 HTTPS 卡片选择「Let's Encrypt · Cloudflare DNS」，手填或选择已保存的 DDNS 域名，填写邮箱和 HTTPS 端口后保存。FoXray 使用该域名的令牌签发证书，默认每 12 小时检查续期，可在 HTTPS 卡片调整。也可继续使用服务器现有的 `/opt/foxray/private/acme/cloudflare.ini`。

DDNS 支持 Cloudflare 和 DuckDNS。Cloudflare 可创建同名 A 记录或更新唯一一条已有 A 记录。DuckDNS 使用已在服务商注册的子域名。两者可手填公网 IPv4 或留空自动检测，启用后按设置的间隔更新，默认 5 分钟。

未启用 HTTPS 的网关可用 HTTP 完成首次登录。登录后的备份支持浏览器下载或写入网关目录，详见同版本配置备份与恢复手册。

也可以直接通过 HTTPS 打开管理页面，在 HTTPS 卡片选择「Let's Encrypt · Cloudflare DNS」，填写证书域名、邮箱和 Cloudflare API 令牌后保存。独立令牌按域名保存，优先用于签发和续期。对于已有证书，保存新令牌会更新后续续期使用的凭据。

已保存的独立 Cloudflare API 令牌默认遮盖。通过 HTTPS 登录后，点击输入框旁的眼睛可临时查看，窗口失焦或 30 秒后自动遮盖。
