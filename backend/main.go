// Command omock is a local WireMock-style HTTP mock server. It can run
// standalone (`omock serve`) or be driven by the Omarchy plugin over a
// JSON-lines protocol (`omock control`).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/duncan-ireri/omarchy-apimock/backend/control"
	"github.com/duncan-ireri/omarchy-apimock/backend/mock"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetFlags(0)
	log.SetPrefix("omock: ")

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		os.Exit(cmdServe(os.Args[2:]))
	case "validate":
		os.Exit(cmdValidate(os.Args[2:]))
	case "control":
		if err := control.Run(version, os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
	case "-v", "--version", "version":
		fmt.Println("omock", version)
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `omock %s
local mock server for APIs and webhooks

usage:
  omock serve -f <file|dir> [-p PORT] [--host ADDR] [-v]
  omock validate -f <file|dir>
  omock control          speak the JSON-lines control protocol on stdin/stdout
  omock --version

serve flags:
  -f, --file   mappings JSON file, or a directory of *.json files (required)
  -p, --port   TCP port to listen on (default 8080)
      --host   bind address (default 127.0.0.1)
  -v           log every request
`, version)
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var file, host string
	var port int
	var verbose bool
	fs.StringVar(&file, "f", "", "mappings file or directory")
	fs.StringVar(&file, "file", "", "mappings file or directory")
	fs.IntVar(&port, "p", 8080, "listen port")
	fs.IntVar(&port, "port", 8080, "listen port")
	fs.StringVar(&host, "host", "127.0.0.1", "bind address")
	fs.BoolVar(&verbose, "v", false, "log every request")
	_ = fs.Parse(args)

	if file == "" {
		log.Println("a mappings file or directory is required (-f)")
		return 2
	}

	store := mock.NewStore(file)
	if err := store.Load(); err != nil {
		log.Printf("cannot load %s: %v", file, err)
		return 1
	}
	store.OnReload(func(rs *mock.Ruleset, err error) {
		if err != nil {
			log.Printf("reload failed, keeping previous mappings: %v", err)
			return
		}
		log.Printf("reloaded %d stub(s) from %s", len(rs.Stubs), file)
	})
	store.StartWatching(500 * time.Millisecond)
	defer store.StopWatching()

	journal := mock.NewJournal(500)
	srv := mock.NewServer(host, port, store, journal)
	if verbose {
		srv.SetLogger(func(format string, a ...any) { log.Printf(format, a...) })
	}
	if err := srv.Start(); err != nil {
		log.Printf("cannot listen on %s:%d: %v", host, port, err)
		return 1
	}
	log.Printf("serving %d stub(s) on http://%s:%d  (Ctrl-C to stop)", len(store.Current().Stubs), host, port)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("shutdown: %v", err)
	}
	return 0
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	var file string
	fs.StringVar(&file, "f", "", "mappings file or directory")
	fs.StringVar(&file, "file", "", "mappings file or directory")
	_ = fs.Parse(args)

	if file == "" {
		log.Println("a mappings file or directory is required (-f)")
		return 2
	}

	rs, err := mock.LoadRuleset(file)
	if err != nil {
		log.Printf("invalid: %v", err)
		return 1
	}
	fmt.Printf("ok, %d stub(s)\n", len(rs.Stubs))
	for _, s := range rs.Summaries() {
		fmt.Printf("  [%d] %-6s %s  (%s)\n", s.Priority, s.Method, s.URL, s.Name)
	}
	return 0
}
