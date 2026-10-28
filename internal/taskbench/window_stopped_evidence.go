package taskbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
)

// This exact stopped execution is the sole additional predecessor for v2.
// It cannot be substituted by caller claims, new totals or a successful replay.
func windowStoppedArtifactPins() map[string]string {
	return map[string]string{
		"actual-manifest.json":                                       "2d6ccad655fb19fb0a945b326a5a80eedf54d0b6a9bb15d220b097127568193e",
		"campaign/claim-000001.json":                                 "52bbf6e7edf8b66099141db5ac7c6782e45b8afc3bcd9145ea1fd51f936eca48",
		"campaign/ledger-000001/accounting.jsonl":                    "787ca17269827a924fce17baa8e475063436402fb6bed50e12ff97b6277d526f",
		"records/startup.json":                                       "94d263e8f7abd368f3ab1e5f6be25de0e12adaafb1a3b4689534433811ef73a1",
		"records/terminal.json":                                      "de9a3f0fdd79cb2aa3bf7559381792f4cdaae7958d559469a1974b7267e37b82",
		"records/window-000001-summary.json":                         "a075e2c631d46e2df68dd513dcc1dad43e0cd51e3653364eca4a5f053133417c",
		"records/window-000001/receipt.json":                         "2f7f73ad528bde066786a451cc32b52064b227b1afe9f08c1a57db9186e2858f",
		"records/window-000001/payload-manifest.json":                "ed93e54cd55ef6267cca8558167dbfcc33afb5f9942c379559a48926cf28a61f",
		"records/window-000001/arm-000001-model.json":                "df6c07efa4730641965eb8bbaf268aed4270be2f973e31c7a9c9c610d64a9b04",
		"records/window-000001/native-payloads/store.json":           "1f0bd4f72a0c340b408693ee4640bcbb0783f30b6dd4487487ad12b12f71f430",
		"records/window-000001/native-payloads/request-000001.json":  "4208b2a772da5fc39e99249c15c447d4a0b2915ac425fb686f1360b2e21c1efc",
		"records/window-000001/native-payloads/request-000001.body":  "d1829878060f53dc4c2e3d6daf9f6444bdf2d3a30014a7d1fe6add19df306b87",
		"records/window-000001/native-payloads/response-000001.json": "2feb5783b1d458facde7d0280301eb24f4814374d21e73bd9bf094fc4554ac47",
		"records/window-000001/native-payloads/response-000001.body": "255d829e95fad2ee0594f94c07227830e953b3c2eb8af8ae0114cb9cb669b140",
	}
}

func loadWindowStoppedBaseline(path, expectedSHA string) (windowStoppedBaseline, error) {
	var out windowStoppedBaseline
	raw, err := readWindowActualFile(path, expectedSHA, 65536)
	if err != nil {
		return out, err
	}
	var evidence windowStoppedEvidence
	if decodeWindowActualJSON(raw, &evidence) != nil || evidence.Schema != "full-window-stopped-baseline.v1" {
		return out, fmt.Errorf("stopped baseline descriptor rejected")
	}
	canonical, e := filepath.EvalSymlinks(evidence.OriginalRoot)
	if e != nil || !filepath.IsAbs(evidence.OriginalRoot) || filepath.Clean(evidence.OriginalRoot) != evidence.OriginalRoot || canonical != evidence.OriginalRoot {
		return out, fmt.Errorf("original stopped root must be canonical")
	}
	// Establish the exact old namespace before touching even its existing lock.
	manifestRaw, e := readWindowActualFile(filepath.Join(evidence.OriginalRoot, "actual-manifest.json"), windowStoppedArtifactPins()["actual-manifest.json"], 8<<20)
	var original windowActualManifest
	if e != nil || decodeWindowActualJSON(manifestRaw, &original) != nil || original.CampaignRoot != filepath.Join(evidence.OriginalRoot, "campaign") || original.SessionRoot != filepath.Join(evidence.OriginalRoot, "records") || original.ClaimsDir != evidence.HistoricalClaimsDir {
		return out, fmt.Errorf("stopped namespace differs from original manifest")
	}
	err = withWindowStoppedLock(filepath.Join(evidence.OriginalRoot, "campaign/.lock"), func() error {
		if err := validateWindowStoppedInventory(evidence.OriginalRoot); err != nil {
			return err
		}
		files, err := readWindowStoppedFiles(evidence.OriginalRoot, windowStoppedArtifactPins(), 8<<20)
		if err != nil {
			return err
		}
		b, err := decodeWindowStoppedBundle(files)
		if err != nil {
			return err
		}
		if b.manifest.CampaignRoot != filepath.Join(evidence.OriginalRoot, "campaign") || b.manifest.SessionRoot != filepath.Join(evidence.OriginalRoot, "records") || b.manifest.ClaimsDir != evidence.HistoricalClaimsDir {
			return fmt.Errorf("stopped roots differ from original frozen manifest")
		}
		pins := map[string]string{}
		for id, sha := range fullWindowCanonicalClaims() {
			pins[id+".json"] = sha
		}
		original, err := readWindowStoppedFiles(evidence.HistoricalClaimsDir, pins, 65536)
		if err != nil {
			return err
		}
		claims := map[string][]byte{}
		for id := range fullWindowCanonicalClaims() {
			claims[id] = original[id+".json"]
		}
		base, err := verifyFullWindowBaselineBytes(claims, fullWindowCanonicalClaims(), V5ReceiverRepeatedID)
		if err != nil {
			return err
		}
		next, liability, err := verifyWindowStoppedBundle(base, b)
		if err != nil {
			return err
		}
		if err = validateWindowStoppedInventory(evidence.OriginalRoot); err != nil {
			return err
		}
		out = windowStoppedBaseline{base: next, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: evidence.OriginalRoot, historicalDir: evidence.HistoricalClaimsDir, liabilityMicroCNY: liability}
		return nil
	})
	if err != nil {
		return windowStoppedBaseline{}, err
	}
	return out, nil
}

func readWindowStoppedFiles(root string, pins map[string]string, max int64) (map[string][]byte, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || canonical != root || max < 1 || max > 8<<20 || len(pins) < 1 || len(pins) > 32 {
		return nil, fmt.Errorf("bounded canonical stopped evidence root required")
	}
	files := map[string][]byte{}
	for name, sha := range pins {
		if name == "" || name == "." || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("invalid stopped evidence name")
		}
		raw, err := readWindowActualFile(filepath.Join(root, filepath.FromSlash(name)), sha, max)
		if err != nil {
			return nil, err
		}
		files[name] = raw
	}
	return files, nil
}

// No create, truncate or write flag. Acquiring the old owner's existing lock
// supplements its immutable terminal evidence; a JSON boolean alone is insufficient.
func withWindowStoppedLock(path string, verify func() error) (err error) {
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path || !filepath.IsAbs(path) || filepath.Clean(path) != path || verify == nil {
		return fmt.Errorf("original owner lock rejected")
	}
	before, e := os.Lstat(path)
	if e != nil || !before.Mode().IsRegular() || before.Size() != 0 {
		return fmt.Errorf("invalid original lock")
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { err = errors.Join(err, f.Close()) }()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || opened.Size() != 0 {
		return fmt.Errorf("original lock replaced")
	}
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fmt.Errorf("original owner still holds lock")
	}
	defer func() { err = errors.Join(err, syscall.Flock(fd, syscall.LOCK_UN)) }()
	err = verify()
	after, ae := os.Lstat(path)
	final, fe := f.Stat()
	if ae != nil || fe != nil || !os.SameFile(opened, after) || final.Size() != 0 {
		err = errors.Join(err, fmt.Errorf("original owner lock changed"))
	}
	return err
}

func decodeWindowStoppedBundle(raw map[string][]byte) (windowStoppedBundle, error) {
	b := windowStoppedBundle{raw: raw}
	for name, target := range map[string]any{
		"actual-manifest.json": &b.manifest, "campaign/claim-000001.json": &b.claim,
		"records/terminal.json": &b.terminal, "records/window-000001-summary.json": &b.summary,
		"records/window-000001/receipt.json": &b.report, "records/window-000001/payload-manifest.json": &b.payload,
	} {
		if err := decodeWindowActualJSON(raw[name], target); err != nil {
			return windowStoppedBundle{}, err
		}
	}
	audit, err := readWindowAccountingBytes(raw["campaign/ledger-000001/accounting.jsonl"])
	if err != nil {
		return windowStoppedBundle{}, err
	}
	b.audit = audit
	if len(b.payload.Packets) != 1 {
		return windowStoppedBundle{}, fmt.Errorf("single original packet required")
	}
	packet := b.payload.Packets[0]
	var req, res windowPayloadPacket
	if decodeWindowActualJSON(raw["records/window-000001/native-payloads/request-000001.json"], &req) != nil || decodeWindowActualJSON(raw["records/window-000001/native-payloads/response-000001.json"], &res) != nil {
		return windowStoppedBundle{}, fmt.Errorf("original packet descriptors rejected")
	}
	wantReq, wantRes := packet, packet
	wantReq.Response = nil
	wantRes.Request, wantRes.RequestDescriptorFile = windowPayloadBody{}, ""
	if !reflect.DeepEqual(req, wantReq) || !reflect.DeepEqual(res, wantRes) {
		return windowStoppedBundle{}, fmt.Errorf("original packet descriptors crossed")
	}
	return b, nil
}

func validateWindowStoppedInventory(root string) error {
	// An extra permanent claim cannot be silently excluded from the new baseline.
	dirs := map[string]map[string]bool{
		"campaign":                              {".lock": false, "claim-000001.json": false, "ledger-000001": true},
		"campaign/ledger-000001":                {"accounting.jsonl": false},
		"records":                               {"startup.json": false, "terminal.json": false, "window-000001-summary.json": false, "window-000001": true},
		"records/window-000001":                 {"receipt.json": false, "payload-manifest.json": false, "arm-000001-model.json": false, "native-payloads": true},
		"records/window-000001/native-payloads": {"store.json": false, "request-000001.json": false, "request-000001.body": false, "response-000001.json": false, "response-000001.body": false},
	}
	for dir, names := range dirs {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil || len(entries) != len(names) {
			return fmt.Errorf("stopped execution inventory changed")
		}
		for _, entry := range entries {
			wantDir, exists := names[entry.Name()]
			if !exists || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() != wantDir || !wantDir && !entry.Type().IsRegular() {
				return fmt.Errorf("unknown stopped execution file")
			}
		}
	}
	return nil
}
