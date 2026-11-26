# FoXray Gateway v0.9.55 配置参考

程序：`foxray_v0.9.55`。优先从控制台保存，避免同时手工修改正在运行的配置。

## 命令行与服务

```sh
/opt/foxray/foxray --help
/opt/foxray/foxray --base /opt/foxray --ui /opt/foxray/web --listen 0.0.0.0:18880
```

| 入口 | 作用 |
| --- | --- |
| `--base` | 运行数据目录，默认`/opt/foxray` |
| `--ui` | 前端静态资源目录 |
| `--listen` | HTTP监听，v0.9.55新进程默认`0.0.0.0:18880`；升级保留既有服务参数 |
| `--read-only` | 只读验证，关闭周期任务与常规写操作；不是权限隔离沙箱 |
| `--initialize` / `--ensure-module` | 安装器使用的初始化/组件准备入口，可能修改系统，不用于日常健康检查 |
| `--apply-manager-update` | 应用已校验管理程序更新计划，可能重启管理服务 |
| `--check-migration` | 核对既有转发接管兼容性；先阅读具体输出，不等于完成迁移 |

服务配置在`foxray.service`及其drop-in中。不要公开输出环境和命令行中的敏感参数。

`FOXRAY_ALLOWED_HOSTS`是允许的额外请求Host列表，逗号分隔，仅填Host名称（不带scheme或port）。对非内置Host进行写请求时需配置；它不替代来源限制、登录和反向代理认证。通过systemd的`Environment=`配置后重启管理服务生效。

## 文件分类

| 文件或目录 | 内容与处理原则 |
| --- | --- |
| `foxray` / `web` | 当前版本入口；升级时切换到独立release目录 |
| `releases/` | 已安装管理程序与静态资源，可用于回退 |
| `config.json` / `engine.json` | 节点和核心配置，可能含连接凭据；备份不得公开 |
| `components.json` | 组件选择；不是实时服务状态 |
| `manager-settings.json` | 管理、监控刷新、布局、主题等设置 |
| `monitoring-items.json` | 指标启用及顺序；以实际安装文件为准 |
| `private/` | 私有计划、备份和运行元数据，限权保存 |
| `*.sqlite*` / 历史文件 | 监控和日志数据；热备份须考虑SQLite一致性 |
| `rules/` | 分流数据与规则资源；保留上游许可和更新记录 |
| `/etc/wireguard/` | 系统WireGuard配置，私钥不得写入公开问题附件 |

## 保存范围

主页服务总览、当前出口与节点一览分别保存布局。局部保存不应覆盖其他视图。颜色设置按深浅主题分开；随机主题支持临时恢复和一分钟保存。页面宽度独立保存，默认80%，范围80%～100%。

后台采集、历史写入、刷新、主动检测分开配置。降低监控频率不等于关闭自动选优，关闭显示也不等于停止核心。

## 组件与升级

FoXray、Xray-core、WARP 和 WireGuard 工具版本独立比较与更新。

`0.0.0.0` 表示监听全部 IPv4 网卡，浏览器使用服务器实际 IP，例如 `http://<服务器IP>:18880`。本机健康检查可继续使用 `127.0.0.1`。直接通过服务器接口 IP 访问时自动识别本次连接的本地地址；自定义域名按 `FOXRAY_ALLOWED_HOSTS` 配置。


## 页面显示偏好

`presentation.content_width_percent` 设置首页宽度，默认 99%。`presentation.module_width_percent` 设置非首页宽度，默认 95%。两项均接受 80～100 的整数；100 时主页侧边工具自动收起。`presentation.home_header_auto_hide` 为布尔值、默认 false，仅控制首页顶部导航的自动隐藏，不停止任何服务。`overview_show_clock`、`overview_show_data_status` 控制单行页尾；指定节点使用 `nodes_visibility_mode` 与 `nodes_visible_ids`，显示数量由 `nodes_limit` 控制（0 为全部）。

## DNS与域名识别

| 配置字段 | 默认值 | 作用 |
| --- | --- | --- |
| `lan_dns_intercept` | `true` | 随IPv4 LAN接管范围启用UDP／TCP 53智能解析 |
| `dns_force_proxy` | `false` | 自定义规则之后强制DNS走当前代理节点 |
| `dns_direct_servers` | 114／阿里／腾讯公开DNS清单 | 直连上游，最多32个明确IP |
| `dns_proxy_servers` | `1.1.1.1`、`8.8.8.8` | 代理上游及直连组故障备用 |
| `domain_identification` | `false` | 通过核心嗅探和Webhook补充域名，不开启文件访问日志 |

智能解析器使用1053端口，内核仅重定向当前受管LAN的传统DNS，并限制其他入口访问；核心直连／代理上游入口仅监听回环16953／16954。上游DNS报文通过TCP传输，支持各记录类型；DoH、DoT和IPv6不被本功能接管。全部对应上游或代理不可用会返回SERVFAIL。
