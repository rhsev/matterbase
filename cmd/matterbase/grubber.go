// grubber subprocess integration — port of matterbase's grubber_client.py.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	bkexec "github.com/rhsev/matterbase/basekit/exec"
)

// minGrubberVersion is the release with --merge-on (annotation +
// collection-index records collapse inside grubber).
var minGrubberVersion = [3]int{0, 12, 0}

// The (id, binder) identity of fileregister collection records, and the
// array field exploded into per-membership rows before the merge.
const (
	collectionMergeKeys    = "id,binder"
	collectionExplodeField = "binder"
)

// grubberBin resolves the grubber binary: $GRUBBER override, else
// "grubber" on PATH.
func grubberBin() string {
	if v := os.Getenv("GRUBBER"); v != "" {
		return v
	}
	return "grubber"
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

func parseVersion(text string) ([3]int, bool) {
	m := versionRe.FindStringSubmatch(text)
	if m == nil {
		return [3]int{}, false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch := 0
	if m[3] != "" {
		patch, _ = strconv.Atoi(m[3])
	}
	return [3]int{major, minor, patch}, true
}

func versionAtLeast(v, min [3]int) bool {
	for i := 0; i < 3; i++ {
		if v[i] != min[i] {
			return v[i] > min[i]
		}
	}
	return true
}

// checkGrubberVersion probes `grubber --version`. Returns (meetsMinimum,
// versionOrReason).
func checkGrubberVersion() (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, grubberBin(), "--version").Output()
	if err != nil {
		return false, "not found"
	}
	text := strings.TrimSpace(string(out))
	parsed, ok := parseVersion(text)
	if !ok {
		if text == "" {
			return false, "unknown"
		}
		return false, text
	}
	return versionAtLeast(parsed, minGrubberVersion), text
}

// runGrubberCmd runs a grubber command and returns the parsed JSON
// records. onError, if set, is called when the output can't be parsed.
func runGrubberCmd(args []string, arrayFields []string, onError func(string)) []Record {
	env := os.Environ()
	if len(arrayFields) > 0 {
		env = append(env, "GRUBBER_ARRAY_FIELDS="+strings.Join(arrayFields, ","))
	}
	out, err := bkexec.Run(context.Background(), 15*time.Second, env, args[0], args[1:]...)
	if err != nil {
		if onError != nil {
			onError(err.Error())
		}
		return nil
	}
	var records []Record
	if err := json.Unmarshal(out, &records); err != nil {
		if onError != nil {
			onError(fmt.Sprintf("grubber: invalid JSON output (%s)", err))
		}
		return nil
	}
	return records
}

var streamModeFlag = map[string]string{
	"frontmatter": "--frontmatter-only",
	"blocks_only": "--blocks-only",
}

// findCollectionDir returns <notesDir>/collections if it contains at
// least one *.jsonl file, "" otherwise — callers skip the merge step
// entirely when the collection index is absent.
func findCollectionDir(notesDir string) string {
	colDir := filepath.Join(notesDir, "collections")
	info, err := os.Stat(colDir)
	if err != nil || !info.IsDir() {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(colDir, "*.jsonl"))
	if len(matches) > 0 {
		return colDir
	}
	return ""
}

// ExtractOpts controls extractToJSONL. The zero value scans everything
// ("-a"), no depth limit, no mmd, no collection merge.
type ExtractOpts struct {
	SearchMode    string
	MMD           bool
	Depth         *int
	CollectionDir string
}

// extractToJSONL scans notesDir once and writes the full record set to
// outPath as JSONL — the only path that touches files on disk; query
// changes replay this cache instead (queryCachedRecords).
func extractToJSONL(notesDir, outPath string, opts ExtractOpts, onError func(string)) bool {
	modeFlag, ok := streamModeFlag[opts.SearchMode]
	if !ok {
		modeFlag = "-a"
	}
	args := []string{"extract", notesDir, modeFlag, "--format=jsonl"}
	if opts.Depth != nil {
		args = append(args, "--depth", strconv.Itoa(*opts.Depth))
	}
	if opts.MMD {
		args = append(args, "--mmd")
	}
	if opts.CollectionDir != "" {
		args = append(args, "--from-jsonl", opts.CollectionDir,
			"--explode", collectionExplodeField, "--merge-on", collectionMergeKeys)
	}
	out, err := bkexec.Run(context.Background(), 30*time.Second, nil, grubberBin(), args...)
	if err != nil {
		if onError != nil {
			onError(err.Error())
		}
		return false
	}
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		if onError != nil {
			onError("grubber: " + err.Error())
		}
		return false
	}
	return true
}

// queryCachedRecords replays the in-session cache through grubber with
// optional -f filters — all expressions in a single grubber call
// (record-level AND), exactly what the yankable command reproduces.
func queryCachedRecords(cachePath string, expressions []string, arrayFields []string, onError func(string)) []Record {
	args := []string{grubberBin(), "extract", "--from-jsonl", cachePath}
	for _, expr := range expressions {
		args = append(args, "-f", expr)
	}
	records := runGrubberCmd(args, arrayFields, onError)
	if records == nil {
		return []Record{}
	}
	return records
}
