# FoXray Gateway v0.9.72 配置备份与恢复

适用于 `foxray_v0.9.72` 及该版本的在线安装包。

## 首次设置和导出

登录 FoXray 后，进入「设置 → FoXray → 配置备份」。勾选「使用密码加密」会导出 `.foxbackup`，取消勾选会导出 `.zip`。ZIP 里的服务配置和密钥可直接读取。备份总配置量上限为 64 MiB。

HTTP 和 HTTPS 页面都可操作。HTTP 页面会提示传输风险。服务器目录留空时，文件由浏览器下载。填写已存在的服务器绝对目录时，应用会以 `0600` 创建一个新备份文件，并显示路径，可经 SSH 或 SFTP 下载。

## 备份范围

- FoXray 节点、订阅、组件及管理设置、监控配置、Xray 引擎配置、应用管理的网络规则、管理账号记录及 HTTPS 配置。
- WireGuard 配置与密钥文件、NetworkManager 连接、FoXray 本地拓扑与 WARP 隔离配置、WARP 注册及端点设置、防火墙的持久配置。
- 默认路径下的 Komari 配置库和插件状态。`manifest.json` 记录源版本、文件清单与可读取的 systemd 服务状态。

系统镜像、软件二进制、浏览器自定义背景、监控历史与实时会话需分别保留。自定义路径和外部依赖请按实际部署另行备份。备份前后应核对清单覆盖范围。

## 重装后恢复

1. 重装系统并准备与清单对应的 FoXray 安装包及各组件。先保留独立的管理通道，准备备份文件和导出时设置的密码。
2. 在可信的 Linux 恢复机上解包到一个尚不存在的目录。密码备份会提示输入密码，ZIP 使用同一命令并直接解包。

   ```bash
   umask 077
   ./foxray_v0.9.72 --stage-config-backup ./foxray-config.foxbackup \
     --backup-dest ./foxray-restore
   ```

   恢复旧版 RSA 加密备份时，额外指定 `--backup-identity`，并提供原私钥。如果原私钥受密码保护，可用 Bash 传入 `--backup-identity <(openssl pkey -in ./foxray-backup-private.pem)`。

3. 查看 `foxray-restore/manifest.json`，对照新主机的接口名、网段、默认网关、DNS 与管理监听地址。核对 NetworkManager 的连接绑定及 WireGuard 钩子脚本路径。Xray、WARP 和 Komari 的单位文件和安装版本应与目标机器匹配。
4. 停止将使用这些文件的服务。将审核后的 `app/` 文件合并到 FoXray 运行目录，将审核后的 `system/` 文件按子路径复制到系统位置。配置文件默认以 `0600` 暂存，部署时按服务账号恢复属主、权限及 SELinux 标签。恢复 SQLite 时对应守护进程必须已停止，旧数据库及其 `-wal`、`-shm` 应一起移到回退目录，然后放入快照。
5. 运行 `systemctl daemon-reload`。参考清单恢复适用的服务启动和开机状态，分步核对 FoXray 健康接口、SSH 回程、防火墙、Xray 出站、WireGuard 握手、WARP 状态和 Komari。WARP 注册如被供应商拒绝，重新注册后再恢复连接。
6. 将暂存恢复目录作为敏感配置处理。验证完成后，在新网关导出新的备份。
