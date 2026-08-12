package main

// ports.go holds all the "what is listening" logic — discovery, the extra
// detail we hang off each process, and the single kill entry point. It has no
// TUI / rendering concerns, so it is easy to test and to grow. Future actions
// (restart, watch-and-notify) belong here too and should reuse Snapshot() as
// the single source of truth.

import (
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Listener is one process listening on one TCP port.
type Listener struct {
	Port    int    `json:"port"`
	Address string `json:"address"`
	PID     int    `json:"pid"`
	Command string `json:"command"` // short command name (e.g. "node")
	Args    string `json:"args"`    // full command line, when available
	Cwd     string `json:"cwd"`
	Age     string `json:"age"` // elapsed time, e.g. "01:23" or "1-04:05:06"
}

var portSuffix = regexp.MustCompile(`:(\d+)$`)

// run executes a command and returns stdout, ignoring a non-zero exit when
// there is still output (lsof/ss sometimes warn on stderr but print useful
// stdout).
func run(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	return string(out)
}

// ListListeners returns every TCP LISTEN socket with its owning process.
func ListListeners() []Listener {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if out := run("lsof", "-nP", "-iTCP", "-sTCP:LISTEN"); out != "" {
			return dedupe(parseLsof(out))
		}
		if out := run("ss", "-tlnpH"); out != "" { // Linux fallback
			return dedupe(parseSS(out))
		}
		if out := run("netstat", "-tlnp"); out != "" {
			return dedupe(parseNetstat(out))
		}
	}
	return nil
}

// parseLsof reads default `lsof` table output. The NAME column looks like
// "*:3000" / "127.0.0.1:3000" / "[::1]:3000" and may be followed by "(LISTEN)".
func parseLsof(out string) []Listener {
	var rows []Listener
	lines := strings.Split(out, "\n")
	for _, line := range lines[1:] { // skip header
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Fields(line)
		var name string
		for _, c := range cols {
			if portSuffix.MatchString(c) {
				name = c // the token that ends in :<port>
			}
		}
		if name == "" {
			continue
		}
		m := portSuffix.FindStringSubmatch(name)
		port, _ := strconv.Atoi(m[1])
		pid, _ := strconv.Atoi(cols[1])
		rows = append(rows, Listener{
			Port:    port,
			Address: portSuffix.ReplaceAllString(name, ""),
			PID:     pid,
			Command: cols[0],
		})
	}
	return rows
}

// parseSS reads `ss -tlnpH`: "LISTEN 0 511 *:4321 *:* users:(("node",pid=6,fd=18))"
func parseSS(out string) []Listener {
	var rows []Listener
	userRe := regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Fields(line)
		if len(cols) < 4 {
			continue
		}
		local := cols[3]
		m := portSuffix.FindStringSubmatch(local)
		if m == nil {
			continue
		}
		port, _ := strconv.Atoi(m[1])
		l := Listener{Port: port, Address: portSuffix.ReplaceAllString(local, ""), Command: "?"}
		if u := userRe.FindStringSubmatch(line); u != nil {
			l.Command = u[1]
			l.PID, _ = strconv.Atoi(u[2])
		}
		rows = append(rows, l)
	}
	return rows
}

// parseNetstat reads `netstat -tlnp` LISTEN lines with a trailing "pid/prog".
func parseNetstat(out string) []Listener {
	var rows []Listener
	pidProg := regexp.MustCompile(`^(\d+)/(.+)$`)
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		cols := strings.Fields(line)
		if len(cols) < 4 {
			continue
		}
		local := cols[3]
		m := portSuffix.FindStringSubmatch(local)
		if m == nil {
			continue
		}
		port, _ := strconv.Atoi(m[1])
		l := Listener{Port: port, Address: portSuffix.ReplaceAllString(local, ""), Command: "?"}
		if pm := pidProg.FindStringSubmatch(cols[len(cols)-1]); pm != nil {
			l.PID, _ = strconv.Atoi(pm[1])
			l.Command = pm[2]
		}
		rows = append(rows, l)
	}
	return rows
}

func dedupe(rows []Listener) []Listener {
	seen := map[string]bool{}
	var out []Listener
	for _, r := range rows {
		key := strconv.Itoa(r.Port) + "|" + strconv.Itoa(r.PID)
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// Enrich fills in cwd, full args and uptime for each listener.
func Enrich(rows []Listener) []Listener {
	for i := range rows {
		rows[i].Cwd = pidCwd(rows[i].PID)
		if a := pidArgs(rows[i].PID); a != "" {
			rows[i].Args = a
		} else {
			rows[i].Args = rows[i].Command
		}
		rows[i].Age = pidAge(rows[i].PID)
	}
	return rows
}

// Snapshot is the one-shot everything-callers-need: discover + enrich.
func Snapshot() []Listener { return Enrich(ListListeners()) }

func pidCwd(pid int) string {
	if pid <= 0 {
		return ""
	}
	if runtime.GOOS == "linux" {
		if p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil {
			return p
		}
		return ""
	}
	// macOS
	out := run("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn")
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:]
		}
	}
	return ""
}

func pidArgs(pid int) string {
	if pid <= 0 {
		return ""
	}
	return strings.TrimSpace(run("ps", "-p", strconv.Itoa(pid), "-o", "command="))
}

func pidAge(pid int) string {
	if pid <= 0 {
		return ""
	}
	return strings.TrimSpace(run("ps", "-p", strconv.Itoa(pid), "-o", "etime="))
}

// AgeSeconds converts an etime string ("1-04:05:06", "04:05", "05") to seconds,
// used to color long-lived (possibly forgotten) processes.
func AgeSeconds(etime string) int {
	if etime == "" {
		return 0
	}
	days := 0
	rest := etime
	if i := strings.IndexByte(etime, '-'); i >= 0 {
		days, _ = strconv.Atoi(etime[:i])
		rest = etime[i+1:]
	}
	secs := 0
	for _, part := range strings.Split(rest, ":") {
		n, _ := strconv.Atoi(part)
		secs = secs*60 + n
	}
	return days*86400 + secs
}

// KillResult reports the outcome of a kill attempt.
type KillResult struct {
	OK     bool
	DryRun bool
	Reason string
}

// killProcess is the single place that terminates a process. Everything else
// only reads; this is the one function with a destructive side effect.
func killProcess(pid int, force, dryRun bool) KillResult {
	if pid <= 1 {
		return KillResult{Reason: "refusing to kill pid <= 1"}
	}
	if dryRun {
		return KillResult{OK: true, DryRun: true}
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return KillResult{Reason: err.Error()}
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := proc.Signal(sig); err != nil {
		return KillResult{Reason: err.Error()}
	}
	return KillResult{OK: true}
}

// waitGone polls briefly so the UI can refresh once a SIGTERM has landed.
func waitGone(pid int, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if proc, err := os.FindProcess(pid); err != nil || proc.Signal(syscall.Signal(0)) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
