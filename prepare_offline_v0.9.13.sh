#!/usr/bin/env bash
# Prepare the FoXray Gateway v0.9.13 offline directory on a connected Rocky 9 host.
set -Eeuo pipefail
umask 077
if [[ $# -ne 3 || ${1:-} == --help ]]; then
  printf '用法：bash prepare_offline_v0.9.13.sh 运行包.zip SHA256SUMS.txt 新目录\n'
  [[ ${1:-} == --help ]]
  exit
fi
. /etc/os-release
[[ $ID == rocky && $VERSION_ID == 9* && $(uname -m) == x86_64 ]] || { echo '准备机需要 Rocky Linux 9 x86_64。' >&2; exit 1; }
for tool in curl dnf createrepo_c jq unzip sha256sum; do command -v "$tool" >/dev/null || { echo "缺少 $tool" >&2; exit 1; }; done
release=$(realpath -e "$1")
release_name=FoXray-Gateway-v0.9.13-linux-amd64.zip
expected=$(awk -v name="$release_name" '$2 == name {print $1}' "$2")
[[ $expected =~ ^[a-f0-9]{64}$ ]] || { echo '缺少可信 FoXray 发行包摘要。' >&2; exit 1; }
printf '%s  %s\n' "$expected" "$release" | sha256sum --strict -c -
destination=$(realpath -m "$3")
[[ ! -e $destination && ! -L $destination ]] || { echo '请选择一个不存在的输出目录。' >&2; exit 1; }
staging=$(mktemp -d -- "${destination}.partial.XXXXXXXX")
trap 'echo "本次准备中止。未完成的目录：$staging" >&2' ERR
curl_fetch() { curl -4 -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 12 --max-time 300 --retry 3 "$@"; }
get_asset() {
  local repo=$1 ref=$2 filename=$3 metadata url digest tag
  metadata=$(curl_fetch "https://api.github.com/repos/$repo/releases/$ref")
  url=$(jq -er --arg name "$filename" '.assets[] | select(.name == $name) | .browser_download_url' <<< "$metadata")
  digest=$(jq -er --arg name "$filename" '.assets[] | select(.name == $name) | .digest' <<< "$metadata")
  tag=$(jq -er '.tag_name' <<< "$metadata")
  [[ $url == "https://github.com/$repo/releases/download/"* && $digest =~ ^sha256:[a-f0-9]{64}$ ]]
  curl_fetch "$url" -o "$staging/$filename"
  printf '%s  %s\n' "${digest#sha256:}" "$staging/$filename" | sha256sum --strict -c -
  printf '%s %s %s %s\n' "$repo" "$tag" "$filename" "$digest" >> "$staging/UPSTREAM-ASSETS.txt"
}
cp -- "$release" "$staging/$release_name"
mkdir "$staging/foxray-0.9.13"
unzip -q "$release" -d "$staging/foxray-0.9.13"
get_asset XTLS/Xray-core latest Xray-linux-64.zip
get_asset vernette/warpscout tags/v0.16.1 warpscout_0.16.1_linux_amd64.tar.gz
curl_fetch https://pkg.cloudflareclient.com/pubkey.gpg -o "$staging/cloudflare-warp.gpg"
curl_fetch https://dl.fedoraproject.org/pub/epel/RPM-GPG-KEY-EPEL-9 -o "$staging/RPM-GPG-KEY-EPEL-9"
dnf_cmd=(dnf)
(( EUID == 0 )) || dnf_cmd=(sudo dnf)
"${dnf_cmd[@]}" -y --setopt=ip_resolve=4 --setopt=install_weak_deps=False \
  --repofrompath=foxray-warp,https://pkg.cloudflareclient.com/rpm/9 \
  --repofrompath=foxray-epel,https://dl.fedoraproject.org/pub/epel/9/Everything/x86_64/ \
  --disablerepo='*' --enablerepo=baseos,appstream,extras,crb,foxray-warp,foxray-epel \
  download --resolve --alldeps --arch=x86_64,noarch --destdir="$staging" \
  curl jq ca-certificates nftables iproute iputils util-linux unzip tar wireguard-tools cloudflare-warp
createrepo_c --quiet "$staging"
printf 'rocky:9:x86_64:0.9.13\n' > "$staging/PLATFORM"
(cd "$staging" && find . -type f ! -name OFFLINE-SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > OFFLINE-SHA256SUMS)
mv -T --no-clobber -- "$staging" "$destination"
[[ ! -e $staging ]] || { echo '输出目录已存在，保留准备结果。' >&2; exit 1; }
printf '离线目录：%s\n' "$destination"
