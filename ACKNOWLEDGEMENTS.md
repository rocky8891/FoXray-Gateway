# 来源与致谢

FoXray Gateway v0.9.1 使用 Go 后端与 TypeScript / React 前端。以下列明独立组件、依赖、公共资源及设计参考；参考交互不代表包含上游的完整实现或能力。

## 独立组件与设计参考

- XTLS/Xray-core：代理协议、TUN 转发及本机 Metrics 接口；FoXray 通过独立二进制与公开接口使用，不嵌入其 Go 源码。https://github.com/XTLS/Xray-core
- Cloudflare WARP：使用官方客户端提供的隧道和状态；与 Cloudflare 无隶属关系，遵守其条款。
- WireGuard：依赖系统内核与 wg / wg-quick 工具；https://www.wireguard.com/
- vernette/warpscout：使用 v0.16.1 独立 CLI 进行用户态隧道扫描；MIT 许可副本见 `third_party/warpscout-LICENSE.txt`。https://github.com/vernette/warpscout
- v2rayN：参考独立代理端口的真连接测量方式、公开检测 URL 和规则交互；没有复制 C# 源码。https://github.com/2dust/v2rayN
- doona、daed、honk：参考节点、策略、探测证据及连接视图的组织方式；没有嵌入其源码。https://github.com/Zakkaus/doona · https://github.com/daeuniverse/daed · https://github.com/daeuniverse/honk
- 1Panel、Cockpit、Komari：管理与监控交互参考；Komari 服务独立部署。
- Xray-core、3x-ui、doona：产品说明结构参考，文档由本项目按实际实现编写。https://github.com/MHSanaei/3x-ui

## 背景与图标

- `frontend/public/background.webp` 来源于 sanrokamlan-prog/komari-theme-Glassmorphism 的 `public/images/default-background-v2.webp`；原图 Git blob 为 `0211d5827d06f4fa51717225f592bb369cc5ecc6`。使用 Lanczos 从 2048×1152 放大为 3840×2160 WebP，MIT 全文保留在 `third_party/Komari-Glassmorphism-LICENSE`。
- 品牌 Logo 由项目维护者提供；SNAT、主页、时钟等图形按提供的 SVG 路径集成。
- Xray 标识参考 XTLS 四芒星轮廓独立绘制。Cloudflare 标识使用官方 `logo-cloudflare-white.svg` 云形子路径，改为 currentColor；WireGuard 使用官方 `https://www.wireguard.com/img/wireguard.svg` 盘龙路径与变换，外圈调整为正圆。标识及商标权归各自权利人。
- Lucide、flag-icons 通过 npm 使用并保留相应许可。Country.is 用于公开入口 IP 国家代码查询；不上传节点认证字段。https://country.is/

## 规则与依赖

- GFWList 域名数据来自 gfwlist/gfwlist，LGPL-2.1 许可副本见 `third_party/gfwlist-COPYING.txt`；来源和摘要位于 `rules/gfwlist_domains.json`。仅转换能等价表示的域名规则，不承诺全部 URL / 正则语法兼容。
- 预定义远程规则引用 Loyalsoldier/v2ray-rules-dat 的 direct-list / proxy-list，源数据与维护权归上游。https://github.com/Loyalsoldier/v2ray-rules-dat
- React、Recharts、Lucide、flag-icons、Go netlink/netns、SQLite 等依赖以锁文件为准；发行包附其许可清单。第三方许可和版权不能因本项目使用 GPL 而删除。
