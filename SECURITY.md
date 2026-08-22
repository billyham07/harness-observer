# Security Policy

Harness Observer reads local coding-agent session evidence, so privacy boundaries are part of the security model.

## Reporting a vulnerability

Do not include credentials, private session content, local filesystem data or exploit details in a public GitHub issue.

For security-sensitive reports, use GitHub's private vulnerability reporting / security advisory flow for this repository once public vulnerability reporting is enabled in repository settings.

If that private channel is temporarily unavailable, open a minimal public issue asking for a private contact channel without including the vulnerability details.

## Security and privacy boundaries

Harness Observer is designed to remain outside the model request path.

It must not:

- proxy or intercept model requests
- extract OAuth tokens, API keys or credentials
- modify agent configuration or provider routing
- persist prompts, responses, reasoning text, tool arguments or tool output
- upload observed session/usage data to a remote service

The local HTTP service binds to loopback by default.

Pricing may perform an optional best-effort refresh of a public model-pricing catalog. That refresh must contain no local usage/session data and the embedded offline snapshot remains the fallback.

## Supported versions

Until the project has multiple supported release lines, security fixes are applied to the latest release and `main`.

## Dependency reports

Dependency vulnerabilities are welcome when they affect the shipped product or build/release integrity. Please distinguish build-time frontend dependencies from runtime dependencies when reporting impact.
