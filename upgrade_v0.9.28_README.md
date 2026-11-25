# 管理程序更新工具 v0.9.28

适用完整脚本：`upgrade_v0.9.28.py`；目标二进制 `foxray_v0.9.28`、前端 v0.9.28。运行平台：Linux amd64、systemd、Python 3.9+。

工具用于已采用 `/opt/foxray/foxray` 与 `/opt/foxray/web` 符号链接的安装。先验证发行包及包内 SHA256 清单，再将管理程序和前端写入独立目录，仅重启 `foxray.service`。Xray 和 WARP PID、出口、配置、接口、主路由、策略路由和 DNS 在更新后核对；基线变化或健康检查失败时回退程序入口。

若缺少公共域名数据文件 `rules/gfwlist_domains.json`，工具从已校验发行包补齐；已有文件不覆盖。该公共资源不包含用户节点或配置，回退程序入口时可保留。

## 准备

下载发行包与校验文件，备份运行配置和数据库，确认无并发配置变更及未确认网络计划，并保留独立管理通道。工具不更新或重装转发组件，不清空业务数据。

```sh
sha256sum --ignore-missing -c SHA256SUMS.txt
unzip FoXray-Gateway-v0.9.28-linux-amd64.zip -d foxray-0.9.28
cd foxray-0.9.28
PACKAGE=../FoXray-Gateway-v0.9.28-linux-amd64.zip
DIGEST=$(sha256sum "$PACKAGE" | cut -d ' ' -f 1)
python3 upgrade_v0.9.28.py --archive "$PACKAGE" --sha256 "$DIGEST"
sudo python3 upgrade_v0.9.28.py --archive "$PACKAGE" --sha256 "$DIGEST" --apply
```

`--sha256` 必须先与可信 Release 提供的校验文件核对，不能仅凭自行计算的值认定下载可信。已有监听地址不同于回环时，添加 `--health http://实际管理地址:端口/api/health`。

## 验收与回退

工具输出应包含 `updated:true`、`version:0.9.28` 和 `baseline_unchanged:true`。随后强制刷新浏览器，核对节点、设置、组件与检测页面。回退记录位于受限私有目录；回退时使用本机实际记录路径：

```sh
sudo python3 upgrade_v0.9.28.py --rollback /opt/foxray/private/migration-v0.9.28-时间戳.json --apply
```

回退会重启管理服务并检查原健康版本。记录、备份和运行目录可能敏感，禁止上传仓库或公开附件。

从 v0.9.1 更新时，公开状态中缺失的 `home_header_auto_hide` 按其关闭默认值比较；已有真实值与其他配置变化仍参与基线校验，不会被忽略。

从0.9.3升级时保留现有DNS与域名识别设置，不需要重新应用代理配置。跨越更早版本时，沿用0.9.3引入的DNS默认值规范化基线，显式选择仍严格比较；需要启用新DNS接管规则时再单独保存并应用代理配置。管理程序更新不自动重启核心。
