#!/usr/bin/env python3
"""Build paired-live from committed main without modifying the working tree.

The linker fields name the archived source explicitly; no Go vcs.* settings are
invented. As with the ordinary VCS build stamp, this trusts the local builder.
The manifest records provenance and hashes, not a supply-chain attestation.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile


def git(repo, *args):
    return subprocess.check_output(["git", "-C", str(repo), *args]).decode().strip()


def sha256(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    repo = Path(git(args.repo, "rev-parse", "--show-toplevel")).resolve()
    if git(repo, "symbolic-ref", "--short", "HEAD") != "main":
        parser.error("archive build requires main")
    commit = git(repo, "rev-parse", "HEAD")
    tree = git(repo, "rev-parse", "HEAD^{tree}")
    output = args.output.absolute()
    manifest_path = Path(str(output) + ".provenance.json")
    if output.exists() or manifest_path.exists():
        parser.error("output or provenance manifest already exists")
    if not output.parent.is_dir():
        parser.error("output directory must already exist")

    env = os.environ.copy()
    # Do not inherit an overlay, workspace, tags, or persistent Go env settings
    # that could replace committed source or alter the controlled build flags.
    env.update(GOFLAGS="", GOWORK="off", GOENV="off", GO111MODULE="on")
    with tempfile.TemporaryDirectory(prefix="paired-live-archive-", dir="/private/tmp") as temp:
        temporary = Path(temp)
        archive = temporary / "source.tar"
        subprocess.run(["git", "-C", str(repo), "archive", "--format=tar", "-o", str(archive), commit], check=True)
        source = temporary / "source"
        source.mkdir()
        with tarfile.open(archive) as contents:
            contents.extractall(source, filter="data")
        if (source / "go.mod").read_text().splitlines()[0] != "module github.com/frostloom/ai-gateway":
            parser.error("wrong source module")
        binary = temporary / "paired-live"
        command = ["go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false",
                   "-ldflags", f"-X=main.archivedCommit={commit} -X=main.archivedTree={tree}",
                   "-o", str(binary), "./cmd/paired-live"]
        subprocess.run(command, cwd=source, env=env, check=True)
        # Go silently ignores -X for missing symbols. Reject an older HEAD that
        # does not yet implement the archive provenance runtime guard.
        symbols = subprocess.check_output(["go", "tool", "nm", str(binary)], cwd=source, env=env).decode()
        names = {line.split()[-1] for line in symbols.splitlines() if line.split()}
        if not {"main.archivedCommit", "main.archivedTree"}.issubset(names):
            parser.error("committed paired-live lacks archive provenance support")
        if git(repo, "rev-parse", "HEAD") != commit or git(repo, "rev-parse", "HEAD^{tree}") != tree:
            parser.error("HEAD changed during archive build")
        manifest = {
            "schema": "paired-live-archive-build.v1",
            "commit": commit,
            "tree": tree,
            "source_archive_sha256": sha256(archive),
            "binary_sha256": sha256(binary),
            "sdkroot": env.get("SDKROOT", ""),
            "go_version": subprocess.check_output(["go", "version"], env=env).decode().strip(),
            "build_flags": "-mod=readonly -trimpath -buildvcs=false",
        }
        # Exclusive creation preserves previous runs, even if another builder
        # claimed these names while this one compiled.
        with output.open("xb") as destination:
            with binary.open("rb") as built:
                shutil.copyfileobj(built, destination)
        output.chmod(0o755)
        with manifest_path.open("x") as destination:
            json.dump(manifest, destination, indent=2, sort_keys=True)
            destination.write("\n")
    print(json.dumps({"binary": str(output), "provenance": str(manifest_path)}))


if __name__ == "__main__":
    main()
