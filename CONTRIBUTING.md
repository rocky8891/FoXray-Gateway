# 贡献指南

适用版本：FoXray Gateway v0.9.3。

先描述目标或可复现问题，附系统、架构、组件版本和脱敏证据。后端位于 `backend/`，前端位于 `frontend/`；需要 Go 1.27+、Node.js 24、npm 和 Python 3。

```sh
go -C backend test ./...
python3 -m unittest discover -s tests
npm --prefix frontend ci
npm --prefix frontend run build
bash -n install_v0.9.3.sh
bash install_v0.9.3.sh --plan
```

内核网络验证只在隔离命名空间或一次性测试机执行，具体入口见 CI 配置；不要把测试指向真实网关变更接口。UI 验证应覆盖窄屏、键盘、禁用状态和确认流程。

提交说明触发条件、行为变化、验证和限制。保留用户配置、真实缺测语义和逐连接归属边界；不提交凭据、备份、数据库和现场日志。发布脚本与配套手册使用相同版本号。引入第三方资源时更新来源和许可；原创贡献遵守 GPL-3.0-only。

## 界面设计

修改界面时遵循 [FoXray UI 设计规范](docs/UI_DESIGN.md) 和该版本的控件规范。

## 发布安装包

从 2026 年 11 月 27 日起，`0.9.x` 每次正式发布使用独立补丁号，包括小修复、界面调整和小升级。本地和 GitHub Releases 各保留最多 10 个正式版本，具体执行口径见 [项目发布规则](AGENTS.md)。

正式发行默认提供在线安装包、源码包和 `SHA256SUMS.txt`。收到用户明确的离线发布要求后，使用发行标签手动运行 `Package offline release`，生成并上传 Rocky Linux 9 和 10 x86_64 两套离线整包及各自的 SHA256 校验文件。若要求两套离线包随新版本同时上线，在草稿阶段运行工作流并核对资产，随后发布草稿。

提供离线整包的版本在发行说明中列出对应安装手册。离线安装使用包内组件和 RPM 仓库。组件运行期的外部连接需求见对应手册。
