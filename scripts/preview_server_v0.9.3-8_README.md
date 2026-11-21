# 防火墙只读预览 v0.9.3-8

适用完整脚本：`preview_server_v0.9.3-8.py`。保留旧版脚本与手册；不替换正式服务。需要 Python 3.9+。

```sh
./foxray-firewall-preview --base ./planner-data --listen 127.0.0.1:18884
python3 preview_server_v0.9.3-8.py --web ./web --listen 127.0.0.1:18881 --upstream http://127.0.0.1:18880 --planner http://127.0.0.1:18884
```

先构建前端 `npm --prefix frontend run build`，复制 `frontend/dist` 为独立 `web` 目录。后端使用 `go -C backend build -o ../foxray-firewall-preview ./cmd/foxray-firewall-preview` 构建，跨平台时设定 GOOS、GOARCH 与 CGO_ENABLED。

`planner-data` 必须是独立目录，不可指向正式数据。初始化组件文件为全部关闭；如需准确显示本应用管理归属，仅从正式防火墙 API 快照提取 managed 条目写入独立 `private/network-managed.json`，禁止复制节点、配置、数据库或凭据。它是启动时管理声明镜像，系统规则与策略每次从实际系统读取；正式服务规则变化后需重建镜像并重启预览规划器。

规划器强制只读、只监听环回地址，不启动采集、组件维护或定时任务；仅允许防火墙快照与防火墙计划。其系统查询需要能够读取 firewalld 状态的权限。前端服务仅允许内网访问；常规 API 仅转发 GET 至正式服务，防火墙快照和计划转发至隔离规划器。POST 仅允许精确路径 `/api/network/plan` 且 kind=firewall。应用、确认、回退、其他写入一律拦截。返回计划带 `preview_only=true`，UI 禁用应用按钮。

计划包含实际读取的运行时与永久差异、生成命令和回退命令。生成计划不执行这些命令。默认行为保持只读；来源归属与区域策略生成计划前需完整读取原值。上游不跟随重定向，请求/响应限制大小，API 查询内容不记日志。

验证：`python -m unittest discover -s tests -p test_preview_server_v0_9_3_8.py`。停止该预览及其规划器即可撤销入口，不影响正式服务；发布正式版本时不能使用预览服务代替正式后端。
