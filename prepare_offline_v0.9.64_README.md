# FoXray Gateway v0.9.64 离线安装

适用 `prepare_offline_v0.9.64.sh`、`install_v0.9.64.sh` 和 FoXray Gateway v0.9.64 Linux amd64 运行包。准备机和目标机使用 Rocky Linux 9 或 10 x86_64，建议采用相同的主版本、小版本和软件源基线。准备机需要联网，目标机安装时使用目录内的 RPM 仓库。

从发行页面选择与目标主版本一致的离线整包 `FoXray-Gateway-v0.9.64-offline-rocky9-amd64.tar.gz` 或 `FoXray-Gateway-v0.9.64-offline-rocky10-amd64.tar.gz`，以及对应的 `OFFLINE-ROCKY9-SHA256SUMS.txt` 或 `OFFLINE-ROCKY10-SHA256SUMS.txt`。在联网电脑下载后复制到目标机，校验并解压，再执行下文第 2 步。已有整包时从第 2 步开始。

在联网准备机自行构建时使用以下步骤。

1. 在联网准备机下载正式 Release 的 `FoXray-Gateway-v0.9.64-linux-amd64.zip` 和 `SHA256SUMS.txt`。解压发行包取得准备脚本。

   ```bash
   sudo dnf install -y dnf-plugins-core createrepo_c curl jq unzip
   unzip FoXray-Gateway-v0.9.64-linux-amd64.zip -d foxray-0.9.64
   bash ./foxray-0.9.64/prepare_offline_v0.9.64.sh \
     FoXray-Gateway-v0.9.64-linux-amd64.zip SHA256SUMS.txt foxray-offline-0.9.64
   ```

   目录包含 FoXray 运行包和展开后的安装文件、Xray、WARPSCOUT、Cloudflare WARP、WireGuard 工具及其 RPM 依赖、仓库元数据和 SHA256 清单。准备脚本从官方发行源获取第三方资产并校验摘要，WARP 依赖从 Rocky BaseOS、AppStream、Extras、CRB 及 Fedora EPEL 解析。安装时 DNF 校验 RPM 签名。

2. 将整个 `foxray-offline-0.9.64` 目录复制到目标机。在目标机进入该目录，校验并一键安装：

   ```bash
   cd foxray-offline-0.9.64
   sha256sum --strict --quiet -c OFFLINE-SHA256SUMS
   sudo bash ./foxray-0.9.64/install_v0.9.64.sh \
     --offline-dir "$PWD" --non-interactive --listen 0.0.0.0:18880 \
     --with-xray --with-wireguard --with-warp --accept-warp-tos --without-komari
   ```

   安装器先检查 IPv4 默认路由并打印探测结果。需要明确指定网卡、网段或 DNS 时，事先按[部署手册](install_v0.9.64_README.md)写入 `/etc/foxray/topology.json`。预检输出是 JSON，只用于核对。需要连接已有 Komari 时，把 `--without-komari` 替换为 `--with-komari --komari-url http://<Komari地址>:25774/`。Komari Server 由管理员独立部署。

3. 验证管理服务、命令和转发组件：

   ```bash
   systemctl is-active foxray.service
   curl -fsS http://127.0.0.1:18880/api/health
   /opt/foxray/xray version
   wg --version
   warp-cli --version
   ```

   管理健康接口应返回 `version: 0.9.64`。Xray 节点、WireGuard 对端需按现场导入，WARP 的注册和连接仍需要能够访问 Cloudflare。已有运行实例使用 [upgrade_v0.9.64.py](upgrade_v0.9.64_README.md)更新管理程序。

离线准备工具也会下载 Certbot、dns-cloudflare 插件及其依赖。安装完成后，实际申请和续期仍需访问证书服务和所选 DNS 服务。
