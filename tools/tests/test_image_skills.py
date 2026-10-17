import base64
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

ROOT = Path(__file__).parents[2] / "frontend/public/downloads/tapmodels-image-skills"


def load_script(name):
    path = ROOT / name / "scripts/generate.py"
    spec = importlib.util.spec_from_file_location(f"image_skill_{name}", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ImageSkillTest(unittest.TestCase):
    def setUp(self):
        self.flare = load_script("gpt-image-flare")
        self.sunburst = load_script("gpt-image-sunburst")
        self.old_env = os.environ.copy()
        # The fixture server must stay local even on hosts with an HTTP proxy.
        os.environ["no_proxy"] = "localhost,127.0.0.1"
        os.environ["NO_PROXY"] = "localhost,127.0.0.1"
        self.temp = tempfile.TemporaryDirectory()
        self.home = Path(self.temp.name)
        os.environ["CODEX_HOME"] = str(self.home)
        for key in ("TAPMODELS_BASE_URL", "TAPMODELS_API_KEY", "OPENAI_API_KEY"):
            os.environ.pop(key, None)

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.old_env)
        self.temp.cleanup()

    def write_config(self, text):
        (self.home / "config.toml").write_text(text, encoding="utf-8")

    def test_env_key_is_authoritative_and_check_config_needs_no_key(self):
        self.write_config('''model_provider = "Tap"
[model_providers.Tap]
base_url = "https://images.example.test"
env_key = "TAP_KEY"
''')
        os.environ["OPENAI_API_KEY"] = "wrong-key"
        with self.assertRaisesRegex(RuntimeError, "TAP_KEY.*missing"):
            self.flare.load_provider()
        name, base, headers, token = self.flare.load_provider(require_token=False)
        self.assertEqual((name, base, token), ("Tap", "https://images.example.test", None))
        old_argv = list(__import__("sys").argv)
        __import__("sys").argv = ["generate.py", "--check-config"]
        try:
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(self.flare.main(), 0)
            self.assertNotIn("wrong-key", output.getvalue())
            self.assertFalse(json.loads(output.getvalue())["credentials_checked"])
        finally:
            __import__("sys").argv = old_argv

    def test_each_skill_keeps_its_fixed_model(self):
        self.assertEqual(self.flare.MODEL, "gpt-image-2.5-flare")
        self.assertEqual(self.sunburst.MODEL, "gpt-image-2.5-sunburst")

    def test_misplaced_secret_in_env_key_is_not_echoed(self):
        self.write_config('''model_provider = "Tap"
[model_providers.Tap]
base_url = "https://images.example.test"
env_key = "sk-should-never-be-echoed"
''')
        with self.assertRaises(RuntimeError) as raised:
            self.flare.load_provider()
        self.assertEqual(str(raised.exception), "Model provider 'Tap' has an invalid env_key")
        self.assertNotIn("sk-", str(raised.exception))

    def test_explicit_tapmodels_override_and_profile_rejection(self):
        os.environ["TAPMODELS_BASE_URL"] = "http://127.0.0.1:43111"
        self.assertEqual(self.flare.load_provider(require_token=False), ("TapModels environment", "http://127.0.0.1:43111", {}, None))
        with self.assertRaisesRegex(RuntimeError, "TAPMODELS_API_KEY"):
            self.flare.load_provider()
        os.environ["TAPMODELS_API_KEY"] = "test-key"
        self.assertEqual(self.flare.load_provider()[3], "test-key")
        os.environ.pop("TAPMODELS_BASE_URL")
        os.environ.pop("TAPMODELS_API_KEY")
        self.write_config('''profile = "work"
[model_providers.OpenAI]
base_url = "https://api.example.test"
experimental_bearer_token = "secret"
''')
        with self.assertRaisesRegex(RuntimeError, "profile-based"):
            self.flare.load_provider(require_token=False)

    def test_key_only_uses_codex_provider_url_and_env_key(self):
        self.write_config('''model_provider = "Tap"
[model_providers.Tap]
base_url = "https://provider.example.test"
env_key = "TAPMODELS_API_KEY"
''')
        os.environ["TAPMODELS_API_KEY"] = "provider-key"
        for skill in (self.flare, self.sunburst):
            name, base, headers, token = skill.load_provider()
            self.assertEqual(name, "Tap")
            self.assertEqual(base, "https://provider.example.test")
            self.assertEqual(headers, {})
            self.assertEqual(token, "provider-key")

    def test_key_only_does_not_replace_a_different_provider_env_key(self):
        self.write_config('''model_provider = "Tap"
[model_providers.Tap]
base_url = "https://provider.example.test"
env_key = "PROVIDER_API_KEY"
''')
        os.environ["TAPMODELS_API_KEY"] = "wrong-provider-key"
        for skill in (self.flare, self.sunburst):
            with self.assertRaisesRegex(RuntimeError, "PROVIDER_API_KEY.*missing"):
                skill.load_provider()

    def test_url_policy_and_redirect_handler(self):
        self.assertEqual(self.flare.images_endpoint("http://localhost:8080", "generations"), "http://localhost:8080/v1/images/generations")
        with self.assertRaisesRegex(RuntimeError, "HTTPS"):
            self.flare.images_endpoint("http://remote.example", "generations")
        with self.assertRaisesRegex(RuntimeError, "query"):
            self.flare.images_endpoint("https://images.example/?key=secret", "generations")
        self.assertIsNone(self.flare.NoRedirect().redirect_request(None, None, 302, "", {}, "https://other.example"))

    def test_real_decoding_for_png_jpeg_webp_and_invalid_input(self):
        from PIL import Image

        for fmt, expected in (("PNG", "png"), ("JPEG", "jpeg"), ("WEBP", "webp")):
            stream = io.BytesIO()
            Image.new("RGB", (7, 5), "red").save(stream, format=fmt)
            self.assertEqual(self.flare.decode_image(stream.getvalue(), "fixture")[:3], (expected, 7, 5))
        with self.assertRaisesRegex(RuntimeError, "decodable"):
            self.flare.decode_image(b"not an image", "fixture")

    def test_dry_run_validates_payload_without_network(self):
        self.write_config('''model_provider = "Tap"
[model_providers.Tap]
base_url = "http://127.0.0.1:43111"
env_key = "MISSING_KEY"
''')
        old_argv = list(__import__("sys").argv)
        __import__("sys").argv = ["generate.py", "--prompt", "draw", "--dry-run"]
        try:
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(self.flare.main(), 0)
            result = json.loads(output.getvalue())
            self.assertTrue(result["dry_run"])
            self.assertEqual(result["payload"]["model"], "gpt-image-2.5-flare")
        finally:
            __import__("sys").argv = old_argv

    def test_http_error_is_safe_and_response_dimensions_are_decoded(self):
        from PIL import Image

        class Handler(BaseHTTPRequestHandler):
            requests = 0

            def do_POST(self):
                Handler.requests += 1
                if self.path.endswith("generations"):
                    image = io.BytesIO()
                    Image.new("RGB", (9, 4), "blue").save(image, format="PNG")
                    body = json.dumps({"data": [{"b64_json": base64.b64encode(image.getvalue()).decode()}]})
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body.encode())

            def log_message(self, *_args):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            os.environ["TAPMODELS_BASE_URL"] = f"http://127.0.0.1:{server.server_port}"
            os.environ["TAPMODELS_API_KEY"] = "fixture-key"
            output_path = self.home / "out.png"
            old_argv = list(__import__("sys").argv)
            __import__("sys").argv = ["generate.py", "--prompt", "draw", "--output", str(output_path)]
            try:
                with contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(self.flare.main(), 0)
            finally:
                __import__("sys").argv = old_argv
            with Image.open(output_path) as image:
                self.assertEqual(image.size, (9, 4))
            self.assertEqual(Handler.requests, 1)
        finally:
            server.shutdown()
            server.server_close()

    def test_api_error_does_not_echo_raw_json_or_key(self):
        error = self.flare.api_failure(401, "invalid_api_key", {"x-request-id": "req-123"})
        text = str(error)
        self.assertEqual(text, "Image API request failed (status=401, type=invalid_api_key, request_id=req-123)")
        self.assertNotIn("Bearer", text)
        self.assertNotIn("message", text)

    def test_output_reservation_rejects_overwrite_and_concurrent_writer(self):
        output = self.home / "reserved.png"
        output.write_bytes(b"user data")
        with self.assertRaisesRegex(RuntimeError, "already exists"):
            with self.flare.reserve_output(output, False):
                pass
        lock = output.parent / f".{output.name}.lock"
        lock.write_text("busy", encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "already being written"):
            with self.flare.reserve_output(output, True):
                pass

    def test_mock_http_error_redacts_body_and_token(self):
        secret = "fixture-secret-token"

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body = json.dumps({"error": {"type": "invalid_api_key", "message": secret}}).encode()
                self.send_response(401)
                self.send_header("x-request-id", "req-safe-9")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_args):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            os.environ["TAPMODELS_BASE_URL"] = f"http://127.0.0.1:{server.server_port}"
            os.environ["TAPMODELS_API_KEY"] = secret
            old_argv = list(__import__("sys").argv)
            __import__("sys").argv = ["generate.py", "--prompt", "draw", "--output", str(self.home / "error.png")]
            try:
                with self.assertRaises(RuntimeError) as raised:
                    self.flare.main()
            finally:
                __import__("sys").argv = old_argv
            message = str(raised.exception)
            self.assertEqual(message, "Image API request failed (status=401, type=invalid_api_key, request_id=req-safe-9)")
            self.assertNotIn(secret, message)
        finally:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
