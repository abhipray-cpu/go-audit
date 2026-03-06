# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.x     | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

**Please do NOT open a public GitHub issue for security vulnerabilities.**

Instead, report vulnerabilities privately via one of the following:

1. **GitHub Security Advisories** — Use the "Report a vulnerability" button on
   the [Security tab](../../security/advisories) of this repository.
2. **Email** — Send details to **security@go-audit.dev**.

### What to include

* Description of the vulnerability
* Steps to reproduce
* Affected versions
* Potential impact
* Suggested fix (if any)

### Response timeline

| Step                  | Target        |
| --------------------- | ------------- |
| Acknowledgement       | 48 hours      |
| Initial assessment    | 5 business days |
| Fix & advisory draft  | 14 business days |
| Public disclosure     | After fix is released |

We will coordinate with you on disclosure timing. Credit will be given unless
you prefer to remain anonymous.

## Security Best Practices for Users

* Always pin a specific release tag or commit hash in `go.mod`.
* Rotate database credentials regularly.
* Use the `SanitizingExtractor` adapter to prevent credentials from leaking
  into audit metadata.
* Enable TLS for all database and object-store connections.
* Restrict access to the OLAP / cold-storage layer to authorized services only.
