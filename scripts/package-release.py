#!/usr/bin/env python3
"""Build complete source and application distribution from the committed tree."""
from pathlib import Path
import hashlib
import json
import shutil
import subprocess
import tarfile

root=Path(__file__).resolve().parent.parent
version=(root/'VERSION').read_text().strip()
dest=root/'dist'
dest.mkdir(exist_ok=True)
commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
source=dest/f'emby-cloud-{version}-source.tar.gz'
subprocess.run(['git','archive','--format=tar.gz','--prefix=emby-cloud/','-o',str(source),'HEAD'],cwd=root,check=True)
metadata={'version':version,'commit':commit,'protocol':1,'database_schema':1,'rollback_safe':True,
          'architecture':'linux-amd64','repository':'https://github.com/czy-code-923/emby-cloud-runtime'}
(dest/'release.json').write_text(json.dumps(metadata,indent=2)+'\n')
shutil.copy2(root/'scripts/manager.py',dest/'manager.py')
for name in ['install.sh','uninstall.sh']:
    shutil.copy2(root/name,dest/name)
bundle=dest/f'emby-cloud-{version}-linux-amd64.tar.gz'
members=[source,dest/'release.json',dest/'manager.py',dest/'install.sh',dest/'uninstall.sh',
         dest/f'backend-{version}-amd64.tar.gz',dest/f'webui-{version}-amd64.tar.gz']
with tarfile.open(bundle,'w:gz',compresslevel=1) as archive:
    for member in members:
        archive.add(member,arcname='emby-cloud-release/'+member.name)
lines=[]
for path in sorted(dest.iterdir()):
    if path.is_file() and path.name!='SHA256SUMS':
        digest=hashlib.file_digest(path.open('rb'),'sha256').hexdigest()
        lines.append(digest+'  '+path.name)
(dest/'SHA256SUMS').write_text('\n'.join(lines)+'\n')
print('Packaged complete source, install/uninstall entries, metadata, application images and SHA256SUMS')
