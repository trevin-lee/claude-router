#!/usr/bin/env python3
"""Publish the Homebrew formula for a release to trevin-lee/homebrew-tap.

  scripts/tap.py vX.Y.Z

The formula builds claude-router from source. That keeps Homebrew's download
quarantine off the binary (an unsigned downloaded binary is killed by
Gatekeeper), and it is how Homebrew packages Go tools.

Needs a deploy key with write access to the tap in TAP_DEPLOY_KEY.
"""

import hashlib
import os
import pathlib
import subprocess
import sys
import tempfile
import urllib.request

NAME = "claude-router"
REPO = f"trevin-lee/{NAME}"
TAP_SSH = "git@github.com:trevin-lee/homebrew-tap.git"
AUTHOR = (f"{NAME} release bot", "134039857+trevin-lee@users.noreply.github.com")
root = pathlib.Path(__file__).resolve().parent.parent


def run(*args, **kw):
    subprocess.run(args, check=True, **kw)


def main(tag: str) -> None:
    version = tag.lstrip("v")
    tarball = f"https://github.com/{REPO}/archive/refs/tags/{tag}.tar.gz"
    with urllib.request.urlopen(tarball) as resp:
        sha = hashlib.sha256(resp.read()).hexdigest()

    formula = (root / "homebrew" / f"{NAME}.rb.tmpl").read_text()
    formula = formula.replace("{{VERSION}}", version).replace("{{SHA256}}", sha)

    key = os.environ["TAP_DEPLOY_KEY"]
    with tempfile.TemporaryDirectory() as tmp:
        tmp = pathlib.Path(tmp)
        key_file = tmp / "deploy_key"
        key_file.write_text(key if key.endswith("\n") else key + "\n")
        key_file.chmod(0o600)
        env = {
            **os.environ,
            "GIT_SSH_COMMAND": f"ssh -i {key_file} -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new",
        }
        clone = tmp / "tap"
        run("git", "clone", "--depth", "1", TAP_SSH, str(clone), env=env)

        (clone / "Formula").mkdir(exist_ok=True)
        (clone / "Formula" / f"{NAME}.rb").write_text(formula)

        run("git", "-C", str(clone), "add", "-A")
        if subprocess.run(["git", "-C", str(clone), "diff", "--cached", "--quiet"]).returncode == 0:
            print("tap already up to date")
            return
        env.update(
            GIT_AUTHOR_NAME=AUTHOR[0], GIT_AUTHOR_EMAIL=AUTHOR[1],
            GIT_COMMITTER_NAME=AUTHOR[0], GIT_COMMITTER_EMAIL=AUTHOR[1],
        )
        run("git", "-C", str(clone), "commit", "-m", f"feat: {NAME} {tag}", env=env)
        run("git", "-C", str(clone), "push", env=env)
    print(f"published {NAME} {version} to the tap (source sha256 {sha[:12]}…)")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: tap.py vX.Y.Z")
    main(sys.argv[1])
