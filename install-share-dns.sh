#!/bin/sh
set -eu
host=${1:?Usage: sudo sh install-share-dns.sh HOSTNAME.ts.net}
case "$host" in *[!a-z0-9.-]*|.*|*..*) echo 'Invalid hostname' >&2; exit 1;; esac
case "$host" in *.ts.net) ;; *) echo 'Expected .ts.net hostname' >&2; exit 1;; esac
entry="127.0.0.1 $host # Tailmux Cal worktree sharing"
if grep -Fqx "$entry" /etc/hosts; then echo 'Already configured'; exit 0; fi
if grep -Fq "$host" /etc/hosts; then echo 'Hostname already appears in /etc/hosts; review that entry first' >&2; exit 1; fi
printf '\n%s\n' "$entry" >> /etc/hosts
dscacheutil -flushcache
killall -HUP mDNSResponder 2>/dev/null || true
