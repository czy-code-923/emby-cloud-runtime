import os
import unittest
if os.name!='posix':raise unittest.SkipTest('Linux host network executor')
import importlib.util
from pathlib import Path
import tempfile
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('network_module',Path(__file__).resolve().parents[1]/'scripts/network.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class NetworkTest(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
  class Manager:pass
  self.manager=Manager();self.manager.root=Path(self.temp.name)/'app';self.manager.state=Path(self.temp.name)/'state'
  self.manager.state.mkdir();(self.manager.root/'runtime').mkdir(parents=True)
  self.network=module.Network(self.manager)
 def test_domain_injection_and_private_key_paths_rejected(self):
  for raw in ['127.0.0.1','*.example.com','x.example.com:443','../key','example.com/path','x.example.com\nreverse_proxy attacker','x..example.com']:
   with self.assertRaises(ValueError):module.validate_domain(raw)
  self.assertEqual(module.validate_domain('JW.Example.com'),'jw.example.com')
 def test_config_revision_and_secrets_never_leave_status(self):
  value={'domain':'jw.example.com','profile':'account','email':'ops@example.com','cloudflare_token':'fixture-token-not-a-real-secret','accept_terms':True,'staging':True,'expected_revision':0}
  result=self.network.configure(value)
  self.assertNotIn(value['cloudflare_token'],str(result))
  self.assertTrue(result['credential_saved'])
  self.assertEqual(result['desired']['revision'],1)
  with self.assertRaises(ValueError):self.network.configure(value)
 def test_staging_certificate_cannot_be_published(self):
  self.network.write('desired-account.json',{'domain':'jw.example.com','staging':True})
  with patch.object(self.network,'certificate',return_value={'staging':True,'expires_at':2**40}):
   with self.assertRaises(ValueError):self.network.publish('account')
 def test_ingress_separates_miniapp_cash_and_admin(self):
  output=self.network.render_ingress({'account':{'domain':'jw.example.com'},'shop':{'domain':'shop.example.com'}})
  self.assertIn('https://jw.example.com',output);self.assertIn('https://shop.example.com',output)
  self.assertIn('respond "Private or separate entrance required" 403',output)
  self.assertIn('/api/v2/commerce/*',output)
  self.assertNotIn(':80 ',output)
 def test_interrupted_publish_restores_prior_configuration(self):
  self.manager.command=lambda *args,**kwargs:None
  self.manager.dc=lambda *args:list(args)
  (self.manager.root/'.env').write_text('PRIOR=value\n')
  self.network.write('pending-publish.json',{'env':'RESTORED=value\n','config':None,'applied':{'account':{'domain':'prior.example.com'},'shop':{}}})
  self.network.write('job.json',{'phase':'running'})
  self.network.recover()
  self.assertEqual((self.manager.root/'.env').read_text(),'RESTORED=value\n')
  self.assertEqual(self.network.read('applied-account.json')['domain'],'prior.example.com')
  self.assertEqual(self.network.read('job.json')['phase'],'interrupted')
  self.assertFalse((self.network.root/'pending-publish.json').exists())

 def test_additional_playback_profiles_have_independent_certificates_and_reuse_saved_scope(self):
  base={'domain':'account.example.com','profile':'account','email':'ops@example.com','cloudflare_token':'fixture-token-not-a-real-secret','accept_terms':True,'staging':False,'expected_revision':0}
  self.network.configure(base)
  alias={**base,'profile':'playback-012345abcdef','domain':'play.example.com','cloudflare_token':''}
  status=self.network.configure(alias)
  self.assertTrue(status['profiles'][alias['profile']]['credential_saved'])
  self.assertTrue((self.network.root/'credentials/play.example.com.ini').exists())
  self.assertEqual(status['profiles']['account']['desired']['domain'],'account.example.com')
  with self.assertRaises(ValueError):self.network.configure({**alias,'profile':'playback-../../escape'})
 def test_additional_playback_ingress_uses_same_auth_and_host_boundary(self):
  config=self.network.render_ingress({'playback-012345abcdef':{'domain':'white.example.com'}})
  self.assertIn('forward_auth',config)
  self.assertIn('header_up X-JW-Host {http.request.host}',config)
  self.assertIn('Private administration entrance required',config)
  self.assertNotIn('/api/v2/commerce/*',config)
