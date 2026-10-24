package pipeline

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcp-pipeline-hub/internal/guard"
)

const (
	finalizeTokenBytes  = 16
	finalizeTokenTTL    = 300 * time.Second
	finalizeTokenPrefix = "tok_"
)

// finalizeTokenFile is the on-disk record. The plaintext token is never stored.
type finalizeTokenFile struct {
	TicketID  string `json:"ticket_id"`
	TokenHash string `json:"token_hash"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// IssueFinalizeToken writes a fresh one-time token for ticketID.
// A later issue for the same ticket replaces the previous file.
func (h *Hub) IssueFinalizeToken(ticketID string) (string, error) {
	if err := guard.ValidateTicketID(ticketID, h.cfg().Pattern()); err != nil {
		return "", asTool(err)
	}
	raw := make([]byte, finalizeTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", &ToolError{Code: "ERR_GIT", Detail: "token entropy"}
	}
	token := finalizeTokenPrefix + hex.EncodeToString(raw)
	now := h.Now().Unix()
	rec := finalizeTokenFile{
		TicketID:  ticketID,
		TokenHash: hashFinalizeToken(token),
		CreatedAt: now,
		ExpiresAt: now + int64(finalizeTokenTTL/time.Second),
	}
	if err := h.writeFinalizeToken(rec); err != nil {
		return "", err
	}
	return token, nil
}

func hashFinalizeToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *Hub) finalizeTokenPath(ticketID string) (string, error) {
	gitDir := filepath.Join(h.Root, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return "", &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	if !info.IsDir() {
		return "", &ToolError{Code: "ERR_GIT", Detail: ".git is not a directory"}
	}
	return filepath.Join(gitDir, "mcp-finalize-tokens", ticketID+".json"), nil
}

func (h *Hub) writeFinalizeToken(rec finalizeTokenFile) error {
	path, err := h.finalizeTokenPath(rec.TicketID)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(dir, ".issue-*.json")
	if err != nil {
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	if err := tmp.Close(); err != nil {
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	if err := os.Rename(tmpName, path); err != nil {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
		}
		if err := os.Rename(tmpName, path); err != nil {
			return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
		}
	}
	cleanup = false
	if err := os.Chmod(path, 0o600); err != nil {
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	return nil
}

// verifyFinalizeToken checks the issued token and does not delete a match.
// An expired record is deleted. A hash mismatch leaves the file in place.
func (h *Hub) verifyFinalizeToken(ticketID, token string, now time.Time) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return &ToolError{Code: "ERR_TOKEN_REQUIRED"}
	}
	path, err := h.finalizeTokenPath(ticketID)
	if err != nil {
		return err
	}
	rec, err := readFinalizeToken(path)
	if err != nil {
		return err
	}
	if rec.TicketID != ticketID || strings.TrimSpace(rec.TokenHash) == "" {
		return &ToolError{Code: "ERR_TOKEN_INVALID"}
	}
	if now.Unix() >= rec.ExpiresAt {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
		}
		return &ToolError{Code: "ERR_TOKEN_EXPIRED"}
	}
	if !finalizeTokenHashMatch(rec.TokenHash, token) {
		return &ToolError{Code: "ERR_TOKEN_INVALID"}
	}
	return nil
}

func readFinalizeToken(path string) (finalizeTokenFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return finalizeTokenFile{}, &ToolError{Code: "ERR_TOKEN_NOT_FOUND"}
		}
		return finalizeTokenFile{}, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	var rec finalizeTokenFile
	if err := json.Unmarshal(body, &rec); err != nil {
		return finalizeTokenFile{}, &ToolError{Code: "ERR_TOKEN_INVALID"}
	}
	return rec, nil
}

func finalizeTokenHashMatch(storedHex, token string) bool {
	stored, err := hex.DecodeString(storedHex)
	if err != nil || len(stored) != sha256.Size {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(stored, sum[:]) == 1
}

// burnFinalizeToken deletes a still-valid token file. Spec search must succeed first.
func (h *Hub) burnFinalizeToken(ticketID, token string, now time.Time) error {
	if err := h.verifyFinalizeToken(ticketID, token, now); err != nil {
		return err
	}
	path, err := h.finalizeTokenPath(ticketID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return &ToolError{Code: "ERR_TOKEN_NOT_FOUND"}
		}
		return &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	return nil
}
