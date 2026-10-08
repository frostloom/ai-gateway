"""Verify frozen report bytes and recorded metrics; Python standard library only."""
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
    repository = directory.parents[2]
    manifest = json.loads((directory / "manifest.json").read_bytes())
    reports = {}
    for entry in manifest["files"]:
        compressed = (directory / entry["archive"]).read_bytes()
        require(sha256(compressed) == entry["archive_sha256"], "archive hash mismatch: " + entry["archive"])
        raw = gzip.decompress(compressed)
        require(sha256(raw) == entry["source_sha256"], "source hash mismatch: " + entry["archive"])
        require(len(raw) == entry["source_bytes"], "source size mismatch: " + entry["archive"])
        require(compressed[4:8] == bytes(4), "gzip timestamp is not zero")
        if check_sources:
            require((repository / entry["source"]).read_bytes() == raw, "original differs: " + entry["source"])
        if entry["source"].endswith(".json"):
            reports[entry["version"]] = json.loads(raw)
        print("PASS " + entry["archive"])
    for version, metadata in manifest["runs"].items():
        report = reports[version]
        for field in ("SchemaVersion", "JevMode", "Model", "DatasetSHA256", "Workers", "Mode", "StartedAt", "Duration"):
            require(report[field] == metadata[field], version + " metadata mismatch: " + field)
        require(report["Metrics"] == metadata["Metrics"], version + " metrics mismatch")
        for field, count_field in (("Preds", "normal_predictions"), ("AdvPreds", "adversarial_predictions")):
            require(len(report[field]) == metadata[count_field], version + " prediction count mismatch")
        normal = report["Preds"]
        failed = sum(p["HTTPCode"] != 200 or bool(p["Err"]) for p in normal)
        require(failed == report["Metrics"]["failed"], version + " failure count mismatch")
        correct = sum(matched(p, False) for p in normal)
        reached = sum(matched(p, True) for p in normal)
        require(correct == report["Metrics"]["correct"], version + " Top-1 count mismatch")
        require(reached == report["Metrics"]["reach"], version + " Reach count mismatch")
        print(f"PASS {version}: normal={len(normal)}, adversarial={len(report['AdvPreds'])}, failed={failed}, correct={correct}, reach={reached}")
    require([p["Sample"] for p in reports["v1"]["Preds"]] == [p["Sample"] for p in reports["v2"]["Preds"]], "normal samples differ")
    require([p["Sample"] for p in reports["v1"]["AdvPreds"]] == [p["Sample"] for p in reports["v2"]["AdvPreds"]], "adversarial samples differ")
    print("PASS: 5 frozen files, all hashes, metadata, failure records and paired samples verified")


def matched(prediction, reach):
    if prediction["HTTPCode"] != 200 or prediction["Err"] or prediction["JevBlocked"]:
        return False
    intent = prediction["Sample"]["intent"]
    if intent == "chitchat":
        return not prediction["AllTools"] and not prediction["Predicted"]
    return intent in (prediction["AllTools"] or []) if reach else prediction["Predicted"] == intent


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check-sources", action="store_true", help="also compare ignored original reports when locally available")
    args = parser.parse_args()
    try:
        verify(args.check_sources)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("FAIL: " + str(error), file=sys.stderr)
        sys.exit(1)
