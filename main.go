package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

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
	cfg := defaultConfig()
	var showVersion bool
	var jsonFlag bool

	flag.StringVar(&cfg.Model, "model", cfg.Model, "Model name")
	flag.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "API base URL")
	flag.IntVar(&cfg.MaxTurns, "max-turns", cfg.MaxTurns, "Maximum agent turns")
	flag.StringVar(&cfg.ProjectDir, "dir", cfg.ProjectDir, "Project directory to search")
	flag.BoolVar(&jsonFlag, "json", false, "Output results as JSON")
	flag.StringVar(&cfg.OutputFormat, "format", cfg.OutputFormat, "Output format: human, json, paths")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Show agent trace on stderr")
	flag.BoolVar(&cfg.Verbose, "v", false, "Show agent trace on stderr")
	flag.BoolVar(&cfg.Quiet, "quiet", false, "Suppress all output except exit code")
	flag.BoolVar(&cfg.Quiet, "q", false, "Suppress all output except exit code")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")

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

	// --quiet wins over --verbose
	if cfg.Quiet {
		cfg.Verbose = false
	}

	// Load env vars for unset flags
	cfg.loadEnv(flagSet)

	// Check prerequisites
	if err := canExecute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return ExitError
	}

	// Resolve project dir
	if err := cfg.resolveProjectDir(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return ExitError
	}

	// Require query
	if flag.NArg() < 1 {
		flag.Usage()
		return ExitError
	}
	query := flag.Arg(0)

	// Build tool context
	toolCtx := ToolContext{
		ProjectDir: cfg.ProjectDir,
		GitIgnore:  LoadGitIgnore(cfg.ProjectDir),
	}

	progress := NewProgress(&cfg)
	result, err := RunAgent(ctx, query, &cfg, toolCtx, progress)
	if err != nil {
		progress.StopSpinner()
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}
	progress.OnDone(result.Turns)

	if len(result.Results) == 0 {
		if cfg.OutputFormat == "json" {
			fmt.Print(FormatResults(result, cfg.OutputFormat, isTTY, useColor))
		}
		return ExitNoResult
	}

	fmt.Print(FormatResults(result, cfg.OutputFormat, isTTY, useColor))
	progress.PrintSummary(len(result.Results))
	return ExitFound
}

func canExecute() error {
	if _, err := exec.LookPath("rg"); err != nil {
		return fmt.Errorf("probe requires ripgrep (rg) to be installed. See https://github.com/BurntSushi/ripgrep#installation")
	}
	return nil
}
