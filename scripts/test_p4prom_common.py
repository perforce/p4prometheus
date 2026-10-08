#!/usr/bin/env python3

import os
import subprocess
import tempfile
import unittest


SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
COMMON_SCRIPT = os.path.join(SCRIPT_DIR, "p4prom_common.sh")


class TestP4PromCommon(unittest.TestCase):
    def run_lock_secret_migration(self, config_path, secrets_path, function_name):
        command = """
set -e
source "$1"
p4monitor_locks_config_file="$2"
p4monitor_locks_secrets_file="$3"
"$4"
"""
        return subprocess.run(
            ["bash", "-c", command, "bash", COMMON_SCRIPT, config_path, secrets_path, function_name],
            check=False,
            capture_output=True,
            text=True,
        )

    def test_migrates_all_inline_lock_notification_credentials(self):
        config_before = """\
notifications:
  slack:
    webhook_url: "https://hooks.slack.com/services/legacy"
    bot_token: "xoxb-legacy-token"
  teams:
    webhook_url: "https://teams.example/legacy"
  email:
    password: "legacy-smtp-password"
"""

        with tempfile.TemporaryDirectory() as temporary_directory:
            config_path = os.path.join(temporary_directory, "p4monitor_locks.yaml")
            secrets_path = os.path.join(temporary_directory, "p4monitor_locks.env")
            with open(config_path, "w", encoding="utf-8") as config_file:
                config_file.write(config_before)
            open(secrets_path, "w", encoding="utf-8").close()

            result = self.run_lock_secret_migration(
                config_path, secrets_path, "migrate_p4monitor_locks_notification_secrets")

            self.assertEqual(result.returncode, 0, result.stderr)
            with open(config_path, encoding="utf-8") as config_file:
                config_after = config_file.read()
            with open(secrets_path, encoding="utf-8") as secrets_file:
                secrets_after = secrets_file.read()

        self.assertNotIn("webhook_url:", config_after)
        self.assertNotIn("bot_token:", config_after)
        self.assertNotIn("password:", config_after)
        self.assertIn('webhook_url_env: "P4MONITOR_SLACK_WEBHOOK_URL"', config_after)
        self.assertIn('bot_token_env: "P4MONITOR_SLACK_BOT_TOKEN"', config_after)
        self.assertIn('webhook_url_env: "P4MONITOR_TEAMS_WEBHOOK_URL"', config_after)
        self.assertIn('password_env: "P4MONITOR_SMTP_PASSWORD"', config_after)
        self.assertEqual(
            secrets_after,
            "P4MONITOR_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/legacy\n"
            "P4MONITOR_SLACK_BOT_TOKEN=xoxb-legacy-token\n"
            "P4MONITOR_TEAMS_WEBHOOK_URL=https://teams.example/legacy\n"
            "P4MONITOR_SMTP_PASSWORD=legacy-smtp-password\n",
        )

    def test_uses_existing_secret_and_rewrites_legacy_yaml_key(self):
        config_before = """\
notifications:
  slack:
    webhook_url: "https://hooks.slack.com/services/legacy"
"""
        existing_secret = "P4MONITOR_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/existing\n"

        with tempfile.TemporaryDirectory() as temporary_directory:
            config_path = os.path.join(temporary_directory, "p4monitor_locks.yaml")
            secrets_path = os.path.join(temporary_directory, "p4monitor_locks.env")
            with open(config_path, "w", encoding="utf-8") as config_file:
                config_file.write(config_before)
            with open(secrets_path, "w", encoding="utf-8") as secrets_file:
                secrets_file.write(existing_secret)

            result = self.run_lock_secret_migration(
                config_path,
                secrets_path,
                "migrate_p4monitor_locks_notification_secrets",
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            with open(config_path, encoding="utf-8") as config_file:
                config_after = config_file.read()
            with open(secrets_path, encoding="utf-8") as secrets_file:
                secrets_after = secrets_file.read()

        self.assertNotIn("webhook_url:", config_after)
        self.assertIn('webhook_url_env: "P4MONITOR_SLACK_WEBHOOK_URL"', config_after)
        self.assertEqual(secrets_after, existing_secret)


if __name__ == "__main__":
    unittest.main()