"""Sharing changes only the origin/service; failures restore local-only access."""
import ast,fcntl,json,pathlib,re,shutil,subprocess,sys,tempfile,time,unittest,urllib.parse
from unittest.mock import patch

SOURCE=pathlib.Path(__file__).with_name('cal-worktree')
NAMES={'canonical_origin','rewrite_origin','share','save','load','unit'}
functions=ast.Module(body=[n for n in ast.parse(SOURCE.read_text()).body if isinstance(n,ast.FunctionDef) and n.name in NAMES],type_ignores=[])

class SharingTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
  self.base=pathlib.Path(self.tmp.name);self.routes=self.base/'routes';self.routes.mkdir()
  self.work=self.base/'work';(self.work/'apps/web').mkdir(parents=True)
  self.local='http://branch.work.cal.localhost'
  (self.work/'.env').write_text('NEXT_PUBLIC_WEBAPP_URL="'+self.local+'"\nNEXTAUTH_URL="'+self.local+'/api/auth"\nDATABASE_URL="postgresql://private/db"\n')
  (self.work/'apps/web/.env.local').write_text('NEXT_PUBLIC_WEBAPP_URL="'+self.local+'"\n')
  self.key='abcdef123456';self.file=self.routes/(self.key+'.json')
  self.record={'host':'branch.work.cal.localhost','port':3100,'path':str(self.work),'active':True,'database':'calwt_'+self.key}
  self.file.write_text(json.dumps(self.record));self.calls=[];self.published=[]
  self.scope=dict(pathlib=pathlib,shutil=shutil,sys=sys,fcntl=fcntl,json=json,re=re,subprocess=subprocess,time=time,urllib=__import__('urllib'),BASE=self.base,ROUTES=self.routes,CONFIG={'host':'work','tailnet_host':'dev-box.example.ts.net'},ENV={},call=lambda args,**kw:self.calls.append(args),healthy=lambda r:True,publish=lambda r,on:self.published.append(on))
  exec(compile(functions,str(SOURCE),'exec'),self.scope)
  self.status={'BackendState':'Running','Self':{'DNSName':'dev-box.example.ts.net.','TailscaleIPs':['100.100.100.100']}}
  p=patch.object(subprocess,'check_output',side_effect=lambda args,**kw:json.dumps(self.status if args[1]=='status' else {}).encode());p.start();self.addCleanup(p.stop)
 def test_enable_disable_keeps_same_database_and_never_installs(self):
  self.scope['share'](self.key,True)
  r=json.loads(self.file.read_text());self.assertTrue(r['shared'])
  self.assertEqual(r['database'],self.record['database'])
  self.assertIn('https://dev-box.example.ts.net:18443', (self.work/'.env').read_text())
  self.assertIn(r['tailnet_url'],(self.work/'apps/web/.env.local').read_text())
  self.assertEqual(self.calls,[['systemctl','--user',action,'cal-worktree-'+self.key+'.service'] for action in ('stop','restart')])
  self.scope['share'](self.key,False)
  self.assertFalse(json.loads(self.file.read_text())['shared'])
  self.assertIn(self.local,(self.work/'.env').read_text())
  self.assertIn('postgresql://private/db',(self.work/'.env').read_text())
  self.assertEqual(self.published,[True,False])
 def test_website_origin_and_symlinked_override(self):
  with (self.work/'.env').open('a') as f:f.write('NEXT_PUBLIC_WEBSITE_URL="http://localhost:3001"\n')
  outside=self.base/'external-env';outside.write_text('NEXT_PUBLIC_WEBAPP_URL="'+self.local+'"\n')
  override=self.work/'apps/web/.env.local';override.unlink();override.symlink_to(outside)
  self.scope['share'](self.key,True)
  self.assertIn('NEXT_PUBLIC_WEBSITE_URL="https://dev-box.example.ts.net:18443"',(self.work/'.env').read_text())
  self.assertFalse(override.is_symlink());self.assertIn(self.local,outside.read_text())
 def test_origin_only_change_preserves_prepared_state(self):
  self.scope['signature']=lambda work:(work/'.env').read_text()
  self.scope['outputs_present']=lambda work:True
  self.record['ready_signature']=(self.work/'.env').read_text();self.file.write_text(json.dumps(self.record))
  self.scope['share'](self.key,True)
  self.assertEqual(json.loads(self.file.read_text())['ready_signature'],(self.work/'.env').read_text())
 def test_partial_publish_failure_restores_local_and_revokes(self):
  def publish(r,on):
   self.published.append(on)
   if on:raise RuntimeError('second endpoint failed')
  self.scope['publish']=publish
  with patch.object(subprocess,'run'):
   with self.assertRaisesRegex(RuntimeError,'second endpoint failed'):self.scope['share'](self.key,True)
  r=json.loads(self.file.read_text());self.assertFalse(r['shared']);self.assertFalse(r['share_busy'])
  self.assertIn(self.local,(self.work/'.env').read_text());self.assertEqual(self.published,[True,False])
 def test_wrong_tailnet_fails_before_env_or_service_changes(self):
  self.status['Self']['DNSName']='other.ts.net.'
  with self.assertRaisesRegex(RuntimeError,'Unexpected Tailscale host'):self.scope['share'](self.key,True)
  self.assertEqual(self.calls,[]);self.assertIn(self.local,(self.work/'.env').read_text())
 def test_archive_cannot_be_shared(self):
  self.record['active']=False;self.file.write_text(json.dumps(self.record))
  with self.assertRaisesRegex(RuntimeError,'Start this worktree'):self.scope['share'](self.key,True)
  self.assertEqual(self.calls,[])

if __name__=='__main__':unittest.main()
