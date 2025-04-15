package main

import (
	"context"
	"fmt"
	"time"

	"dagger/signoff/internal/dagger"
)

type Signoff struct {
	// +private
	Sources *dagger.Directory
	// +private
	Token     *dagger.Secret
	Container *dagger.Container
}

func New(
	sources *dagger.Directory,
	token *dagger.Secret,
) *Signoff {
	s := &Signoff{
		Sources: sources,
		Token:   token,
	}
	s.Container = s.container()
	return s
}

func (m *Signoff) IsClean(ctx context.Context) error {
	if out, err := m.WithGitExec([]string{"status", "--porcelain"}).Stdout(ctx); err != nil || out != "" {
		return fmt.Errorf("found uncommitted changes in the repo")
	}

	if exitCode, err := m.WithGitExec([]string{"rev-parse", "--abbrev-ref", "@{push}"}).ExitCode(ctx); err!=nil || exitCode != 0 {
		return fmt.Errorf("no tracking branch found")
	}

	if out, err := m.WithGitExec([]string{"log", "@{push}.."}).Stdout(ctx); err!=nil|| out != "" {
		return fmt.Errorf("found unpushed commits in the repo")
	}
	return nil
}

func (m *Signoff) Create(ctx context.Context) error {
	if err := m.IsClean(ctx); err != nil {
		return err
	}

	sha, err := m.Sha(ctx)
	if err != nil {
		return err
	}

	_, err = m.WithGhExec([]string{
		"api",
		"--method", "POST",
		"repos/:owner/:repo/statuses/" + sha,
		"-f", "state=success",
		"-f", "context=signoff",
		"-f", "description=\"${user} signed off\"",
	}).ExitCode(ctx)

	if err != nil {
		return err
	}

	fmt.Println("✓ Signed off on " + sha)

	return nil
}

func (m *Signoff) Sha(ctx context.Context) (string, error) {
	return m.WithGitExec([]string{"rev-parse", "HEAD"}).Stdout(ctx)
}

func (m *Signoff) WithExec(args []string) *Signoff {
	m.Container = m.Container.WithExec(args)
	return m
}

func (m *Signoff) WithGitExec(args []string) *Signoff {
	return m.WithExec(append([]string{"git"}, args...))
}

func (m *Signoff) WithGhExec(args []string) *Signoff {
	return m.WithExec(append([]string{"gh"}, args...))
}

func (m *Signoff) Terminal() *dagger.Container {
	return m.Container.Terminal()
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
