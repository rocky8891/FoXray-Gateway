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

## 升级及卸载

已有安装使用[管理程序更新工具](upgrade_v0.9.4_README.md)，不重装转发组件。后续版本可在版本管理卡片查看更新说明后确认更新。

停止管理服务不等于撤销透明接管或停止VPN。没有“一条删除目录即可安全还原网络”的卸载承诺。先通过独立管理通道恢复接管前路由/DNS/防火墙、确认业务路径，再按需停用所选组件；备份运行数据后才删除程序文件。外部组件和非FoXray规则不能无条件删除。
