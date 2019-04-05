package main

import (
	"sync"

	"xrc/base/go/flag"
	"xrc/base/go/log"
	"xrc/base/go/xrc"
	"xrc/buildtools/repotracker"
)

var (
	file           = "-"
	maxConcurrency = 20
)

func init() {
	flag.String(&file, "file", "A file contains the output of `bazel query --output=proto //external:all`. If the given file is '-', read from stdin instead.")
	flag.Int(&maxConcurrency, "concurrency", "Max number of go routines to run when checking updates.")
}

func main() {
	xrc.Init()
	defer xrc.Flush()

	// config -> repoinfo -> changelog -> report
	r, err := loadRepos(file)
	if err != nil {
		log.Errorf("Unable to get repo information: %v", err)
		return
	}

	for _, gr := range r {
		cl, err := repotracker.NewCommits(gr.Revision, gr.URL)
		if err != nil {
			log.Errorf("%s: Failed to check update: %v", gr, err)
			continue
		}
		if err := writeToFile(cl); err != nil {
			log.Errorf("%s: Failed to be written into a markdown file: %v", r, err)
			continue
		}
		log.Infof("%s: Finish checking update.", gr)
	}

	gitRepos := make(chan *gitPackageInfo)
	go func(r *reposInfo) {
		// TODO(chenchun): make this concurrent.
		for _, g := range r.Go {
			gr, err := g.GoToGit()
			if err != nil {
				log.Errorf("%s: Failed to resolve go import path to repo: %v", g, err)
				continue
			}
			gitRepos <- gr
		}
		for _, h := range r.HTTP {
			gr, err := h.HTTPToGit()
			if err != nil {
				log.Errorf("%s: Failed to resolve http urls to git repo: %v", h, err)
				continue
			}
			gitRepos <- gr
		}
		for _, gr := range r.Git {
			gitRepos <- gr
		}
		close(gitRepos)
	}(r)

	var wg sync.WaitGroup
	for worker := 0; worker < maxConcurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for gr := range gitRepos {
				log.Infof("%s: Start checking update.", gr)
				cl, err := repotracker.NewCommits(gr.Revision, gr.URL)
				if err != nil {
					log.Errorf("%s: Failed to check update: %v", gr, err)
					continue
				}
				if err := writeToFile(cl); err != nil {
					log.Errorf("%s: Failed to be written into a markdown file: %v", r, err)
					continue
				}
				log.Infof("%s: Finish checking update.", gr)
			}
		}()
	}
	wg.Wait()
	log.Info("Have finished all checks.")
}
