import hashlib
import io
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from test_manager import m


class PrivateReleaseTests(unittest.TestCase):
    def test_token_is_write_only_revision_guarded_and_clearable(self):
        with tempfile.TemporaryDirectory() as folder:
            manager = m.Manager(Path(folder)/'app', Path(folder)/'state')
            token = 'github_pat_' + 'a'*40
            status = manager.configure_credentials({'token': token, 'clear': False, 'expected_revision': 0})
            self.assertNotIn(token, str(status))
            self.assertEqual(status['credentials'], {'configured': True, 'revision': 1})
            with self.assertRaises(ValueError):
                manager.configure_credentials({'token': '', 'clear': True, 'expected_revision': 0})
            self.assertEqual(manager.release_credentials()['token'], token)
            manager.configure_credentials({'token': '', 'clear': True, 'expected_revision': 1})
            self.assertEqual(manager.credentials_status(), {'configured': False, 'revision': 2})

    def test_redirect_removes_authorization(self):
        requests = []
        class Opener:
            def open(self, request, **kwargs):
                requests.append(request)
                if len(requests) == 1:
                    raise m.urllib.error.HTTPError(request.full_url, 302, 'Found',
                        {'Location': 'https://release-assets.githubusercontent.com/verified/file'}, io.BytesIO())
                return io.BytesIO(b'asset')
        with patch.object(m.urllib.request, 'build_opener', return_value=Opener()):
            with m.github_open(f'https://api.github.com/repos/{m.REPO}/releases/assets/123', 'secret', 'application/octet-stream') as response:
                self.assertEqual(response.read(), b'asset')
        self.assertEqual(requests[0].get_header('Authorization'), 'Bearer secret')
        self.assertIsNone(requests[1].get_header('Authorization'))

    def test_private_asset_still_checks_digest_and_uses_fixed_id(self):
        content = b'verified release content'
        release = {'tag_name':'v0.5.0','assets':[{'id': 123,'name':'source.tar.gz','size':len(content),
            'digest':'sha256:'+hashlib.sha256(content).hexdigest(),
            'browser_download_url':f'https://github.com/{m.REPO}/releases/download/v0.5.0/source.tar.gz'}]}
        with tempfile.TemporaryDirectory() as folder, patch.object(m, 'github_open', return_value=io.BytesIO(content)) as opened:
            target = Path(folder)/'source'
            m.download_asset(release, 'source.tar.gz', target, token='secret')
            self.assertEqual(target.read_bytes(), content)
            self.assertEqual(opened.call_args.args[0], f'https://api.github.com/repos/{m.REPO}/releases/assets/123')

    def test_rejects_redirect_before_another_request(self):
        for target in ['https://evil.example/file', 'http://release-assets.githubusercontent.com/file',
                       'https://user:secret@release-assets.githubusercontent.com/file']:
            with self.subTest(target=target):
                error=m.urllib.error.HTTPError('api', 302, 'Found', {'Location': target}, io.BytesIO())
                with patch.object(m.urllib.request, 'build_opener') as factory:
                    factory.return_value.open.side_effect=error
                    with self.assertRaises(ValueError):
                        m.github_open(f'https://api.github.com/repos/{m.REPO}/releases/assets/123', 'secret', 'application/octet-stream')
                    self.assertEqual(factory.return_value.open.call_count, 1)


if __name__ == '__main__':
    unittest.main()
