package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/newtoallofthis/probe/internal/agent"
	"github.com/newtoallofthis/probe/internal/config"
	"github.com/newtoallofthis/probe/internal/history"
	"github.com/newtoallofthis/probe/internal/output"
	"github.com/newtoallofthis/probe/internal/sandbox"
	"github.com/newtoallofthis/probe/internal/tools"
	"golang.org/x/term"
)

const version = "dev"

const (
	ExitFound    = 0
	ExitNoResult = 1
	ExitError    = 2
)

var (
	useColor bool
	isTTY    bool
)

func main() {
	// Detect NO_COLOR per https://no-color.org/
	_, noColor := os.LookupEnv("NO_COLOR")
	useColor = !noColor

	isTTY = term.IsTerminal(int(os.Stdout.Fd()))

	os.Exit(run())
}

func run() int {
	// Signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)
	go func() {
		sig := <-sigCh
		cancel()
		switch sig {
		case syscall.SIGPIPE:
			os.Exit(ExitFound)
		case syscall.SIGINT:
			fmt.Fprintf(os.Stderr, "interrupted\n")
			os.Exit(1)
		default:
			os.Exit(1)
		}
	}()
	// Flag parsing
	cfg := config.DefaultConfig()
	var showVersion bool
	var jsonFlag bool

	flag.StringVar(&cfg.Model, "model", cfg.Model, "Model name")
	flag.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "API base URL")
	flag.IntVar(&cfg.MaxTurns, "max-turns", cfg.MaxTurns, "Maximum agent turns")
	flag.StringVar(&cfg.ProjectDir, "dir", cfg.ProjectDir, "Project directory to search")
	flag.BoolVar(&jsonFlag, "json", false, "Output results as JSON")
	flag.StringVar(&cfg.OutputFormat, "format", cfg.OutputFormat, "Output format: human, json, paths, qf")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Show agent trace on stderr")
	flag.BoolVar(&cfg.Verbose, "v", false, "Show agent trace on stderr")
	flag.BoolVar(&cfg.Quiet, "quiet", false, "Suppress all output except exit code")
	flag.BoolVar(&cfg.Quiet, "q", false, "Suppress all output except exit code")
	flag.BoolVar(&cfg.Think, "think", false, "Use thorough search mode (more turns, deeper verification)")
	flag.BoolVar(&cfg.Think, "t", false, "Use thorough search mode (more turns, deeper verification)")
	var stdinFlag bool
	flag.BoolVar(&stdinFlag, "stdin", false, "Read file list from stdin (one path per line)")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	var listFlag, allFlag, silentFlag bool
	var showID int
	flag.BoolVar(&listFlag, "list", false, "List past queries for the current directory")
	flag.BoolVar(&allFlag, "all", false, "List all past queries across all directories")
	flag.IntVar(&showID, "show", 0, "Show full results of a history entry by ID")
	flag.BoolVar(&silentFlag, "silent", false, "Suppress all stderr output")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: probe [flags] <query>\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if showVersion {
		fmt.Printf("probe version %s\n", version)
		return ExitFound
	}

	// Track which flags were explicitly set
	flagSet := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		flagSet[f.Name] = true
	})

	// --json overrides --format
	if jsonFlag {
		cfg.OutputFormat = "json"
	}

	// --silent implies --quiet
	if silentFlag {
		cfg.Quiet = true
	}

	// --quiet wins over --verbose
	if cfg.Quiet {
		cfg.Verbose = false
	}

	// Load config file (between defaults and env)
	// Need to resolve dir first for .probe.toml lookup
	projDir := cfg.ProjectDir
	if abs, err := filepath.Abs(projDir); err == nil {
		projDir = abs
	}
	if err := cfg.LoadFile(projDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	// Load env vars for unset flags
	cfg.LoadEnv(flagSet)

	// Check prerequisites
	if err := canExecute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return ExitError
	}

	// Resolve project dir
	if err := cfg.ResolveProjectDir(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return ExitError
	}

	// --list / --all / --show: history commands, exit early
	if listFlag || allFlag || showID > 0 {
		dbPath, err := history.DBPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}
		db, err := history.Open(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}
		defer db.Close()
		if err := history.Init(db); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}

		if showID > 0 {
			raw, err := history.GetByID(db, int64(showID))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: no history entry with id %d\n", showID)
				return ExitError
			}
			var result agent.AgentResult
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				// Legacy entry stored as plain text
				fmt.Print(raw)
				return ExitFound
			}
			fmt.Print(output.FormatResults(&result, cfg.OutputFormat, isTTY, useColor, cfg.ShowReasons))
			return ExitFound
		}

		var entries []history.Entry
		if allFlag {
			entries, err = history.ListAll(db)
		} else {
			entries, err = history.ListByDir(db, cfg.ProjectDir)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return ExitError
		}
		for _, e := range entries {
			fmt.Printf("[%d]  %s  %s  %s\n", e.ID, e.Timestamp.Format("2006-01-02 15:04"), e.Dir, e.Query)
		}
		return ExitFound
	}

	// Read file list from stdin if --stdin
	var allowList []string
	if stdinFlag {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintf(os.Stderr, "error: --stdin requires piped input, not a terminal\n")
			return ExitError
		}
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			resolved, err := sandbox.SafePath(cfg.ProjectDir, line)
			if err != nil {
				continue // skip paths outside project
			}
			if _, err := os.Stat(resolved); err != nil {
				continue // skip non-existent files
			}
			allowList = append(allowList, resolved)
		}
		if len(allowList) == 0 {
			fmt.Fprintf(os.Stderr, "error: no valid files in stdin input\n")
			return ExitNoResult
		}
	}

	// Require query
	if flag.NArg() < 1 {
		flag.Usage()
		return ExitError
	}
	query := flag.Arg(0)

	// Build tool context
	toolCtx := tools.ToolContext{
		ProjectDir:        cfg.ProjectDir,
		GitIgnore:         sandbox.LoadGitIgnore(cfg.ProjectDir),
		MaxResultsPerGrep: cfg.MaxResultsPerGrep,
		MaxFileReadLines:  cfg.MaxFileReadLines,
		AllowList:         allowList,
	}

	progress := output.NewProgress(cfg.Verbose, cfg.Quiet)
	result, err := agent.RunAgent(ctx, query, &cfg, toolCtx, progress)
	if err != nil {
		progress.StopSpinner()
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}
	progress.OnDone(result.Turns)

	if len(result.Results) == 0 {
		if cfg.OutputFormat == "json" {
			fmt.Print(output.FormatResults(result, cfg.OutputFormat, isTTY, useColor, cfg.ShowReasons))
		}
		if !cfg.Quiet {
			if result.Summary != "" {
				fmt.Fprintf(os.Stderr, "No results: %s\n", result.Summary)
			} else {
				fmt.Fprintf(os.Stderr, "No results found.\n")
			}
		}
		return ExitNoResult
	}

	formatted := output.FormatResults(result, cfg.OutputFormat, isTTY, useColor, cfg.ShowReasons)
	fmt.Print(formatted)
	progress.PrintSummary(len(result.Results))

	// Store in history (best-effort, as JSON for format-independent retrieval)
	if dbPath, err := history.DBPath(); err == nil {
		if db, err := history.Open(dbPath); err == nil {
			defer db.Close()
			if history.Init(db) == nil {
				if raw, err := json.Marshal(result); err == nil {
					history.Insert(db, query, string(raw), cfg.ProjectDir)
				}
			}
		}
	}

	return ExitFound
}

func canExecute() error {
	if _, err := exec.LookPath("rg"); err != nil {
		return fmt.Errorf("probe requires ripgrep (rg) to be installed. See https://github.com/BurntSushi/ripgrep#installation")
	}
	return nil
}
