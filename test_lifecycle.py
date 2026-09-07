import importlib.util,json,pathlib,tempfile,unittest
from unittest.mock import patch
from types import SimpleNamespace

class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.base=pathlib.Path(self.tmp.name)
        (self.base/'routes').mkdir();self.work=self.base/'work';self.work.mkdir()
        # Load the code with a private test home; no real system services are touched.
        home=self.base/'home';configdir=home/'.local/share/cal-worktrees';configdir.mkdir(parents=True)
        (configdir/'config.json').write_text(json.dumps({'root':'/repo','orca':'/test/orca-ide'}))
        with patch('pathlib.Path.home',return_value=home):
            spec=importlib.util.spec_from_file_location('reconcile',pathlib.Path(__file__).with_name('reconcile.py'))
            self.m=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.m)
        self.m.base=self.base
        (self.base/'routes/abc.json').write_text(json.dumps({'path':str(self.work),'root':'/repo','active':True}))
        self.calls=[]
    def tearDown(self):self.tmp.cleanup()
    def tick(self,rows,ok=True):
        def run(args,**kwargs):
            self.calls.append(args)
            if 'worktree' in args:return SimpleNamespace(returncode=0,stdout=json.dumps({'ok':ok,'result':{'worktrees':rows,'truncated':False}}))
            return SimpleNamespace(returncode=1 if 'is-active' in args else 0)
        with patch.object(self.m.subprocess,'run',side_effect=run):self.m.tick()
    def test_archive_then_restore(self):
        self.tick([{'path':'/repo'},{'path':str(self.work),'isArchived':True}])
        self.assertTrue((self.base/'abc.orca-archived').exists())
        self.assertTrue(any('stop' in c for c in self.calls))
        self.calls.clear()
        self.tick([{'path':'/repo'},{'path':str(self.work),'isArchived':False}])
        self.assertTrue(any(c[0]=='systemd-run' for c in self.calls))
        self.assertFalse((self.base/'abc.orca-archived').exists())
    def test_failed_or_incomplete_snapshot_does_not_stop_servers(self):
        self.tick([],False);self.tick([])
        self.assertFalse(any('stop' in c for c in self.calls))
    def test_closing_client_does_not_stop_active_worktree(self):
        self.tick([{'path':'/repo'},{'path':str(self.work),'isArchived':False}])
        self.assertEqual(len(self.calls),1)

if __name__=='__main__':unittest.main()
