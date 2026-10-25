import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

from scripts.release import publish


class ReleasePublishTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='release-publish-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.files = self.root / 'release'
        self.files.mkdir()
        self.plan = dict(channel='kdan', version='0.2.18.1', tag='kdan/v0.2.18.1',
                         commit='a' * 40, image='ghcr.io/holeenlu/kdan',
                         portal_image='ghcr.io/holeenlu/kdan-vendor')
        self.marker = '<!-- release-plan:' + json.dumps(self.plan) + ' -->'
        self.notes = self.root / 'notes.md'
        self.notes.write_text(self.marker + '\nComplete notes\n')
        self.archive = 'kdan_0.2.18.1_linux_amd64.tar.gz'
        (self.files / self.archive).write_bytes(b'validated archive')
        (self.files / 'checksums.txt').write_text(publish.artifact_hash(self.files / self.archive) + '  ' + self.archive + '\n')
        manifest = dict(self.plan, image_digest='sha256:' + 'b' * 64, portal_image_digest='sha256:' + 'c' * 64)
        (self.files / 'release-manifest.json').write_text(json.dumps(manifest))
        (self.files / 'compose.vendor-release.json').write_text(json.dumps({
            'services': {'vendor-portal': {'image': self.plan['portal_image'] + '@' + manifest['portal_image_digest']}}}))
        self.release = dict(id=22, tag_name=self.plan['tag'], draft=True, body=self.marker, assets=[])
        self.peers = []
        self.direct = None
        self.tag = dict(type='commit', sha=self.plan['commit'])
        self.calls = []
        self.patcher = patch.object(publish, 'api', side_effect=self.api)
        self.mock = self.patcher.start()
        self.addCleanup(self.patcher.stop)

    def asset(self, name, id=100):
        file = self.files / name
        return dict(id=id, name=name, size=file.stat().st_size, state='uploaded',
                    digest='sha256:' + publish.artifact_hash(file))

    def api(self, method, path, **kwargs):
        self.calls.append((method, path, kwargs))
        if method == 'GET':
            if path.endswith('/releases/22'):
                return copy.deepcopy(self.release)
            if path.endswith('/releases?per_page=100'):
                return [[copy.deepcopy(self.release), *copy.deepcopy(self.peers)]]
            if '/releases/tags/' in path:
                return copy.deepcopy(self.direct or (self.release if not self.release['draft'] else None))
            if '/git/ref/tags/' in path:
                return {'object': self.tag}
            if '/git/tags/' in path:
                return {'object': dict(type='commit', sha=self.plan['commit'])}
        if method == 'POST':
            self.assertTrue(path.startswith('https://uploads.github.com/repos/holeenlu/sub2api/releases/22/assets?'))
            name = parse_qs(urlsplit(path).query)['name'][0]
            self.assertEqual(kwargs['file'], self.files / name)
            asset = self.asset(name, 100 + len(self.release['assets']))
            self.release['assets'].append(asset)
            return copy.deepcopy(asset)
        if method == 'DELETE':
            id = int(path.rsplit('/', 1)[1])
            self.assertTrue(any(a['id'] == id for a in self.release['assets']))
            self.release['assets'] = [a for a in self.release['assets'] if a['id'] != id]
            return None
        if method == 'PATCH':
            self.assertEqual(path, 'repos/holeenlu/sub2api/releases/22')
            self.assertEqual(kwargs['payload'], dict(body=self.notes.read_text(), draft=False, make_latest='true'))
            self.release.update(kwargs['payload'])
            return copy.deepcopy(self.release)
        self.fail((method, path, kwargs))

    def execute(self):
        publish.publish('holeenlu/sub2api', 22, self.plan, self.files, self.notes)

    def writes(self):
        return [c for c in self.calls if c[0] != 'GET']

    def test_uploads_and_publication_use_one_reserved_id(self):
        self.execute()
        self.assertFalse(self.release['draft'])
        self.assertEqual(len(self.release['assets']), 4)
        self.assertEqual([c[0] for c in self.writes()], ['POST'] * 4 + ['PATCH'])
        self.assertFalse(any('/releases/tags/' in c[1] for c in self.writes()))

    def test_duplicate_published_or_draft_tag_blocks_all_writes(self):
        for draft in (False, True):
            with self.subTest(draft=draft):
                self.peers = [dict(self.release, id=11, draft=draft)]
                with self.assertRaisesRegex(ValueError, 'also used by IDs'):
                    self.execute()
                self.assertEqual(self.writes(), [])

    def test_direct_published_lookup_catches_stale_release_list(self):
        self.direct = dict(self.release, id=11, draft=False)
        with self.assertRaisesRegex(ValueError, 'also used by IDs'):
            self.execute()
        self.assertEqual(self.writes(), [])

    def test_tag_or_provenance_changes_block_all_writes(self):
        for change in ('tag', 'source', 'provenance'):
            with self.subTest(change=change):
                self.release.update(tag_name=self.plan['tag'], body=self.marker)
                self.tag = dict(type='commit', sha=self.plan['commit'])
                if change == 'tag':
                    self.release['tag_name'] = 'kdan/v0.2.18.2'
                elif change == 'source':
                    self.tag['sha'] = 'f' * 40
                else:
                    self.release['body'] = '<!-- release-plan:' + json.dumps(dict(self.plan, commit='f' * 40)) + ' -->'
                with self.assertRaises(ValueError):
                    self.execute()
                self.assertEqual(self.writes(), [])

    def test_annotated_tag_resolves_to_frozen_commit(self):
        self.tag = dict(type='tag', sha='d' * 40)
        self.execute()
        self.assertFalse(self.release['draft'])

    def test_partial_draft_retry_skips_matching_assets_and_replaces_only_own_asset(self):
        correct = self.asset(self.archive, 80)
        wrong = dict(self.asset('checksums.txt', 81), digest='sha256:' + 'f' * 64)
        self.release['assets'] = [correct, wrong]
        self.execute()
        self.assertEqual([c[1] for c in self.writes() if c[0] == 'DELETE'],
                         ['repos/holeenlu/sub2api/releases/assets/81'])
        self.assertEqual(len([c for c in self.writes() if c[0] == 'POST']), 3)

    def test_corrupt_checksum_or_manifest_blocks_all_writes(self):
        for name in ('checksums.txt', 'release-manifest.json', 'compose.vendor-release.json'):
            with self.subTest(file=name):
                path = self.files / name
                old = path.read_text()
                path.write_text('{}')
                with self.assertRaises(ValueError):
                    self.execute()
                self.assertEqual(self.writes(), [])
                path.write_text(old)

    def test_published_retry_is_readonly_and_different_content_is_rejected(self):
        self.execute()
        self.calls.clear()
        self.execute()
        self.assertEqual(self.writes(), [])
        self.release['assets'][0]['digest'] = 'sha256:' + 'f' * 64
        with self.assertRaisesRegex(ValueError, 'refusing to overwrite'):
            self.execute()
        self.assertEqual(self.writes(), [])

    def test_publish_error_does_not_fall_back_to_another_release(self):
        def fail(method, path, **kwargs):
            if method == 'PATCH':
                raise RuntimeError('422 already_exists')
            return self.api(method, path, **kwargs)
        self.mock.side_effect = fail
        with self.assertRaisesRegex(RuntimeError, '422'):
            self.execute()
        self.assertTrue(self.release['draft'])
        self.assertEqual(len(self.release['assets']), 4)

    def test_api_treats_only_explicit_404_as_missing(self):
        self.patcher.stop()
        for code in (404, 403, 422, 500):
            with self.subTest(status=code), patch.object(publish.subprocess, 'run', return_value=
                    subprocess.CompletedProcess([], 1, json.dumps({'status': str(code)}), 'request failed')):
                if code == 404:
                    self.assertIsNone(publish.api('GET', 'repos/example/releases/tags/test', missing_ok=True))
                else:
                    with self.assertRaises(RuntimeError):
                        publish.api('GET', 'repos/example/releases/tags/test', missing_ok=True)


if __name__ == '__main__':
    unittest.main()
