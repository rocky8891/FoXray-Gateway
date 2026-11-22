# FoXray只读预览 v0.9.3-18

适用完整脚本：`preview_server_v0.9.3-18.py`。保留历史脚本与手册，不更新正式服务。

本版给所有分页选择框预留下拉箭头空间；含自动分页的选择框至少14em宽，字体或浏览器缩放后仍容纳完整文字。WireGuard接口与服务列表的受保护状态改为单独一行，备注与保护说明分开显示。

```sh
npm --prefix frontend run build
python3 preview_server_v0.9.3-18.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880
```

将frontend/dist复制为独立web目录，需要Python3.9+。本次纯前端预览不启动规划器，API只转发GET，修改请求全部409。接口备注保存和网络变更仅在正式服务执行。停止foxray-ui-preview-093-18.service即可撤销预览。
