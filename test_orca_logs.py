import importlib.util,pathlib,tempfile,unittest
from unittest.mock import Mock
spec=importlib.util.spec_from_file_location('orca_logs',pathlib.Path(__file__).with_name('orca-logs.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class LogTerminalTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);m.base=pathlib.Path(self.tmp.name)
 def test_existing_named_tab_is_reused(self):
  m.api=Mock(return_value={'terminals':[{'handle':'a','connected':True,'writable':True}],'visualLayouts':[{'title':'Cal logs','panes':{'handle':'a'}}]})
  m.ensure(pathlib.Path('/work'));self.assertEqual(m.api.call_count,1)
 def test_remembered_terminal_prevents_duplicate_after_title_change(self):
  m.api=Mock(side_effect=[{'terminals':[]},{'terminal':{'handle':'new'}},{'terminals':[{'handle':'new','connected':True,'writable':True,'title':'shell'}]}])
  m.ensure(pathlib.Path('/work'));m.ensure(pathlib.Path('/work'))
  self.assertEqual(sum(c.args[:2]==('terminal','create') for c in m.api.call_args_list),1)
 def test_incomplete_list_does_not_create(self):
  m.api=Mock(return_value={'truncated':True,'terminals':[]})
  with self.assertRaises(RuntimeError):m.ensure(pathlib.Path('/work'))
  self.assertEqual(m.api.call_count,1)
