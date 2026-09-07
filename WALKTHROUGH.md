# Set up your own Cal development worktrees

Allow roughly **20–40 minutes once your main Cal checkout, Orca and Tailmux already work**. This is a planning estimate, not a measured installer time. Provisioning an empty VPS is additional work. Someone only viewing a shared branch needs no scripts: join the right Tailscale network, have policy access to the shared ports, and open the HTTPS link.

## 1. Prepare the Linux host

Use your ordinary development user, not root. You need:

- A Linux VPS with a working systemd user session and Python 3.10+.
- Git and Go (the proxy uses only the standard library).
- The Node and Yarn versions required by your Cal checkout. The original deployment used Node 24 and Yarn 4.17.1. Yarn must be available as a persistent executable, not just a shell alias.
- A working Cal checkout with dependencies installed, a private `.env`, and a migrated local development PostgreSQL database. The database login needs `CREATEDB`; this kit clones development data. Never point it at production.
- Matching PostgreSQL `pg_dump` and `pg_restore` tools, or a running PostgreSQL Docker container with those tools inside it. Docker access must work for your development user.
- Orca running on this host and the Cal checkout added as a repository. On Linux, use the Orca IDE CLI, often `~/.local/bin/orca-ide`; the unrelated `orca` screen reader is not this CLI.
- At least 6 GiB free for a fresh checkout, plus room for databases and caches. Several simultaneous Cal processes can need substantial RAM.

The kit expects Cal's `packages/prisma/migrations`, Yarn layout and generated outputs. Older or substantially different Cal versions may need adaptations.

Clone this private repository on the host using your own authorized GitHub login:

```sh
git clone git@github.com:sean-brydon/cal-worktree-kit.git
cd cal-worktree-kit
```

The owner must grant you repository access first. Do not copy someone else's GitHub credentials or commit your Cal environment files.

## 2. Install the host services

Set your actual checkout and executable paths. Run this from the kit checkout:

```sh
python3 install-host.py \
  --root "$HOME/work/cal" \
  --namespace work \
  --node "$(command -v node)" \
  --yarn "$(command -v yarn)" \
  --orca "$HOME/.local/bin/orca-ide" \
  --check
```

If checks pass, rerun the same command without `--check`. For PostgreSQL inside Docker, add `--postgres-container YOUR_CONTAINER_NAME` to both commands. The container is expected to expose PostgreSQL internally on port 5432; your `.env` still uses its host-mapped local port.

For optional tailnet sharing, first obtain the host's exact native Tailscale name:

```sh
tailscale status --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["Self"]["DNSName"].rstrip("."))'
```

Add `--tailnet-host YOUR_HOST.YOUR_TAILNET.ts.net` to both install commands. Omit it to leave sharing disabled. This must be the host's native Tailscale connection, not a Tailmux userspace peer. [HTTPS certificates must be enabled](https://tailscale.com/docs/features/tailscale-serve) for the tailnet. Existing Tailscale access policy must allow viewers to reach app ports 18443–18462 and Studio ports 19443–19462.

The sharing helper runs `sudo -n tailscale serve ...`: have the host administrator configure narrowly scoped noninteractive permission for those Serve operations. Do not grant blanket passwordless sudo just for this kit. Without permission, ordinary local worktrees still work; enabling sharing reports an error and rolls back.

Installation writes helpers/config to `~/.local/share/cal-worktrees`, a command link under `~/.local/bin`, and two user services. It enables lingering so the user services can run after logout. If your system requires administrator approval for lingering, ask the host admin to run `loginctl enable-linger YOUR_USERNAME`, then rerun the installer.

The installer refuses a changed existing configuration. It does not migrate an old bespoke installation. Paths containing spaces or systemd special characters are unsupported by this first version.

Check:

```sh
systemctl --user status cal-worktree-proxy cal-worktree-lifecycle
```

## 3. Connect your Mac with Tailmux

Install/configure Tailmux first using its [repository instructions](https://github.com/sean-brydon/Tailmux). Your host should appear in `tailmux status`, and `tailmux ssh work/YOUR_HOST` should work. Substitute your actual Tailmux target in all commands below; the `work` URL namespace is independent of the target's name.

Forward the remote router as raw TCP:

```sh
tailmux forward work/YOUR_HOST 18080:18080 --save cal-worktree-router
```

On your Mac, clone this private kit, build the proxy, and install its loopback port-80 service:

```sh
git clone git@github.com:sean-brydon/cal-worktree-kit.git
cd cal-worktree-kit
go build -o proxy proxy.go
sudo sh install-mac.sh "$PWD/proxy"
```

The installer replaces the `io.tailmux.cal-worktrees` launch daemon if already installed. Port 80 on 127.0.0.1 must be available. If macOS blocks administrator access to the checkout, copy the binary and installer to a temporary directory outside Documents and run them there.

The router recognizes `*.work.cal.localhost`. Browsers resolve `.localhost` to loopback; wildcard DNS is not needed. For a second, personal host, install there with `--namespace personal` and add:

```sh
tailmux forward personal/YOUR_HOST 18081:18080 --save personal-cal-worktree-router
```

This release supports one host per namespace per Mac. Saved forwards reconnect when Tailmux networking starts; the Mac proxy starts at boot.

## 4. Optional: shared HTTPS from a Mac on a different tailnet

Skip this section if your Mac's native Tailscale is already on the same tailnet as the host and can resolve/reach it directly. Colleagues on that tailnet also skip it.

If your Mac stays on another tailnet, use Tailmux to preserve the shared HTTPS URL:

```sh
tailmux forward work/YOUR_HOST 18443-18462 19443-19462 --save cal-worktree-https
sudo sh install-share-dns.sh YOUR_HOST.YOUR_TAILNET.ts.net
```

This maps that exact hostname to 127.0.0.1 on this Mac. The forwards carry TLS unchanged to relays on the host, then to Tailscale Serve. Other services accessed by the same hostname need their own forwards because the hosts override affects all ports. Tailmux's target-based SSH is unaffected.

With default ports, this bridge supports one shared host per Mac. Do not add a second hostname expecting it to route to a second host on the same local ports. Supporting that requires additional address/port configuration not included here.

## 5. Add the Orca hooks

In the **remote Cal repository's Worktree Hooks** settings, paste:

**Setup script**

```sh
"$HOME/.local/bin/cal-worktree" setup
```

**Archive script**

```sh
"$HOME/.local/bin/cal-worktree" archive
```

Choose **Run by default** and enable **Wait for setup to complete before starting agent**. Repeat for each host's Cal repository. These scripts run on the host, not your Mac.

Create a worktree through Orca and let setup finish. It prints its URL, app port, logs, Studio and sharing controls. It also tries to open **Cal logs** and **Cal links** terminals. If Orca reports a running terminal is not discoverable, use the focus command printed by Orca. The web links work independently of terminal discovery.

## 6. Smoke-test the whole installation

Use a disposable development branch created through Orca:

1. Wait for `READY` and open the printed local URL. Sign in using an account in your development database.
2. Open `/__worktree/logs`; verify both setup and running-app output. Open `/__worktree/studio` and confirm it shows this branch's database.
3. If configured, open `/__worktree/share` and enable sharing. Wait for the restart, then copy the actual HTTPS URL. Confirm it opens from an authorized second tailnet device; local success alone does not prove your colleague's access policy permits it.
4. Confirm local app links redirect while sharing, and login/generated booking links use the shared origin. External OAuth integrations may require new callback registrations; some Cal development integrations hardcode localhost and need app changes.
5. Stop sharing and confirm the local origin works again.
6. Archive using Orca. App/Studio should stop and any shared endpoints disappear. Restore the worktree and confirm the same path keeps its URL and database. If a quick archive/restore happens between watcher polls, rerun setup manually.

Sharing exposes **the app, logs and writable Prisma Studio** to tailnet peers allowed by policy. It uses Tailscale Serve, never Funnel; it does not publish to the internet. Visitors share branch data, but have separate login sessions. Changing sharing restarts the existing app; it does not create another instance or database.

Original measurements: fresh setup about 54 seconds; unchanged healthy setup about 0.4 seconds; restarting a prepared checkout about 4 seconds; changing sharing about 18 seconds. These are observations from the original host, not guarantees.

## 7. Everyday use, updates and recovery

Inside an existing worktree on the host:

```sh
~/.local/bin/cal-worktree setup
~/.local/bin/cal-worktree logs
~/.local/bin/cal-worktree links
```

Use `setup --force` to rerun preparation checks. Closing the laptop or Orca leaves running branches up. Archive stops them but retains database state and reserved URLs. Each host has 20 persistent sharing reservations, including archived branches. No automatic database deletion is performed.

For diagnostics:

```sh
~/.local/bin/cal-worktree status
journalctl --user -u cal-worktree-proxy -u cal-worktree-lifecycle -n 80
```

Setup logs live at `~/.local/share/cal-worktrees/<id>-setup.log`. Do not post raw logs without checking them for application secrets.

To update, pull the kit and rerun the same host installer arguments. It preserves matching configuration and route data; router/watcher restart briefly interrupts access. Running apps pick up new runtime behavior on their next restart/setup. Rebuild/reinstall the Mac proxy if its source changed. Keep a copy of the previous kit commit for rollback; running an old installer does not automatically reverse state-format changes.

To stop using the kit, first archive managed worktrees through Orca so their app/Studio services and shared Serve endpoints stop. Remove the hooks, then disable infrastructure:

```sh
# Linux host
systemctl --user disable --now cal-worktree-proxy cal-worktree-lifecycle
```

```sh
# Mac
sudo launchctl bootout system/io.tailmux.cal-worktrees
tailmux unforward cal-worktree-router
# Only if you created these groups:
tailmux unforward personal-cal-worktree-router
tailmux unforward cal-worktree-https
```

Remove only the kit's marked hostname line from `/etc/hosts`, then run `sudo dscacheutil -flushcache` and `sudo killall -HUP mDNSResponder`. Databases and helper state remain for deliberate cleanup; never bulk-drop databases based only on their prefix.
