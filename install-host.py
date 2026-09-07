#!/usr/bin/env python3
"""Install into the current Linux user's home. Run without sudo."""
import argparse,json,os,pathlib,re,shlex,shutil,subprocess,sys

def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('--root',required=True,type=pathlib.Path)
 p.add_argument('--namespace',required=True,choices=['work','personal'])
 p.add_argument('--node',default=shutil.which('node'))
 p.add_argument('--yarn',default=shutil.which('yarn'))
 p.add_argument('--orca',default=str(pathlib.Path.home()/'.local/bin/orca-ide'))
 p.add_argument('--postgres-container',default='')
 p.add_argument('--tailnet-host',default='',help='Exact native Tailscale DNSName, without trailing dot; omit to disable sharing')
 p.add_argument('--check',action='store_true',help='Validate prerequisites without changing files or services')
 a=p.parse_args();root=a.root.expanduser().resolve()
 if sys.platform!='linux' or os.geteuid()==0:p.error('Run as the development user on Linux, without sudo')
 if any(c.isspace() or c in '%"\\' for c in str(pathlib.Path.home())+str(root)):p.error('Home and checkout paths must not contain whitespace, percent, quotes or backslashes')
 for name in ['git','systemctl','loginctl','go']:
  if not shutil.which(name):p.error('Missing executable: '+name)
 for name in ['node','yarn','orca']:
  value=getattr(a,name)
  if not value or not pathlib.Path(value).is_file() or not os.access(value,os.X_OK):p.error('Provide an executable --'+name+' path')
 if not (root/'.env').is_file() or not (root/'node_modules').is_dir() or not (root/'packages/prisma/migrations').is_dir():p.error('Root must be a prepared Cal checkout with .env, node_modules and migrations')
 for tool in (['docker'] if a.postgres_container else ['pg_dump','pg_restore']):
  if not shutil.which(tool):p.error('Missing '+tool)
 subprocess.run(['systemctl','--user','show-environment'],check=True,stdout=subprocess.DEVNULL)
 if a.tailnet_host:
  if not re.fullmatch(r'[a-z0-9.-]+\.ts\.net',a.tailnet_host):p.error('Expected a full .ts.net hostname')
  status=json.loads(subprocess.check_output(['tailscale','status','--json']))
  if status.get('BackendState')!='Running' or status['Self']['DNSName'].rstrip('.')!=a.tailnet_host:p.error('Native Tailscale does not match --tailnet-host')
 config=dict(host=a.namespace,root=str(root),node=str(pathlib.Path(a.node).absolute()),orca=str(pathlib.Path(a.orca).absolute()),postgres_container=a.postgres_container,tailnet_host=a.tailnet_host)
 base=pathlib.Path.home()/'.local/share/cal-worktrees'
 if (base/'config.json').exists() and json.loads((base/'config.json').read_text())!=config:p.error('Existing configuration differs; migrate it deliberately before installing. No changes made.')
 link=pathlib.Path.home()/'.local/bin/cal-worktree'
 if (link.exists() or link.is_symlink()) and link.resolve()!=base/'cal-worktree':p.error('Existing cal-worktree command points elsewhere; no changes made')
 if a.check:print('Prerequisite checks passed. Database privileges, Serve permissions and Cal startup still require the walkthrough smoke test.');return
 base.mkdir(parents=True,exist_ok=True,mode=0o700)
 source=pathlib.Path(__file__).resolve().parent
 subprocess.run(['go','build','-o',str(base/'proxy.new'),str(source/'proxy.go')],check=True)
 for name in ['cal-worktree','database.cjs','reconcile.py','orca-logs.py','color-logs.py']:
  shutil.copy2(source/name,base/name);(base/name).chmod(0o755)
 (base/'proxy.new').replace(base/'proxy')
 file=base/'config.json';file.write_text(json.dumps(config,indent=2)+'\n');file.chmod(0o600)
 for name in ['routes','bin']:(base/name).mkdir(exist_ok=True,mode=0o700)
 wrapper=base/'bin/yarn';wrapper.write_text('#!/bin/sh\nexec '+shlex.quote(str(pathlib.Path(a.yarn).absolute()))+' "$@"\n');wrapper.chmod(0o755)
 bindir=pathlib.Path.home()/'.local/bin';bindir.mkdir(parents=True,exist_ok=True)
 link=bindir/'cal-worktree'
 if not link.exists():link.symlink_to(base/'cal-worktree')
 units=pathlib.Path.home()/'.config/systemd/user';units.mkdir(parents=True,exist_ok=True)
 commands={'proxy':str(base)+'/proxy --routes '+str(base/'routes'),'lifecycle':sys.executable+' '+str(base/'reconcile.py')}
 for name,command in commands.items():
  (units/('cal-worktree-'+name+'.service')).write_text('[Unit]\nDescription=Cal worktree '+name+'\nAfter=network.target\n[Service]\nExecStart='+command+'\nEnvironment="PATH='+str(pathlib.Path(a.node).parent)+':'+os.environ.get('PATH','/usr/bin:/bin')+'"\nRestart=on-failure\nRestartSec=10\nUMask=0077\n[Install]\nWantedBy=default.target\n')
 subprocess.run(['systemctl','--user','daemon-reload'],check=True)
 subprocess.run(['loginctl','enable-linger',os.environ['USER']],check=True)
 subprocess.run(['systemctl','--user','enable','--now','cal-worktree-proxy','cal-worktree-lifecycle'],check=True)
 subprocess.run(['systemctl','--user','restart','cal-worktree-proxy','cal-worktree-lifecycle'],check=True)
 print('Installed. Add the setup/archive hooks described in WALKTHROUGH.md.')
if __name__=='__main__':main()
