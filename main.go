package main

// main.go wires up the CLI. By default it launches the TUI; the headless flags
// (--list / --json) exist so you can script onibi and so an AI coding agent can
// inspect the output without a terminal.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	var (
		list   = flag.Bool("list", false, "print a table once and exit")
		asJSON = flag.Bool("json", false, "print listeners as JSON and exit")
		kill   = flag.String("kill", "", "kill whatever listens on this port (or PID), then exit")
		dryRun = flag.Bool("dry-run", false, "show what would be killed, don't kill")
		force  = flag.Bool("force", false, "use SIGKILL instead of SIGTERM")
		help   = flag.Bool("help", false, "show help")
	)
	flag.Usage = usage
	flag.Parse()

	switch {
	case *help:
		usage()
	case *asJSON:
		b, _ := json.MarshalIndent(Snapshot(), "", "  ")
		fmt.Println(string(b))
	case *list:
		printTable(Snapshot())
	case *kill != "":
		runKill(*kill, *force, *dryRun)
	default:
		runTUI(*dryRun)
	}
}

func runKill(target string, force, dryRun bool) {
	var matches []Listener
	for _, l := range Snapshot() {
		if strconv.Itoa(l.Port) == target || strconv.Itoa(l.PID) == target {
			matches = append(matches, l)
		}
	}
	if len(matches) == 0 {
		fmt.Fprintf(os.Stderr, "no listener matches %q\n", target)
		os.Exit(1)
	}
	for _, l := range matches {
		r := killProcess(l.PID, force, dryRun)
		verb := "killed"
		if r.DryRun {
			verb = "[dry-run] would kill"
		} else if !r.OK {
			verb = "failed"
		}
		line := fmt.Sprintf("%s :%d pid %d (%s)", verb, l.Port, l.PID, l.Command)
		if r.Reason != "" {
			line += " — " + r.Reason
		}
		fmt.Println(line)
	}
}

func printTable(rows []Listener) {
	cols := []struct {
		head string
		get  func(Listener) string
	}{
		{"PORT", func(l Listener) string { return strconv.Itoa(l.Port) }},
		{"PID", func(l Listener) string { return strconv.Itoa(l.PID) }},
		{"AGE", func(l Listener) string { return dash(l.Age) }},
		{"COMMAND", func(l Listener) string { return truncate(firstNonEmpty(l.Args, l.Command), 60) }},
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c.head)
	}
	for _, r := range rows {
		for i, c := range cols {
			if n := len(c.get(r)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	printRow := func(vals []string) {
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = pad(v, widths[i])
		}
		fmt.Println(strings.Join(parts, "  "))
	}
	head := make([]string, len(cols))
	rule := make([]string, len(cols))
	for i, c := range cols {
		head[i] = c.head
		rule[i] = strings.Repeat("-", widths[i])
	}
	printRow(head)
	printRow(rule)
	for _, r := range rows {
		vals := make([]string, len(cols))
		for i, c := range cols {
			vals[i] = c.get(r)
		}
		printRow(vals)
	}
	if len(rows) == 0 {
		fmt.Println("(no listening ports found)")
	}
}

func usage() {
	fmt.Print(`onibi — see the processes haunting your ports, and kill them

USAGE
  onibi                 launch the interactive TUI
  onibi --list          print a table once and exit
  onibi --json          print listeners as JSON (pipe into jq, etc.)
  onibi --kill 3000     kill whatever listens on port 3000 (or pass a PID)
  onibi --kill 3000 --dry-run   show what would be killed, don't kill
  onibi --kill 3000 --force     use SIGKILL instead of SIGTERM
  onibi --help          this message

TUI KEYS
  up/down or j/k   move            enter   kill selected (asks to confirm)
  /                filter          r       refresh now
  a                toggle auto     f       toggle SIGKILL / SIGTERM
  q / Ctrl-C       quit
`)
}

// --- small string helpers ---

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
