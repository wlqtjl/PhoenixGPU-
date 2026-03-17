# Adopters

> This file tracks organizations using PhoenixGPU in production or active evaluation.
> Want to add your organization? Submit a PR!

## Production Users

| Organization | Use Case | Since |
|------|------|------|
| *(招募中 — 首批高校用户)* | | |

## Evaluating / Pilot

| Organization | Use Case |
|------|------|
| *(欢迎高校 GPU 集群管理员联系我们进行 PoC)* | |

---

# GOVERNANCE.md

## Project Governance

PhoenixGPU follows a lightweight governance model suitable for an early-stage project.

### Maintainers

Current maintainers are listed in [MAINTAINERS.md](MAINTAINERS.md).
Maintainers have commit access and make project decisions by consensus.

### Decision Making

- **Minor changes** (bug fixes, docs, small features): any maintainer can merge after CI passes.
- **Significant changes** (architecture, new components, breaking API): requires 2 maintainer approvals.
- **Major decisions** (governance changes, CNCF application): discussed in GitHub Discussions with 7-day comment period.

### Becoming a Maintainer

Contributors who have:
- Made significant contributions over ≥ 3 months
- Demonstrated understanding of the codebase and project goals
- Been active in code review and community discussions

...may be nominated by an existing maintainer. Approval requires unanimous maintainer agreement.

### Code of Conduct

This project follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).
Violations can be reported to the maintainers via GitHub Issues (private) or email.

---

# SECURITY.md

## Security Policy

### Supported Versions

| Version | Supported |
|---------|-----------|
| v0.1.x  | ✅ |
| < v0.1  | ❌ |

### Reporting a Vulnerability

**Please do NOT open a public GitHub Issue for security vulnerabilities.**

Report security vulnerabilities by:
1. Opening a [GitHub Security Advisory](https://github.com/wlqtjl/PhoenixGPU-/security/advisories/new) (preferred)
2. Or emailing the maintainers directly (see MAINTAINERS.md)

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Any proposed fix (optional)

### Response Timeline

- **Acknowledgment**: within 72 hours
- **Initial assessment**: within 7 days
- **Fix & disclosure**: coordinated release, typically within 30 days

### Security Hardening Notes

PhoenixGPU requires elevated privileges (CRIU needs `CAP_SYS_PTRACE`).
Operators should:
- Run the checkpoint agent as a dedicated ServiceAccount with minimal RBAC
- Enable PodSecurityAdmission in `restricted` mode where possible
- Regularly update to the latest release for security patches
- Scan images with Trivy or similar before deployment
