// Package skills embeds the agent-neutral skills shipped by atlas-ai.
package skills

import "embed"

// FS holds one SKILL.md per skill directory, exactly as it is installed
// into each supported agent.
//
//go:embed */SKILL.md
var FS embed.FS
