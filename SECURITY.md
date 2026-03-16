# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Ember, please report it responsibly.

**Do not open a public GitHub issue.**

Instead, email alex.daubois+ember[at]gmail.com with:

- A description of the vulnerability
- Steps to reproduce
- Potential impact

We will work with you to understand the issue and coordinate a fix before any public disclosure.

## Scope

Ember is a monitoring TUI that connects to the Caddy admin API. Security concerns include:

- Exposure of sensitive data through the Prometheus `/metrics` or `/healthz` endpoints
- Command injection or unexpected behavior from user-supplied flags
- Denial of service through crafted Prometheus metric responses

## Supported Versions

Only the latest release is supported with security updates.
