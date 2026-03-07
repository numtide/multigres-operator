# Observer Skills

AI agent skills for working with the multigres observer. Each skill is a structured prompt that teaches an agent how to use the observer effectively for a specific task.

Skills follow the [SKILL.md format](https://docs.anthropic.com/en/docs/claude-code/skills) with YAML frontmatter (`name`, `description`) and step-by-step instructions.

## Available Skills

| Skill | Description |
|-------|-------------|
| [diagnose_with_observer](diagnose_with_observer/SKILL.md) | Fetch diagnostics from `/api/status`, triage findings by severity, trace root causes through component logs and code, and produce actionable bug reports |

## Usage

Copy a skill's `SKILL.md` into your agent's skills directory (e.g., `.agent/skills/<name>/SKILL.md`) or reference it directly. The skill will deploy the observer if it isn't already running.
