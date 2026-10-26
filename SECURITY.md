# Security Policy

AbiData holds people's data and runs their scripts, so we take security reports seriously and thank everyone who reports responsibly.

## Supported versions

Security fixes go into the latest minor release line. Fixes are released as patch versions (see [RELEASING.md](RELEASING.md)).

| Version | Supported |
|---|---|
| 1.5.x (latest minor) | ✅ |
| Older 1.x minors | ❌ (upgrade to the latest; migrations are automatic within 1.x) |

## Reporting a vulnerability

**Please do not open a public issue, discussion or pull request for a vulnerability.**

Report it privately through GitHub: open the repository's **Security** tab, choose **Report a vulnerability**, or use this link: <https://github.com/AbishYoung/AbiData/security/advisories/new>.

Please include:

- the affected version or commit, and the backend (SQLite or PostgreSQL);
- the component, such as the HTTP API, authentication, user SQL sandbox, Lua sandbox, egress gateway, backups, public apps or blogs;
- the steps to reproduce, a proof of concept if you have one, and the impact you expect;
- whether the issue is already public or known to others.

## What to expect

- **Acknowledgement** within 3 working days.
- **An initial assessment** (confirmed or not, and its severity) within 10 working days.
- **A fix and an advisory.** We aim to release a fix within 30 days for high and critical issues, and in the next release for lower ones. We publish a GitHub Security Advisory, request a CVE where appropriate, and credit you unless you would rather stay anonymous.
- **Updates** on our progress until the report is resolved.

We ask that you give us reasonable time to fix the issue before you disclose it, and that you make a good-faith effort to avoid privacy violations, data destruction and service disruption while you research. Only test against installations you own or have permission to test. We will not pursue legal action against research done in good faith under this policy.

## Scope

In scope: the code in this repository, including the server (`cmd/`, `internal/`), the web app (`web/`), the official Docker image and the release binaries.

Especially interesting areas:

- authentication, sessions, CSRF and API keys;
- authorization between workspaces and roles;
- escaping the user SQL sandbox or the Lua sandbox;
- SSRF through the egress gateway;
- backup and archive integrity;
- XSS in forms, reports, public apps and blogs.

Out of scope:

- vulnerabilities in third-party dependencies that do not affect AbiData (report those upstream; Dependabot tracks our dependencies);
- issues that need a compromised host, or an administrator acting against their own installation;
- missing hardening that the [operations security guide](docs/content/operations/security.md) leaves to the operator, such as TLS termination or PostgreSQL server settings;
- denial of service through sheer request volume against an installation with no rate limiting in front of it.

## Hardening guidance

For deployment advice (TLS, reverse proxies, the PostgreSQL role, egress allowlists and backups), see the [operations security guide](docs/content/operations/security.md).
