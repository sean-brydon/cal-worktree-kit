# Cal worktree kit

Private, standalone tooling for Cal.com development branches on Linux hosts, with Orca hooks and a macOS/Tailmux client.

**Start with [the setup walkthrough](WALKTHROUGH.md).** Detailed behavior, troubleshooting and original timings are in [OPERATIONS.md](OPERATIONS.md).

Each worktree gets a stable local URL, its own database on the existing PostgreSQL server, cached dependency installation, coloured logs and on-demand Prisma Studio. Optional Tailscale Serve sharing uses the same app instance, with local shortcuts redirecting to HTTPS while shared.

This is a Cal-specific starter kit, not a general-purpose or one-click VPS provisioner. Start with a working Cal checkout, PostgreSQL and Orca. Supported local namespaces are `work` and `personal`, one host per namespace per Mac. The optional cross-tailnet HTTPS bridge supports one shared host per Mac with the default port ranges.

No environment files, authentication tokens, database dumps or deployed state belong in this repository. Keep it private; grant repository access separately to intended collaborators.

## Validation

```sh
python3 -m unittest discover -s . -p 'test_*.py'
go test proxy.go proxy_test.go
node test_ansi.cjs
```

The original host deployment was tested with login, HMR, sharing, Studio and archive/restore. The configurable packaging is covered by automated checks; installation on a fresh teammate machine still requires the walkthrough smoke test. Existing installations are not automatically migrated or updated by this repository.
