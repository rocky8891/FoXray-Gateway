# FoXray只读预览 v0.9.3-25

适用完整脚本：`preview_server_v0.9.3-25.py`。Python3.9+，历史版本保留。

服务器“对称矩形”（metrics）四个指标格采用与Xray相同的1px边框和圆角，使用已有主题边框颜色。选择器限定服务器metrics且排除自适应圆形，圆形及其他模块不改。

```sh
npm --prefix frontend run build
python3 preview_server_v0.9.3-25.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880
```

frontend/dist复制至web。只转发GET，全部修改409，正式服务不变。浏览器通过模拟显示配置验证矩形边框和圆形样式，不保存正式设置。
