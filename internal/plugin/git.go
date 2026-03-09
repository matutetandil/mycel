package plugin

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GitResolver handles git operations for plugin resolution.
type GitResolver struct {
	Logger *slog.Logger
}

// ListTags lists all semver tags from a remote git repository.
// Tries SSH first for known hosts, falls back to HTTPS.
func (g *GitResolver) ListTags(ctx context.Context, repoURL string) ([]Version, error) {
	sshURL := NormalizeGitURL(repoURL)
	httpsURL := NormalizeGitURLHTTPS(repoURL)

	// Try SSH first, then HTTPS (if they differ)
	urls := []string{sshURL}
	if httpsURL != sshURL {
		urls = append(urls, httpsURL)
	}

	var lastErr error
	for _, url := range urls {
		versions, err := g.listTagsFromURL(ctx, url)
		if err == nil {
			return versions, nil
		}
		lastErr = err
		if g.Logger != nil && len(urls) > 1 {
			g.Logger.Debug("git ls-remote failed, trying next URL", "url", url, "error", err)
		}
	}

	return nil, fmt.Errorf("git ls-remote failed for %s: %w", repoURL, lastErr)
}

func (g *GitResolver) listTagsFromURL(ctx context.Context, url string) ([]Version, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--tags", "--refs", url)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %s", url, strings.TrimSpace(stderr.String()))
	}

	var versions []Version
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: "<hash>\trefs/tags/<tag>"
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		ref := parts[1]
		tag := strings.TrimPrefix(ref, "refs/tags/")

		v, err := ParseVersion(tag)
		if err != nil {
			continue // Skip non-semver tags
		}
		versions = append(versions, v)
	}

	SortVersions(versions)
	return versions, nil
}

// Clone clones a git repository at a specific tag into destDir.
// Tries SSH first for known hosts, falls back to HTTPS.
func (g *GitResolver) Clone(ctx context.Context, repoURL, tag, destDir string) error {
	sshURL := NormalizeGitURL(repoURL)
	httpsURL := NormalizeGitURLHTTPS(repoURL)

	urls := []string{sshURL}
	if httpsURL != sshURL {
		urls = append(urls, httpsURL)
	}

	var lastErr error
	for _, url := range urls {
		err := g.cloneFromURL(ctx, url, tag, destDir, repoURL)
		if err == nil {
			return nil
		}
		lastErr = err
		if g.Logger != nil && len(urls) > 1 {
			g.Logger.Debug("git clone failed, trying next URL", "url", url, "error", err)
		}
		// Clean up partial clone before retrying
		os.RemoveAll(destDir)
	}

	return lastErr
}

func (g *GitResolver) cloneFromURL(ctx context.Context, url, tag, destDir, repoURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", tag, url, destDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if g.Logger != nil {
		g.Logger.Info("cloning plugin", "source", repoURL, "tag", tag)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed for %s@%s: %s", repoURL, tag, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// ResolveVersion finds the best matching version for the given constraint.
func (g *GitResolver) ResolveVersion(ctx context.Context, repoURL string, cs ConstraintSet) (Version, error) {
	tags, err := g.ListTags(ctx, repoURL)
	if err != nil {
		return Version{}, err
	}

	if len(tags) == 0 {
		return Version{}, fmt.Errorf("no semver tags found in %s", repoURL)
	}

	best, ok := BestMatch(tags, cs)
	if !ok {
		available := make([]string, 0, len(tags))
		for _, t := range tags {
			available = append(available, t.String())
		}
		return Version{}, fmt.Errorf("no version matching constraint in %s (available: %s)",
			repoURL, strings.Join(available, ", "))
	}

	return best, nil
}

// NormalizeGitURL converts a short source path to a full git URL.
// For known hosts (GitHub, GitLab, Bitbucket), returns SSH URL (git@...).
// SSH is preferred because most developers have SSH keys configured.
// Falls back to HTTPS for unknown hosts or already-complete URLs.
func NormalizeGitURL(source string) string {
	// Already a full URL — use as-is
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "git://") || strings.HasPrefix(source, "ssh://") ||
		strings.Contains(source, "@") {
		return source
	}

	// Known hosts: use SSH (git@host:org/repo.git)
	for _, host := range []string{"github.com", "gitlab.com", "bitbucket.org"} {
		if strings.HasPrefix(source, host+"/") {
			path := strings.TrimPrefix(source, host+"/")
			return "git@" + host + ":" + path + ".git"
		}
	}

	// Unknown host — assume HTTPS
	return "https://" + source + ".git"
}

// NormalizeGitURLHTTPS converts a short source path to an HTTPS git URL.
// Used as fallback when SSH fails.
func NormalizeGitURLHTTPS(source string) string {
	// Already a full URL
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "git://") || strings.HasPrefix(source, "ssh://") ||
		strings.Contains(source, "@") {
		return source
	}

	return "https://" + source + ".git"
}

// IsGitSource returns true if the source looks like a git repository URL.
func IsGitSource(source string) bool {
	// Not a local path
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "../") {
		return false
	}

	// Known hosts
	for _, host := range []string{"github.com/", "gitlab.com/", "bitbucket.org/"} {
		if strings.HasPrefix(source, host) {
			return true
		}
	}

	// Full URLs
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "git://") || strings.HasPrefix(source, "ssh://") {
		return true
	}

	// Contains a dot and a slash — likely a hostname/path (e.g., "git.example.com/org/repo")
	if strings.Contains(source, ".") && strings.Contains(source, "/") {
		return true
	}

	return false
}

// GitAvailable checks if the git binary is installed.
func GitAvailable() error {
	_, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git is required for remote plugins; install git and try again")
	}
	return nil
}
