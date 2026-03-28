// Package swarm implements the Vortex Agent Swarm infrastructure on the Go side.
//
// Architecture:
//
//	Python Swarm Service ──► Go internal/swarm/* ──► LLM / VCS / DB
//
// Go is the credential holder: all LLM calls, VCS tokens, and database access
// flow through this package. Python agents authenticate with per-agent JWTs
// and call Go endpoints for everything.
package swarm
