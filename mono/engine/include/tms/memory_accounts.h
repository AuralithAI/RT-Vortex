/**
 * Memory Accounts — Domain-aware query routing
 *
 * Classifies CodeChunks and queries into one of four accounts:
 *   DEV      — application code (default)
 *   OPS      — CI/CD, Docker, Makefiles, infrastructure
 *   SECURITY — auth, crypto, secrets, tokens
 *   HISTORY  — changelogs, PR metadata, commit references
 *
 * During ingestion each chunk receives an "account:X" tag.
 * During retrieval the query is classified and routed to the top-2
 * accounts; results are RRF-merged before CMA.
 *
 * Gated: config.memory_accounts_enabled = false (default OFF).
 */

#pragma once

#include <string>
#include <vector>
#include "tms/tms_types.h"

namespace aipr::tms {

// ── Account enum ───────────────────────────────────────────────────────────

enum class MemoryAccount {
    DEV      = 0,
    OPS      = 1,
    SECURITY = 2,
    HISTORY  = 3,
};

/** Human-readable name for an account (lowercase, matches tag). */
const char* accountName(MemoryAccount a);

/** Number of defined accounts. */
constexpr int ACCOUNT_COUNT = 4;

// ── Classifier ─────────────────────────────────────────────────────────────

class MemoryAccountClassifier {
public:
    /**
     * Classify a chunk into a memory account.
     *
     * Rules (first match wins):
     *   .github/, Dockerfile, Makefile, Jenkinsfile, *.yml CI → OPS
     *   auth, crypto, token, secret, password, cve in path/content → SECURITY
     *   pr_, CHANGELOG, commit, migration, HISTORY → HISTORY
     *   else → DEV
     */
    MemoryAccount classify(const CodeChunk& chunk) const;

    /**
     * Classify a free-text query into ranked accounts.
     * Returns all accounts sorted by confidence (highest first).
     * The top-2 are used for retrieval routing.
     */
    std::vector<MemoryAccount> classifyQuery(const std::string& query_text) const;

    /** Build the "account:xxx" tag string for a given account. */
    static std::string accountTag(MemoryAccount a);
};

} // namespace aipr::tms
