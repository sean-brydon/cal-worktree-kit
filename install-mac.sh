#!/bin/sh
set -eu
# Run with administrator privileges after building proxy.go for this Mac.
source_binary=$1
dest='/Library/Application Support/Tailmux Worktrees'
plist='/Library/LaunchDaemons/io.tailmux.cal-worktrees.plist'
/usr/bin/install -d -o root -g wheel -m 755 "$dest"
/usr/bin/install -o root -g wheel -m 755 "$source_binary" "$dest/proxy"
/bin/cat > "$plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>io.tailmux.cal-worktrees</string>
<key>ProgramArguments</key><array><string>/Library/Application Support/Tailmux Worktrees/proxy</string><string>--laptop</string><string>--listen</string><string>127.0.0.1:80</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>/var/log/tailmux-cal-worktrees.log</string>
<key>StandardErrorPath</key><string>/var/log/tailmux-cal-worktrees.log</string>
</dict></plist>
PLIST
/usr/sbin/chown root:wheel "$plist"
/bin/chmod 644 "$plist"
/bin/launchctl bootout system/io.tailmux.cal-worktrees 2>/dev/null || true
/bin/launchctl bootstrap system "$plist"
