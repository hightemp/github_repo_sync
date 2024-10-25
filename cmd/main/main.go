package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/google/go-github/v45/github"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v2"
)

type Config struct {
	GithubToken  string `yaml:"github_token"`
	GithubUser   string `yaml:"github_user"`
	ReposDir     string `yaml:"repos_dir"`
	PollInterval string `yaml:"poll_interval"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func updateRepo(repoPath string, repo *github.Repository) error {
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		log.Printf("Cloning %s...", *repo.Name)
		_, err := git.PlainClone(repoPath, false, &git.CloneOptions{
			URL: *repo.CloneURL,
		})
		if err != nil {
			return fmt.Errorf("failed to clone repository: %v", err)
		}
	} else {
		log.Printf("Pulling updates for %s...", *repo.Name)
		r, err := git.PlainOpen(repoPath)
		if err != nil {
			return fmt.Errorf("failed to open repository: %v", err)
		}

		w, err := r.Worktree()
		if err != nil {
			return fmt.Errorf("failed to get worktree: %v", err)
		}

		err = w.Pull(&git.PullOptions{})
		if err == git.NoErrAlreadyUpToDate {
			log.Printf("Repository %s is already up to date", *repo.Name)
		} else if err != nil {
			return fmt.Errorf("failed to pull repository: %v", err)
		}
	}
	return nil
}

func syncRepos(config *Config) error {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.GithubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	if err := os.MkdirAll(config.ReposDir, 0755); err != nil {
		return fmt.Errorf("failed to create repos directory: %v", err)
	}

	repos, _, err := client.Repositories.List(ctx, config.GithubUser, nil)
	if err != nil {
		return fmt.Errorf("failed to get repositories list: %v", err)
	}

	for _, repo := range repos {
		repoPath := filepath.Join(config.ReposDir, *repo.Name)
		if err := updateRepo(repoPath, repo); err != nil {
			log.Printf("Error processing repository %s: %v", *repo.Name, err)
		}
	}

	return nil
}

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	interval, err := time.ParseDuration(config.PollInterval)
	if err != nil {
		log.Fatalf("Invalid poll interval: %v", err)
	}

	log.Printf("Starting repository sync service...")
	log.Printf("Repositories will be stored in: %s", config.ReposDir)
	log.Printf("Polling interval: %s", interval)

	for {
		if err := syncRepos(config); err != nil {
			log.Printf("Error during sync: %v", err)
		}
		time.Sleep(interval)
	}
}
