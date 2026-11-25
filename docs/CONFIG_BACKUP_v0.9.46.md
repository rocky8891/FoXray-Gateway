# FoXray Gateway v0.9.46 配置备份与恢复

适用于 `foxray_v0.9.46` 及该版本的在线安装包。

## 首次设置和导出

在可信的管理电脑上生成专用 RSA 密钥对。将私钥保存在独立于网关的安全位置，恢复时需要它。

```bash
umask 077
openssl genpkey -algorithm RSA -aes-256-cbc -pkeyopt rsa_keygen_bits:3072 -out foxray-backup-private.pem
openssl pkey -in foxray-backup-private.pem -pubout -out foxray-backup-public.pem
```

把 `foxray-backup-public.pem` 传到网关，在网关登记公钥。

```bash
sudo /opt/foxray/foxray --backup-public-key ./foxray-backup-public.pem
```

打开「设置 → 全局 → 配置备份」，刷新状态并点击「导出加密备份」。将 `.foxbackup` 保存到网关以外的位置。每次导出都使用已登记的公钥和独立的随机加密密钥。备份总配置量上限为 64 MiB。

## 备份范围

- FoXray 节点、订阅、组件及管理设置、监控配置、Xray 引擎配置、应用管理的网络规则。
- WireGuard 配置与密钥文件、NetworkManager 连接、FoXray 本地拓扑与 WARP 隔离配置、WARP 注册及端点设置、防火墙的持久配置。
- 默认路径下的 Komari 配置库和插件状态。`manifest.json` 记录源版本、文件清单与可读取的 systemd 服务状态。

系统镜像、软件二进制、浏览器自定义背景、监控历史与实时会话需分别保留。自定义路径和外部依赖请按实际部署另行备份。备份前后应核对清单覆盖范围。

## 重装后恢复

1. 重装系统并准备与清单对应的 FoXray 安装包及各组件。先保留独立的管理通道，将备份及恢复时所需的私钥放在可访问的位置。
2. 在可信的 Linux 恢复机上解包到一个尚不存在的目录。下面的 Bash 命令使用 OpenSSL 解开私钥并通过文件描述符交给恢复工具。应用仅将备份内容写入指定暂存目录。

   ```bash
   umask 077
   ./foxray_v0.9.46 --stage-config-backup ./foxray-config.foxbackup \
     --backup-identity <(openssl pkey -in ./foxray-backup-private.pem) \
     --backup-dest ./foxray-restore
   ```

3. 查看 `foxray-restore/manifest.json`，对照新主机的接口名、网段、默认网关、DNS 与管理监听地址。核对 NetworkManager 的连接绑定及 WireGuard 钩子脚本路径。Xray、WARP 和 Komari 的单位文件和安装版本应与目标机器匹配。
4. 停止将使用这些文件的服务。将审核后的 `app/` 文件合并到 FoXray 运行目录，将审核后的 `system/` 文件按子路径复制到系统位置。配置文件默认以 `0600` 暂存，部署时按服务账号恢复属主、权限及 SELinux 标签。恢复 SQLite 时对应守护进程必须已停止，旧数据库及其 `-wal`、`-shm` 应一起移到回退目录，然后放入快照。
5. 运行 `systemctl daemon-reload`。参考清单恢复适用的服务启动和开机状态，分步核对 FoXray 健康接口、SSH 回程、防火墙、Xray 出站、WireGuard 握手、WARP 状态和 Komari。WARP 注册如被供应商拒绝，重新注册后再恢复连接。
6. 将暂存恢复目录作为敏感配置处理。验证完成后，在新网关重新登记备份公钥并导出新的备份。
