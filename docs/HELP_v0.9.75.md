# FoXray 帮助中心与使用指南 v0.9.75

适用版本：FoXray v0.9.75。入口：设置 → FoXray 后方的帮助中心。支持标题、正文、关键词模糊搜索、分类、命中高亮、常用文档与本浏览器收藏。命令仅供复制，帮助中心不会替用户执行命令。

## 部署前检查与在线安装

适用 FoXray v0.9.75，Linux x86_64。先确认发行包与操作系统、CPU 架构和依赖兼容；不同大版本的 RPM 不能混用。
### 部署前检查
```sh
cat /etc/os-release
uname -m
ip -br address
ip route
ss -ltn 'sport = :18880'
getenforce
```
记录管理入口、默认网关、LAN 接口和 SSH 回程；不要照抄其他服务器的 ens16 或私网网段。
### 安装
下载对应发行包与 SHA256SUMS.txt，核对校验后解压。在解压目录执行：
```sh
sha256sum -c SHA256SUMS.txt
sudo bash install_v0.9.75.sh --plan
sudo bash install_v0.9.75.sh --non-interactive --with-xray --with-wireguard --with-warp --accept-warp-tos --without-komari --listen 0.0.0.0:18880
```
按需要选择组件，安装前阅读随包同版本手册。Komari 为独立部署实例。正式开放管理入口前配置账号认证和 HTTPS。
### 验收
```sh
systemctl status foxray.service --no-pager
curl -fsS http://127.0.0.1:18880/api/health
ss -ltnp 'sport = :18880'
```
0.0.0.0 表示监听全部 IPv4 地址，浏览器需要使用实际服务器地址。服务监听与防火墙放行是两件事。

## 离线部署与依赖包准备

在线发行包包含 FoXray 程序、前端、安装与升级工具、同版本手册和校验信息，不等于包含所有系统 RPM 和外部组件。
### 准备流程
在与目标系统大版本、架构相同的联网环境中运行同版本依赖准备工具，先查看可用参数：
```sh
bash prepare_offline_v0.9.75.sh --help
```
根据同版本 prepare_offline 手册准备所选组件及传递依赖；目标使用 Rocky 9 时不能直接使用 Rocky 10 的 RPM。内核模块必须匹配目标内核，WireGuard 可用性还需检查内核支持。
将依赖、程序、脚本、手册和 SHA256 文件一起传入目标机；先校验，再按离线手册安装。禁止安装过程中因依赖缺失悄悄转为联网下载。

## SELinux 拒绝访问的检查与处理

SELinux 是独立于传统文件权限和防火墙的强制访问控制。开启 Enforcing 并不代表 FoXray 一定被阻止；必须先找到与服务操作对应的 AVC 拒绝。
### 只读检查
```sh
getenforce
systemctl status foxray.service --no-pager
journalctl -u foxray.service -b -n 100 --no-pager
sudo ausearch -m AVC,USER_AVC -ts recent -i
ls -Zd /opt/foxray /opt/foxray/foxray /opt/foxray/web
ps -eZ | grep foxray
```
若 ausearch 不存在，先按系统版本准备 audit 工具。检查实际链接目标的标签和权限，不要直接把服务启动失败归因于 SELinux。
### 标签异常
先预览默认标签的修复范围，确认适合当前部署再执行：
```sh
sudo restorecon -nRv /opt/foxray
sudo restorecon -Rv /opt/foxray
```
若自定义目录需要永久标签，使用 semanage fcontext 配合 restorecon，并按实际服务域和文件用途确定类型。不要把所有目录都改成同一种类型。
### 端口与策略
```sh
sudo semanage port -l | grep -E '18880|18443|http_port_t'
```
只有 AVC 证据表明某个受限服务域缺少绑定指定端口权限时，才考虑对应端口标签；给端口加 http_port_t 不是所有 Go 服务的通用修复。不要默认关闭 SELinux，也不要未经审查直接应用 audit2allow 输出。

## IPv4 forwarding 对 VPN 与网关的影响

访问服务器自身和经服务器转发不是一回事。WireGuard 握手成功只说明隧道密钥交换发生过；LAN 经 VPN 访问远端还需要内核允许转发。
### 检查
```sh
sysctl net.ipv4.ip_forward
```
值为 0 时，本机可以正常通信，但跨接口 IPv4 转发会被阻止。FoXray 的 WireGuard 转发功能会检查此设置；应用不会把此开关当成防火墙放行的替代。
### 持久开启
先确认此服务器用于路由转发，再设置：
```sh
printf 'net.ipv4.ip_forward = 1
' | sudo tee /etc/sysctl.d/90-foxray-forwarding.conf
sudo sysctl -p /etc/sysctl.d/90-foxray-forwarding.conf
sysctl net.ipv4.ip_forward
```
该操作修改全局 IPv4 转发能力；Linux 在切换此值时可能重置其他 IPv4 配置为主机/路由器默认值，应记录并复核 rp_filter、redirect 等既有定制。还需正确的路由、AllowedIPs、回程路由与 FORWARD 策略。
IPv6 转发由独立参数控制；不要因为开启 IPv4 就假设 IPv6 也已放行。

## WireGuard 接入角色、转发方向与 SNAT

服务端/客户端是 FoXray 的接入与配置模板预设，不是业务访问方向。WireGuard Peer 可以双向通信。作为服务端接收接入，也可以由本端 LAN 主动访问远端。
### 本地 LAN → VPN
来源填写本地实际 LAN 网段，目标填写远端网段；目标留空时使用启用 Peer 的 IPv4 AllowedIPs。该方向 SNAT 使用 WireGuard 出接口地址。
### VPN → 本地 LAN
来源填写进入隧道的实际源网段，例如对端隧道 /32 或对端后方的源 LAN。目标填写本地 LAN 网段；留空时使用导出目标网段。该方向 SNAT 使用 LAN 出接口地址。
### 转发与 SNAT 分开
每个方向都有独立的允许转发与 SNAT 开关。允许转发用于生成相应放行；SNAT 只改变匹配流量的源地址。只开 SNAT 不会自动保证防火墙放行。
不开 SNAT 时，需要目的侧有返回原始来源的路由，并在 WireGuard 对端 AllowedIPs 中接纳对应来源。不要把目标 LAN 填入来源字段。
### 导出与旧配置
本端接入地址用于生成对端 Endpoint；导出目标写入对端 AllowedIPs，不会动态推送。旧配置保留原方向，升级不自动写配置；首次编辑新方向时请核对变更预览。
### 接口保护
行操作和批量操作可独立设置接口保护；保护状态只写管理元数据，不启停隧道。接口启停、配置、路由与开机自启各自独立。

## WireGuard 握手、探测与路由排查

接口 UP 不等于 Peer 可达；最近握手不等于远端业务一定可达。启用探测后看配置目标的探测结果，未启用则看最近握手和传输统计。
### 安全检查
```sh
wg show interfaces
wg show wg-example latest-handshakes
wg show wg-example transfer
wg show wg-example allowed-ips
ip -4 rule show
ip -4 route show table main
ip -4 route get 172.18.0.1 from 192.168.1.10 iif eth0
```
将示例接口和地址替换成实际值；不要读取或分享 PrivateKey/PresharedKey。测试经过网关的路径应携带实际 source 与 iif，不能仅凭服务器自身 ping 判断 LAN 转发。
自动撤销路由仅针对明确配置的 Peer 探测与本模块管理范围；关闭接口、对端丢包、MTU、底层链路抖动和回程路由都可能影响结果。
MTU 应按路径测量确定；不要盲目增大。

## 防火墙规则分类、区域与匹配方向

域内规则用于区域内相关入站过滤展示；域间规则用于区域之间的转发策略。SNAT 通常在路由确定后的出站阶段修改源地址；DNAT 通常在入站阶段修改目的地址，也存在本机 OUTPUT 等其他路径。
区域可以按接口或来源地址归属；不要只凭规则所属区域推断唯一实际出接口。规则诊断属于运行时快照推演，需同时检查路由、链跳转、NAT、集合和连接跟踪。
### 检查
```sh
firewall-cmd --get-active-zones
firewall-cmd --info-policy=your-policy
firewall-cmd --zone=public --list-all
sudo nft list ruleset
```
运行时与永久配置可能不同，工具视图也可能只覆盖某个管理后端。WireGuard、Docker、WARP 的规则应按来源识别，不能因为显示系统/外部就认定无效。纳管前查看预览与回退范围，不清空其他组件的表。

## WARP 隔离网络与服务器正常通信

WARP 应与服务器管理链路隔离，避免全局策略把 SSH 或 Web 回包送入隧道。接入成功与业务出网是两个检查层次。
本机 warp-svc 显示 inactive 不一定代表隔离空间中的 WARP 不工作，需核对服务实际运行位置。
### 检查
```sh
systemctl status foxray-warp-network.service --no-pager
ip netns list
ip -4 rule show
ip -4 route show table main
ip -4 route get <管理客户端IP> from <服务器IP>
```
隔离空间中的 WARP 客户端建立 Cloudflare 外层连接时，可能需要从隔离接口进入主机再经物理网卡出网。不要把这类 warp → public 配套放行与 LAN → WARP 业务方向混淆。删除规则前核对实际源地址和链路径。

## HTTPS、登录与管理入口

管理监听、HTTP 跳转、HTTPS 证书与登录认证是不同设置。切换 HTTP/HTTPS 后应使用对应端口，证书域名必须与访问方式匹配。
Let’s Encrypt + Cloudflare DNS 使用 DNS 验证，必须安装相应插件并使用最小权限 DNS API 凭据；不要在日志、截图和笔记中展示 Token。凭据文件应设置 0600。
Cloudflare Origin CA 与浏览器公开信任证书用途不同，直接访问源站时不能假设浏览器信任 Origin CA。
修改 HTTPS 或认证前保留 SSH/控制台入口，并先确认新入口可访问。登录失败先检查协议、端口、时间、跳转和浏览器会话，不要直接删除认证配置。

## 备份、升级与回退

升级前在设置中备份配置；发行包与系统依赖分别校验。备份可能含敏感配置，应限制存储目录和下载文件的访问权限。
使用同版本升级脚本的 --help 查看参数和随包手册，先预览，再应用。升级验收包括健康版本、实际运行程序、前端资源、配置摘要、路由、VPN 状态和管理入口。
网络变更预览、应用和确认保留是不同阶段。有待确认变更时先核对业务，确认期限内选择保留或回退。不要在未确认变更期间同时叠加其他网络操作。

## 自动列宽、列筛选与表格操作

自动列宽按当前页内容计算，默认最大 480 px，空间不足时横向滚动。该说明已从各表格 i 图标统一移到帮助中心。
拖动表头分隔线可手动调整宽度，保存在当前浏览器；双击分隔线恢复单列，点击自动列宽恢复整表。
筛选与排序独立。多个列筛选取交集；单列重置保留其他条件，全局重置清除搜索和筛选。当前列表为空时应先检查筛选条件。
