"""Check embedded editors against the backend schema, not a second key list."""
from pathlib import Path
import re
import unittest
ROOT=Path(__file__).resolve().parents[1]/'upstream/twilight'
class PanelContracts(unittest.TestCase):
 def test_telegram_editor_fields_exist_in_server_schema(self):
  schema=(ROOT/'internal/api/config_admin.go').read_text('utf-8')
  section=schema.split('{Key: "Telegram", Title:',1)[1].split('{Key: "SAR", Title:',1)[0]
  keys=set(re.findall(r'\{Key: "([^"]+)"',section))
  page=(ROOT/'webui/src/app/(main)/admin/telegram/page.tsx').read_text('utf-8')
  fields=re.findall(r'"([a-z_]+)"',page.split('const TELEGRAM_BASE_FIELDS = [',1)[1].split('] as const',1)[0])
  self.assertFalse(set(fields)-keys)
  self.assertIn('bot_token',fields)
if __name__=='__main__':unittest.main()
