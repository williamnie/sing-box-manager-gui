#!/usr/bin/env bash
# 初始化专用 Linux systemd 服务；不启用网关，不修改路由、nftables、DHCP 或 sysctl。
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

usage() {
  cat <<'USAGE'
用法: scripts/install-gateway.sh /绝对路径/sbm /绝对路径/sbm-gateway --install [--lan-http 局域网IP [--allow-network 网段]]
先构建两个 Linux 二进制，再显式执行初始化。已有策略和数据会保留。
脚本不启动/启用任何服务；安装后请核对 /etc/sbm-gateway/policy.json。

  --lan-http IP         管理界面以明文 HTTP 监听该局域网地址，局域网内的浏览器和 Agent 电脑可直接访问
                        （适合内网部署；首次设置密码仍需在本机或 SSH 隧道完成）
  --allow-network CIDR  允许访问管理界面的来源网段，默认取该 IP 所在网卡的网段
USAGE
}
if [[ $# -lt 3 || ${3:-} != --install ]]; then usage; exit 2; fi
SBM_SOURCE=$1 GATEWAY_SOURCE=$2
shift 3
LAN_IP='' LAN_NETWORK=''
while [[ $# -gt 0 ]]; do
  case $1 in
    --lan-http) [[ $# -ge 2 ]] || { usage; exit 2; }; LAN_IP=$2; shift 2 ;;
    --allow-network) [[ $# -ge 2 ]] || { usage; exit 2; }; LAN_NETWORK=$2; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done
[[ -z $LAN_NETWORK || -n $LAN_IP ]] || { echo '--allow-network 需要和 --lan-http 一起使用' >&2; exit 2; }
[[ $(uname -s) == Linux && $EUID == 0 ]] || { echo '需要 Linux root' >&2; exit 1; }
for source in "$SBM_SOURCE" "$GATEWAY_SOURCE"; do
  [[ $source == /* && -f $source && -x $source ]] || { echo '需要绝对路径的可执行二进制' >&2; exit 1; }
done
for tool in ip ss nft sysctl systemctl install getent; do
  command -v "$tool" >/dev/null || { echo "缺少依赖: $tool" >&2; exit 1; }
done
MANAGER_LISTEN='-listen 127.0.0.1 -port 9090'
if [[ -n $LAN_IP ]]; then
  # 只接受本机网卡上的 IPv4 地址，并按该网卡前缀推出默认来源网段。
  prefix=$(ip -o -4 addr show | awk -v ip="$LAN_IP" '{ split($4, a, "/"); if (a[1] == ip) { print a[2]; exit } }')
  [[ -n $prefix ]] || { echo "--lan-http 需要本机网卡上的 IPv4 地址: $LAN_IP" >&2; exit 1; }
  if [[ -z $LAN_NETWORK ]]; then
    IFS=. read -r o1 o2 o3 o4 <<<"$LAN_IP"
    value=$(( (o1 << 24) | (o2 << 16) | (o3 << 8) | o4 ))
    mask=$(( prefix == 0 ? 0 : (0xFFFFFFFF << (32 - prefix)) & 0xFFFFFFFF ))
    value=$(( value & mask ))
    LAN_NETWORK="$(( value >> 24 & 255 )).$(( value >> 16 & 255 )).$(( value >> 8 & 255 )).$(( value & 255 ))/$prefix"
  fi
  [[ $LAN_NETWORK =~ ^[0-9a-fA-F.:]+/[0-9]{1,3}(,[0-9a-fA-F.:]+/[0-9]{1,3})*$ ]] || { echo "--allow-network 网段格式无效: $LAN_NETWORK" >&2; exit 2; }
  MANAGER_LISTEN="-listen $LAN_IP -port 9090 -allow-insecure-http -allow-network $LAN_NETWORK"
fi
# 防止覆盖非本项目的系统服务；已有单机服务须由操作者先核对迁移。
for unit in singbox-manager.service sbm-gateway.service sbm-gateway-dhcp.service; do
  if [[ -e /etc/systemd/system/$unit ]] && ! grep -q '^# Managed by singbox-manager gateway installer$' "/etc/systemd/system/$unit"; then
    echo "已有非本安装器管理的服务: $unit；请先核对、备份并迁移" >&2; exit 1
  fi
done
if ! getent group singbox-manager >/dev/null; then groupadd --system singbox-manager; fi
if ! id singbox-manager >/dev/null 2>&1; then
  useradd --system --gid singbox-manager --home-dir /var/lib/singbox-manager --shell /usr/sbin/nologin singbox-manager
fi
install -d -o root -g root -m 0755 /usr/local/libexec/singbox-manager
install -o root -g root -m 0755 "$SBM_SOURCE" /usr/local/libexec/singbox-manager/sbm
install -o root -g root -m 0755 "$GATEWAY_SOURCE" /usr/local/libexec/singbox-manager/sbm-gateway
install -d -o root -g root -m 0700 /etc/sbm-gateway /var/lib/sbm-gateway
install -d -o singbox-manager -g singbox-manager -m 0700 /var/lib/singbox-manager /var/lib/sbm-gateway-dhcp
if [[ ! -e /etc/sbm-gateway/policy.json ]]; then
  (umask 077; cat > /etc/sbm-gateway/policy.json <<'POLICY'
{
  "allow_gateway": false,
  "allow_dns_bypass": false,
  "allow_routed_traffic": false,
  "allowed_lan_interfaces": [],
  "allowed_uplink_interfaces": [],
  "allowed_lan_cidrs": [],
  "allow_dhcp": false,
  "allow_nat": false,
  "managed_executable": "/var/lib/singbox-manager/bin/sing-box",
  "managed_config": "/var/lib/singbox-manager/generated/config.json",
  "managed_data_dir": "/var/lib/singbox-manager"
}
POLICY
  )
fi
if [[ ! -e /var/lib/sbm-gateway/dnsmasq.conf ]]; then
  (umask 077; printf 'port=0\n' > /var/lib/sbm-gateway/dnsmasq.conf)
fi
cat > /etc/systemd/system/sbm-gateway.service <<'UNIT'
# Managed by singbox-manager gateway installer
[Unit]
Description=SingBox Manager gateway privilege helper
After=network.target

[Service]
Type=simple
User=root
Group=singbox-manager
ExecStart=/usr/local/libexec/singbox-manager/sbm-gateway
RuntimeDirectory=sbm-gateway
RuntimeDirectoryMode=0750
UMask=0077
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ProtectControlGroups=true
ProtectKernelModules=true
RestrictSUIDSGID=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
CapabilityBoundingSet=CAP_NET_ADMIN CAP_DAC_READ_SEARCH CAP_SYS_PTRACE
ReadWritePaths=/run/sbm-gateway /var/lib/sbm-gateway /proc/sys/net

[Install]
WantedBy=multi-user.target
UNIT
cat > /etc/systemd/system/singbox-manager.service <<UNIT
# Managed by singbox-manager gateway installer
[Unit]
Description=SingBox Manager
After=network-online.target sbm-gateway.service
Wants=network-online.target sbm-gateway.service

[Service]
Type=simple
User=singbox-manager
Group=singbox-manager
KillMode=process
WorkingDirectory=/var/lib/singbox-manager
ExecStart=/usr/local/libexec/singbox-manager/sbm -data /var/lib/singbox-manager $MANAGER_LISTEN
Environment=HOME=/var/lib/singbox-manager
UMask=0077
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true
ProtectKernelModules=true
RestrictSUIDSGID=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
ReadWritePaths=/var/lib/singbox-manager

[Install]
WantedBy=multi-user.target
UNIT
# 可选 dnsmasq 仅处理 DHCP；按其官方方式由 root 初始化后降权为专用账户。
DNSMASQ=$(command -v dnsmasq || true)
DNSMASQ=${DNSMASQ:-/usr/sbin/dnsmasq}
cat > /etc/systemd/system/sbm-gateway-dhcp.service <<UNIT
# Managed by singbox-manager gateway installer
[Unit]
Description=SingBox Manager optional DHCP service
After=network-online.target sbm-gateway.service

[Service]
Type=simple
User=root
Group=singbox-manager
ExecStart=$DNSMASQ --keep-in-foreground --user=singbox-manager --group=singbox-manager --conf-file=/var/lib/sbm-gateway/dnsmasq.conf --pid-file= --dhcp-leasefile=/var/lib/sbm-gateway-dhcp/leases --log-facility=-
UMask=0077
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true
ProtectKernelModules=true
RestrictSUIDSGID=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK AF_PACKET
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID CAP_CHOWN CAP_DAC_OVERRIDE
ReadWritePaths=/var/lib/sbm-gateway-dhcp
UNIT
systemctl daemon-reload
cat <<'DONE'
初始化完成，所有服务保持原有启动状态，未自动启动新服务。
1. 检查策略允许的接口和网段，DNS 分流另需 allow_dns_bypass；完整网关按需要独立允许 NAT/DHCP。
2. 初始化时可执行: systemctl enable --now sbm-gateway.service singbox-manager.service
3. 使用 SSH 隧道访问本地管理界面，完成首次设置密码、网关预览和检查。
4. 此后网关的检查、应用、状态与恢复由管理界面执行；不要并行运行旧 TProxy。
DONE
if [[ -n $LAN_IP ]]; then
  cat <<DONE
5. 管理界面已配置为局域网明文 HTTP：http://$LAN_IP:9090（允许来源 $LAN_NETWORK）。
   首次设置密码后，局域网内的浏览器可直接登录；在「Agent 接入」页复制一键命令到 Agent 电脑执行即可接入 Agent。
DONE
else
  echo "提示：需要局域网直接访问管理界面或接入 Agent 时，重新执行本脚本并加上 --lan-http <本机局域网IP>。"
fi
