"""Verify experiment and adjacent behavior archives offline using only stdlib."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import sys


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def verify(check_sources=False):
    directory = Path(__file__).resolve().parent
    root = directory.parents[2]
    manifest = json.loads((directory / "manifest.json").read_bytes())
    total_bytes = 0
    for entry in manifest["files"]:
        compressed = (directory / entry["archive"]).read_bytes()
        require(sha256(compressed) == entry["archive_sha256"], "archive hash mismatch: " + entry["archive"])
        require(compressed[4:8] == bytes(4), "gzip timestamp must be zero")
        raw = gzip.decompress(compressed)
        require(len(raw) == entry["source_bytes"], "source size mismatch")
        require(sha256(raw) == entry["source_sha256"], "source hash mismatch: " + entry["source"])
        if check_sources:
            require((root / entry["source"]).read_bytes() == raw, "original bytes differ: " + entry["source"])
        if "sidecar" in entry:
            sidecar = (directory / entry["sidecar"]).read_bytes()
            require(sha256(sidecar) == entry["sidecar_sha256"], "sidecar hash mismatch")
            metadata = json.loads(sidecar)
            for field in ("source", "source_bytes", "source_sha256", "archive_sha256"):
                require(metadata[field] == entry[field], "sidecar metadata mismatch")
        total_bytes += len(compressed)
        print("PASS " + entry["archive"])
    for entry in manifest["supporting_files"]:
        require(sha256((directory / entry["path"]).read_bytes()) == entry["sha256"], "supporting file hash mismatch")
    print(f"PASS {len(manifest['files'])} byte-exact archives; compressed bytes={total_bytes}; supporting provenance and summaries verified")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check-sources", action="store_true")
    args = parser.parse_args()
    try:
        verify(args.check_sources)
    except (OSError, ValueError, AssertionError, KeyError) as error:
        print("FAIL: " + str(error), file=sys.stderr)
        sys.exit(1)
