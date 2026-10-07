#!/usr/bin/env bash
# 初始化专用 Linux systemd 服务；不启用网关，不修改路由、nftables、DHCP 或 sysctl。
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin

usage() {
  cat <<'USAGE'
用法: scripts/install-gateway.sh /绝对路径/sbm /绝对路径/sbm-gateway --install
先构建两个 Linux 二进制，再显式执行初始化。已有策略和数据会保留。
脚本不启动/启用任何服务；安装后请核对 /etc/sbm-gateway/policy.json。
USAGE
}
if [[ $# != 3 || ${3:-} != --install ]]; then usage; exit 2; fi
[[ $(uname -s) == Linux && $EUID == 0 ]] || { echo '需要 Linux root' >&2; exit 1; }
for source in "$1" "$2"; do
  [[ $source == /* && -f $source && -x $source ]] || { echo '需要绝对路径的可执行二进制' >&2; exit 1; }
done
for tool in ip ss nft sysctl systemctl install getent; do
  command -v "$tool" >/dev/null || { echo "缺少依赖: $tool" >&2; exit 1; }
done
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
install -o root -g root -m 0755 "$1" /usr/local/libexec/singbox-manager/sbm
install -o root -g root -m 0755 "$2" /usr/local/libexec/singbox-manager/sbm-gateway
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
cat > /etc/systemd/system/singbox-manager.service <<'UNIT'
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
ExecStart=/usr/local/libexec/singbox-manager/sbm -data /var/lib/singbox-manager -listen 127.0.0.1 -port 9090
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
3. 使用 SSH 隧道访问本地管理界面，完成登录、网关预览和检查。
4. 此后网关的检查、应用、状态与恢复由管理界面执行；不要并行运行旧 TProxy。
DONE
