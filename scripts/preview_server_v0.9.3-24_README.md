# FoXray只读预览 v0.9.3-24

适用完整脚本：`preview_server_v0.9.3-24.py`。保留历史版本，Python3.9+。

默认行为按“运行时 · 系统默认（default）”显示，原有说明放入紧随系统默认的i提示，移除常驻解释段落。其他目标保留真实运行时值说明。主题与随机主题使用本次提供的花瓣、风车图形，按独立闭合轮廓使用多色填充，不修改原图形，不受按钮hover颜色覆盖。

```sh
npm --prefix frontend run build
python3 preview_server_v0.9.3-24.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880
```

frontend/dist复制为独立web目录，只转发GET，所有修改409；随机主题仅本地试用。正式服务与规则不变。
