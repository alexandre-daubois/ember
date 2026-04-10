# AI Agent Skills

Ember provides skills for AI coding agents (Claude Code, Cursor, Copilot, etc.) so they can help you set up, troubleshoot, and deploy Ember directly from your agent.

## Available Skills

| Skill | Description |
|-------|-------------|
| ember-setup | Installation, Caddy configuration, TLS/mTLS, first launch |
| ember-troubleshoot | Diagnose connection issues, missing metrics, FrankenPHP problems |
| ember-production | Daemon mode, Prometheus export, Docker deployment |
| ember-json | JSON output, `jq` scripting, `ember diff`, `ember wait`, CI pipelines |

## Installation

Install with [skills.sh](https://skills.sh/):

```bash
npx skills add https://github.com/alexandre-daubois/ember --skill ember-setup
npx skills add https://github.com/alexandre-daubois/ember --skill ember-troubleshoot
npx skills add https://github.com/alexandre-daubois/ember --skill ember-production
npx skills add https://github.com/alexandre-daubois/ember --skill ember-json
```

## What Are Skills?

Skills are Markdown files that give AI coding agents specialized knowledge about a tool or workflow. Instead of the agent exploring the codebase to find answers, it gets accurate, curated instructions instantly resulting in faster, more precise responses.

Learn more at [skills.sh](https://skills.sh/).
