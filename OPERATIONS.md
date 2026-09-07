# Operation and original deployment measurements

These notes describe the implementation and measurements from its original deployment.
The portable installer has automated checks but has not yet been exercised end to end on a new machine.
Use [WALKTHROUGH.md](WALKTHROUGH.md) for installation. Example hostnames are placeholders.

## URLs and lifecycle

A URL looks like `http://branch-name-a1b2c3.work.cal.localhost` or
`http://branch-name-a1b2c3.personal.cal.localhost`. The short suffix prevents collisions;
it and the port remain stable when rerunning setup or restoring the same worktree path.
Browsers resolve `.localhost` locally; no wildcard DNS records or `/etc/hosts` changes are needed.
Some non-browser tools need `--resolve HOST:80:127.0.0.1`.

The Mac's loopback-only launch daemon listens on port 80 and forwards to saved Tailmux
TCP tunnels: port 18080 for work and 18081 for personal. Each host has a loopback-only
router on 18080 that reads its local worktree registry and forwards to ports 3100–4099.
HTTP cookies, redirects, host names and WebSocket upgrades pass through unchanged.
Start Tailmux networking after a Mac reboot; saved tunnels reconnect when its daemon starts.
The Mac proxy itself starts at boot.

Setup copies `.env` and `.env.appStore` if absent, converts a shared `.env` symlink to a
private file, adjusts web/auth URLs, assigns a port, creates an isolated development database,
installs dependencies using the main checkout's download cache and a host-wide Yarn
content-addressed hard-link store, applies pending migrations and seeds
once when no compatible database snapshot is available. It waits for the login endpoint before printing `READY: URL`.
Existing per-worktree environment settings are retained except the managed routing/database keys.
The main checkout must have working dependencies and local development DB credentials.
Fresh worktrees require at least 6 GiB free disk space.

A separate database is necessary: Cal's migrations explicitly name `public` views, tables
and enums. Setting only `?schema=...` would not isolate them. No new PostgreSQL server is started.
Compatible new databases copy the main development database, including its data and migration history.
Older or divergent branches fall back to a fresh database and development seed.

Closing the Orca window or laptop does not stop a running service. Active worktree services
are enabled in systemd and restart after a host reboot. Archive cancels pending setup, stops
and disables the service, and removes the live route. It retains the database, environment,
URL and port. Re-running setup is safe and restores those settings.

The lifecycle service polls Orca's CLI every 15 seconds. It observes archive/removal and
starts setup when a previously observed archived worktree reappears as active. It leaves
services alone if Orca is unavailable or returns an incomplete snapshot. A very rapid
archive/unarchive between polls may not be observed; rerun the setup command in that case.
Failed automatic setup is left failed for diagnosis, not retried indefinitely.
Changing a worktree's filesystem path creates a new identity/database/URL.

## Inspect and recover

```sh
~/.local/bin/cal-worktree status
systemctl --user status cal-worktree-proxy cal-worktree-lifecycle
journalctl --user -u 'cal-worktree-*' -n 80
```

Per-worktree setup logs: `~/.local/share/cal-worktrees/<id>-setup.log`.
Registry records contain host, port, path and database name, not passwords.
Passwords remain in private `.env` files. Archive does not delete databases; remove an
unused database only after confirming its associated checkout is no longer needed.

To disable the local Mac proxy:

```sh
sudo launchctl bootout system/io.tailmux.cal-worktrees
```

Host routers/lifecycle watcher can be disabled with:

```sh
systemctl --user disable --now cal-worktree-proxy cal-worktree-lifecycle
```

## Validation

Verified on both hosts: fresh setup, migrations and seed; archive (route becomes 404);
repeat setup (same URL/port/database); authenticated login and event-types page through
port-free URLs; Next.js HMR WebSocket upgrade (101); loopback-only app bindings and
systemd boot enablement. Lifecycle unit tests cover archive/restore snapshots, client
closure, and failed/incomplete snapshots. A physical host reboot was not performed.

`proxy.go` builds with Go's standard library. `test_lifecycle.py` uses Python's standard library.
`install-host.py` installs the host units; `install-mac.sh` installs the root-owned loopback
proxy on macOS. Build the matching proxy binary before running either installer.

## Dependency sharing

Hooks set `YARN_NM_MODE=hardlinks-global` and keep the installed-file store in
`~/.local/share/cal-worktrees/yarn-global`. Each worktree retains its own dependency
layout and workspace links; it does not symlink the whole node_modules directory to
the main checkout. Identical package files share disk blocks through Yarn-managed
hard links. Use Yarn patches rather than editing installed package files in place.
The first install populates the store; later installs reuse it. No shared-store
configuration is committed to Cal.com or imposed on unrelated projects.
Setup now prints elapsed times for installation, migrations and seeding.

A September 7 benchmark on the work host measured 58.7s for the first install
populating the store and 25.8s for a second fresh checkout of the same commit.
Across those two node_modules trees, the second added 273 MiB of unique disk
usage; identical package files had matching inodes. Migrations took about 7s
and fresh seeding about 41s. Timings vary with the branch and host load. Both
checkouts served the login endpoint before the temporary worktrees were removed.

## Database snapshots

The helper takes a consistent `pg_dump` snapshot of the main checkout's local development
DB into a reusable, offline template. It keeps the source running; it never terminates
source sessions. New databases clone the template and `prisma migrate deploy` applies
only pending migrations. Existing worktree databases are never replaced.

The template key includes the source database identity and applied migration checksums.
A changed migration baseline automatically builds another template. Each applied migration
must exist with the same checksum in the new worktree, otherwise setup uses a fresh database.
An unfinished source migration also forces the fresh path. Builds and clones are serialized;
failed or interrupted template restores cannot be used as published templates.

Data-only changes in main do not refresh the snapshot automatically. To include those in
**future** worktrees, run on the relevant host:

```sh
date -u +%s > ~/.local/share/cal-worktrees/snapshot-generation
```

The next compatible setup builds a new template. Existing databases retain their own data.
Old offline templates remain available; they use disk space and are named `caltpl_*`.
Temporary dump files are private and removed after restoration.
Select native PostgreSQL tools or a Docker container with the installer options.

September 7 validation: work's initial template build plus clone took 2.7s; a cached
clone took 0.18s. Two clones each had the source's migration and user counts; writing a
probe table in one did not affect the other or main. Personal correctly rejected a source
migration missing from its checkout and used the fresh path.

A full fresh worktree then measured database preparation 0.2s, dependency installation
26.0s, and pending migrations 2.3s, with no seed step. Login, authenticated session and
event-types returned successfully through its port-free URL. App compilation/startup
is additional time. The temporary worktree was removed through Orca with archive hooks.

## Live logs

Append `/__worktree/logs` to any registered worktree URL. The host proxy serves this
page independently of Next.js, including during setup, failures and after archive.
Choose **Running app** or **Setup**, pause/resume, and toggle automatic scrolling.
The page polls every two seconds while visible. Runtime output is the latest 200
journal entries, bounded to 128 KiB; setup output is the last 128 KiB of the setup log.
It is available through the existing loopback/Tailmux route, and through HTTPS when work-tailnet sharing is enabled.

In an Orca terminal **inside the worktree**, run:

```sh
~/.local/bin/cal-worktree logs
```

For setup output instead:

```sh
~/.local/bin/cal-worktree setup-logs
```

These follow output until Ctrl+C; stopping the viewer does not stop the app.
The same command can be saved as an Orca terminal command titled “Cal logs”.

## Reusing a prepared worktree

Successful setup records a fingerprint of the commit, tracked edits, untracked
non-ignored source files, parsed environment settings, Node version and helper scripts.
Dependency and generated output sentinels must also exist. Unchanged worktrees skip
installation and migration deployment, after confirming their database still exists.
A healthy running app is kept running. Stopped, unchanged worktrees with the standard
Cal dev command start Next directly, retaining their own Next.js filesystem cache and
already-prepared assets. Source/configuration changes use the normal startup command.
No Next.js cache or generated output is shared between different worktree URLs.

To force dependency and migration checks after manual changes to generated files or
database state, run `~/.local/bin/cal-worktree setup --force`.
Setup prints database, install, migration, app readiness and total timings; stage timings
are also appended to the setup log. All readiness timings include a successful login-page
response, not just an open TCP port.

Work VPS measurements on September 7:
- Final fresh worktree: 53.4s end to end (26.0s install, 2.3s migrations, 24.4s startup/first page); earlier baseline 55.2s.
- Re-running setup on a healthy, unchanged worktree: 0.4s, preserving the running process.
- Restarting a stopped, unchanged worktree: 4.2s total (3.9s startup + first page).

The fresh install includes about 19s of linking/third-party build work and 5s of project
post-install generation. Fresh application startup and first-page work adds about 24–26s.
These are measured work-host results, not guarantees for the personal host or every branch.

Validation includes log-page controls in a browser, archived logs on both hosts, an Orca
terminal following the journal, signature invalidation tests, and login/event-types/HMR
after the fast restart. Helpers and proxy tests do not require Cal application changes.

Log colours: setup and future app starts force ANSI output. The terminal viewer preserves
ANSI and highlights plain error/warning/success/info lines. The web viewer renders ANSI
(including 256-colour and RGB sequences) as styled text, with the same severity fallback
for older plain logs. Refresh an existing web tab or rerun the terminal logs command after
upgrading. Already-running app processes pick up forced colour on their next restart.

## Prisma Studio

Open `/__worktree/studio` on the worktree URL. It redirects to
`http://studio.<worktree-host>/`, giving this Prisma version its required root path
without conflicting with Cal's assets or API. The existing Mac and host proxies route it.
Studio starts on the first visit, binds only to 127.0.0.1, and uses the worktree's own
DATABASE_URL/config/schema. Its reserved port is recorded as `studio_port` (6100–7099).
It has no startup cost until opened. The `cal-studio-<key>` user service is stopped by
archive and is also PartOf the corresponding app service; it is not enabled at boot.
Archived worktrees cannot start Studio through the proxy. Work-tailnet sharing also publishes Studio when enabled; otherwise it stays local/Tailmux-only.

Verified on the work VPS: redirect, Studio HTML/assets, database model counts in browser,
matching worktree database credentials (without printing them), loopback binding, and
systemd lifecycle dependency. The original deployment was tested on both hosts. Setup also prints the Studio
shortcut and app port alongside the existing app/log URLs.

Successful setup now opens a **Cal logs** Orca terminal automatically. It reuses an
existing connected tab (including its remembered handle if the shell changed the title)
and refuses to create when Orca returns an incomplete terminal list. The viewer prints
app URL/port, browser logs and Studio links before following app output. Orca failures
are reported without failing the already-running application. Setup hooks remain unchanged.

## Share with the work tailnet

On a running work worktree, open **`/__worktree/share`** on its local URL. Use
**Share with tailnet**, **Copy link**, and **Stop sharing**. Setup and the Cal links
terminal print the control URL. Only the local/Tailmux entry point can change sharing;
the shared page is read-only. The worktree must be running to change this setting.

Sharing uses **one existing app process and the same branch database**. It changes
Cal's managed web/auth origins and briefly restarts the app; it does not install
packages, run migrations, or copy the database. The local app shortcut becomes a
non-cacheable temporary redirect to the shared HTTPS origin while enabled. Local
sharing controls and logs stay available. Stopping sharing restores the local origin.
Login sessions can need a fresh sign-in after changing origins. Multiple visitors
have independent browser sessions but operate on the same branch data.

Shared addresses use the work VPS's existing HTTPS name, for example:

```
App:    https://dev-box.example.ts.net:18443
Logs:   https://dev-box.example.ts.net:18443/__worktree/logs
Studio: https://dev-box.example.ts.net:18443/__worktree/studio
```

Studio redirects to its own root on port 19443 and starts on demand. Both app and
Studio are reachable by work-tailnet peers permitted by existing Tailscale policy.
This includes Studio database writes. **Tailscale Serve is used; Funnel is never used.**
No public-internet endpoint or broad ACL change is created. Tailscale handles the
HTTPS certificate. No custom domain or port-free branch domain is configured.
OAuth integrations still require registration of each shared callback URL with the
provider; this helper cannot update Google/GitHub/etc. application registrations.
Some Cal integrations explicitly hardcode localhost callbacks in development and
need an app change before they can be exercised through the shared origin.

Each reserved worktree has its own pair of ports (18443–18462 and 19443–19462).
NextAuth cookies are namespaced at the shared proxy so separate branches on the same
hostname cannot reuse one another's session, CSRF, or callback cookies. App HTTP
traffic reaches loopback ingress 18082; local controls use 18080. Shared ingress
cannot reach local-only controls even with a forged Host/Origin header.

On this Mac, the work hostname maps to 127.0.0.1 and the saved Tailmux group
`cal-worktree-https` forwards both port ranges. VPS loopback TLS relays send the
unchanged encrypted bytes to Tailscale Serve on the VPS's work-tailnet IP. Thus the
Mac can remain on the personal tailnet while using work HTTPS links. Other work
colleagues use ordinary MagicDNS and do not need this hosts entry or Tailmux.
The Mac override is for this development hostname; other services on that hostname
need their own forwards. Existing `tailmux ssh the work host` access is unchanged.

To install the Mac DNS mapping after configuring the forwards, run
`install-share-dns.sh HOSTNAME.ts.net` with administrator privileges. To undo it, remove the single
`# Tailmux Cal worktree sharing` line from `/etc/hosts`, flush the DNS cache, and run
`tailmux unforward cal-worktree-https`. Do not remove other hosts entries or forwards.

Archive removes the Serve endpoints and stops app/Studio. The sharing preference
and reserved URLs are retained for reopening; setup republishes them after the app
is ready. The 20 reserved slots are persistent, including archived worktrees. An
administrator can reclaim an unused reservation after confirming its worktree will
not be restored. Failed sharing changes revoke the shared routes and restore the
local origin; private proxy journal output contains the failure detail.

September 7 sharing validation: HTTPS certificate verification through the Mac's
Tailmux route, browser login with the development account, authenticated event-types,
Studio schema/model counts, coloured logs, and Next's current `/_next/hmr` WebSocket
(101). Shared ingress rejects local management access; foreign dev-resource origins
are rejected; cookie tests cover cross-branch isolation and chunked session cookies.
Turning sharing off/on took 18.2/18.3 seconds on the test branch. Archive removed
both HTTPS endpoints, and reopening restored the same URLs and database. Origin
changes clear Next's generated fetch-data cache because it can contain booking URLs;
compiled code and dependency caches remain. Browser-facing localhost:3001 settings,
including website and embed URLs, are mapped to the selected branch origin.
