package repotracker

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"xrc/base/go/log"
)

var (
	cacheDir     = os.TempDir()
	pathUnsafeRE = regexp.MustCompile(`[^\w]+`)
)

// Changelog stores a repo's information and its commit history
type Changelog struct {
	Rule RepoRule
	Repo *GitRepo
	Log  []*Commit
}

func (cl *Changelog) FormatRule()

// TagURL gets the url of tags
func (cl *Changelog) TagURL(tag string) string {
	return cl.Repo.URL + "/releases/tag/" + tag
}

// CommitURL gets the url of commits
func (cl *Changelog) CommitURL(hash string) string {
	return cl.Repo.URL + "/commit/" + hash
}

// Commit defines the storage structure of single commit log.
type Commit struct {
	Hash  string
	Tags  []string
	Date  time.Time
	Title string
}

// NewCommits returns the commits since the rev passed to NewGitRepo to the lastest commit.
// It's a convenient substitution of calling CheckUpdate(rev, "HEAD") and CommitLog(rev, "HEAD")
func NewCommits(rev, url string) (*Changelog, error) {
	r, err := NewGitRepo(rev, url)
	if err != nil {
		log.Errorf("%s: Failed to generate a new gitRepo: %v", url, err)
		return nil, err
	}

	p, err := r.CheckUpdate(r.Revision, "HEAD")
	if err != nil {
		log.Errorf("%s: Failed to check if it has been updated: %v", r, err)
		return nil, err
	}
	if !p {
		return nil, nil
	}

	commits, err := r.CommitLog(r.Revision, "HEAD")
	if err != nil {
		log.Errorf("%s: Failed to retrieve the commit log: %v", r, err)
		return nil, err
	}

	return &Changelog{r, commits}, nil
}

// GitRepo saves the url, the local cache directory and current revision of a git repository
type GitRepo struct {
	Revision string
	URL      string
	Dir      string
}

// NewGitRepo creates new GitRepo based on revision and url
func NewGitRepo(rev, url string) (*GitRepo, error) {
	r := &GitRepo{Revision: rev, URL: url}
	if err := r.ensureCacheDir(); err != nil {
		return nil, err
	}
	if err := r.updateCache(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *GitRepo) ensureCacheDir() error {
	u, err := url.Parse(r.URL)
	if err != nil {
		return err
	}
	hostName := pathUnsafeRE.ReplaceAllString(u.Host, "_")
	repoName := pathUnsafeRE.ReplaceAllString(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git"), "_")
	dir := filepath.Join(cacheDir, hostName, repoName)
	log.Infof("%s: Cache directory is %q", r, dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	r.Dir = dir
	return nil
}

func (r *GitRepo) String() string {
	return r.URL
}

func (r *GitRepo) runGit(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("`git %v` failed with output %q: %v", args, out, err)
	}
	return out, nil
}

func (r *GitRepo) updateCache() error {
	// check if we need to pull or clone
	isEmpty, err := isDirEmpty(r.Dir)
	if err != nil {
		return err
	}
	if isEmpty {
		if _, err = r.runGit("clone", r.URL, "--quiet", "."); err != nil {
			return err
		}
		return nil
	}
	if _, err = r.runGit("pull", "origin", "master"); err != nil {
		return err
	}
	return nil
}

func isDirEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	// read in only one file, if the file is EOF then the dir is empty.
	if _, err = f.Readdir(1); err == io.EOF {
		return true, nil
	}
	return false, err
}

// CheckUpdate checks if <fromCommit> is an ancestor of <toCommit>.
func (r *GitRepo) CheckUpdate(fromCommit, toCommit string) (hasUpdate bool, _ error) {
	if _, err := r.runGit("merge-base", "--is-ancestor", fromCommit, toCommit); err != nil {
		log.Errorf("%s: Failed to compare the current version to the latest one: %v", r, err)
		return false, err
	}
	return true, nil
}

// CommitLog obtains the change log between two commits.
func (r *GitRepo) CommitLog(fromCommit, toCommit string) ([]*Commit, error) {
	output, err := r.runGit("log",
		"--pretty=format:%H%x00%D%x00%ct%x00%s",
		"--decorate=short",
		"--decorate-refs=refs/tags",
		fromCommit+".."+toCommit)
	if err != nil {
		log.Errorf("%s: Failed to get the commit log: %v", r, err)
		return nil, err
	}
	cl, err := r.parseChangelog(bytes.NewReader(output))
	if err != nil {
		log.Errorf("%s: Failed to parse the commit log: %v", r, err)
		return nil, err
	}
	return cl, nil
}

// parseChangelog parses lines of changelog in the following format:
// $hash "\0 $refs "\0" $time "\0" $summary
func (r *GitRepo) parseChangelog(rd io.Reader) ([]*Commit, error) {
	scn := bufio.NewScanner(rd)

	var cs []*Commit
	for scn.Scan() {
		line := scn.Text()
		// the split result would be wrong if any field contains "\0"
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("line %q is in a wrong format", line)
		}
		ts, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			log.Errorf("Failed to parse time: %v", err)
			return nil, err
		}
		var t []string
		if parts[1] != "" {
			t = strings.Split(parts[1], ", ")
			for i := range t {
				t[i] = strings.TrimPrefix(t[i], "tag: ")
			}
		}
		cs = append(cs, &Commit{
			Hash:  parts[0],
			Tags:  t,
			Date:  time.Unix(ts, 0),
			Title: parts[3],
		})
	}

	if err := scn.Err(); err != nil {
		return nil, err
	}
	return cs, nil
}
