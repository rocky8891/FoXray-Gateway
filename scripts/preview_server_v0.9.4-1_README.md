# FoXray只读预览 v0.9.4-1

适用完整脚本：`preview_server_v0.9.4-1.py`。Python3.9+，保留历史版本。

服务器四项数值采用独立资源色板：平均负载暖金#e6cf6a、CPU珊瑚红#ff8f9c、内存冰青#62dce8、磁盘黄绿#bad77a。浅色主题分别使用#806500、#b43d58、#007784、#567319。仅作用于主页服务器数值文字；矩形与圆形共用新数值色板，图标、圆环、上传／下载／延迟／连接数等原配色和布局不变。

```sh
npm --prefix frontend run build
python3 preview_server_v0.9.4-1.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880
```

frontend/dist复制至独立web目录。仅转发GET，所有修改409，不变更正式服务。
