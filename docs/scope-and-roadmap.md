# atlas-ai: scope and roadmap

This document records the scope of atlas-ai, the decisions taken so far and the planned roadmap. It is the first written statement of those decisions: until now they existed only in discussion. Later features are expected to start from this document and to update it when a decision changes.

Every statement below carries one of four status labels. Read the label before relying on the statement.

| Label | Meaning |
|---|---|
| **Decided** | Confirmed by the project owner. |
| **Proposed** | A recommendation awaiting the owner's confirmation. It is not a decision. |
| **Open** | No recommendation yet; a decision is needed. |
| **Unverified** | A technical claim not yet checked against the relevant documentation. |

## 1. Purpose and audience

**Decided.** atlas-ai is an installer that configures AI coding agents with the Atlas workflow: a knowledge vault, persistent memory, a session logbook, a code knowledge graph, token-saving tools, skills, hooks and MCP server configuration.

**Decided.** It is meant for anyone, on Linux and Windows.

**Decided.** It is a new project. It is inspired by gentle-ai but it is not a fork of it and not a layer on top of it.

## 2. Supported agents (v1)

**Decided.** Version 1 supports three agents:

- Claude Code
- GitHub Copilot CLI
- GitHub Copilot in VS Code

**Decided.** Other agents come later, as adapters.

macOS and Cursor are not part of v1. This follows from the platform and agent lists above; it was not decided as a separate question, and neither has been discussed as a later target.

The only agent configuration locations verified so far are the personal skills directories: `~/.claude/skills` for Claude Code, and `~/.copilot/skills` for both GitHub Copilot CLI and GitHub Copilot in VS Code. No other configuration path is stated in this document, because none has been verified.

## 3. Distribution

**Decided.** atlas-ai is distributed as a Go binary, built in GitHub Actions and published as GitHub releases with checksums.

## 4. Security principles

**Decided.** Security comes first, by design and by default.

**Proposed.** The concrete rules that follow from this principle:

- Verify the checksum of every download.
- Back up before every write.
- Never write or print secrets.
- Show what will change and ask before changing it.
- No network access beyond the stated downloads.
- Least privilege: no elevation unless a component requires it and the user agrees.

## 5. Components

The table summarizes each component. The status column refers to the facts in the notes column; where a row mixes statuses, the notes say which part has which status.

| Component | What the installer does | Status | Notes |
|---|---|---|---|
| Skills | Copies embedded skills, unchanged, to each agent's skills directory | Decided | One agent-neutral text per skill. The first three are shipped by `neutral-skills`. |
| Engram | Installs the persistent memory tool and registers it with each agent | Decided (component); Proposed (installation); Unverified (Windows) | See below. |
| cbm (codebase-memory-mcp) | Installs the code knowledge graph tool and registers it with each agent | Decided (component); Proposed (installation); Unverified (Windows) | See below. |
| RTK | Installs the token-saving command proxy | Decided (component); Proposed (installation); Unverified (Windows) | See below. |
| Headroom | Installs a token-saving tool | Decided (component); Proposed (optional) | Currently installed through `uv`. |
| Logbook | Installs the session logbook script | Decided | Shipped as a Python script embedded in the binary; requires Python. See below. |
| Atlas vault | Provides a local knowledge vault | Decided (component, local per user, path asked with a default, English names); Proposed (`ATLAS_VAULT` variable); Open (default path, exact names) | See below. |
| Hooks | Writes hooks per agent through adapters | Decided (component); Unverified (files and formats) | |
| MCP server configuration | Writes MCP configuration per agent through adapters | Decided (component); Unverified (files and formats) | |
| Private-only items | Not installed | Proposed | Excluded from the public installer. |

### Skills

**Decided.** Skills are embedded in the binary and copied unchanged to each agent's skills directory. There is one agent-neutral text per skill. The first three skills, `session-start`, `session-end` and `checked-loop`, are shipped by the `neutral-skills` feature, which is complete but not yet merged.

### Engram, cbm and RTK

**Decided.** Engram (persistent memory), cbm / codebase-memory-mcp (code knowledge graph) and RTK (token-saving command proxy) are components of the installer.

**Proposed.** How they are installed: download the upstream release binary, verify it, and register it with each agent.

**Unverified.** Whether each of the three is available for Windows.

### Headroom

**Decided.** Headroom (a token-saving tool) is a component. It is currently installed through `uv`, a Python tool.

**Proposed.** Make Headroom an optional component, since its current installation route depends on Python tooling.

### Logbook

**Decided.** The session logbook is a component. Today it is a dependency-free Python script that is not in version control.

**Decided.** The logbook is shipped as the Python script. The alternative, porting it to a subcommand of the Go binary, was considered and not chosen.

Consequences of this decision: Python becomes a prerequisite of the logbook component on every machine, including Windows, and the script has to enter version control before the installer can ship it.

**Decided.** On a machine without Python the installer stops and asks the user to install it. It does not install Python itself and does not skip the logbook silently.

**Decided.** The script lives in this repository and is embedded in the binary, as the skills are. The installer downloads nothing for this component.

The script has not been imported yet. Its current default logbook directory is specific to one machine and has to be made neutral when it is imported, under the same rule the skills follow.

### Atlas vault

**Decided.** The Atlas vault is a component.

**Decided.** Each user has a local Obsidian vault on their own machine. The installer does not depend on any private MCP gateway.

**Proposed.** The vault is located through an `ATLAS_VAULT` environment variable. The shipped skills already refer to the vault by that name.

**Decided.** The installer asks for the vault path and offers a default inside the user's home directory. The user accepts it or types another path; if the folder does not exist, the installer creates it. The installer does not use a fixed path without asking and does not scan the disk for existing vaults.

**Open.** Which default path the installer offers, and how the chosen path is made available to the agents on each platform.

**Decided.** The vault uses English folder and field names. Spanish names and names configurable per language at install time were considered and not chosen; the second would have required generating the skills from templates, against the decision to copy them unchanged.

Consequence: the skills shipped by `neutral-skills` still assume some Spanish folder and field names and have to be updated. That change is not done yet.

**Open.** The exact list of English folder and field names.

### Hooks and MCP server configuration

**Decided.** Hooks and MCP server configuration are components, written per agent through adapters.

**Unverified.** The exact files and formats each agent uses for hooks and for MCP servers.

### Excluded from the public installer

**Proposed.** Skills and MCP servers that only work on a private network, and any private MCP gateway, are not part of the public installer.

## 6. Behavior features

### Model per phase

**Decided** as a goal: different phases of the workflow can use different models, and different providers where possible.

Feasibility differs per agent:

- GitHub Copilot in VS Code supports a model per custom agent and a model choice on handoff between agents. This was verified in its documentation.
- Claude Code supports a model per subagent, within Claude models. **Unverified:** switching to a model from another provider.
- GitHub Copilot CLI: **Unverified.**

**Proposed.** For v1: model per phase on all three agents, and provider per phase only where the agent allows it.

### Persona

**Decided.** The agent does not explain or teach unless asked, because tokens are limited. Security always comes first.

### Project stack detection

**Decided** as in scope.

**Decided.** The detected stack drives which skills and rules get enabled. Only reporting the stack, without enabling anything, was considered and not chosen.

Consequence: this needs skills and rules that are specific to a stack. None exist yet; the three shipped skills do not depend on the stack. Until some exist, detection has nothing to enable.

**Open.** Which stacks are detected, and which skills and rules each one enables.

### Automatic backup and restore

**Decided.** The installer backs up, and can restore, everything it modifies.

### Workflow shipped

**Decided.** The installer ships the organic task-driven workflow (ODD) only. A spec-driven workflow (SDD) is not part of v1; it may be added later.

Consequence for the model-per-phase feature: its phases have to be defined in terms of ODD, since there are no SDD phases to map models to.

**Decided.** Three phases, matching the points where ODD delegates work:

| Phase | Work | Model tier |
|---|---|---|
| Explore | Read code and documentation | Cheap and fast |
| Implement | Write the change | Intermediate |
| Review | Check the result before it is accepted | Most capable |

**Decided.** The main conversation, which orchestrates and decides, keeps the model the user selected in their agent. atlas-ai does not change it.

**Open.** Which concrete model each tier maps to in each agent, and whether the user can override the mapping at install time.

## 7. Open questions

This list consolidates every Open and Unverified item above, followed by three further questions.

**Open**

1. Which default vault path the installer offers, and how the chosen path is made available to the agents on each platform.
2. The exact list of English folder and field names the vault uses.
3. Which stacks are detected, and which skills and rules each one enables.
4. Which concrete model each phase tier maps to in each agent, and whether the user can override the mapping at install time.

**Unverified**

5. Whether Engram, cbm and RTK are each available for Windows.
6. The exact files and formats for hooks and MCP server configuration in each agent.
7. Whether Claude Code can switch to a provider other than Claude for a subagent.
8. Whether GitHub Copilot CLI supports a model per phase.

**Further questions**

9. Repository visibility: public or private.
10. License.
11. Whether the shipped skills run correctly inside each agent. This has not been verified in any agent.

## 8. Roadmap

**Proposed.** The order below is a recommendation, not a decision.

One guiding rule: backup and restore comes before any feature that writes to a user's configuration, following the security principles.

| # | Feature | Goal | Depends on | State |
|---|---|---|---|---|
| 1 | `neutral-skills` | Agent-neutral skills and their contract test | - | Done, awaiting merge |
| 2 | `scope-roadmap` | This document | - | In progress |
| 3 | `cli-skeleton` | Command structure, platform paths for Linux and Windows, CI running tests on both | - | Not started |
| 4 | `backup-restore` | Back up and restore every file the installer may modify | 3 | Not started |
| 5 | `agent-adapters-skills` | Install the embedded skills into the three agents | 1, 3, 4 | Not started |
| 6 | `mcp-config` | Register MCP servers per agent | 3, 4 | Not started |
| 7 | `component-installers` | Download, verify and install Engram, cbm, RTK, Headroom and the logbook | 3, 4 | Not started |
| 8 | `hooks-and-rules` | Hooks, global instructions and the persona | 3, 4 | Not started |
| 9 | `model-per-phase` | Model per phase across the supported agents | 3, 4 | Not started |
| 10 | `stack-detection` | Detect the project stack and enable the skills and rules for it | 3, 4, 5, 8 | Not started |
| 11 | `release-workflow` | Published binaries with checksums | 3 | Not started |

Features 5 to 10 depend on `backup-restore` because each of them writes to a user's configuration.
