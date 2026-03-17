# MAINTAINERS.md

## Current Maintainers

| Name | GitHub | Role |
|------|--------|------|
| PhoenixGPU Team | @wlqtjl | Founder & Lead Maintainer |

## Emeritus Maintainers

*None yet*

## How to Become a Maintainer

See [GOVERNANCE.md](docs/community/ADOPTERS_GOVERNANCE_SECURITY.md) for the full process.

---

# CODE_OF_CONDUCT.md

## Contributor Covenant Code of Conduct

### Our Pledge

We as members, contributors, and leaders pledge to make participation in our community
a harassment-free experience for everyone, regardless of age, body size, visible or 
invisible disability, ethnicity, sex characteristics, gender identity and expression,
level of experience, education, socio-economic status, nationality, personal appearance,
race, caste, color, religion, or sexual identity and orientation.

We pledge to act and interact in ways that contribute to an open, welcoming, diverse,
inclusive, and healthy community.

### Our Standards

Examples of behavior that contributes to a positive environment:
* Demonstrating empathy and kindness toward other people
* Being respectful of differing opinions, viewpoints, and experiences
* Giving and gracefully accepting constructive feedback
* Accepting responsibility and apologizing to those affected by our mistakes

Examples of unacceptable behavior:
* The use of sexualized language or imagery
* Trolling, insulting or derogatory comments, and personal or political attacks
* Public or private harassment
* Publishing others' private information without explicit permission

### Enforcement

Instances of abusive, harassing, or otherwise unacceptable behavior may be reported
by opening a GitHub Issue (private) or contacting maintainers directly.

This Code of Conduct is adapted from the [Contributor Covenant](https://www.contributor-covenant.org/),
version 2.1.

---

# ADOPTERS.md

## Organizations Using PhoenixGPU

> Want to be listed? Open a PR adding your organization!

### Production

| Organization | Type | Use Case | Since |
|------|------|---------|------|
| *(your org here)* | University / Company | GPU cluster HA | |

### Evaluation / Pilot

| Organization | Type | Status |
|------|------|------|
| *(contact us for free PoC support)* | | |

---

## Recruitment Email Template

**Subject**: [PhoenixGPU] 免费 PoC 合作邀请 — GPU 故障零中断解决方案

---

尊敬的 [姓名/团队]:

我是 PhoenixGPU 项目的核心开发者。

PhoenixGPU 是一个开源（Apache 2.0）的 Kubernetes GPU 虚拟化平台，核心差异化在于：
**GPU 节点宕机后，训练任务自动在 60 秒内从断点恢复，无需修改任何代码**。

目前 HAMi、Dynamia 等方案均不具备此能力。

**我们正在寻找 3-5 所高校/科研机构作为早期合作用户**，提供：
- 免费部署支持（远程协助 + 文档）
- 优先 Bug 修复和功能定制
- 发布合作案例（可匿名）

合作要求：
- 有实际 GPU 集群（≥ 4 张 GPU，K8s 环境）
- 提供真实使用反馈
- 允许在 Adopters.md 列出机构名（可不公开详情）

如果感兴趣，请回复此邮件或在 GitHub 提 Issue：
https://github.com/wlqtjl/PhoenixGPU-/issues

期待合作！

PhoenixGPU 团队
