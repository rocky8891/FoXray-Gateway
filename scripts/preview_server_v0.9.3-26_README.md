# FoXray只读预览 v0.9.3-26

适用完整脚本：`preview_server_v0.9.3-26.py`。Python3.9+，保留历史版本。

区域策略的运行时默认行为、地址伪装及ICMP状态恢复绿色，其余来源、接口及服务标签沿用灰白色。服务器对称矩形指标悬浮与Xray一致：背景轻微高亮、边框隐藏；只作用metrics矩形，圆形样式不变。主题和随机主题直接使用本次提供的原始gateway-icon-spectrum.svg、gateway-rotor-spectrum.svg，保留渐变配色与全部路径，不重绘图案。SVG以独立图像资源加载，渐变ID隔离。

```sh
npm --prefix frontend run build
python3 preview_server_v0.9.3-26.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880
```

frontend/dist复制至web。只转发GET，其他写入409。浏览器模拟矩形与圆形显示配置，不保存正式设置。停止foxray-ui-preview-093-26.service即可撤销预览。
