/**
 * AI PR Reviewer - Review Signals Interface
 */

#ifndef AIPR_REVIEW_SIGNALS_H
#define AIPR_REVIEW_SIGNALS_H

#include "types.h"
#include <string>
#include <vector>
#include <memory>

namespace aipr {

/**
 * Heuristic check interface
 */
class HeuristicCheck {
public:
    virtual ~HeuristicCheck() = default;
    
    virtual std::string getId() const = 0;
    virtual CheckCategory getCategory() const = 0;
    virtual std::string getDescription() const = 0;
    
    /**
     * Run the check on a diff
     */
    virtual std::vector<HeuristicFinding> check(
        const ParsedDiff& diff
    ) = 0;
};

/**
 * Built-in heuristic checks
 */
namespace heuristics {

/**
 * Detect potential secrets (API keys, passwords, etc.)
 */
class SecretsDetector : public HeuristicCheck {
public:
    std::string getId() const override { return "secrets-detector"; }
    CheckCategory getCategory() const override { return CheckCategory::Security; }
    std::string getDescription() const override { 
        return "Detects potential secrets, API keys, and credentials in code";
    }
    std::vector<HeuristicFinding> check(const ParsedDiff& diff) override;
    
    // Configuration
    void addPattern(const std::string& name, const std::string& regex);
    void setRedactionEnabled(bool enabled);
    
private:
    std::vector<std::pair<std::string, std::string>> patterns_;
    bool redaction_enabled_ = true;
};

/**
 * Detect risky API usage
 */
class RiskyApiDetector : public HeuristicCheck {
public:
    std::string getId() const override { return "risky-api-detector"; }
    CheckCategory getCategory() const override { return CheckCategory::Security; }
    std::string getDescription() const override {
        return "Detects usage of risky APIs (exec, eval, unsafe functions)";
    }
    std::vector<HeuristicFinding> check(const ParsedDiff& diff) override;
};

/**
 * Detect TODO/FIXME/HACK comments
 */
class TodoDetector : public HeuristicCheck {
public:
    std::string getId() const override { return "todo-detector"; }
    CheckCategory getCategory() const override { return CheckCategory::Reliability; }
    std::string getDescription() const override {
        return "Detects new TODO/FIXME/HACK comments that may need attention";
    }
    std::vector<HeuristicFinding> check(const ParsedDiff& diff) override;
};

/**
 * Detect large files being added
 */
class LargeFileDetector : public HeuristicCheck {
public:
    std::string getId() const override { return "large-file-detector"; }
    CheckCategory getCategory() const override { return CheckCategory::Other; }
    std::string getDescription() const override {
        return "Detects unusually large file additions";
    }
    std::vector<HeuristicFinding> check(const ParsedDiff& diff) override;
    
    void setThreshold(size_t lines) { threshold_ = lines; }
    
private:
    size_t threshold_ = 500;
};

/**
 * Detect test file changes without corresponding tests
 */
class TestCoverageHint : public HeuristicCheck {
public:
    std::string getId() const override { return "test-coverage-hint"; }
    CheckCategory getCategory() const override { return CheckCategory::Testing; }
    std::string getDescription() const override {
        return "Suggests adding tests when source files change without test changes";
    }
    std::vector<HeuristicFinding> check(const ParsedDiff& diff) override;
};

/**
 * Detect config file changes
 */
class ConfigChangeDetector : public HeuristicCheck {
public:
    std::string getId() const override { return "config-change-detector"; }
    CheckCategory getCategory() const override { return CheckCategory::Architecture; }
    std::string getDescription() const override {
        return "Highlights configuration file changes that may affect deployment";
    }
    std::vector<HeuristicFinding> check(const ParsedDiff& diff) override;
};

} // namespace heuristics

/**
 * Review signals aggregator
 */
class ReviewSignals {
public:
    ReviewSignals();
    ~ReviewSignals();
    
    /**
     * Register a heuristic check
     */
    void registerCheck(std::unique_ptr<HeuristicCheck> check);
    
    /**
     * Run all registered checks
     */
    std::vector<HeuristicFinding> runAllChecks(const ParsedDiff& diff);
    
    /**
     * Run specific checks by category
     */
    std::vector<HeuristicFinding> runChecks(
        const ParsedDiff& diff,
        const std::vector<CheckCategory>& categories
    );
    
    /**
     * Get registered check IDs
     */
    std::vector<std::string> getRegisteredChecks() const;
    
    /**
     * Factory with default checks
     */
    static std::unique_ptr<ReviewSignals> createWithDefaults();
    
private:
    std::vector<std::unique_ptr<HeuristicCheck>> checks_;
};

} // namespace aipr

#endif // AIPR_REVIEW_SIGNALS_H
