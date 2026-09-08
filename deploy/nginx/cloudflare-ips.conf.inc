# Cloudflare's published edge IP ranges — https://www.cloudflare.com/ips/.
#
# Included at server level in every retratar server block (deploy/nginx/*.conf)
# so the origin only accepts connections that actually came through Cloudflare's
# proxy. Proxying alone (decision 0004) hides the origin from casual discovery
# but does not firewall it — anyone who finds the Pi's public IP could hit
# nginx directly, bypassing Cloudflare's WAF, rate limiting and CSAM scanning
# entirely. This closes that gap.
#
# Cloudflare updates this list occasionally (new PoPs, retired ranges) — check
# https://www.cloudflare.com/ips-v4 and /ips-v6 every so often and copy any
# changes in here by hand. There is no automation for it; a stale list fails
# safe (a legitimate Cloudflare request from a brand-new range gets a 403,
# not the origin silently opening up), so there is no urgency, but it should
# not go unreviewed for years either.
#
# Last checked: 2026-09-08.

allow 173.245.48.0/20;
allow 103.21.244.0/22;
allow 103.22.200.0/22;
allow 103.31.4.0/22;
allow 141.101.64.0/18;
allow 108.162.192.0/18;
allow 190.93.240.0/20;
allow 188.114.96.0/20;
allow 197.234.240.0/22;
allow 198.41.128.0/17;
allow 162.158.0.0/15;
allow 104.16.0.0/13;
allow 104.24.0.0/14;
allow 172.64.0.0/13;
allow 131.0.72.0/22;
allow 2400:cb00::/32;
allow 2606:4700::/32;
allow 2803:f800::/32;
allow 2405:b500::/32;
allow 2405:8100::/32;
allow 2a06:98c0::/29;
allow 2c0f:f248::/32;
deny all;
