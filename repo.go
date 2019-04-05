package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"xrc/base/go/log"
	"xrc/buildtools/repotracker"

	"github.com/golang/protobuf/proto"
	"golang.org/x/tools/go/vcs"

	bpb "google/devtools/build/lib/query2/proto/build_proto"
)

func readQueryResult(file string) (*bpb.QueryResult, error) {
	var r io.Reader
	if file == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}

	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var qr bpb.QueryResult
	if err := proto.Unmarshal(b, &qr); err != nil {
		return nil, err
	}
	return &qr, nil
}

type repoRule interface {
	Name() string
	Git() (*gitPackageInfo, error)
}

type rules []repoRule

type reposInfo struct {
	Go   []*goPackageInfo
	Git  []*gitPackageInfo
	HTTP []*httpPackageInfo
}

func loadRepos(file string) ([]repoRule, error) {
	qr, err := readQueryResult(file)
	if err != nil {
		log.Errorf("Failed to retrieve the bazel query result: %v", err)
		return nil, err
	}
	var rules []repoRule
	for _, t := range qr.GetTarget() {
		if t.GetType() != bpb.Target_RULE {
			continue
		}
		rules = append(rules, resolveRule(t.GetRule()))
	}
	return &r, nil
}

func resolveGoPackageRule(rl *bpb.Rule) *goPackageInfo {

}

func resolveRule(rl *bpb.Rule) repoRule {
	switch rl.GetRuleClass() {
	case "go_repository":
		var g goPackageInfo
		for _, a := range rl.GetAttribute() {
			switch a.GetName() {
			case "importpath":
				g.ImportPath = a.GetStringValue()
			case "tag", "commit":
				if value := a.GetStringValue(); value != "" {
					g.Revision = value
				}
			}
		}
		return g
	case "git_repository":
		g := &gitPackageInfo{
			Name: rl.GetName(),
		}
		for _, a := range rl.GetAttribute() {
			switch a.GetName() {
			case "remote":
				g.URL = a.GetStringValue()
			case "tag", "commit":
				if value := a.GetStringValue(); value != "" {
					g.Revision = value
				}
			}
		}
		return g
	case "http_archive", "new_http_archive":
		var h httpPackageInfo
		h.Name = rl.GetName()
		for _, a := range rl.GetAttribute() {
			switch a.GetName() {
			case "url":
				if value := a.GetStringValue(); value != "" {
					h.URLs = []string{value}
					break
				}
			case "urls":
				if value := a.GetStringListValue(); len(value) > 0 {
					h.URLs = value
					break
				}
			}
		}
		return h
	}
}

type gitPackageInfo struct {
	Revision string
	URL      string
}

func (gr *gitPackageInfo) String() string {
	return gr.URL
}

type goPackageInfo struct {
	Revision   string
	ImportPath string
}

func (g *goPackageInfo) String() string {
	return g.ImportPath
}

// GoToGit converts a go package to git format
func (g *goPackageInfo) Git() (*gitPackageInfo, error) {
	rr, err := vcs.RepoRootForImportPath(g.ImportPath, false)
	if err != nil {
		log.Errorf("%s: %v", g, err)
		return nil, err
	}
	if rr.VCS.Cmd != "git" {
		return nil, fmt.Errorf("resolved repo uses an unsupported VCS %q; want git", rr.VCS.Cmd)
	}
	return &gitPackageInfo{Revision: g.Revision, URL: rr.Repo}, nil
}

var (
	// /google/containerregistry/archive/v0.0.27.tar.gz
	// /bazelbuild/bazel-gazelle/releases/download/0.10.1/bazel-gazelle-0.10.1.tar.gz
	githubRE = regexp.MustCompile(`^/([^/]+)/([^/]+)/(?:archive|releases/download)/([^/]+)(?:\.(?:tar\.gz|zip)|/[^/]+)$`)
	// /golang/tools/zip/5d2fd3ccab986d52112bf301d47a819783339d0e
	githubCodeloadRE = regexp.MustCompile(`^/([^/]+)/([^/]+)/(?:[^/]+)/(.+)$`)
)

var httpGitMapper = map[string]func(u *url.URL) (*gitPackageInfo, error){
	"github.com": func(u *url.URL) (*gitPackageInfo, error) {
		m := githubRE.FindStringSubmatch(u.Path)
		if len(m) == 0 {
			return nil, errors.New("unsupported url")
		}
		return &gitPackageInfo{
			URL:      fmt.Sprintf("https://github.com/%s/%s", m[1], m[2]),
			Revision: m[3],
		}, nil
	},
	"codeload.github.com": func(u *url.URL) (*gitPackageInfo, error) {
		m := githubRE.FindStringSubmatch(u.Path)
		if len(m) == 0 {
			return nil, errors.New("unsupported url")
		}
		return &gitPackageInfo{
			URL:      fmt.Sprintf("https://github.com/%s/%s", m[1], m[2]),
			Revision: m[3],
		}, nil
	},
}

type httpPackageInfo struct {
	URLs []string
	Name string
}

func (h *httpPackageInfo) String() string {
	return h.Name
}

// HTTPToGit converts a http url to git repo format
func (h *httpPackageInfo) HTTPToGit() (*gitPackageInfo, error) {
	for _, e := range h.URLs {
		u, err := url.Parse(e)
		if err != nil {
			log.Warningf("%s: %v", h, err)
			continue
		}
		m, ok := httpGitMapper[u.Host]
		if !ok {
			log.Warningf("%s: Unprocessable hostname in %q, continue.", h, u)
			continue
		}
		g, err := m(u)
		if err != nil {
			log.Warningf("%s: Failed to extract git repo from %q, continue: %v", h, u, err)
			continue
		}
		return g, nil
	}
	return nil, fmt.Errorf("no valid url")
}

func writeToFile(cl *repotracker.Changelog) error {
	filedir := filepath.Join(cl.Repo.Dir, "report.md")
	f, err := os.Create(filedir)
	if err != nil {
		log.Errorf("%s: Failed to create a commit log file: %v", cl.Repo, err)
		return err
	}
	if err = repotracker.TextReport(f, cl); err != nil {
		f.Close()
		log.Errorf("%s: Failed to generate text report:", cl.Repo, err)
		return err
	}
	return f.Close()
}
