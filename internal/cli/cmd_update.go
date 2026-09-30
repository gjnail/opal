package cli

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"opal/internal/config"
)

// SourceDir is the checkout this binary was built from, stamped in by the
// installers (-ldflags "-X opal/internal/cli.SourceDir=..."), so `opal update`
// can rebuild from it.
var SourceDir = ""

func exeName() string {
	if runtime.GOOS == "windows" {
		return "opal.exe"
	}
	return "opal"
}

// DefaultRepo is where releases come from unless [update] repo says otherwise.
const DefaultRepo = "gjnail/opal"

// cmdUpdate upgrades opal in place. A binary built from a checkout rebuilds
// from it (git pull + go build); anything else, or --release, downloads the
// latest GitHub release.
func cmdUpdate(args []string) int {
	u := newUI()
	cleanupOldBinary()
	from, release := "", false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--from" && i+1 < len(args):
			from = args[i+1]
			i++
		case args[i] == "--release":
			release = true
		}
	}
	cfg, _ := config.Load()
	repo := cfg.Update.Repo
	if repo == "" {
		repo = DefaultRepo
	}
	if from == "" && !release && SourceDir != "" {
		if _, err := os.Stat(filepath.Join(SourceDir, "go.mod")); err == nil {
			from = SourceDir
		}
	}

	tmpDir, err := os.MkdirTemp("", "opal-update-")
	if err != nil {
		errorf("%v", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)
	newBin := filepath.Join(tmpDir, exeName())

	switch {
	case from != "":
		if _, err := os.Stat(filepath.Join(from, "go.mod")); err != nil {
			errorf("%s isn't an opal checkout", from)
			return 1
		}
		if _, err := os.Stat(filepath.Join(from, ".git")); err == nil {
			fmt.Printf("  %s git pull in %s\n", u.muted("→"), from)
			if err := runGit(from, "pull", "--ff-only", "--quiet"); err != nil {
				fmt.Printf("  %s pull failed (%v); building what's there\n", u.warn("!"), err)
			}
		}
		fmt.Printf("  %s go build\n", u.muted("→"))
		// Quoted inside -ldflags: checkout paths often contain spaces.
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-X 'opal/internal/cli.SourceDir="+from+"'", "-o", newBin, ".")
		cmd.Dir = from
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			errorf("build failed: %v", err)
			return 1
		}
	default:
		tag, err := latestTag(repo)
		if err != nil {
			errorf("checking %s: %v", repo, err)
			return 1
		}
		if strings.TrimPrefix(tag, "v") == Version {
			fmt.Printf("  %s already on the latest release (%s)\n", u.ok("✓"), tag)
			return 0
		}
		fmt.Printf("  %s downloading %s\n", u.muted("→"), tag)
		if err := downloadRelease(repo, newBin); err != nil {
			errorf("%v", err)
			return 1
		}
	}

	out, err := exec.Command(newBin, "version").Output()
	if err != nil {
		errorf("the new build doesn't run: %v", err)
		return 1
	}
	if err := replaceSelf(newBin); err != nil {
		errorf("installing the new binary: %v", err)
		return 1
	}
	fmt.Printf("  %s %s %s\n", u.ok("✓"), strings.TrimSpace(string(out)), u.muted("(open a new tab, or run reload)"))
	return 0
}

// replaceSelf swaps the running binary for newBin. Windows can't overwrite a
// running .exe but can rename it, so the old one steps aside and is deleted
// on a later run.
func replaceSelf(newBin string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := self + ".old"
		os.Remove(old)
		if err := os.Rename(self, old); err != nil {
			return err
		}
		if err := copyFile(newBin, self, 0o755); err != nil {
			os.Rename(old, self)
			return err
		}
		return nil
	}
	tmp := self + ".new"
	if err := copyFile(newBin, tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, self)
}

// cleanupOldBinary deletes the .old left by a previous Windows update.
func cleanupOldBinary() {
	if self, err := os.Executable(); err == nil {
		os.Remove(self + ".old")
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

func latestTag(repo string) (string, error) {
	resp, err := httpClient.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub says %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.Tag, nil
}

func fetch(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// downloadRelease fetches this platform's archive from the latest release,
// checks it against SHA256SUMS.txt, and extracts the binary to dest. A
// release without a matching checksum is refused.
func downloadRelease(repo, dest string) error {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	name := fmt.Sprintf("opal_%s_%s.%s", runtime.GOOS, runtime.GOARCH, ext)
	base := "https://github.com/" + repo + "/releases/latest/download/"
	archive, err := fetch(base + name)
	if err != nil {
		return err
	}
	sums, err := fetch(base + "SHA256SUMS.txt")
	if err != nil {
		return fmt.Errorf("can't check the download: %w", err)
	}
	sum := sha256.Sum256(archive)
	want := ""
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = f[0]
		}
	}
	if want == "" || !strings.EqualFold(want, hex.EncodeToString(sum[:])) {
		return errors.New("checksum mismatch; not installing " + name)
	}
	var bin io.Reader
	if ext == "zip" {
		zr, err := zip.NewReader(strings.NewReader(string(archive)), int64(len(archive)))
		if err != nil {
			return err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == exeName() {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				bin = rc
				break
			}
		}
	} else {
		gz, err := gzip.NewReader(strings.NewReader(string(archive)))
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			if filepath.Base(h.Name) == exeName() && h.Typeflag == tar.TypeReg {
				bin = tr
				break
			}
		}
	}
	if bin == nil {
		return fmt.Errorf("%s has no %s inside", name, exeName())
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, bin); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
