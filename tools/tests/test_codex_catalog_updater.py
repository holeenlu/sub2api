import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('catalog_updater', ROOT / 'frontend/public/install/update-codex-models.py')
UPDATER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(UPDATER)


def model(slug='future-model'):
    return {'slug': slug, 'input_modalities': ['text', 'image'], 'supported_reasoning_levels': [{'effort': 'high'}], 'default_reasoning_level': 'high', 'context_window': 123456, 'model_messages': {'instructions_template': 'You are a coding assistant.'}}


class CodexCatalogUpdaterTests(unittest.TestCase):
    def test_valid_new_model_does_not_require_any_compiled_model_list(self):
        data = {'models': [model('never-seen-before-model')]}
        self.assertEqual(UPDATER.validate_manifest(json.dumps(data).encode(), {'model': 'never-seen-before-model'}), data)

    def test_empty_malformed_duplicate_and_missing_default_are_rejected(self):
        values = [{'data': [{'id': 'wrong-endpoint'}]}, {'models': []}, {'models': [model(), model()]}, {'models': [{'slug': 'partial'}]}]
        for data in values:
            with self.subTest(data=data), self.assertRaises(ValueError):
                UPDATER.validate_manifest(json.dumps(data).encode(), {})
        with self.assertRaises(ValueError):
            UPDATER.validate_manifest(json.dumps({'models': [model()]}).encode(), {'model': 'removed-model'})

    def test_configuration_and_credentials_are_read_without_rewriting_them(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / 'config.toml'
            original = 'model_provider = "private"\nmodel_catalog_json = "models.json"\n[model_providers.private]\nbase_url = "https://gateway.example/v1"\nenv_key = "CATALOG_TEST_KEY"\n'
            config.write_text(original)
            with patch.dict(os.environ, {'CATALOG_TEST_KEY': 'test-key'}):
                parsed, endpoint, token, output = UPDATER.load_configuration(config)
            self.assertEqual(endpoint, 'https://gateway.example/backend-api/codex/models')
            self.assertEqual(token, 'test-key')
            self.assertEqual(output, config.parent / 'models.json')
            self.assertEqual(config.read_text(), original)

    def test_atomic_replace_failure_keeps_previous_file(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'models.json'
            output.write_text('previous-valid-catalog')
            with patch.object(UPDATER.os, 'replace', side_effect=OSError('read only')), self.assertRaises(OSError):
                UPDATER.atomic_catalog_write(output, {'models': [model()]})
            self.assertEqual(output.read_text(), 'previous-valid-catalog')
            self.assertEqual(sorted(p.name for p in output.parent.iterdir()), ['models.json'])

    def test_successful_replace_contains_only_valid_model_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'models.json'
            data = {'models': [model()]}
            UPDATER.atomic_catalog_write(output, data)
            self.assertEqual(json.loads(output.read_text()), data)

    def test_redirects_cannot_forward_the_credential_to_another_origin(self):
        with self.assertRaises(ValueError):
            UPDATER.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://other.example')


if __name__ == '__main__':
    unittest.main()
