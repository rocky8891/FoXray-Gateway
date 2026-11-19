# 贡献指南

适用版本：FoXray Gateway v0.9.1。

先描述目标或可复现问题，附系统、架构、组件版本和脱敏证据。后端位于 `backend/`，前端位于 `frontend/`；需要 Go 1.27+、Node.js 24、npm 和 Python 3。

```sh
go -C backend test ./...
python3 -m unittest discover -s tests
npm --prefix frontend ci
npm --prefix frontend run build
bash -n install_v0.9.1.sh
bash install_v0.9.1.sh --plan
```

内核网络验证只在隔离命名空间或一次性测试机执行，具体入口见 CI 配置；不要把测试指向真实网关变更接口。UI 验证应覆盖窄屏、键盘、禁用状态和确认流程。

提交说明触发条件、行为变化、验证和限制。保留用户配置、真实缺测语义和逐连接归属边界；不提交凭据、备份、数据库和现场日志。发布脚本与配套手册使用相同版本号。引入第三方资源时更新来源和许可；原创贡献遵守 GPL-3.0-only。
