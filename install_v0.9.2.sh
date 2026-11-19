#!/usr/bin/env bash
# FoXray Gateway 0.9.2 — Go / React, optional Xray / WARP / WireGuard; Komari is never installed.
set -Eeuo pipefail
umask 077
source_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
target=/opt/foxray
listen=0.0.0.0:18880
xray=auto; warp=auto; wireguard=auto; komari=auto; komari_url=""; accept_terms=0; plan=0; interactive=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --with-xray) xray=true;; --without-xray) xray=false;;
    --with-wireguard) wireguard=true;; --without-wireguard) wireguard=false;;
    --komari-url) shift; komari_url="${1:?missing Komari URL}";;
    --with-warp) warp=true;; --without-warp) warp=false;;
    --with-komari) komari=true;; --without-komari) komari=false;;
    --accept-warp-tos) accept_terms=1;; --non-interactive) interactive=0;;
    --listen) shift; listen="${1:?missing listen address}";;
    --plan) plan=1; interactive=0;;
    --help|-h) echo 'bash install_v0.9.2.sh [--with-xray|--without-xray] [--with-wireguard|--without-wireguard] [--with-warp|--without-warp] [--with-komari|--without-komari] [--komari-url HTTP_URL] [--accept-warp-tos] [--listen IPv4:port] [--non-interactive] [--plan]'; exit 0;;
    *) echo "未知参数：$1" >&2;exit 2;;
  esac
  shift
done
[[ "$listen" =~ ^[0-9.]+:[0-9]+$ ]] || { echo '监听地址须为 IPv4:port' >&2;exit 2; }
if [[ $interactive -eq 1 && -t 0 ]]; then
  if [[ $xray == auto && ! -e "$target/components.json" ]]; then
    read -r -p '是否启用 Xray？默认启用 [Y/n] ' answer
    xray=true; [[ $answer =~ ^[Nn]([Oo])?$ ]] && xray=false
  fi
  if [[ $wireguard == auto && ! -e "$target/components.json" ]]; then
    read -r -p '是否启用 WireGuard 管理？默认不启用 [y/N] ' answer
    wireguard=false; [[ $answer =~ ^[Yy]([Ee][Ss])?$ ]] && wireguard=true
  fi
  if [[ $warp == auto && ! -e "$target/components.json" ]]; then
    read -r -p '是否安装 WARP？默认不安装 [y/N] ' answer
    warp=false; [[ $answer =~ ^[Yy]([Ee][Ss])?$ ]] && warp=true
  fi
  if [[ $komari == auto && ! -e "$target/components.json" ]]; then
    read -r -p '是否连接已独立部署的 Komari？默认不启用 [y/N] ' answer
    komari=false; [[ $answer =~ ^[Yy]([Ee][Ss])?$ ]] && komari=true
  fi
fi
resolve_choice(){
  local choice="$1" key="$2" fallback="${3:-false}"
  if [[ $choice != auto ]];then printf '%s' "$choice";return;fi
  if [[ -f "$target/components.json" ]] && command -v jq >/dev/null;then jq -r --arg key "$key" --arg fallback "$fallback" 'if has($key) then .[$key] == true else $fallback == "true" end' "$target/components.json";else printf '%s' "$fallback";fi
}
if [[ $plan -eq 1 ]];then
  printf 'FoXray Gateway 0.9.2\n目录：%s\n监听：%s\nXray：%s\nWireGuard：%s\nWARP：%s\nKomari（仅连接）：%s\n未指定的已有组件沿用 manifest；新装默认 Xray 启用，其余不选。此命令不写文件或更改服务。\n' "$target" "$listen" "$xray" "$wireguard" "$warp" "$komari"
  exit 0
fi
[[ $EUID -eq 0 ]] || { echo '请使用 root 执行' >&2;exit 1; }
[[ ! -L "$target" ]] || { echo '安装目录不能是符号链接' >&2;exit 1; }
[[ -d /run/systemd/system ]] || { echo '当前安装器需要 systemd' >&2;exit 1; }
missing=0
for command in curl jq nft ip ping setpriv unzip tar;do command -v "$command" >/dev/null || missing=1;done
if [[ $missing -eq 1 ]];then
  if command -v dnf >/dev/null;then dnf install -y curl jq ca-certificates nftables iproute iputils util-linux unzip tar
  elif command -v apt-get >/dev/null;then apt-get update;apt-get install -y curl jq ca-certificates nftables iproute2 iputils-ping util-linux unzip tar
  else echo '请先安装 curl、jq、nftables、iproute、iputils、util-linux、unzip、tar、wireguard-tools' >&2;exit 1;fi
fi
# Current transparent-routing implementation is validated for this topology.
ip link show dev ens16 >/dev/null || { echo '透明接管当前要求 ens16；请先按部署手册核对拓扑，未改动现有路由。' >&2;exit 1; }
xray="$(resolve_choice "$xray" xray true)";wireguard="$(resolve_choice "$wireguard" wireguard "$([[ -e "$target/components.json" ]] && echo true || echo false)")";warp="$(resolve_choice "$warp" warp)";komari="$(resolve_choice "$komari" komari)"
if [[ $warp == true && $accept_terms -ne 1 ]] && ! command -v warp-cli >/dev/null;then echo '新装 WARP 需要 --accept-warp-tos 明确接受官方条款。' >&2;exit 1;fi
work="$(mktemp -d /tmp/foxray-install-XXXXXXXX)"
trap 'rm -rf -- "$work"' EXIT
binary="$source_dir/foxray_v0.9.2";web="$source_dir/web"
if [[ ! -f "$binary" ]];then
  if [[ -f "$source_dir/output/foxray_v0.9.2" && -d "$source_dir/frontend/dist" ]];then binary="$source_dir/output/foxray_v0.9.2";web="$source_dir/frontend/dist"
  else
    command -v go >/dev/null && command -v npm >/dev/null || { echo '源码部署需要 Go 1.27+ 与 Node.js；也可使用已编译发行包。' >&2;exit 1; }
    go -C "$source_dir/backend" test ./...
    CGO_ENABLED=0 go -C "$source_dir/backend" build -trimpath -ldflags='-s -w' -o "$work/foxray_v0.9.2" ./cmd/foxray
    npm --prefix "$source_dir/frontend" ci
    npm --prefix "$source_dir/frontend" run build
    binary="$work/foxray_v0.9.2";web="$source_dir/frontend/dist"
  fi
fi
[[ -f "$web/index.html" ]] || { echo '缺少 React 构建产物' >&2;exit 1; }
install -d -m 0755 "$target" "$target/web" "$target/rules" "$target/third_party"
install -d -m 0700 "$target/private"
backup="$target/private/deploy-backup-$(date +%Y%m%d-%H%M%S)"
install -d -m 0700 "$backup"
for file in config.json engine.json components.json;do [[ ! -f "$target/$file" ]] || cp -p -- "$target/$file" "$backup/$file";done
[[ ! -f "$target/foxray_v0.9.2" ]] || cp -p -- "$target/foxray_v0.9.2" "$backup/foxray_v0.9.2"
install -m 0755 "$binary" "$target/foxray_v0.9.2.next"
install_component(){
  local component="$1" repo="$2" asset="$3" member="$4" destination="$5" tag="${6:-latest}"
  [[ ! -x "$destination" ]] || { echo "$component：保留已安装版本";return; }
  local metadata="$work/$component.json" archive="$work/$component.asset" url digest expected actual
  local endpoint="https://api.github.com/repos/$repo/releases/latest"
  [[ $tag == latest ]] || endpoint="https://api.github.com/repos/$repo/releases/tags/$tag"
  curl -4 -fsSL --proto '=https' --connect-timeout 10 --max-time 60 "$endpoint" -o "$metadata"
  url="$(jq -er --arg name "$asset" '.assets[]|select(.name==$name)|.browser_download_url' "$metadata")"
  digest="$(jq -er --arg name "$asset" '.assets[]|select(.name==$name)|.digest' "$metadata")"
  [[ $url == "https://github.com/$repo/releases/download/"* && $digest =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "$component 官方资产或 SHA256 元数据无效" >&2;exit 1; }
  curl -4 -fL --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 300 --retry 2 "$url" -o "$archive"
  expected="${digest#sha256:}";actual="$(sha256sum "$archive")";actual="${actual%% *}"
  [[ $expected == "$actual" ]] || { echo "$component SHA256 校验失败" >&2;exit 1; }
  install -d -m 0755 "$(dirname -- "$destination")"
  case "$component" in
    xray) unzip -p "$archive" "$member" > "$destination.next";;
    warpscout) tar -xOf "$archive" "$member" > "$destination.next";;
    *) cp -- "$archive" "$destination.next";;
  esac
  chmod 0755 "$destination.next";mv -f -- "$destination.next" "$destination"
  jq -r '.tag_name' "$metadata" > "$(dirname -- "$destination")/$component-version.txt"
}
case "$(uname -m)" in
  x86_64) xray_asset=Xray-linux-64.zip;arch=amd64;;
  aarch64) xray_asset=Xray-linux-arm64-v8a.zip;arch=arm64;;
  *) echo '自动部署只支持 amd64／arm64' >&2;exit 1;;
esac
if [[ -n $komari_url ]];then
  "$target/foxray_v0.9.2.next" --normalize-komari-url "$komari_url" > "$work/komari-url.json"
  komari_url="$(jq -r .url "$work/komari-url.json")"
fi
if [[ $xray == true ]];then "$target/foxray_v0.9.2.next" --base "$target" --ensure-module xray;fi
if [[ $wireguard == true ]];then "$target/foxray_v0.9.2.next" --base "$target" --ensure-module wireguard;fi
if [[ $warp == true ]];then
  module_args=(--base "$target" --ensure-module warp)
  [[ $accept_terms -ne 1 ]] || module_args+=(--accept-warp-tos)
  "$target/foxray_v0.9.2.next" "${module_args[@]}"
  install_component warpscout vernette/warpscout "warpscout_0.16.1_linux_$arch.tar.gz" warpscout "$target/probe-tools/warpscout" v0.16.1
  id foxray-scout >/dev/null 2>&1 || useradd --system --no-create-home --shell /sbin/nologin foxray-scout
  install -d -m 0700 -o foxray-scout -g foxray-scout /var/lib/foxray-scout
fi
if [[ $komari == true ]];then
  if [[ -z $komari_url && -f "$target/components.json" ]];then komari_url="$(jq -r '.komari_url // empty' "$target/components.json")";fi
  komari_host="${listen%:*}"
  [[ $komari_host != 0.0.0.0 ]] || komari_host=127.0.0.1
  komari_url="${komari_url:-http://$komari_host:25774/}"
  "$target/foxray_v0.9.2.next" --check-komari-url "$komari_url" > "$work/komari-check.json"
  jq -e '.recognized and .reachable' "$work/komari-check.json" >/dev/null || { echo '未识别可用的 Komari，未安装或覆盖任何 Komari 服务。' >&2;exit 1; }
  komari_url="$(jq -r '.url' "$work/komari-check.json")"
fi
warp_terms=false
if [[ -f "$target/components.json" ]];then warp_terms="$(jq -r '.warp_terms_accepted // false' "$target/components.json")";fi
[[ $accept_terms -ne 1 ]] || warp_terms=true
jq -n --argjson terms "$warp_terms" --argjson xray "$xray" --argjson warp "$warp" --argjson wireguard "$wireguard" --argjson komari "$komari" --arg url "$komari_url" '{version:2,xray:$xray,warp:$warp,wireguard:$wireguard,komari:$komari,komari_url:$url,warp_terms_accepted:$terms}' > "$target/components.json.next"
chmod 0600 "$target/components.json.next";mv -- "$target/components.json.next" "$target/components.json"
init_args=(--base "$target" --initialize)
[[ $warp != true ]] || init_args+=(--with-warp)
[[ $komari != true ]] || init_args+=(--with-komari)
"$target/foxray_v0.9.2.next" "${init_args[@]}"
was_active=0
if systemctl is-active --quiet foxray.service;then
  was_active=1
  curl -fsS "http://$listen/api/state" -o "$work/state.json"
  jq -n --slurpfile state "$work/state.json" --arg pid "$(systemctl show foxray-engine -p MainPID --value)" --argjson time "$(date +%s)" '{active:$state[0].state.active,applied:$state[0].state.applied,last_switch:$state[0].state.last_switch,time:$time,engine_pid:$pid}' > "$target/private/go-adoption.json"
  "$target/foxray_v0.9.2.next" --base "$target" --check-migration > "$work/migration.json"
  jq -e '.compatible and .core_validation' "$work/migration.json" >/dev/null || { echo '当前核心配置兼容检查未通过，原管理服务保持运行。' >&2;exit 1; }
fi
install -d -m 0755 /etc/systemd/system/foxray.service.d
cat > /etc/systemd/system/foxray.service.d/90-go-main.conf <<UNIT
[Service]
ExecStart=
ExecStart=/opt/foxray/foxray --base /opt/foxray --ui /opt/foxray/web --listen $listen
ExecStop=
ExecStopPost=
UNIT
if [[ ! -f /etc/systemd/system/foxray.service ]];then
  cat > /etc/systemd/system/foxray.service <<'UNIT'
[Unit]
Description=FoXray Gateway
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
WorkingDirectory=/opt/foxray
Restart=on-failure
RestartSec=3
UMask=0077
[Install]
WantedBy=multi-user.target
UNIT
fi
if [[ $xray == true && ! -f /etc/systemd/system/foxray-engine.service ]];then
cat > /etc/systemd/system/foxray-engine.service <<'UNIT'
[Unit]
Description=FoXray Xray-core
After=network-online.target
ConditionPathExists=/opt/foxray/engine.json
[Service]
User=vpm-engine
Group=vpm-engine
ExecStart=/opt/foxray/xray run -c /opt/foxray/engine.json
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
ExecStopPost=-+/usr/sbin/nft delete table inet vpm
Restart=on-failure
RestartSec=3
[Install]
WantedBy=multi-user.target
UNIT
fi
systemctl daemon-reload
[[ $was_active -ne 1 ]] || systemctl stop foxray.service
mv -f -- "$target/foxray_v0.9.2.next" "$target/foxray_v0.9.2"
ln -sfn foxray_v0.9.2 "$target/foxray"
cp -a -- "$web/." "$target/web/"
[[ ! -d "$source_dir/rules" ]] || cp -a -- "$source_dir/rules/." "$target/rules/"
systemctl enable foxray.service >/dev/null
[[ $xray != true ]] || systemctl enable foxray-engine.service >/dev/null
systemctl start foxray.service
echo "FoXray Gateway 0.9.2 已启动，监听：$listen"
if [[ ${listen%:*} == 0.0.0.0 ]];then
  echo "请通过服务器 IPv4 地址访问：http://<服务器IP>:${listen##*:}"
else
  echo "访问：http://$listen"
fi
if [[ $warp == true && ! -e /run/netns/foxray-warp ]];then echo 'WARP 软件与隔离服务配置已准备；在管理页启动服务后创建独立命名空间，连接隧道前请确认 VPN 排除范围。';fi
