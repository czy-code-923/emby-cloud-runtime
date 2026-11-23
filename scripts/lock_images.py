import json, pathlib, subprocess
services=['postgres','backend','webui','gateway','emby']
c=json.loads(subprocess.check_output(['docker','compose','config','--format','json']))
lines=['# Exact pulled image digests; generated locally.','services:']
for name in services:
    image=c['services'][name]['image']
    data=json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]
    digests=data.get('RepoDigests',[])
    if not digests and name not in ['backend','webui']: raise SystemExit('No registry digest for '+name)
    exact=digests[0] if digests else data['Id']
    lines += ['  '+name+':','    image: '+exact,'    pull_policy: never']
pathlib.Path('images.lock.yaml').write_text('\n'.join(lines)+'\n')
