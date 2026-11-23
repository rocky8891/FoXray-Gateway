# FoXray Gateway v0.9.4 部署手册

适用完整脚本：`install_v0.9.4.sh`；对应二进制 `foxray_v0.9.4` 与前端 v0.9.4。打包脚本与手册为 `build_release_v0.9.4.py` / `build_release_v0.9.4_README.md`。

## 部署要求

- Linux amd64；正式验证环境Rocky Linux 10，使用systemd、nftables与Linux网络工具。
- root用于系统安装和网络管理。不是无特权容器应用。
- 当前透明接管安装器要求网卡`ens16`，并非任意接口名均可开箱使用。先核对网关、管理网、默认路由、DNS和现有代理/VPN。
- 安装依赖包括curl、jq、nft、ip、ping、setpriv、unzip、tar；启用WireGuard还需相应内核/工具。WARP新安装要明确接受官方条款。
- 服务器需要访问所选组件的官方源；不要在无法恢复的唯一SSH连接上首次修改默认路由。

安装器提供apt/dnf依赖分支，不表示两个系统系列的所有组件更新操作均已验证。现有组件接管、端点扫描、透明路由等应按实际系统单独验证。无经过本版验证的Docker部署方案。

## 校验与安装

从官方Release下载发行包和`SHA256SUMS.txt`。不要将未知镜像源的校验值当作真实性证明。

```sh
sha256sum --ignore-missing -c SHA256SUMS.txt
unzip FoXray-Gateway-v0.9.4-linux-amd64.zip -d foxray-0.9.4
cd foxray-0.9.4
bash install_v0.9.4.sh --plan
sudo bash install_v0.9.4.sh --non-interactive --with-xray --without-wireguard --without-warp --without-komari
```

`--plan`只显示计划、不写入系统；正式安装会创建目录、安装依赖并初始化/接管所选组件。

目标目录固定为`/opt/foxray`。新安装默认监听`0.0.0.0:18880`，即全部 IPv4 网卡；浏览器使用服务器实际 IP 访问。首次安装默认启用Xray，其他组件默认不启用；已有`components.json`在未显式指定组件选项时沿用。Komari仅连接独立部署实例。

| 参数 | 含义 |
| --- | --- |
| `--plan` | 只查看安装选择，不执行 |
| `--non-interactive` | 不询问组件选择 |
| `--with-xray` / `--without-xray` | 启用/不启用Xray组件管理 |
| `--with-wireguard` / `--without-wireguard` | 启用/不启用WireGuard管理 |
| `--with-warp` / `--without-warp` | 安装/启用或不启用WARP |
| `--accept-warp-tos` | 明确接受WARP条款；新装WARP所需 |
| `--with-komari` / `--without-komari` | 连接/不连接独立Komari服务 |
| `--komari-url HTTP_URL` | 指定Komari地址并执行相应校验 |
| `--listen IPv4:port` | 设置监听地址；非回环监听需自行限制来源 |

例：已验证拓扑上需要全部VPN组件时：

```sh
sudo bash install_v0.9.4.sh --non-interactive --with-xray --with-wireguard --with-warp --accept-warp-tos --without-komari
```

## 访问控制台

浏览器直接访问 `http://<服务器IP>:18880`。若使用 SSH 隧道：

```sh
ssh -N -L 18880:127.0.0.1:18880 admin@gateway.example
```

SSH 隧道建立后可打开`http://127.0.0.1:18880`。没有内置登录系统；仅允许回环/内网来源不是用户身份认证。需要反向代理时先部署TLS、独立认证和访问名单，再配置`FOXRAY_ALLOWED_HOSTS`允许明确的Host名称，见配置与安全手册。

不要为了省去访问控制直接将管理端口公开。若在可信内网监听其他IPv4地址，必须同步核对Host允许名单与主机防火墙。

## 安装后核验

```sh
systemctl is-active foxray.service
curl -fsS http://127.0.0.1:18880/api/health
readlink -f /opt/foxray/foxray
journalctl -u foxray.service -n 60 --no-pager
```

健康接口应返回`ok:true`与`version:0.9.4`。已有部署可能沿用不同监听地址，替换命令中的地址。核对节点、组件服务、现有DNS、主路由、策略路由和受保护接口，再执行检测或切换。

## Rocky Linux 9.7 跨主机部署验收

本节适用于 `install_v0.9.4.sh` 和 Linux amd64 发行包。2026-11-23 在 Rocky Linux 9.7、x86_64 测试机完成预检。只读 FoXray 已验证首页和健康接口。该主机使用 `ens3`，默认网关来自 DHCP，WireGuard 内核模块存在，`wireguard-tools` 与 `cloudflare-warp` 尚未安装。当前 0.9.4 的安装器要求 `ens16`，代理出站、局域网匹配和 DNS 解析也包含原环境硬编码。`--plan` 会正常打印配置，但不会执行这些前置校验。此主机的四组件实装仍待拓扑适配。

### 部署前检查

```sh
id -u
uname -m
cat /etc/os-release
systemctl --version | head -n 1
free -h
df -h /opt /tmp
for tool in curl jq nft ip ping setpriv unzip tar; do
  command -v "$tool" || printf '缺少 %s\n' "$tool"
done
ip -br address
ip -4 route show default
ip -4 rule show
ip link show dev ens16
ss -ltn 'sport = :18880'
systemctl is-active foxray.service foxray-test-094.service firewalld
getenforce
modinfo -F filename wireguard
test -c /dev/net/tun
rpm -q wireguard-tools cloudflare-warp epel-release
```

核对实际出口接口、测试 LAN 的 CIDR、上游 DNS 和管理连接的回程路径。当前源码中的拓扑要在安装校验、Xray 出站、透明路由、DNS、诊断、WARP 端点探测、VPN 规则和管理接口保护中一致。仅修改安装器网卡检查不能通过功能验收。保持 SELinux Enforcing，出现执行拒绝时检查 journald 和 SELinux 审计记录。

检查 Xray Release API、实际 ZIP 下载地址和 WARP 仓库的可达性。测试机曾能访问 GitHub API 和 Cloudflare 仓库，但 GitHub 发行包下载直连曾超时。可从可达的可信主机下载 FoXray 包与校验清单后上传，Xray 与 WARPSCOUT 的软件包也需要独立核对来源和校验。Cloudflare 的消费版 WARP 文档要求 RHEL 9 启用 EPEL，Rocky Linux 9.7 仍需实测安装与连接。参考 [Cloudflare WARP 安装要求](https://developers.cloudflare.com/warp-client/get-started/)。

### 安装顺序

以下步骤用于拓扑已符合本版要求的测试机，或使用完成拓扑适配且通过验证的测试包。对当前 `ens3` 测试机，先在预检阶段停止，保留现有 SSH 接入和只读实例。

1. 核对发行包 SHA256，再解压到独立目录。运行下面的 `--plan` 查看四组件选择和监听地址。

   ```sh
   sha256sum --ignore-missing -c SHA256SUMS.txt
   unzip FoXray-Gateway-v0.9.4-linux-amd64.zip -d foxray-0.9.4
   cd foxray-0.9.4
   bash -n install_v0.9.4.sh
   bash install_v0.9.4.sh --plan --with-xray --with-wireguard --with-warp \
     --without-komari --listen 0.0.0.0:18880
   ```

2. 在实际管理网卡所属的 firewalld 区域确认 `18880/tcp` 的访问控制。控制台没有内置登录，应仅允许已确认的管理来源，使用按来源限制的 rich rule。当前测试机的 `ens3` 属于 `public`，端口尚未放行。核对区域默认动作、已有服务和 rich rules，再做允许来源与其他来源的访问测试。参考 [firewalld rich rule 语法](https://firewalld.org/documentation/man-pages/firewalld.richlanguage.html)。
   ```sh
   firewall-cmd --zone=public --list-all
   firewall-cmd --permanent --zone=public --list-all
   ```

   启动全地址监听前先确认访问范围，再添加临时规则。将示例 CIDR 换成实际管理客户端的地址或授权网段，并按实际接口所属区域修改 `public`。

   ```sh
   admin_cidr='192.0.2.10/32'
   manager_rule="rule family=\"ipv4\" source address=\"$admin_cidr\" port port=\"18880\" protocol=\"tcp\" accept"
   sudo firewall-cmd --zone=public --add-rich-rule="$manager_rule" --timeout=600
   ```

   完成第 4 步后，从管理客户端验证页面与健康接口，从其他来源验证访问限制。通过后用同一条规则分别写入永久配置和运行时配置，无需重载整个防火墙。

   ```sh
   sudo firewall-cmd --permanent --zone=public --add-rich-rule="$manager_rule"
   sudo firewall-cmd --zone=public --remove-rich-rule="$manager_rule"  # 若临时规则仍有效
   sudo firewall-cmd --zone=public --add-rich-rule="$manager_rule"
   ```

3. 核对 WireGuard 软件包可用，按发行版的仓库策略准备 WARP 的 EPEL 依赖。确认 Xray 和 WARPSCOUT 官方资产可下载，或已准备好校验过的组件。有独立控制台时记录安装前主路由、策略路由和防火墙基线，供联动测试比对。
4. 在这台测试机完成适配后，若 `foxray-test-094.service` 仍占用 18880，先停止它。由部署者阅读并接受 WARP 条款后执行四组件安装命令。

   ```sh
   sudo systemctl stop foxray-test-094.service  # 仅当该测试单元存在并占用 18880 时执行
   ss -ltn 'sport = :18880'
   sudo bash install_v0.9.4.sh --non-interactive --with-xray --with-wireguard \
     --with-warp --accept-warp-tos --without-komari --listen 0.0.0.0:18880
   ```

5. 安装阶段先验证管理页面和组件状态。Xray 切换、WARP 连接及 WireGuard 对端握手逐项进行。每一步均检查 SSH 可用、回程正确，确认上一步通过后再启用下一个组件。

   ```sh
   systemctl is-active foxray.service
   ss -ltn 'sport = :18880'
   curl -fsS http://127.0.0.1:18880/api/health | jq '{ok,version}'
   curl -fsS http://127.0.0.1:18880/api/state | jq '{version,read_only,applied:.state.applied}'
   /opt/foxray/xray version
   warp-cli --version
   wg --version
   ip -4 route show default
   ip -4 rule show
   ```

| 组件 | 功能通过的依据 |
| --- | --- |
| FoXray | 允许来源可打开网页，健康检查返回 0.9.4，`read_only` 为 `false`，可保存并读回一项非网络设置 |
| Xray | 测试节点检测通过，受控客户端可经预期出口访问，停用后恢复原有路径 |
| WARP | 软件和隔离服务安装成功，连接后用隔离检测验证实际出口，管理连接保持可用 |
| WireGuard | 用独立测试配置和测试对端验证近期握手、双向流量及停用恢复，测试配置避免覆盖现有管理路由 |

## 升级及卸载

已有安装使用[管理程序更新工具](upgrade_v0.9.4_README.md)，不重装转发组件。后续版本可在版本管理卡片查看更新说明后确认更新。

停止管理服务不等于撤销透明接管或停止VPN。没有“一条删除目录即可安全还原网络”的卸载承诺。先通过独立管理通道恢复接管前路由/DNS/防火墙、确认业务路径，再按需停用所选组件；备份运行数据后才删除程序文件。外部组件和非FoXray规则不能无条件删除。
