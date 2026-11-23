import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('manager',Path(__file__).resolve().parents[1]/'scripts/manager.py')
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class ReleaseBoundaryTests(unittest.TestCase):
    def archive(self, folder, name, kind=tarfile.REGTYPE):
        path=Path(folder)/'source.tar.gz'
        with tarfile.open(path,'w:gz') as stream:
            entry=tarfile.TarInfo(name)
            entry.type=kind
            entry.linkname='/etc/passwd' if kind==tarfile.SYMTYPE else ''
            entry.size=2 if kind==tarfile.REGTYPE else 0
            stream.addfile(entry,io.BytesIO(b'ok') if entry.size else None)
        return path

    def test_source_rejects_traversal_links_and_private_files(self):
        for name,kind in [('emby-cloud/../../escaped',tarfile.REGTYPE),('/etc/passwd',tarfile.REGTYPE),
                          ('emby-cloud/runtime/secret',tarfile.REGTYPE),('emby-cloud/.env',tarfile.REGTYPE),
                          ('emby-cloud/linked',tarfile.SYMTYPE)]:
            with self.subTest(name=name),tempfile.TemporaryDirectory() as folder:
                with self.assertRaises(ValueError):
                    m.unpack_source(self.archive(folder,name,kind),Path(folder)/'out')
                self.assertFalse((Path(folder)/'escaped').exists())

    def test_checksum_mismatch_never_leaves_installable_asset(self):
        class Response(io.BytesIO):
            def __enter__(self):return self
            def __exit__(self,*_args):self.close()
        name='manager.py'
        release={'tag_name':'v0.2.0','assets':[{'name':name,'size':3,'digest':'sha256:'+hashlib.sha256(b'good').hexdigest(),
                 'browser_download_url':f'https://github.com/{m.REPO}/releases/download/v0.2.0/{name}'}]}
        with tempfile.TemporaryDirectory() as folder,patch.object(m.urllib.request,'urlopen',return_value=Response(b'bad')):
            target=Path(folder)/name
            with self.assertRaises(ValueError):m.download_asset(release,name,target)
            self.assertFalse(target.exists())

    def test_upstream_asset_cannot_redirect_initial_origin(self):
        release={'tag_name':'v0.2.0','assets':[{'name':'manager.py','size':3,'digest':'sha256:'+'a'*64,
                 'browser_download_url':'https://attacker.invalid/payload'}]}
        with tempfile.TemporaryDirectory() as folder,patch.object(m.urllib.request,'urlopen') as request:
            with self.assertRaises(ValueError):m.download_asset(release,'manager.py',Path(folder)/'file')
            request.assert_not_called()

    def test_latest_check_handles_legacy_prerelease(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/'app';root.mkdir();(root/'VERSION').write_text('0.1.0-alpha.3')
            manager=m.Manager(root,Path(folder)/'state')
            with patch.object(m,'release_info',return_value={'tag_name':'v0.2.0','html_url':'release','published_at':'today'}):
                self.assertTrue(manager.check()['latest']['available'])

    def test_no_parallel_jobs_and_no_stale_version_apply(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/'app';root.mkdir();(root/'VERSION').write_text('0.2.0')
            manager=m.Manager(root,Path(folder)/'state')
            with self.assertRaises(ValueError):manager.start('v0.3.0','0.1.0')
            manager.progress('applying')
            with self.assertRaises(ValueError):manager.start('v0.3.0','0.2.0')

    def test_schema_change_refused_before_application_commands(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/'app';root.mkdir();(root/'VERSION').write_text('0.2.0')
            manager=m.Manager(root,Path(folder)/'state')
            release={'tag_name':'v0.3.0'}
            def download(_release,name,dest):dest.write_text(json.dumps({'version':'0.3.0','protocol':1,'database_schema':2,'rollback_safe':True}))
            with patch.object(m,'release_info',return_value=release),patch.object(m,'download_asset',side_effect=download),patch.object(manager,'command') as command:
                with self.assertRaisesRegex(ValueError,'manual data migration'):manager._install('v0.3.0')
                command.assert_not_called()
            self.assertEqual((root/'VERSION').read_text(),'0.2.0')

    def test_uninstall_rejects_unrelated_directory_before_commands(self):
        with tempfile.TemporaryDirectory() as folder:
            manager=m.Manager(Path(folder)/'app',Path(folder)/'state')
            with patch.object(manager,'command') as command:
                with self.assertRaises(ValueError):manager.uninstall(True,True)
                command.assert_not_called()


if __name__=='__main__':unittest.main()
