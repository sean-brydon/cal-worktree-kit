import importlib.util,json,pathlib,tempfile,unittest
from contextlib import ExitStack,redirect_stdout,redirect_stderr
from io import StringIO
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('installer',pathlib.Path(__file__).with_name('install-host.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class InstallerChecks(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
  self.home=pathlib.Path(self.tmp.name);self.root=self.home/'cal'
  (self.root/'packages/prisma/migrations').mkdir(parents=True)
  (self.root/'node_modules').mkdir();(self.root/'.env').write_text('')
  self.exe=self.home/'tool';self.exe.write_text('#!/bin/sh\n');self.exe.chmod(0o755)
 def invoke(self,*extra):
  with ExitStack() as s:
   s.enter_context(patch.object(m.sys,'platform','linux'))
   s.enter_context(patch.object(m.os,'geteuid',return_value=1000))
   s.enter_context(patch.object(m.pathlib.Path,'home',return_value=self.home))
   s.enter_context(patch.object(m.shutil,'which',return_value=str(self.exe)))
   calls=s.enter_context(patch.object(m.subprocess,'run'))
   s.enter_context(patch.object(m.subprocess,'check_output',return_value=json.dumps({'BackendState':'Running','Self':{'DNSName':'box.example.ts.net.'}}).encode()))
   s.enter_context(patch.object(m.sys,'argv',['install-host.py','--root',str(self.root),'--namespace','work','--orca',str(self.exe),'--check',*extra]))
   s.enter_context(redirect_stdout(StringIO()));s.enter_context(redirect_stderr(StringIO()))
   m.main();return calls
 def test_check_does_not_install_or_enable_services(self):
  calls=self.invoke('--tailnet-host','box.example.ts.net')
  self.assertFalse((self.home/'.local').exists())
  self.assertEqual(calls.call_count,1)
  self.assertEqual(calls.call_args.args[0],['systemctl','--user','show-environment'])
 def test_wrong_tailnet_rejected_without_installing(self):
  with self.assertRaises(SystemExit):self.invoke('--tailnet-host','wrong.example.ts.net')
  self.assertFalse((self.home/'.local').exists())
 def test_changed_existing_config_is_preserved(self):
  base=self.home/'.local/share/cal-worktrees';base.mkdir(parents=True)
  config=base/'config.json';config.write_text('{"root":"/other"}')
  with self.assertRaises(SystemExit):self.invoke()
  self.assertEqual(config.read_text(),'{"root":"/other"}')
