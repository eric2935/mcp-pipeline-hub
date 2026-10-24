package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func finalizeTokenPathFor(dir, ticketID string) string {
	return filepath.Join(dir, ".git", "mcp-finalize-tokens", ticketID+".json")
}

func TestIssueFinalizeTokenStoresHashOnly(t *testing.T) {
	dir := initRepo(t)
	h := New(dir)
	fixed := time.Unix(1_700_000_000, 0)
	h.Now = func() time.Time { return fixed }
	token, err := h.IssueFinalizeToken("IEMS-451")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != len("tok_")+32 || !strings.HasPrefix(token, "tok_") {
		t.Fatalf("%q", token)
	}
	body, err := os.ReadFile(finalizeTokenPathFor(dir, "IEMS-451"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), token) {
		t.Fatalf("plaintext stored: %s", body)
	}
	var rec finalizeTokenFile
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(token))
	if rec.TicketID != "IEMS-451" || rec.TokenHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("%#v", rec)
	}
	if rec.CreatedAt != fixed.Unix() || rec.ExpiresAt != fixed.Unix()+300 {
		t.Fatalf("%#v", rec)
	}
	info, err := os.Stat(finalizeTokenPathFor(dir, "IEMS-451"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	again, err := h.IssueFinalizeToken("IEMS-451")
	if err != nil {
		t.Fatal(err)
	}
	if again == token {
		t.Fatal("reissue reused the plaintext")
	}
	body, err = os.ReadFile(finalizeTokenPathFor(dir, "IEMS-451"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), token) || !strings.Contains(string(body), hashFinalizeToken(again)) {
		t.Fatalf("%s", body)
	}
}

func TestFinalizeTokenGateOrder(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature/IEMS-451")
	h := New(dir)
	now := time.Unix(1_700_000_000, 0)
	h.Now = func() time.Time { return now }
	token, err := h.IssueFinalizeToken("IEMS-451")
	if err != nil {
		t.Fatal(err)
	}
	in := finalizeIn{TicketID: "IEMS-451", FinalizeToken: token, CommitType: "feat", CommitScope: "Tooling", CommitSummary: "hub"}

	_, err = h.Finalize(context.Background(), finalizeIn{TicketID: "IEMS-451", CommitType: "feat", CommitScope: "Tooling", CommitSummary: "hub"})
	if err == nil || !strings.Contains(err.Error(), "ERR_TOKEN_REQUIRED") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(finalizeTokenPathFor(dir, "IEMS-451")); statErr != nil {
		t.Fatal(statErr)
	}

	bad := in
	bad.FinalizeToken = "tok_0123456789abcdef0123456789abcdef"
	_, err = h.Finalize(context.Background(), bad)
	if err == nil || !strings.Contains(err.Error(), "ERR_TOKEN_INVALID") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(finalizeTokenPathFor(dir, "IEMS-451")); statErr != nil {
		t.Fatal("invalid token removed the file", statErr)
	}

	_, err = h.Finalize(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "ERR_SPEC_NOT_ACTIVE") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(finalizeTokenPathFor(dir, "IEMS-451")); statErr != nil {
		t.Fatal("missing spec burned the token", statErr)
	}

	h.Now = func() time.Time { return now.Add(300 * time.Second) }
	_, err = h.Finalize(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "ERR_TOKEN_EXPIRED") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(finalizeTokenPathFor(dir, "IEMS-451")); !os.IsNotExist(statErr) {
		t.Fatal("expired file remains", statErr)
	}

	h.Now = func() time.Time { return now }
	token, err = h.IssueFinalizeToken("IEMS-451")
	if err != nil {
		t.Fatal(err)
	}
	path := finalizeTokenPathFor(dir, "IEMS-451")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	in.FinalizeToken = token
	_, err = h.Finalize(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "ERR_TOKEN_INVALID") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("corrupt file removed", statErr)
	}

	token, err = h.IssueFinalizeToken("IEMS-451")
	if err != nil {
		t.Fatal(err)
	}
	writeSpec(t, dir, "SPEC - [IEMS-451] [Tooling] Hub.md")
	in.FinalizeToken = token
	if _, err := h.Finalize(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("success left the token file", statErr)
	}
	_, err = h.Finalize(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "ERR_TOKEN_NOT_FOUND") {
		t.Fatal(err)
	}
}

func TestIssueFinalizeTokenRejectsBadTicketAndGitFile(t *testing.T) {
	dir := initRepo(t)
	h := New(dir)
	if _, err := h.IssueFinalizeToken("../IEMS-451"); err == nil || !strings.Contains(err.Error(), "ERR_INVALID_PATH") {
		t.Fatal(err)
	}
	bare := t.TempDir()
	if err := os.WriteFile(filepath.Join(bare, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h = New(bare)
	if _, err := h.IssueFinalizeToken("PROJ-1"); err == nil || !strings.Contains(err.Error(), "ERR_GIT") {
		t.Fatal(err)
	}
}
