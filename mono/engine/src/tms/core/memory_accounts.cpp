/**
 * MemoryAccountClassifier — Domain-aware chunk and query classification
 *
 * Lightweight keyword/path-pattern classifier.  No ML — pure heuristic
 * so it runs in microseconds per chunk during ingestion and per query
 * during retrieval.
 */

#include "tms/memory_accounts.h"
#include "metrics.h"

#include <algorithm>
#include <cctype>
#include <string>
#include <vector>

namespace aipr::tms {

// ── accountName ────────────────────────────────────────────────────────────

const char* accountName(MemoryAccount a) {
    switch (a) {
        case MemoryAccount::DEV:      return "dev";
        case MemoryAccount::OPS:      return "ops";
        case MemoryAccount::SECURITY: return "security";
        case MemoryAccount::HISTORY:  return "history";
    }
    return "dev";
}

// ── helpers ────────────────────────────────────────────────────────────────

static std::string toLower(const std::string& s) {
    std::string out;
    out.reserve(s.size());
    for (char c : s) out.push_back(static_cast<char>(std::tolower(static_cast<unsigned char>(c))));
    return out;
}

static bool containsAny(const std::string& haystack,
                         const std::vector<std::string>& needles) {
    for (const auto& n : needles) {
        if (haystack.find(n) != std::string::npos) return true;
    }
    return false;
}

// ── OPS patterns ───────────────────────────────────────────────────────────

static const std::vector<std::string> OPS_PATH_PATTERNS = {
    ".github/", ".gitlab-ci", "jenkinsfile", "dockerfile",
    "docker-compose", "makefile", ".circleci/", ".travis",
    "terraform/", "ansible/", "k8s/", "kubernetes/",
    "helm/", "deploy/", "infra/", "infrastructure/",
    "ci/", "cd/", ".buildkite/",
};

static const std::vector<std::string> OPS_EXT_PATTERNS = {
    ".yml", ".yaml",  // CI pipelines, k8s manifests
};

// Only match YAML files as OPS if they're CI/infra-related
static const std::vector<std::string> OPS_YAML_KEYWORDS = {
    "pipeline", "workflow", "deploy", "build", "stage",
    "docker", "container", "service", "helm", "terraform",
    "ansible", "kubernetes", "k8s", "ci", "cd",
};

// ── SECURITY patterns ──────────────────────────────────────────────────────

static const std::vector<std::string> SECURITY_PATTERNS = {
    "auth", "crypto", "token", "secret", "password",
    "credential", "oauth", "jwt", "saml", "ssl",
    "tls", "certificate", "encrypt", "decrypt", "hash",
    "rbac", "permission", "acl", "cve", "vulnerability",
    "security", "sanitize", "xss", "csrf", "injection",
};

// ── HISTORY patterns ───────────────────────────────────────────────────────

static const std::vector<std::string> HISTORY_PATTERNS = {
    "changelog", "change_log", "changes.md", "history.md",
    "migration", "pr_", "pull_request", "commit",
    "release_note", "version_history", "whatsnew",
};

// ── classify (chunk) ───────────────────────────────────────────────────────

MemoryAccount MemoryAccountClassifier::classify(const CodeChunk& chunk) const {
    std::string path_lower = toLower(chunk.file_path);
    std::string type_lower = toLower(chunk.type);

    // --- OPS check ---
    if (containsAny(path_lower, OPS_PATH_PATTERNS)) {
        return MemoryAccount::OPS;
    }
    // YAML files: only OPS if content looks like CI/infra
    bool is_yaml = path_lower.size() >= 4 &&
        (path_lower.compare(path_lower.size()-4, 4, ".yml") == 0 ||
         path_lower.compare(path_lower.size()-5, 5, ".yaml") == 0);
    if (is_yaml) {
        std::string content_lower = toLower(chunk.content);
        if (containsAny(content_lower, OPS_YAML_KEYWORDS)) {
            return MemoryAccount::OPS;
        }
    }
    // Makefile, Dockerfile by name
    {
        auto fname = path_lower;
        auto slash = fname.rfind('/');
        if (slash != std::string::npos) fname = fname.substr(slash + 1);
        if (fname == "makefile" || fname == "dockerfile" ||
            fname == "docker-compose.yml" || fname == "docker-compose.yaml" ||
            fname == "jenkinsfile" || fname == "vagrantfile") {
            return MemoryAccount::OPS;
        }
    }

    // --- SECURITY check (path + content) ---
    if (containsAny(path_lower, SECURITY_PATTERNS)) {
        return MemoryAccount::SECURITY;
    }
    // Check content for security keywords (only first 2KB to keep fast)
    {
        std::string content_prefix = toLower(
            chunk.content.substr(0, std::min<size_t>(chunk.content.size(), 2048)));
        if (containsAny(content_prefix, SECURITY_PATTERNS)) {
            // Require at least 2 matches to avoid false positives from
            // variable names like "token_count"
            int matches = 0;
            for (const auto& kw : SECURITY_PATTERNS) {
                if (content_prefix.find(kw) != std::string::npos) {
                    matches++;
                    if (matches >= 2) return MemoryAccount::SECURITY;
                }
            }
        }
    }

    // --- HISTORY check ---
    if (containsAny(path_lower, HISTORY_PATTERNS)) {
        return MemoryAccount::HISTORY;
    }
    if (type_lower == "changelog" || type_lower == "migration") {
        return MemoryAccount::HISTORY;
    }

    // --- Default: DEV ---
    return MemoryAccount::DEV;
}

// ── classifyQuery (query text → ranked accounts) ───────────────────────────

std::vector<MemoryAccount> MemoryAccountClassifier::classifyQuery(
    const std::string& query_text) const
{
    std::string q = toLower(query_text);

    // Score each account by keyword presence
    struct Scored {
        MemoryAccount account;
        int score;
    };

    std::vector<Scored> scores = {
        {MemoryAccount::DEV, 0},
        {MemoryAccount::OPS, 0},
        {MemoryAccount::SECURITY, 0},
        {MemoryAccount::HISTORY, 0},
    };

    // OPS keywords
    static const std::vector<std::string> OPS_QUERY_KW = {
        "docker", "dockerfile", "ci", "cd", "pipeline", "deploy",
        "build", "makefile", "kubernetes", "k8s", "helm", "terraform",
        "ansible", "jenkins", "github actions", "workflow", "container",
        "infrastructure", "infra", "devops",
    };
    for (const auto& kw : OPS_QUERY_KW) {
        if (q.find(kw) != std::string::npos) scores[1].score += 2;
    }

    // SECURITY keywords
    for (const auto& kw : SECURITY_PATTERNS) {
        if (q.find(kw) != std::string::npos) scores[2].score += 2;
    }

    // HISTORY keywords
    static const std::vector<std::string> HISTORY_QUERY_KW = {
        "changelog", "history", "migration", "release note",
        "commit", "pull request", "pr ", "version",
        "what changed", "breaking change", "deprecat",
    };
    for (const auto& kw : HISTORY_QUERY_KW) {
        if (q.find(kw) != std::string::npos) scores[3].score += 2;
    }

    // DEV is default: gets a base score of 1 so it always appears
    scores[0].score += 1;

    // Sort by score descending
    std::sort(scores.begin(), scores.end(),
              [](const Scored& a, const Scored& b) { return a.score > b.score; });

    std::vector<MemoryAccount> result;
    for (const auto& s : scores) {
        result.push_back(s.account);
    }

    // Record which account is top
    auto top = result[0];
    switch (top) {
        case MemoryAccount::DEV:
            metrics::Registry::instance().incCounter(metrics::ACCOUNT_QUERIES_DEV_TOTAL);
            break;
        case MemoryAccount::OPS:
            metrics::Registry::instance().incCounter(metrics::ACCOUNT_QUERIES_OPS_TOTAL);
            break;
        case MemoryAccount::SECURITY:
            metrics::Registry::instance().incCounter(metrics::ACCOUNT_QUERIES_SECURITY_TOTAL);
            break;
        case MemoryAccount::HISTORY:
            metrics::Registry::instance().incCounter(metrics::ACCOUNT_QUERIES_HISTORY_TOTAL);
            break;
    }

    return result;
}

// ── accountTag ─────────────────────────────────────────────────────────────

std::string MemoryAccountClassifier::accountTag(MemoryAccount a) {
    return std::string("account:") + accountName(a);
}

} // namespace aipr::tms
