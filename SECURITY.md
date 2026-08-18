# Security

## Reporting a vulnerability

Report privately. Do not open a public issue.

Use GitHub's [private vulnerability
reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
on this repository.

Please include what you did, what happened, and what you expected. A proof of
concept helps. You will get an acknowledgement within a few days.

This is a one-person project with no bug bounty. Credit in the advisory is
offered for any report that leads to a fix.

## Scope

In scope: authentication and session handling, the boundary between the app
domain and the page domains, content sanitising, access control on pages and
uploads.

Out of scope: findings from automated scanners with no demonstrated impact,
missing headers with no exploit path, denial of service through volume, and
social engineering.

Please do not test against other people's accounts or data. Use your own.
