import json
import os
from pathlib import Path
import subprocess

plan = json.loads(Path('release-plan.json').read_text())
manifest = {**plan, 'image_digest': os.environ['IMAGE_DIGEST']}
Path('dist/release/release-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
changes = subprocess.check_output(['git', 'log', '--first-parent', '-20', '--format=- %s (%h)', plan['commit']], text=True)
Path('release-notes.md').write_text(
    '<!-- release-plan:' + json.dumps(plan) + ' -->\n\n'
    f"基于官方 Sub2API **{plan['base']}**，发布渠道 **{plan['channel']}**。\n\n"
    f"镜像：`{plan['image']}:{plan['version']}`\n\n"
    f"固定摘要：`{plan['image']}@{manifest['image_digest']}`\n\n"
    'Docker Compose 可在配置宿主机更新服务后从管理后台在线更新。\n\n'
    '更新会执行当前版本自带的数据库迁移；升级前请备份数据库。镜像回退不回滚数据库。\n\n'
    '### 最近提交\n\n' + changes)
