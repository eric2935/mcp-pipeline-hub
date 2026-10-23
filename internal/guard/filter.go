package guard

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// PathTokenError is ERR_INVALID_PATH.
type PathTokenError struct{ Field, Reason string }

func (e *PathTokenError) Error() string {
	if e.Reason == "" {
		return "ERR_INVALID_PATH"
	}
	return "ERR_INVALID_PATH: " + e.Field + " " + e.Reason
}

// ValidatePathToken rejects traversal and control characters.
func ValidatePathToken(field, value string) error {
	if strings.Contains(value, "..") || strings.ContainsAny(value, `/\`) {
		return &PathTokenError{Field: field, Reason: "path separator"}
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return &PathTokenError{Field: field, Reason: "control character"}
		}
	}
	return nil
}

// ValidateTicketID allows IEMS-451 and IEMS-401-2 when pattern is the iEMS default.
func ValidateTicketID(id string, pattern *regexp.Regexp) error {
	if err := ValidatePathToken("ticket_id", id); err != nil {
		return err
	}
	if pattern == nil || !pattern.MatchString(id) {
		return &PathTokenError{Field: "ticket_id", Reason: "pattern"}
	}
	return nil
}

// ValidateSlug allows an empty slug. A present slug must start with a lowercase letter.
func ValidateSlug(slug string) error {
	if slug == "" {
		return nil
	}
	if err := ValidatePathToken("slug", slug); err != nil {
		return err
	}
	if !slugPattern.MatchString(slug) {
		return &PathTokenError{Field: "slug", Reason: "pattern"}
	}
	return nil
}

// BranchName is feature/{ticket} or feature/{ticket}-{slug}.
func BranchName(ticketID, slug string, pattern *regexp.Regexp) (string, error) {
	if err := ValidateTicketID(ticketID, pattern); err != nil {
		return "", err
	}
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	if slug == "" {
		return "feature/" + ticketID, nil
	}
	return "feature/" + ticketID + "-" + slug, nil
}

// FinalizeBranchOK matches only the branch that belongs to ticketID.
func FinalizeBranchOK(branch, ticketID string, pattern *regexp.Regexp) (bool, error) {
	if err := ValidateTicketID(ticketID, pattern); err != nil {
		return false, err
	}
	re, err := regexp.Compile(`^feature/` + regexp.QuoteMeta(ticketID) + `(-[a-z][a-z0-9]*(-[a-z0-9]+)*)?$`)
	if err != nil {
		return false, err
	}
	return re.MatchString(branch), nil
}

// IsBlacklisted reports paths that must not block iems_prepare_branch.
// Directory patterns end with / or contain a slash and match that prefix.
// A bare file name matches that base name. Close-ticket staging uses IsCommitExcluded.
func IsBlacklisted(rel string, patterns []string) bool {
	return pathListed(rel, patterns)
}

// IsCommitExcluded reports paths -close-ticket must not stage.
func IsCommitExcluded(rel string, patterns []string) bool {
	return pathListed(rel, patterns)
}

func pathListed(rel string, patterns []string) bool {
	p := cleanRel(rel)
	if p == "." || p == "" {
		return false
	}
	base := path.Base(p)
	for _, raw := range patterns {
		entry := strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/"))
		if entry == "" {
			continue
		}
		asDir := strings.HasSuffix(entry, "/")
		cleaned := path.Clean(strings.TrimSuffix(entry, "/"))
		if cleaned == "." || cleaned == "" {
			continue
		}
		if asDir || strings.Contains(strings.TrimSuffix(entry, "/"), "/") {
			if p == cleaned || strings.HasPrefix(p, cleaned+"/") {
				return true
			}
			continue
		}
		if p == cleaned || base == cleaned {
			return true
		}
	}
	return false
}

func cleanRel(rel string) string {
	return path.Clean(strings.ReplaceAll(rel, `\`, "/"))
}

// ParseFeatureTicket extracts the ticket from feature/{ticket} or feature/{ticket}-{slug}.
func ParseFeatureTicket(branch string, pattern *regexp.Regexp) (string, error) {
	if pattern == nil {
		return "", &PathTokenError{Field: "branch", Reason: "pattern"}
	}
	inner := strings.TrimPrefix(pattern.String(), "^")
	inner = strings.TrimSuffix(inner, "$")
	re, err := regexp.Compile(`^feature/(` + inner + `)(?:-[a-z][a-z0-9]*(?:-[a-z0-9]+)*)?$`)
	if err != nil {
		return "", err
	}
	m := re.FindStringSubmatch(branch)
	if m == nil {
		return "", &PathTokenError{Field: "branch", Reason: "pattern"}
	}
	if err := ValidateTicketID(m[1], pattern); err != nil {
		return "", err
	}
	ok, err := FinalizeBranchOK(branch, m[1], pattern)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &PathTokenError{Field: "branch", Reason: "pattern"}
	}
	return m[1], nil
}

// SplitDirty separates tracked dirty paths into stageable and excluded lists.
func SplitDirty(paths, blacklist []string) (stageable, excluded []string) {
	for _, p := range paths {
		if IsBlacklisted(p, blacklist) {
			excluded = append(excluded, p)
			continue
		}
		stageable = append(stageable, p)
	}
	return stageable, excluded
}
