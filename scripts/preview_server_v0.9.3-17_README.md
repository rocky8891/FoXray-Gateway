# FoXray 只读预览 v0.9.3-17

适用完整脚本：`preview_server_v0.9.3-17.py`。保留历史脚本与同版本手册，不更新正式服务。需要 Python 3.9+。

## 本版变化

WireGuard新增接口与完整编辑表单提供可选备注（最多200字符）。已有接口增加独立的编辑备注操作，运行中的接口也能修改。备注以JSON字符串保存在wg-quick配置的FoXray专用注释中，列表、服务列表和配置编辑均可查看。单独备注计划不运行任何服务或路由命令，保留其他配置字节，纳入预览、确认、120秒回退及配置版本核对；修改地址、Peer等仍要求先停用接口。备注与受保护状态相互独立，不凭名称识别用途。

节点一览默认按页面高度计算自动条数。分页下拉菜单提供“自动（N项/页）”，用户手动选择10／20／50／100后，自动容量不再限制选择，超出区域允许滚动。切回自动后恢复随高度调整。

## 构建与部署

```sh
npm --prefix frontend run build
go -C backend build -o ../foxray-wireguard-preview ./cmd/foxray-wireguard-preview
./foxray-wireguard-preview --base ./planner-data --listen 127.0.0.1:18885
python3 preview_server_v0.9.3-17.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880 --planner http://127.0.0.1:18885
```

跨平台构建明确指定GOOS=linux、GOARCH=amd64、CGO_ENABLED=0。复制frontend/dist至独立web目录。planner-data必须与正式数据隔离，components.json仅启用wireguard以生成接口计划，其他组件全部关闭；New初始化不启动后台任务。不得复制正式配置、数据库或凭据。规划器只读取WireGuard状态及脱敏profile，仅允许接口变更计划，不启动后台任务，不提供应用、确认、回退、密钥生成或客户端模板接口。预览不执行真实备注修改。

Python服务只允许内网客户端，API GET读取正式服务，WireGuard列表／profile及计划使用隔离规划器；其他所有写入一律409。计划标注preview_only并禁用应用按钮。以systemd的ProtectSystem=strict额外禁止系统配置写入，仅允许隔离数据目录初始化。

## 验证与撤销

```sh
python -m unittest discover -s tests -p test_preview_server_v0_9_3_17.py
go -C backend test ./...
```

单元验证覆盖备注校验、配置字节保留、解析／渲染、无运行时步骤、持久回退及过期配置拒绝。浏览器检查备注入口与只读计划、自动分页、手动覆盖、滚动及窗口变化。停止foxray-ui-preview-093-17和foxray-wireguard-preview-093-17即可撤销本次预览；正式服务不变。
