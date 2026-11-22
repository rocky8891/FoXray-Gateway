# FoXray只读预览 v0.9.3-27

适用完整脚本：`preview_server_v0.9.3-27.py`。Python3.9+，保留历史版本。

页眉FoXray Gateway名称中的X使用橙色#ff8033，其余文字沿用主题文字颜色、字号与字重。名称保持完整无障碍标签。

```sh
npm --prefix frontend run build
python3 preview_server_v0.9.3-27.py --web ./web --listen 192.168.1.8:18881 --upstream http://192.168.1.8:18880
```

复制frontend/dist至web。仅代理GET，写入409，正式服务不变。
