# Public release checklist

This repository was developed privately before its first public release. Treat the visibility change as a publication event, not a normal merge.

## 1. Merge the public-release preparation PR

The public README, MIT license, contribution guide and security policy should land before visibility changes.

## 2. Use a GitHub noreply address for future commits

Enable **Keep my email addresses private** in GitHub account email settings and use the GitHub-provided noreply address in local Git configuration.

For the current maintainer account, the expected address form is:

```text
79525475+billyham07@users.noreply.github.com
```

Configure it in this repository before further public commits:

```bash
git config user.email "79525475+billyham07@users.noreply.github.com"
```

Also enable GitHub's setting that blocks command-line pushes which expose a private email address if desired.

## 3. Do not rely on `.mailmap` to hide old email metadata

`.mailmap` only changes how some Git tools display identity. It does not remove the original author/committer email from commit objects.

The private development history contains commits authored with a personal email address. Those objects must not simply be made public as-is.

## 4. Choose a history-publication strategy

### Recommended: publish a clean public history

The safest privacy boundary is to keep the existing development repository private and publish a sanitized public repository/history from the current reviewed tree.

A clean initial public commit avoids exposing:

- historical author/committer personal email metadata
- obsolete private development branches
- merged PR head refs that can keep old commit objects reachable even after ordinary branch deletion
- accidental private-history artifacts that no longer exist on `main`

If preserving the same GitHub repository is important, use the rewrite path below and complete GitHub-side cleanup before changing visibility.

### Same-repository rewrite path

Before the repository becomes public, rewrite every reachable branch/tag that will remain public so personal author/committer email metadata is replaced with the GitHub noreply identity. `git filter-repo` is preferred over legacy `filter-branch`.

Inventory identities from the private history; do not copy a personal address into this file or any other tracked document:

```bash
git log --format='%ae%n%ce' | sort -u
```

Then rewrite only those personal addresses, using a local uncommitted command rather than a checked-in script:

```bash
git clone --mirror git@github.com:billyham07/harness-observer.git harness-observer-mirror.git
cd harness-observer-mirror.git

git filter-repo --force --commit-callback '
old = b"<personal-email-to-replace>"
new = b"79525475+billyham07@users.noreply.github.com"
if commit.author_email.lower() == old.lower():
    commit.author_email = new
if commit.committer_email.lower() == old.lower():
    commit.committer_email = new
'
```

Review the rewritten history locally before pushing. Confirm the working tree and this checklist still contain no personal email address.

Important: merged/closed GitHub pull requests can retain server-side PR refs and cached commit views that are not removed merely by deleting feature branches or force-pushing `main`. If using the same repository, complete GitHub's documented sensitive-data/history cleanup process (including Support assistance where required) before changing visibility. Do not assume a force-push alone makes historical personal email metadata unreachable.

## 5. Scan the complete history for secrets

Run a full Git-history scanner, not only a working-tree grep. For example:

```bash
gitleaks git .
```

or an equivalent history-aware scanner.

Investigate every credible credential/token/private-key finding before publication. `.gitignore` only prevents future additions; it does not clean history.

## 6. Remove obsolete branches before publication

Delete merged feature/fix branches after confirming their work is in `main`.

Pay special attention to any branch that is diverged rather than simply merged. A diverged branch can keep commits reachable that are absent from `main`.

The ideal public branch set for the first release is:

```text
main
```

Create new feature branches normally after the repository is public.

## 7. Enable repository security settings

Before announcing the project:

- enable private vulnerability reporting / security advisories
- enable dependency/security alerts appropriate for the repository
- review GitHub Actions permissions
- keep the default branch protected as appropriate once external contributions begin

## 8. Change visibility only after the privacy checks pass

At this point verify:

- public README renders correctly
- LICENSE is present
- no personal email is reachable in the history being published
- this checklist and other tracked docs contain no personal email address
- no secret scan findings remain
- obsolete private branches are gone
- CI/release workflows use only intended permissions

Then change the repository visibility from private to public.

## 9. Publish `v0.1.0`

After the public repository is visible and the release workflow is ready:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Verify that the published Windows/Linux archives unpack and that the packaged binary can locate the adjacent DuckDB library and report the expected version.
