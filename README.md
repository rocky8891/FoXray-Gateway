# FoXray Gateway

**自托管 Linux 网关的智能出口管理与可观测控制台。**

FoXray 将 Xray-core、Cloudflare WARP、WireGuard、服务器监控和 Linux 网络管理放在同一套 Go + React 控制台中。查看真实运行状态、管理节点与订阅、检测出口质量，并通过计划与确认流程执行网络变更。

**当前发行版：v0.9.46** · Linux amd64 · GPL-3.0-only

[下载](https://github.com/rocky8891/FoXray-Gateway/releases/tag/v0.9.46) · [部署](install_v0.9.46_README.md) · [使用](foxray_v0.9.46_README.md) · [发行说明](release-notes/v0.9.46.md) · [运维](docs/OPERATIONS_v0.9.46.md)

## 产品定位

面向家庭实验室、个人网关和单一信任域的小型网络，集中管理出口与系统网络，提供真实检测和可解释的监控。FoXray 管理程序与转发组件独立运行；更新控制台不等于更新 Xray、WARP 或 WireGuard。

## 功能

| 模块 | 能力 |
| --- | --- |
| 主页 | 服务总览、当前出口、节点一览；可编辑布局、主题与指标 |
| 监控 | CPU、负载、内存、磁盘 I/O、网络与连接；历史图和 CSV |
| Xray | 节点、订阅、检测评分、出口选优、DNS 和备份 |
| WARP | 隧道状态、连接操作、候选端点扫描与检测 |
| WireGuard | 接口、peer、服务动作、接口级网络计划和监控 |
| 网络与防火墙 | 接口、路由、策略路由、DNS、端口规则、SNAT 与端口转发 |
| 诊断与任务 | 连接诊断、任务进度、操作记录、检测与切换日志 |
| 版本与集成 | 管理程序和组件独立检查更新；连接独立部署的 Komari |

节点、订阅等资源使用紧凑列表，支持搜索、筛选、排序、跨页选择、CSV 导出和省略号操作菜单；节点与订阅也可切换宫格并单击多选。管理列表与宫格默认每页 10 项，数量与选择统计显示在下方。数据缺失会明确显示，不以零填充，也不凭当前出口猜测逐连接归属。

## 部署要求

- 发行包：Linux amd64；完整部署验证环境为 Rocky Linux 10、systemd、nftables。
- 安装前使用 `--check-topology` 核对 IPv4 默认出口、LAN 网段和上游 DNS。
- 系统网络操作需要 root；发行包不提供经过验证的 Docker 部署。
- 控制台无内置账号认证、TLS、RBAC 或多租户；默认监听 `0.0.0.0:18880`，通过服务器实际 IPv4 地址访问。公网访问必须另配认证、TLS 和访问控制。

## 快速开始

### 在线安装

目标机可以访问 GitHub、组件下载地址及系统软件源时，使用 `FoXray-Gateway-<版本>-linux-amd64.zip` 在线安装。

从 Release 下载发行包和 `SHA256SUMS.txt`，放在同一目录：

```sh
sha256sum --ignore-missing -c SHA256SUMS.txt
unzip FoXray-Gateway-v0.9.46-linux-amd64.zip -d foxray-0.9.46
cd foxray-0.9.46
chmod u+x foxray_v0.9.46
./foxray_v0.9.46 --check-topology
bash install_v0.9.46.sh --plan \
  --with-xray --with-wireguard --without-warp --without-komari \
  --listen 0.0.0.0:18880
# 核对系统与网络拓扑后开始安装。
sudo bash install_v0.9.46.sh --non-interactive \
  --with-xray --with-wireguard --without-warp --without-komari \
  --listen 0.0.0.0:18880
```

按上述步骤部署 Xray 和 WireGuard，控制台监听 `0.0.0.0:18880`（全部 IPv4 网卡）。浏览器访问 `http://<服务器IP>:18880`，将占位符换为服务器实际 IPv4 地址。需要 SSH 隧道时可使用：

```sh
ssh -N -L 18880:127.0.0.1:18880 admin@gateway.example
```

通过 SSH 隧道时访问 `http://127.0.0.1:18880`。进入控制台后核对组件状态，导入节点，手动检测并确认出口。已有安装使用[管理程序更新工具](upgrade_v0.9.46_README.md)。

### 离线安装

离线整包按需发布。适合无法访问 GitHub、组件下载地址或软件源的 **Rocky Linux 9 或 10 x86_64** 主机。离线包内含安装脚本、组件包和本地 RPM 仓库。在可联网的电脑下载与目标系统主版本对应的离线整包和校验文件，一并复制到目标机。以下命令适用于提供离线整包的版本，将 `0.9.x` 换成实际版本：

```sh
version=0.9.x
rocky_major=9   # Rocky Linux 10 使用 10
sha256sum --ignore-missing -c "OFFLINE-ROCKY${rocky_major}-SHA256SUMS.txt"
tar -xzf "FoXray-Gateway-v${version}-offline-rocky${rocky_major}-amd64.tar.gz"
cd "foxray-offline-${version}"
sudo bash "foxray-${version}/install_v${version}.sh" --offline-dir "$PWD" \
  --non-interactive --with-xray --with-wireguard --without-warp --without-komari
```

具备联网准备机时，可按[离线安装手册](prepare_offline_v0.9.46_README.md)生成与目标主机主版本一致的离线目录。

## 文档

| 文档 | 内容 |
| --- | --- |
| [产品说明](docs/PRODUCT_v0.9.46.md) | 场景、架构、组件关系与功能边界 |
| [离线安装手册](prepare_offline_v0.9.46_README.md) | Rocky Linux 9 或 10 x86_64 依赖下载与一键安装 |
| [在线安装与部署手册](install_v0.9.46_README.md) | 系统要求、安装参数、首次访问与卸载 |
| [使用手册](foxray_v0.9.46_README.md) | 节点、订阅、检测、监控与网络操作 |
| [配置参考](docs/CONFIGURATION_v0.9.46.md) | 程序参数、文件与设置范围 |
| [控件规范](docs/UI_v0.9.46.md) | 按钮尺寸、字体、状态及跨页面使用规则 |
| [更新与回退](upgrade_v0.9.46_README.md) | 校验、独立管理程序更新和回退 |
| [配置备份与恢复](docs/CONFIG_BACKUP_v0.9.46.md) | 加密导出、重装和配置恢复 |
| [运维与排障](docs/OPERATIONS_v0.9.46.md) | 健康检查、备份与问题定位 |
| [构建与打包](build_release_v0.9.46_README.md) | 源码、构建、许可证清单与发行校验 |
| [安全](SECURITY.md) / [贡献](CONTRIBUTING.md) | 管理边界、问题报告和开发验证 |
| [许可](docs/LICENSING_v0.9.46.md) / [致谢](ACKNOWLEDGEMENTS.md) | GPL 与第三方资源来源 |

## 源码构建

需要 Go 1.27+、Node.js 24、npm 和 Python 3：

```sh
go -C backend test ./...
mkdir -p output
CGO_ENABLED=0 go -C backend build -buildvcs=false -trimpath -ldflags='-s -w' -o ../output/foxray_v0.9.46 ./cmd/foxray
npm --prefix frontend ci
npm --prefix frontend run build
python3 build_release_v0.9.46.py
```

Linux 内核集成验证在隔离命名空间执行，见 CI 配置。普通用户或 Windows 下被跳过的内核测试不代表已经验证相应网络能力。

## 许可与反馈

项目原创代码采用 [GPL-3.0-only](LICENSE)。第三方依赖、数据、图标和独立组件保留各自许可，WARP 还受其服务条款约束。发行包附对应源码和依赖许可清单。

使用问题请提交 [Issue](https://github.com/rocky8891/FoXray-Gateway/issues)，包含版本、系统、复现步骤和脱敏日志。安全问题请按 [SECURITY.md](SECURITY.md) 报告。
