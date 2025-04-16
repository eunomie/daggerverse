// This module provides tools to allow to signoff commits
// from the developer machine. THis helps to reduce CI time
// by moving the CI back to the developer's machine.
// This modules requires a GitHub token to access the different
// GitHub APIs.

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dagger/signoff/internal/dagger"
)

type Signoff struct {
	// Source directory containing the local git clone
	// +private
	Sources *dagger.Directory
	// GitHub token to access GitHub APIs
	// +private
	Token *dagger.Secret
	// Container containing git and github cli tools
	Container *dagger.Container
	// Name of the check, default to 'signoff'
	CheckName string
}

func New(
	// The local directory containing the git clone to work on.
	sources *dagger.Directory,
	// The GitHub token to get access to the GitHub APIs
	token *dagger.Secret,
	// Name of the check, default to 'signoff'
	// +optional
	// +default="signoff"
	CheckName string,
) *Signoff {
	s := &Signoff{
		Sources:   sources,
		Token:     token,
		CheckName: CheckName,
	}
	s.Container = s.container()
	return s
}

// Check if the local directory is clean.
//
// This means that the three following constraints are verified:
// - no uncommited changes
// - the local branch is tracking a remote one
// - all commits have already been pushed
// If one of those constraint is failing, the return error will contain the explanation.
func (m *Signoff) IsClean(ctx context.Context) error {
	if out, err := m.WithGitExec([]string{"status", "--porcelain"}).Stdout(ctx); err != nil || out != "" {
		return fmt.Errorf("found uncommitted changes in the repo")
	}

	if exitCode, err := m.WithGitExec([]string{"rev-parse", "--abbrev-ref", "@{push}"}).ExitCode(ctx); err != nil || exitCode != 0 {
		return fmt.Errorf("no tracking branch found")
	}

	if out, err := m.WithGitExec([]string{"log", "@{push}.."}).Stdout(ctx); err != nil || out != "" {
		return fmt.Errorf("found unpushed commits in the repo")
	}
	return nil
}

// Sign off the current commit.
//
// This first ensures the repository is clean, then
// mark the status of the signoff check (or any other configured
// name) as success.
func (m *Signoff) Create(ctx context.Context) error {
	if err := m.IsClean(ctx); err != nil {
		return err
	}

	sha, err := m.Sha(ctx)
	if err != nil {
		return err
	}

	user, err := m.WhoIs(ctx)
	if err != nil {
		return err
	}

	out, err := m.WithGhExec([]string{
		"api",
		"--method", "POST",
		"repos/:owner/:repo/statuses/" + sha,
		"-f", "state=success",
		"-f", "context=" + m.CheckName,
		"-f", fmt.Sprintf("description=\"%s signed off\"", user),
	}).Out(ctx)

	if err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}

	fmt.Println("✓ Signed off on " + sha)

	return nil
}

// Install signoff requirement on the defined branch or on the default one
func (m *Signoff) Install(
	ctx context.Context,
	// Branch to install the signoff requirement. If not set, the default branch will be used
	// +optional
	branch string,
) error {
	if branch == "" {
		var err error
		if branch, err = m.DefaultBranch(ctx); err != nil {
			return fmt.Errorf("could not get the default branch: %w", err)
		}
	}

	if branch == "" {
		return fmt.Errorf("could not install without a branch name")
	}

	out, err := m.WithGhExec([]string{
		"api",
		fmt.Sprintf("/repos/:owner/:repo/branches/%s/protection", branch),
		"--method", "PUT",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		"--field", "required_status_checks[strict]=false",
		"--field", "required_status_checks[contexts][]=" + m.CheckName,
		"--field", "enforce_admins=null",
		"--field", "required_pull_request_reviews=null",
		"--field", "restrictions=null",
	}).Out(ctx)
	if err != nil {
		return fmt.Errorf("could not install signoff check %q to branch %q: %w\n%s", m.CheckName, branch, err, out)
	}

	fmt.Printf("✓ GitHub %s branch now requires signoff on check %q", m.CheckName, branch)

	return nil
}

// Uninstall signoff requirement on the defined branch or on the default one.
// This will delete all branch protection on the selected branch.
func (m *Signoff) Uninstall(
	ctx context.Context,
	// Branch to uninstall the signoff requirement. If not set, the default branch will be used
	// +optional
	branch string,
) error {
	if branch == "" {
		var err error
		if branch, err = m.DefaultBranch(ctx); err != nil {
			return fmt.Errorf("could not get the default branch: %w", err)
		}
	}

	if branch == "" {
		return fmt.Errorf("could not uninstall without a branch name")
	}

	out, err := m.WithGhExec([]string{
		"api",
		fmt.Sprintf("/repos/:owner/:repo/branches/%s/protection", branch),
		"--method", "DELETE",
	}).Out(ctx)
	if err != nil {
		return fmt.Errorf("could not uninstall branch protection for branch %q: %w\n%s", branch, err, out)
	}

	fmt.Printf("✓ GitHub %s branch no longer requires signoff", m.CheckName)

	return nil
}

// Retrieve the commit SHA of the most recent commit.
func (m *Signoff) Sha(ctx context.Context) (string, error) {
	out, err := m.WithGitExec([]string{"rev-parse", "HEAD"}).Stdout(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Get the username of the user who is currently authenticated
func (m *Signoff) WhoIs(ctx context.Context) (string, error) {
	out, err := m.WithGhExec([]string{
		"api", "user", "--jq", ".login",
	}).Out(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Get the pull request url of the current branch (to the default branch) if any
func (m *Signoff) PullRequest(ctx context.Context) (string, error) {
	defaultBranch, err := m.DefaultBranch(ctx)
	if err != nil {
		return "", err
	}
	
	out, err := m.WithGhExec([]string{
		"api",
		"repos/:owner/:repo/pulls",
		"--jq", fmt.Sprintf(".[] | select(.state == \"open\") | select(.base.ref == \"%s\") | .html_url", defaultBranch),
	}).Stdout(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Open a pull request for the current branch
func (m *Signoff) OpenPR(
	ctx context.Context,
	// fill with verbose information
	// +optional
	// +default=false
	verbose bool,
) (string, error) {
	fill := "--fill"
	if verbose {
		fill = "--fill-verbose"
	}
	return m.WithGhExec([]string{
		"pr",
		"create",
		fill,
	}).Out(ctx)
}

// Exec any command
func (m *Signoff) WithExec(args []string) *Signoff {
	m.Container = m.Container.WithExec(args, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	return m
}

// Exec any git command. 'git' will be automatically added to the arguments
func (m *Signoff) WithGitExec(args []string) *Signoff {
	return m.WithExec(append([]string{"git"}, args...))
}

// Exec any gh command. 'gh' will be automatically added to the arguments
func (m *Signoff) WithGhExec(args []string) *Signoff {
	return m.WithExec(append([]string{"gh"}, args...))
}

// Open an interactive terminal into the container with git and gh tools
func (m *Signoff) Terminal() *dagger.Container {
	return m.Container.Terminal()
}

func (m *Signoff) Out(ctx context.Context) (string, error) {
	stdOut, err := m.Container.Stdout(ctx)
	if err != nil {
		return "", err
	}
	stdErr, err := m.Container.Stderr(ctx)
	if err != nil {
		return "", err
	}
	out := stdOut + "\n" + stdErr
	exitCode, err := m.Container.ExitCode(ctx)
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return out, fmt.Errorf("exit code %d", exitCode)
	}
	return out, nil
}

func (m *Signoff) Stdout(ctx context.Context) (string, error) {
	return m.Container.Stdout(ctx)
}

func (m *Signoff) ExitCode(ctx context.Context) (int, error) {
	return m.Container.ExitCode(ctx)
}

func (m *Signoff) Stderr(ctx context.Context) (string, error) {
	return m.Container.Stderr(ctx)
}

// Get the default branch configured on the repository using gh API
func (m *Signoff) DefaultBranch(ctx context.Context) (string, error) {
	return m.WithGhExec([]string{
		"api",
		"repos/:owner/:repo",
		"--jq", ".default_branch",
	}).Stdout(ctx)
}

func (m *Signoff) base() *dagger.Container {
	return dag.Wolfi().
		Container(dagger.WolfiContainerOpts{
			Packages: []string{
				"gh",
				"git",
			},
		}).
		WithEnvVariable("GH_PROMPT_DISABLED", "true").
		WithEnvVariable("GH_NO_UPDATE_NOTIFIER", "true").
		WithExec([]string{"gh", "auth", "setup-git", "--force", "--hostname", "github.com"}) // Use force to avoid network call and cache setup even when no token is provided.
}

func (m *Signoff) container() *dagger.Container {
	return m.base().
		WithEnvVariable("CACHE_BUSTER", time.Now().Format(time.RFC3339Nano)).
		WithSecretVariable("GITHUB_TOKEN", m.Token).
		WithWorkdir("/work/repo").
		WithMountedDirectory("/work/repo", m.Sources)
}
