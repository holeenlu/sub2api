import http.client
import importlib.util
from pathlib import Path
import threading
import unittest
from unittest.mock import patch
from urllib.parse import urlencode

spec = importlib.util.spec_from_file_location("siwc_helper", Path(__file__).with_name("openai_siwc_authorize.py"))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


class SIWCLoopbackTests(unittest.TestCase):
    def setUp(self):
        self.query = {
            "client_id": "dynamic_agent_client", "agent_name_hint": "Sub2API",
            "ext_agent_host_id": "urn:uuid:test", "state": "expected-state", "nonce": "nonce",
            "redirect_uri": helper.CALLBACK_URL, "resource": "https://api.openai.com/v1",
            "scope": "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct",
            "response_type": "code", "code_challenge_method": "S256", "code_challenge": "x" * 43,
        }

    def auth_url(self, **overrides):
        return helper.AUTHORIZE_URL + "?" + urlencode({**self.query, **overrides})

    def test_authorize_only_official_destination_exact_callback_and_pkce(self):
        self.assertEqual(helper.validate_authorization_url(self.auth_url()), self.query)
        for url in [self.auth_url().replace("auth.openai.com/", "auth.openai.com.evil/"),
                    self.auth_url(redirect_uri="http://localhost:1455/auth/callback"),
                    self.auth_url(resource="https://example.com"),
                    self.auth_url(code_challenge_method="plain"),
                    self.auth_url() + "&state=duplicate"]:
            with self.subTest(url=url), self.assertRaises(ValueError):
                helper.validate_authorization_url(url)

    def test_callback_state_and_registration_are_not_interchangeable(self):
        callback = "/auth/callback?" + urlencode({"state": "expected-state", "code": "one-use", "client_id": "oaiapp_test"})
        self.assertTrue(helper.validate_callback(callback, "127.0.0.1:1455", self.query).startswith(helper.CALLBACK_URL))
        for target, host in [(callback.replace("expected-state", "wrong"), "127.0.0.1:1455"),
                             (callback, "localhost:1455"), (callback + "&code=another", "127.0.0.1:1455"),
                             (callback.replace("/auth/callback", "/callback"), "127.0.0.1:1455"),
                             (callback.replace("oaiapp_test", "dynamic_agent_client"), "127.0.0.1:1455")]:
            with self.subTest(target=target), self.assertRaises(ValueError):
                helper.validate_callback(target, host, self.query)
        returning = {**self.query, "client_id": "oaiapp_selected"}
        with self.assertRaises(ValueError):
            helper.validate_callback(callback, "127.0.0.1:1455", returning)
        without_id = "/auth/callback?state=expected-state&code=one-use"
        self.assertIn("code=one-use", helper.validate_callback(without_id, "127.0.0.1:1455", returning))

    def test_denial_is_captured_only_after_matching_state(self):
        denial = "/auth/callback?state=expected-state&error=access_denied"
        self.assertIn("error=access_denied", helper.validate_callback(denial, "127.0.0.1:1455", self.query))
        with self.assertRaises(ValueError):
            helper.validate_callback(denial + "&code=conflicting", "127.0.0.1:1455", self.query)

    def test_listener_precedes_browser_and_failed_binding_never_opens_browser(self):
        events = []

        class Server:
            callback = None

            def __init__(self, address, _handler):
                self.assertion = address
                events.append("bound")

            def __enter__(self):
                return self

            def __exit__(self, *_):
                events.append("closed")

            def handle_request(self):
                self.callback = "local-callback"

        with patch.object(helper, "HTTPServer", Server), patch.object(helper.webbrowser, "open", side_effect=lambda *_a, **_k: events.append("browser") or True):
            self.assertEqual(helper.capture_callback(self.auth_url()), "local-callback")
        self.assertEqual(events, ["bound", "browser", "closed"])
        with patch.object(helper, "HTTPServer", side_effect=OSError("in use")), patch.object(helper.webbrowser, "open") as browser:
            with self.assertRaises(OSError):
                helper.capture_callback(self.auth_url())
            browser.assert_not_called()

    def test_real_listener_rejects_wrong_state_accepts_one_callback_and_does_not_reflect_code(self):
        with helper.HTTPServer(("127.0.0.1", 0), helper.CallbackHandler) as server:
            server.pending = self.query
            server.callback = None
            worker = threading.Thread(target=server.serve_forever, daemon=True)
            worker.start()
            try:
                for path, expected in [
                    ("/auth/callback?state=wrong&code=secret&client_id=oaiapp_test", 400),
                    ("/auth/callback?state=expected-state&code=secret&client_id=oaiapp_test", 200),
                    ("/auth/callback?state=expected-state&code=second&client_id=oaiapp_test", 409),
                ]:
                    connection = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=2)
                    connection.request("GET", path, headers={"Host": "127.0.0.1:1455"})
                    response = connection.getresponse()
                    self.assertEqual(response.status, expected)
                    self.assertEqual(response.getheader("Cache-Control"), "no-store")
                    self.assertNotIn("secret", response.read().decode())
                    connection.close()
                self.assertIn("code=secret", server.callback)
                self.assertNotIn("code=second", server.callback)
            finally:
                server.shutdown()
                worker.join(timeout=2)


if __name__ == "__main__":
    unittest.main()
