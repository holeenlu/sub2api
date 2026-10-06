#!/usr/bin/env python3
"""Create fresh Tokensavy credentials without reading or replacing another deployment."""
import argparse
import os
from pathlib import Path
import re
import secrets


def initialize(directory: Path, domain: str, email: str) -> None:
    if not re.fullmatch(r"[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?", domain) or "." not in domain:
        raise ValueError("domain must be a hostname without scheme, path or port")
    if not re.fullmatch(r"[a-zA-Z0-9_.+%-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}", email):
        raise ValueError("invalid administrator email")
    directory.mkdir(parents=True, exist_ok=True)
    env_path = directory / ".env"
    credentials_path = directory / "credentials.txt"
    if env_path.exists() or credentials_path.exists():
        raise FileExistsError(".env or credentials.txt already exists; existing credentials were preserved")
    values = {
        "DOMAIN": domain,
        "ACME_EMAIL": email,
        "ADMIN_EMAIL": email,
        "ADMIN_PASSWORD": secrets.token_urlsafe(24),
        "POSTGRES_USER": "tokensavy",
        "POSTGRES_DB": "tokensavy",
        "POSTGRES_PASSWORD": secrets.token_hex(32),
        "REDIS_PASSWORD": secrets.token_hex(32),
        "JWT_SECRET": secrets.token_hex(32),
        "TOTP_ENCRYPTION_KEY": secrets.token_hex(32),
        "TZ": "Asia/Shanghai",
        "SERVER_PORT": "8080",
        "DATABASE_MAX_OPEN_CONNS": "50",
        "DATABASE_MAX_IDLE_CONNS": "10",
        "REDIS_POOL_SIZE": "128",
        "REDIS_MIN_IDLE_CONNS": "10",
        "SETUP_MIGRATION_TIMEOUT_SECONDS": "300",
    }
    env = "# Tokensavy private deployment configuration. Keep out of Git.\n"
    env += "".join(f"{key}={value}\n" for key, value in values.items())
    report = f"Tokensavy — https://{domain}\n管理员登录：{email}\n管理员初始密码：{values['ADMIN_PASSWORD']}\n\n"
    report += "以下是新部署生成的独立凭据，首次启动后才会创建账号。\n"
    report += "".join(f"{key}={values[key]}\n" for key in (
        "POSTGRES_USER", "POSTGRES_DB", "POSTGRES_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET", "TOTP_ENCRYPTION_KEY"))
    report += "\n数据库主机 postgres:5432；Redis 主机 redis:6379（仅容器内网）。\n"
    report += "管理员邮箱是登录标识；未创建邮箱服务，也未配置 SMTP。\n"
    report += "持久化 .env 和数据卷一起备份；已有数据库不会因修改 ADMIN_PASSWORD 而重置密码。\n"
    # O_EXCL and restrictive creation mode prevent overwrites and a world-readable window.
    for path, content in ((env_path, env), (credentials_path, report)):
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as stream:
            stream.write(content)
    print(f"Created {env_path} and {credentials_path} (mode 600). No secrets printed.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, default=Path(__file__).resolve().parent)
    parser.add_argument("--domain", default="tokensavy.ai")
    parser.add_argument("--admin-email", default="ikung1970@gmail.com")
    args = parser.parse_args()
    try:
        initialize(args.directory, args.domain, args.admin_email)
    except (ValueError, FileExistsError) as error:
        parser.exit(1, f"{error}\n")
