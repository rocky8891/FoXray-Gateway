# 防火墙验证 v0.9.3-22

适用完整脚本：`preview_server_v0.9.3-22.py`。沿用隔离防火墙规划器；新增GET服务状态入口，独立读取服务状态。仅验证页面与计划，不执行应用。其他修改接口409。

构建前端并复制dist至web，构建backend/cmd/foxray-firewall-preview至独立目录。

```sh
./foxray-firewall-preview --base ./planner-data --listen 127.0.0.1:18885
python3 preview_server_v0.9.3-22.py --web ./web --listen 192.168.1.8:18883 --upstream http://192.168.1.8:18880 --planner http://127.0.0.1:18885
```

planner-data为独立受限目录，组件全部关闭，不复制正式配置与凭据。规划器不启动后台作业，系统只读保护。服务状态先加载，规则并行读取并复用区域详情；策略、接口、来源及服务值使用绿色，标题与标签字体按本次要求统一。测试通过后发布正式包，停止临时服务并清理隔离目录。
