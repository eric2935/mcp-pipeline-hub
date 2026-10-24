package pipeline

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"mcp-pipeline-hub/internal/guard"
)

const closeTicketCommand = "mcp-hub -close-ticket"

// CloseIO is the human barrier for -close-ticket.
// WaitMerge and Open are optional. The CLI waits on stdin and opens the compare URL.
type CloseIO struct {
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
	WaitMerge func(branch, info string) error
	Open      func(url string) error
}

// CloseTicket commits eligible files, waits until origin/main contains the branch, then marks TODO on main.
func (h *Hub) CloseTicket(ctx context.Context, cio CloseIO) error {
	if cio.Out == nil {
		cio.Out = os.Stdout
	}
	if cio.Err == nil {
		cio.Err = os.Stderr
	}
	if cio.In == nil {
		cio.In = os.Stdin
	}
	branch, err := h.gitLocalOut(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	branch = strings.TrimSpace(branch)
	ticket, err := guard.ParseFeatureTicket(branch, h.cfg().Pattern())
	if err != nil {
		return &ToolError{Code: "ERR_INVALID_BRANCH", Detail: branch}
	}
	specs, err := h.archivedSpecs(ticket)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return &ToolError{Code: "ERR_SPEC_NOT_ACTIVE", Detail: ticket}
	}
	if len(specs) > 1 {
		return &ToolError{Code: "ERR_SPEC_AMBIGUOUS", Detail: ticket}
	}
	body, err := os.ReadFile(filepath.Join(h.Root, filepath.FromSlash(specs[0])))
	if err != nil {
		return &ToolError{Code: "ERR_SPEC_NOT_ACTIVE", Detail: err.Error()}
	}
	meta, err := parseCommitMeta(string(body))
	if err != nil {
		return err
	}
	msg := commitMessage(meta.Type, meta.Scope, ticket, meta.Summary)
	if err := h.gitSync(ctx, "fetch", "origin"); err != nil {
		return err
	}
	files, err := h.commitCandidates(ctx)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		add := append([]string{"add", "--"}, files...)
		if err := h.gitLocal(ctx, add...); err != nil {
			return err
		}
		if err := h.gitLocal(ctx, "commit", "-m", msg); err != nil {
			return err
		}
		fmt.Fprintf(cio.Out, "committed %d\n%s\n", len(files), msg)
	}
	countRaw, err := h.gitSyncOut(ctx, "rev-list", "--count", "origin/main..HEAD")
	if err != nil {
		return err
	}
	ahead, err := strconv.Atoi(strings.TrimSpace(countRaw))
	if err != nil {
		return &ToolError{Code: "ERR_GIT", Detail: countRaw}
	}
	if ahead > 0 {
		if err := h.gitSync(ctx, "push", "origin", branch); err != nil {
			return err
		}
		info := h.compareInfo(ctx, branch)
		fmt.Fprintln(cio.Out, info)
		if err := waitMerge(cio, branch, info); err != nil {
			return err
		}
		if err := h.gitSync(ctx, "fetch", "origin"); err != nil {
			return err
		}
		ok, err := h.isAncestor(ctx, "HEAD", "origin/main")
		if err != nil {
			return err
		}
		squash := false
		if !ok {
			mentioned, err := h.mainCommitMentionsTicket(ctx, ticket)
			if err != nil {
				return err
			}
			if !mentioned {
				return &ToolError{Code: "ERR_GIT", Detail: "origin/main does not contain " + branch}
			}
			squash = true
		}
		return h.finishOnMain(ctx, cio, ticket, branch, squash)
	}
	fmt.Fprintln(cio.Out, "FAST_PATH")
	return h.finishOnMain(ctx, cio, ticket, branch, false)
}

func waitMerge(cio CloseIO, branch, info string) error {
	if cio.WaitMerge != nil {
		return cio.WaitMerge(branch, info)
	}
	if u := firstHTTPURL(info); u != "" {
		open := cio.Open
		if open == nil {
			open = openBrowser
		}
		if err := open(u); err != nil {
			fmt.Fprintf(cio.Err, "open: %s\n", err)
		}
	}
	fmt.Fprintln(cio.Out, "GitHub 웹 머지 완료 후 엔터를 누르세요...")
	_, err := bufio.NewReader(cio.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	return nil
}

func openBrowser(raw string) error {
	name, args := browserCommand(runtime.GOOS, raw)
	return exec.Command(name, args...).Start()
}

func browserCommand(goos, raw string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{raw}
	case "windows":
		return "cmd", []string{"/c", "start", raw}
	default:
		return "xdg-open", []string{raw}
	}
}

func firstHTTPURL(info string) string {
	for _, field := range strings.Fields(info) {
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return field
		}
	}
	return ""
}

func (h *Hub) compareInfo(ctx context.Context, branch string) string {
	remote, err := h.gitLocalOut(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "branch: " + branch
	}
	remote = strings.TrimSpace(remote)
	u, err := githubCompareURL(remote, "main", branch)
	if err != nil {
		return "origin: " + remote + "\nbranch: " + branch + "\nbase: main"
	}
	return u
}

func githubCompareURL(remote, base, branch string) (string, error) {
	ownerRepo, err := githubOwnerRepo(remote)
	if err != nil {
		return "", err
	}
	return "https://github.com/" + ownerRepo + "/compare/" + url.PathEscape(base) + "..." + branchRefEscape(branch) + "?expand=1", nil
}

func branchRefEscape(branch string) string {
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func githubOwnerRepo(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		return strings.TrimPrefix(remote, "git@github.com:"), nil
	case strings.HasPrefix(remote, "ssh://git@github.com/"):
		return strings.TrimPrefix(remote, "ssh://git@github.com/"), nil
	case strings.HasPrefix(remote, "https://github.com/"):
		return strings.TrimPrefix(remote, "https://github.com/"), nil
	case strings.HasPrefix(remote, "http://github.com/"):
		return strings.TrimPrefix(remote, "http://github.com/"), nil
	default:
		return "", fmt.Errorf("not a github remote")
	}
}

func (h *Hub) isAncestor(ctx context.Context, commit, of string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	_, stderr, err := h.Run.Run(ctx, h.Root, "git", "merge-base", "--is-ancestor", commit, of)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false, &ToolError{Code: "ERR_TIMEOUT"}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, execErr(err, stderr)
}

const squashSearchDepth = 50

// mainCommitMentionsTicket is the squash-merge fallback.
// isAncestor stays false after a squash because the feature commit is not in origin/main.
// A literal "[ticket]" in one of the last squashSearchDepth origin/main messages counts as merged.
// --fixed-strings is required: the brackets are regex metacharacters without it.
func (h *Hub) mainCommitMentionsTicket(ctx context.Context, ticket string) (bool, error) {
	needle := "[" + ticket + "]"
	out, err := h.gitSyncOut(ctx, "log", "origin/main", "-n", strconv.Itoa(squashSearchDepth), "--fixed-strings", "--grep="+needle)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (h *Hub) finishOnMain(ctx context.Context, cio CloseIO, ticket, feature string, squash bool) error {
	dirty, err := h.trackedDirty(ctx)
	if err != nil {
		return err
	}
	var hold *todoHold
	if containsPath(dirty, h.todoRel()) {
		got, err := h.holdTodo(ctx)
		if err != nil {
			return err
		}
		hold = got
	}
	pop := func() error {
		return h.releaseTodo(ctx, hold)
	}
	if err := h.gitLocal(ctx, "checkout", "main"); err != nil {
		_ = pop()
		return err
	}
	if err := h.gitSync(ctx, "pull", "--ff-only", "origin", "main"); err != nil {
		_ = pop()
		return err
	}
	if err := pop(); err != nil {
		return err
	}
	todoPath := filepath.Join(h.Root, h.todoRel())
	body, err := os.ReadFile(todoPath)
	if err != nil {
		return &ToolError{Code: "ERR_TODO_SECTION", Detail: err.Error()}
	}
	next, err := moveTodoDone(string(body), ticket, h.cfg().Pattern())
	if err != nil {
		return err
	}
	if next != string(body) {
		if err := os.WriteFile(todoPath, []byte(next), 0o644); err != nil {
			return &ToolError{Code: "ERR_TODO_SECTION", Detail: err.Error()}
		}
	}
	diff, err := h.gitLocalOut(ctx, "status", "--porcelain=v1", "--", h.todoRel())
	if err != nil {
		return err
	}
	if strings.TrimSpace(diff) != "" {
		if err := h.gitLocal(ctx, "add", "--", h.todoRel()); err != nil {
			return err
		}
		if err := h.gitLocal(ctx, "commit", "-m", "docs(todo): mark "+ticket+" as done"); err != nil {
			return err
		}
		if err := h.gitSync(ctx, "push", "origin", "main"); err != nil {
			return err
		}
	}
	if err := h.gitSync(ctx, "fetch", "--prune", "origin"); err != nil {
		return err
	}
	deleteFlag := "-d"
	if squash {
		deleteFlag = "-D"
	}
	if err := h.gitLocal(ctx, "branch", deleteFlag, feature); err != nil {
		return err
	}
	fmt.Fprintln(cio.Out, "closed "+ticket)
	return nil
}

func (h *Hub) archivedSpecs(ticketID string) ([]string, error) {
	dir := filepath.Join(h.Root, filepath.FromSlash(h.cfg().GetSpecArchiveDir()))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	prefix := "SPEC - [" + ticketID + "]"
	var out []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".md") {
			out = append(out, h.cfg().GetSpecArchiveDir()+"/"+name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (h *Hub) commitCandidates(ctx context.Context) ([]string, error) {
	stdout, err := h.gitLocalOut(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range parseStatusZ(stdout, true) {
		if guard.IsCommitExcluded(p, h.cfg().CloseExcludePaths) {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func parseStatusZ(s string, includeUntracked bool) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, "\x00")
	var out []string
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if rec == "" || len(rec) < 4 {
			continue
		}
		xy := rec[:2]
		path := rec[3:]
		if xy == "!!" || (xy == "??" && !includeUntracked) {
			continue
		}
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			i++
			if i < len(parts) {
				path = parts[i]
			}
		}
		out = append(out, filepath.ToSlash(path))
	}
	if out == nil {
		return []string{}
	}
	return out
}

type commitMeta struct {
	Type    string
	Scope   string
	Summary string
}

func commitMessage(typ, scope, ticket, summary string) string {
	return fmt.Sprintf("%s(%s): [%s] %s", typ, scope, ticket, summary)
}

func parseCommitMeta(content string) (commitMeta, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return commitMeta{}, &ToolError{Code: "ERR_SPEC_MALFORMED"}
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return commitMeta{}, &ToolError{Code: "ERR_SPEC_MALFORMED"}
	}
	fm := rest[:end]
	typ, ok1 := frontValue(fm, "commit_type")
	scope, ok2 := frontValue(fm, "commit_scope")
	summary, ok3 := frontValue(fm, "commit_summary")
	if !ok1 || !ok2 || !ok3 || strings.TrimSpace(typ) == "" || strings.TrimSpace(scope) == "" || strings.TrimSpace(summary) == "" {
		return commitMeta{}, &ToolError{Code: "ERR_SPEC_MALFORMED", Detail: "commit metadata"}
	}
	return commitMeta{Type: typ, Scope: scope, Summary: summary}, nil
}

func frontValue(fm, key string) (string, bool) {
	for _, line := range strings.Split(fm, "\n") {
		k, val, ok := splitFrontmatterKey(line)
		if ok && k == key {
			return unquoteYAML(val), true
		}
	}
	return "", false
}

func splitFrontmatterKey(line string) (string, string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return "", "", false
	}
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	return line[:i], strings.TrimSpace(line[i+1:]), true
}

func yamlDoubleQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '\\' || r == '"' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func unquoteYAML(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	var b strings.Builder
	esc := false
	for _, r := range s[1 : len(s)-1] {
		if esc {
			b.WriteRune(r)
			esc = false
			continue
		}
		if r == '\\' {
			esc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func moveTodoDone(text, ticketID string, pattern *regexp.Regexp) (string, error) {
	if err := guard.ValidateTicketID(ticketID, pattern); err != nil {
		return "", err
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	trail := strings.HasSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	reIdx := -1
	for i, line := range lines {
		if todoItem(line, ticketID) {
			reIdx = i
			break
		}
	}
	if reIdx < 0 {
		return "", &ToolError{Code: "ERR_TODO_SECTION", Detail: ticketID}
	}
	section := sectionTitle(lines, reIdx)
	checked := checkTodo(lines[reIdx])
	switch section {
	case "In Progress":
		lines = append(lines[:reIdx], lines[reIdx+1:]...)
		if !todoExists(lines, ticketID) {
			var err error
			lines, err = insertDone(lines, checked)
			if err != nil {
				return "", err
			}
		}
	case "Done":
		lines[reIdx] = checked
	default:
		return "", &ToolError{Code: "ERR_TODO_SECTION", Detail: section}
	}
	out := strings.Join(lines, "\n")
	if trail && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

func todoItem(line, ticketID string) bool {
	for _, prefix := range []string{"- [ ] ", "- [x] ", "- [X] "} {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := line[len(prefix):]
		if rest == ticketID || strings.HasPrefix(rest, ticketID) && (len(rest) == len(ticketID) || isTicketBoundary(rest[len(ticketID)])) {
			return true
		}
	}
	return false
}

func isTicketBoundary(b byte) bool {
	return b < '0' || (b > '9' && b != '-')
}

func checkTodo(line string) string {
	if strings.HasPrefix(line, "- [ ] ") {
		return "- [x] " + line[len("- [ ] "):]
	}
	if strings.HasPrefix(line, "- [X] ") {
		return "- [x] " + line[len("- [X] "):]
	}
	return line
}

func sectionTitle(lines []string, idx int) string {
	for i := idx; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "## ") {
			return strings.TrimSpace(strings.TrimPrefix(lines[i], "## "))
		}
	}
	return ""
}

func todoExists(lines []string, ticketID string) bool {
	for _, line := range lines {
		if todoItem(line, ticketID) {
			return true
		}
	}
	return false
}

func insertDone(lines []string, item string) ([]string, error) {
	at := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Done" {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, &ToolError{Code: "ERR_TODO_SECTION", Detail: "Done"}
	}
	end := len(lines)
	for i := at + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	ins := end
	for ins > at+1 && strings.TrimSpace(lines[ins-1]) == "" {
		ins--
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:ins]...)
	out = append(out, item)
	out = append(out, lines[ins:]...)
	return out, nil
}
