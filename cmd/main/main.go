package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v45/github"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v2"
)

type Config struct {
	GithubToken   string `yaml:"github_token"`
	GithubUser    string `yaml:"github_user"`
	ReposDir      string `yaml:"repos_dir"`
	PollInterval  string `yaml:"poll_interval"`
	WorkerCount   int    `yaml:"worker_count"`
	WorkQueueSize int    `yaml:"queue_size"`
}

type RepoTask struct {
	Repo     *github.Repository
	RepoPath string
}

type Worker struct {
	id          int
	client      *github.Client
	tasksChan   chan RepoTask
	wg          *sync.WaitGroup
	config      *Config
	rateLimiter *time.Ticker
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := Config{
		WorkerCount:   5,
		WorkQueueSize: 100,
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func newWorker(id int, client *github.Client, tasksChan chan RepoTask, wg *sync.WaitGroup, config *Config) *Worker {
	return &Worker{
		id:          id,
		client:      client,
		tasksChan:   tasksChan,
		wg:          wg,
		config:      config,
		rateLimiter: time.NewTicker(time.Second / 10),
	}
}

func (w *Worker) start(ctx context.Context) {
	go func() {
		for {
			select {
			case task, ok := <-w.tasksChan:
				if !ok {
					return
				}
				<-w.rateLimiter.C
				if err := w.processRepo(task); err != nil {
					log.Printf("Worker %d: Error processing repository %s: %v",
						w.id, *task.Repo.Name, err)
				}
				w.wg.Done()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *Worker) processRepo(task RepoTask) error {
	auth := &http.BasicAuth{
		Username: "git",
		Password: w.config.GithubToken,
	}

	if _, err := os.Stat(task.RepoPath); os.IsNotExist(err) {
		log.Printf("Worker %d: Cloning %s...", w.id, *task.Repo.Name)
		_, err := git.PlainClone(task.RepoPath, false, &git.CloneOptions{
			URL:  *task.Repo.CloneURL,
			Auth: auth,
		})
		if err != nil {
			return fmt.Errorf("failed to clone repository: %v", err)
		}
	} else {
		log.Printf("Worker %d: Pulling updates for %s...", w.id, *task.Repo.Name)
		r, err := git.PlainOpen(task.RepoPath)
		if err != nil {
			return fmt.Errorf("failed to open repository: %v", err)
		}

		w, err := r.Worktree()
		if err != nil {
			return fmt.Errorf("failed to get worktree: %v", err)
		}

		err = w.Pull(&git.PullOptions{
			Auth: auth,
		})
		if err == git.NoErrAlreadyUpToDate {
			log.Printf("Repository %s is already up to date", *task.Repo.Name)
		} else if err != nil && err != git.ErrNonFastForwardUpdate {
			return fmt.Errorf("failed to pull repository: %v", err)
		}
	}
	return nil
}

func syncRepos(ctx context.Context, config *Config) error {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.GithubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	if err := os.MkdirAll(config.ReposDir, 0755); err != nil {
		return fmt.Errorf("failed to create repos directory: %v", err)
	}

	tasksChan := make(chan RepoTask, config.WorkQueueSize)
	defer close(tasksChan)
	var wg sync.WaitGroup

	for i := 0; i < config.WorkerCount; i++ {
		worker := newWorker(i, client, tasksChan, &wg, config)
		worker.start(ctx)
	}

	opt := &github.RepositoryListOptions{
		Visibility:  "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	totalReposCnt := 0

	for {
		repos, resp, err := client.Repositories.List(ctx, "", opt)
		if err != nil {
			return fmt.Errorf("failed to get repositories list: %v", err)
		}

		totalReposCnt += len(repos)

		for _, repo := range repos {
			repoPath := filepath.Join(config.ReposDir, *repo.Name)
			wg.Add(1)
			tasksChan <- RepoTask{
				Repo:     repo,
				RepoPath: repoPath,
			}
		}

		if resp.NextPage == 0 {
			fmt.Printf("[!] Found %d repositories\n", totalReposCnt)
			break
		}
		opt.Page = resp.NextPage
	}

	wg.Wait()
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
	log.Printf("Using %d workers with queue size %d", config.WorkerCount, config.WorkQueueSize)
	log.Printf("Polling interval: %s", interval)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	errorChan := make(chan error, 1)
	defer close(errorChan)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if err := syncRepos(ctx, config); err != nil {
					errorChan <- err
					return
				}
				log.Println("Syncing repos finished")
				time.Sleep(interval)
			}
		}
	}()

	select {
	case <-sigChan:
		log.Println("Received shutdown signal. Finishing current tasks...")
		cancel()
	case err := <-errorChan:
		log.Printf("Error during sync: %v", err)
		cancel()
	}

	log.Println("Service stopped")
}
