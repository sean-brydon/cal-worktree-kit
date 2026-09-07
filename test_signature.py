"""Reuse must be invalidated by changed inputs and missing generated output."""
import ast
import hashlib
import os
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SOURCE=pathlib.Path(__file__).with_name('cal-worktree')
tree=ast.parse(SOURCE.read_text())
functions=ast.Module(body=[n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name in ('signature','outputs_present')],type_ignores=[])

class SignatureTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base=pathlib.Path(self.temp.name)
        self.work=self.base/'work';self.work.mkdir()
        self.script=self.base/'cal-worktree';self.script.write_text('runtime v1')
        (self.base/'database.cjs').write_text('database v1')
        self.diff=b'';self.files=b'';self.envhash=b'env-v1'
        self.scope=dict(hashlib=hashlib,os=os,pathlib=pathlib,subprocess=subprocess,BASE=self.base,SCRIPT=str(self.script),NODE='test-node')
        exec(compile(functions,str(SOURCE),'exec'),self.scope)
        self.patcher=patch.object(subprocess,'check_output',side_effect=self.command)
        self.patcher.start();self.addCleanup(self.patcher.stop)
    def command(self,args):
        if args[0]=='test-node':return b'v24.18.0' if args[1]=='--version' else self.envhash
        if args[3]=='rev-parse':return b'commit-1'
        if args[3]=='diff':return self.diff
        if args[3]=='ls-files':return self.files
        raise AssertionError(args)
    def sig(self):return self.scope['signature'](self.work)
    def test_tracked_edits_and_environment_invalidate_reuse(self):
        initial=self.sig();self.assertEqual(initial,self.sig())
        self.diff=b'changed migration';self.assertNotEqual(initial,self.sig())
        self.diff=b'';self.envhash=b'changed-env';self.assertNotEqual(initial,self.sig())
    def test_untracked_source_and_runtime_updates_invalidate_reuse(self):
        initial=self.sig();p=self.work/'new.ts';p.write_text('new code');self.files=b'new.ts\0'
        self.assertNotEqual(initial,self.sig())
        self.files=b'';self.script.write_text('runtime v2');self.assertNotEqual(initial,self.sig())
    def test_missing_outputs_disable_reuse(self):
        self.assertFalse(self.scope['outputs_present'](self.work))

if __name__=='__main__':unittest.main()
