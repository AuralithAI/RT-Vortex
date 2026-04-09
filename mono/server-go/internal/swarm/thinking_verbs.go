// Package swarm — Rotating "thinking verbs"
// animated status indicator shown while LLMs are processing.
//
// The verb list is also available on the frontend (components/swarm/thinking-verb-rotator.tsx)
// so it can animate instantly without an API call. This Go-side copy is
// used when broadcasting agent status events over WebSocket.
package swarm

import "sync/atomic"

// ThinkingVerbs is a long, curated list of developer-focused action verbs
// displayed while an LLM provider is still generating its response. Inspired
// by the Claude CLI "thinking" animation — keeps the UI alive and playful.
var ThinkingVerbs = []string{
	// ── Core reasoning ──────────────────────────────────────────────────
	"reasoning deeply",
	"synthesizing thoughts",
	"distilling insights",
	"crystallizing ideas",
	"pondering possibilities",
	"hypothesis testing",
	"chain-of-thought stepping",
	"self-reflecting",
	"emergent thinking",
	"insight harvesting",
	"thought compressing",
	"idea refining",

	// ── Pattern & structure ─────────────────────────────────────────────
	"weaving patterns",
	"forging connections",
	"pattern matching",
	"semantic stitching",
	"context surfing",
	"knowledge retrieving",

	// ── ML / neural flavoured ───────────────────────────────────────────
	"chunking context",
	"munging tokens",
	"embedding vectors",
	"gradient descending",
	"attention focusing",
	"token dancing",
	"latent space navigating",
	"vector vortexing",
	"probabilistic dreaming",
	"memory consolidating",
	"orchestrating neurons",
	"parallel processing",

	// ── Code-review specific ────────────────────────────────────────────
	"scanning diff hunks",
	"tracing call graphs",
	"analysing dependencies",
	"mapping code paths",
	"cross-referencing modules",
	"evaluating complexity",
	"checking invariants",
	"verifying contracts",
	"linting logic",
	"profiling hot paths",

	// ── Creative / fun ──────────────────────────────────────────────────
	"quantum leaping",
	"creativity sparking",
	"hallucination checking",
	"coherence tuning",
	"wisdom distilling",
	"entropy reducing",
	"signal amplifying",
	"noise filtering",
	"context window juggling",
	"attention head aligning",
	"transformer layering",
	"weight adjusting",
	"beam searching",
	"temperature sampling",

	// ── RTVortex-flavoured ──────────────────────────────────────────────
	"vortex spinning",
	"swarm coordinating",
	"consensus building",
	"agent syncing",
	"plan drafting",
	"diff assembling",
	"review composing",
	"insight merging",
	"feedback integrating",
	"strategy forming",
}

// thinkingVerbIdx is the global rotating index for GetNextThinkingVerb.
var thinkingVerbIdx uint64

// GetNextThinkingVerb returns the next verb in the list, rotating atomically.
// Safe for concurrent use from multiple goroutines.
func GetNextThinkingVerb() string {
	idx := atomic.AddUint64(&thinkingVerbIdx, 1) - 1
	return ThinkingVerbs[idx%uint64(len(ThinkingVerbs))]
}
