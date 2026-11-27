# i 说明检查与编写规范 v0.9.72

适用产品：FoXray Gateway v0.9.72，同版本界面更新。

## 检查范围

检查共享 SettingInfo、Field 的 info/hint、页面标题 description、服务管理、模块设置、WireGuard、Xray、WARP、DNS、路由、防火墙、监控、主页布局、外观与配置保存说明。已抽取 154 个静态说明样本，来自 36 个文件；动态说明另检查条件分支和组合字段。

## 内容规范

- 简短格式提示保留一句；包含多个独立要点的说明分项展示。
- 较长说明按用途、影响范围、取值、操作步骤和生效条件组织，不给所有提示套同一个标题。
- 需要按顺序操作时使用有序列表，包含并列选择时使用无序列表；步骤和子条件可以嵌套。
- 保留动态值、命令和已有结构化内容。明确区分显示开关、监控采集、主动检测和历史写入。
- 区分服务当前运行状态与开机自启；WireGuard 备注修改不重启接口，地址和路由变更影响以预览为准。
- 表格选择列与操作列不设置排序筛选，所有数据列提供相应按钮。

## 本次改动

1. 统一渲染入口处理普通字符串、React 组合片段、Field 说明与提示、动态段落，并保留已有多级列表。
2. 重写服务管理、VPN 接口、模块开关、监控频率、流量来源、区域归属、默认动作、主页布局等说明，补充小标题及分层步骤。
3. 说明弹层使用 13px 字号与 1.7 行高；长内容在视口内滚动，支持键盘打开和 Escape 关闭。
4. WireGuard 对端隧道 IP 的保存提示移入标题 i 说明。取消标题行总筛选；接口、状态、识别方式、目标与候选地址都支持排序与筛选。

## 静态样本分布

| 文件 | 样本数 |
|---|---:|
| `config_backup.tsx` | 1 |
| `connections.tsx` | 2 |
| `cross_zone_rules.tsx` | 2 |
| `custom_rules.tsx` | 1 |
| `dns.tsx` | 1 |
| `firewall.tsx` | 5 |
| `firewall_diagnosis.tsx` | 2 |
| `firewall_editors.tsx` | 8 |
| `firewall_workspace.tsx` | 10 |
| `host.tsx` | 7 |
| `hubs.tsx` | 4 |
| `log_settings.tsx` | 1 |
| `management_security.tsx` | 1 |
| `modules.tsx` | 2 |
| `monitor_items.tsx` | 2 |
| `monitoring_settings.tsx` | 6 |
| `network.tsx` | 18 |
| `network_hub.tsx` | 7 |
| `node_visibility_picker.tsx` | 1 |
| `nodes.tsx` | 2 |
| `overview_clock.tsx` | 4 |
| `overview_surfaces.tsx` | 6 |
| `page_background.tsx` | 2 |
| `presentation_settings.tsx` | 17 |
| `probe_interval_settings.tsx` | 1 |
| `settings_save.tsx` | 1 |
| `traffic.tsx` | 2 |
| `traffic_source_setting.tsx` | 1 |
| `ui.tsx` | 1 |
| `version_panel.tsx` | 2 |
| `views.tsx` | 18 |
| `vpn_exclusions.tsx` | 4 |
| `warp.tsx` | 5 |
| `wg_monitor.tsx` | 2 |
| `wg_probe_targets.tsx` | 1 |
| `xray_observation_policy.tsx` | 4 |

## 验收结果

- 前端 TypeScript 检查和生产构建通过。
- 浏览器逐项打开 154 个静态说明样本，检查非空内容、列表结构、无非法段落嵌套及弹层视口边界。
- 补充检查 Field 组合说明、代码与文本混排、动态段落、真实 WireGuard 接口管理说明。
- 390px 窄屏验证服务管理多级步骤和命令显示；154 个静态样本中 108 个包含列表。
- WireGuard 5 个数据列分别验证排序状态、独立筛选、重置；检查总筛选位置和说明迁移。
- 本次未修改后端。更新时仅切换 web 目录，保留运行中的管理服务和 VPN 接口。

## 说明标题标点

- 短标题与解释正文使用全角冒号，不使用句号分隔。
- 本轮统一了 162 处加粗说明标签，并保留正文句号及原有列表层级。

## 筛选交互标准补充

WireGuard 目标表仅保留列头筛选，删除标题行重复入口。所有列筛选的清除按钮均使用「重置」，只清空当前列条件；工具栏重置仍作用于整表。参数区与目标表恢复 16px 标准间距，已在浏览器中测量并验证。
