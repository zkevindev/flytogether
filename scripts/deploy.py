#!/usr/bin/env python3
"""Build and deploy to a configured SSH server; real settings stay outside Git."""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def settings(path: Path) -> dict[str, str]:
    result = {}
    for line in path.expanduser().read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, sep, value = line.partition("=")
        if not sep:
            raise ValueError("Invalid deployment config line")
        result[key.strip()] = value.strip().strip("\"'")
    return result


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True, help="Local config file outside the repository")
    parser.add_argument("--skip-build", action="store_true", help="Use an already verified frontend build")
    args = parser.parse_args()
    config_path = args.config.expanduser().resolve()
    if config_path.is_relative_to(ROOT):
        raise ValueError("Store real deployment configuration outside this repository")
    config = settings(config_path)
    host = config.get("DEPLOY_HOST", "")
    if not host or not re.fullmatch(r"[A-Za-z0-9.:-]+", host):
        raise ValueError("Set DEPLOY_HOST to your server hostname or IP")
    user = config.get("DEPLOY_USER") or "root"
    if user != "root":
        raise ValueError("This installer currently requires root SSH access")
    port = int(config.get("DEPLOY_SSH_PORT") or "22")
    app_port = int(config.get("DEPLOY_APP_PORT") or "8080")
    if not 1 <= port <= 65535 or not 1024 <= app_port <= 65535:
        raise ValueError("Invalid SSH or application port")
    app_root = config.get("DEPLOY_PATH") or "/opt/flytogether"
    if not re.fullmatch(r"/[A-Za-z0-9_/-]+", app_root) or ".." in app_root or len(app_root) < 5:
        raise ValueError("Invalid DEPLOY_PATH")
    domain = config.get("DEPLOY_DOMAIN", "")
    if domain and not re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?", domain):
        raise ValueError("Invalid DEPLOY_DOMAIN")

    import paramiko
    client = paramiko.SSHClient()
    client.load_system_host_keys()
    client.set_missing_host_key_policy(paramiko.RejectPolicy())
    password = None
    if config.get("DEPLOY_PASSWORD_FILE"):
        password = Path(config["DEPLOY_PASSWORD_FILE"]).expanduser().read_text().strip()
    try:
        client.connect(
            host, port=port, username=user, password=password,
            key_filename=config.get("DEPLOY_KEY_FILE") or None,
            allow_agent=password is None, look_for_keys=password is None, timeout=12,
        )
        _, out, _ = client.exec_command("uname -m", timeout=10)
        machine = out.read().decode().strip()
        arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(machine)
        if not arch:
            raise ValueError("Unsupported server architecture")
        if not args.skip_build:
            subprocess.run(["npm", "ci", "--no-audit", "--no-fund"], cwd=ROOT / "web", check=True)
            subprocess.run(["npm", "run", "build"], cwd=ROOT / "web", check=True)
        if not (ROOT / "web/dist/index.html").is_file():
            raise ValueError("Missing verified frontend build")
        release = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
        public_url = "https://" + domain if domain else "http://" + host + ":" + str(app_port)
        with tempfile.TemporaryDirectory(prefix="flytogether-release-") as temp:
            temp_path = Path(temp)
            binary = temp_path / "server"
            build_env = dict(os.environ, GOOS="linux", GOARCH=arch, CGO_ENABLED="0")
            subprocess.run(
                ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(binary), "./cmd/server"],
                cwd=ROOT, env=build_env, check=True,
            )
            archive = temp_path / "release.tar.gz"
            with tarfile.open(archive, "w:gz") as bundle:
                bundle.add(binary, arcname="server")
                bundle.add(ROOT / "web/dist", arcname="web")
            remote_archive = "/tmp/flytogether-release-" + release + ".tar.gz"
            remote_installer = "/tmp/flytogether-install-" + release + ".sh"
            with client.open_sftp() as transfer:
                transfer.put(str(archive), remote_archive)
                transfer.put(str(ROOT / "deploy/install.sh"), remote_installer)
            env = {
                "APP_ROOT": app_root, "APP_PORT": str(app_port), "PUBLIC_URL": public_url,
                "APP_DOMAIN": domain, "RELEASE_ID": release, "RELEASE_ARCHIVE": remote_archive,
            }
            command = " ".join(key + "=" + shlex.quote(value) for key, value in env.items())
            command += " bash " + shlex.quote(remote_installer)
            _, stdout, stderr = client.exec_command(command, timeout=600)
            # Read incrementally so a package installation cannot fill SSH's receive window.
            channel = stdout.channel
            while not channel.exit_status_ready() or channel.recv_ready() or channel.recv_stderr_ready():
                if channel.recv_ready():
                    text = channel.recv(32768).decode(errors="replace")
                    print(text.replace(password, "[REDACTED]") if password else text, end="", flush=True)
                if channel.recv_stderr_ready():
                    text = channel.recv_stderr(32768).decode(errors="replace")
                    print(text.replace(password, "[REDACTED]") if password else text, end="", file=sys.stderr, flush=True)
                if not channel.recv_ready() and not channel.recv_stderr_ready():
                    import time
                    time.sleep(0.1)
            status = channel.recv_exit_status()
            if status:
                raise RuntimeError("Remote installation failed; inspect the service on your server")
            client.exec_command("rm -f " + shlex.quote(remote_archive) + " " + shlex.quote(remote_installer))
            print("\nDeployment complete:", public_url)
    finally:
        client.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Credentials are never passed as command-line arguments or written to Git.
        print(type(error).__name__ + ": " + str(error), file=sys.stderr)
        raise SystemExit(1)
