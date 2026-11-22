# FoXray只读预览 v0.9.3-23

适用完整脚本：`preview_server_v0.9.3-23.py`。Python3.9+，保留历史版本。

采用需求提供的骰子SVG替换主页随机主题图标。防火墙绑定接口、配置来源、允许服务及区域策略值恢复灰白色（var(--muted)，与允许服务标签一致），转发规则查看链接保持绿色下划线。default显示为系统默认（其余拒绝），并说明未匹配规则的入站流量默认允许ICMP、其他拒绝，ICMP阻断可改变处理，转发另受区域转发及策略影响。只解释原有配置，不改变防火墙默认目标。

```sh
npm --prefix frontend run build
python3 preview_server_v0.9.3-23.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880
```

frontend/dist复制至web。只读GET代理，所有修改请求409，不启动规划器，正式服务不变。停止foxray-ui-preview-093-23.service可撤销。
