# 构建与打包 v0.9.22

适用完整脚本：`build_release_v0.9.22.py`；产品、安装脚本、更新工具和手册均为 v0.9.22。需要 Go 1.27+、Node.js 24、npm、Python 3.9+ 和 Git。

```sh
go -C backend test ./...
python3 -m unittest discover -s tests
mkdir -p output
CGO_ENABLED=0 go -C backend build -buildvcs=false -trimpath -ldflags='-s -w' -o ../output/foxray_v0.9.22 ./cmd/foxray
npm --prefix frontend ci
npm --prefix frontend run build
python3 build_release_v0.9.22.py
```

Windows 交叉编译时显式设置 GOOS=linux、GOARCH=amd64、CGO_ENABLED=0；命令参数 `--go` 可指定 Go 可执行文件。打包要求产品源码干净且已提交；用户自有的 assets 目录不纳入源码或发行包，也不参与产品源码洁净检查。发行源码由同一 Git 快照导出，不包含 `.git`、依赖目录、构建缓存或运行配置。

输出位于 output：Linux amd64 ZIP、对应源码 ZIP、SHA256SUMS.txt。运行包附 SHA256 成员清单和 npm / Go 依赖许可；打包会验证包版本与 ELF 平台，并重新读取全部成员校验摘要。Xray、WARP 等外部程序由各组件的官方源获取。Rocky Linux 9 离线安装使用随包提供的 `prepare_offline_v0.9.22.sh` 下载第三方依赖。

先验证校验文件再解包；仅在独立测试机执行安装器。`--plan` 可用于不改变系统的参数核验。

发行二进制禁用构建目录的 VCS 标记（`-buildvcs=false`）；源码由发布提交导出，包与成员摘要负责对应校验，避免用户未跟踪目录影响二进制标记。

运行包仅包含0.9.22安装／升级工具及对应文档，旧版本脚本与手册保留在源码历史中。前端构建沿用已验证的check_radius_v0.9.3.mjs控件检查器，其脚本版本不代表产品版本。

收到明确的离线发布要求后，手动运行 `Package offline release` 工作流分别在 Rocky Linux 9 和 10 x86_64 容器内组装依赖。在线包通过校验后，工作流生成两个离线整包及各自的 SHA256 文件。
