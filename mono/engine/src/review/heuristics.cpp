/**
 * AI PR Reviewer - Heuristic Checks
 * 
 * Non-LLM pattern-based checks for common issues.
 */

#include "review_signals.h"
#include "types.h"
#include <string>
#include <vector>
#include <regex>
#include <unordered_set>

namespace aipr {
namespace heuristics {

// ============================================================================
// Secrets Detector
// ============================================================================

std::vector<HeuristicFinding> SecretsDetector::check(const ParsedDiff& diff) {
    std::vector<HeuristicFinding> findings;
    
    // Default patterns
    std::vector<std::pair<std::string, std::regex>> default_patterns = {
        {"AWS Access Key", std::regex(R"(AKIA[0-9A-Z]{16})")},
        {"GitHub Token", std::regex(R"(gh[ps]_[a-zA-Z0-9]{36})")},
        {"GitHub Token (fine-grained)", std::regex(R"(github_pat_[a-zA-Z0-9_]{22,})")},
        {"OpenAI API Key", std::regex(R"(sk-[a-zA-Z0-9]{48})")},
        {"Slack Token", std::regex(R"(xox[baprs]-[0-9a-zA-Z-]{10,})")},
        {"Private Key", std::regex(R"(-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----)")},
        {"Generic API Key", std::regex(R"((?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?[a-zA-Z0-9_-]{20,}['"]?)")},
        {"Generic Secret", std::regex(R"((?i)(secret|password|passwd|pwd)\s*[:=]\s*['"]?[^\s'"]{8,}['"]?)")},
        {"JWT Token", std::regex(R"(eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*)")},
        {"Connection String", std::regex(R"((?i)(mongodb|postgres|mysql|redis|amqp)://[^\s'"]+)")},
    };
    
    for (const auto& hunk : diff.hunks) {
        size_t line_num = hunk.new_start;
        
        for (const auto& line : hunk.added_lines) {
            for (const auto& [name, pattern] : default_patterns) {
                std::smatch match;
                if (std::regex_search(line, match, pattern)) {
                    HeuristicFinding finding;
                    finding.id = "secret-" + std::to_string(findings.size());
                    finding.category = CheckCategory::Security;
                    finding.severity = Severity::Critical;
                    finding.confidence = 0.9f;
                    finding.file_path = hunk.file_path;
                    finding.line = line_num;
                    finding.message = "Potential " + name + " detected in added code";
                    finding.evidence = redaction_enabled_ ? "[REDACTED]" : match[0].str();
                    finding.suggestion = "Remove this secret and use environment variables or a secrets manager instead";
                    findings.push_back(finding);
                    break;  // One finding per line
                }
            }
            line_num++;
        }
    }
    
    return findings;
}

void SecretsDetector::addPattern(const std::string& name, const std::string& regex) {
    patterns_.push_back({name, regex});
}

void SecretsDetector::setRedactionEnabled(bool enabled) {
    redaction_enabled_ = enabled;
}

// ============================================================================
// Risky API Detector
// ============================================================================

std::vector<HeuristicFinding> RiskyApiDetector::check(const ParsedDiff& diff) {
    std::vector<HeuristicFinding> findings;
    
    std::vector<std::tuple<std::regex, std::string, Severity>> patterns = {
        // Command injection risks
        {std::regex(R"(\beval\s*\()"), "eval() can execute arbitrary code", Severity::Error},
        {std::regex(R"(\bexec\s*\()"), "exec() can execute arbitrary commands", Severity::Error},
        {std::regex(R"(subprocess\.(?:call|run|Popen)\s*\([^)]*shell\s*=\s*True)"), 
            "subprocess with shell=True is vulnerable to injection", Severity::Error},
        {std::regex(R"(os\.system\s*\()"), "os.system() is vulnerable to command injection", Severity::Warning},
        {std::regex(R"(Runtime\.getRuntime\(\)\.exec)"), "Runtime.exec() can be dangerous", Severity::Warning},
        
        // SQL injection risks
        {std::regex(R"((?:execute|query)\s*\([^)]*\+|(?:execute|query)\s*\([^)]*\%)"), 
            "Potential SQL injection - use parameterized queries", Severity::Error},
        {std::regex(R"(f["'][^"']*(?:SELECT|INSERT|UPDATE|DELETE)[^"']*\{)"), 
            "f-string in SQL query - use parameterized queries", Severity::Error},
        
        // Insecure deserialization
        {std::regex(R"(pickle\.loads?\s*\()"), "pickle can execute arbitrary code during deserialization", Severity::Warning},
        {std::regex(R"(yaml\.(?:load|unsafe_load)\s*\([^)]*Loader\s*=\s*yaml\.(?:Unsafe)?Loader)"), 
            "Unsafe YAML loading can execute arbitrary code", Severity::Warning},
        {std::regex(R"(ObjectInputStream)"), "Java deserialization can be exploited", Severity::Info},
        
        // Crypto weaknesses
        {std::regex(R"(\bMD5\b|\bmd5\()"), "MD5 is cryptographically weak", Severity::Warning},
        {std::regex(R"(\bSHA1\b|\bsha1\()"), "SHA1 is cryptographically weak", Severity::Info},
        {std::regex(R"(Math\.random\(\))"), "Math.random() is not cryptographically secure", Severity::Info},
        
        // Other risks
        {std::regex(R"(dangerouslySetInnerHTML)"), "dangerouslySetInnerHTML can cause XSS", Severity::Warning},
        {std::regex(R"(innerHTML\s*=)"), "innerHTML can cause XSS", Severity::Warning},
        {std::regex(R"(\bdebugg?er\b)"), "Debugger statement found", Severity::Info},
    };
    
    for (const auto& hunk : diff.hunks) {
        size_t line_num = hunk.new_start;
        
        for (const auto& line : hunk.added_lines) {
            for (const auto& [pattern, message, severity] : patterns) {
                if (std::regex_search(line, pattern)) {
                    HeuristicFinding finding;
                    finding.id = "risky-api-" + std::to_string(findings.size());
                    finding.category = CheckCategory::Security;
                    finding.severity = severity;
                    finding.confidence = 0.8f;
                    finding.file_path = hunk.file_path;
                    finding.line = line_num;
                    finding.message = message;
                    finding.evidence = line;
                    findings.push_back(finding);
                }
            }
            line_num++;
        }
    }
    
    return findings;
}

// ============================================================================
// TODO Detector
// ============================================================================

std::vector<HeuristicFinding> TodoDetector::check(const ParsedDiff& diff) {
    std::vector<HeuristicFinding> findings;
    
    std::regex todo_pattern(R"((?i)\b(TODO|FIXME|HACK|XXX|BUG|OPTIMIZE)\b[:\s]*(.*))", 
                           std::regex::icase);
    
    for (const auto& hunk : diff.hunks) {
        size_t line_num = hunk.new_start;
        
        for (const auto& line : hunk.added_lines) {
            std::smatch match;
            if (std::regex_search(line, match, todo_pattern)) {
                HeuristicFinding finding;
                finding.id = "todo-" + std::to_string(findings.size());
                finding.category = CheckCategory::Reliability;
                
                std::string keyword = match[1].str();
                std::transform(keyword.begin(), keyword.end(), keyword.begin(), ::toupper);
                
                if (keyword == "FIXME" || keyword == "BUG") {
                    finding.severity = Severity::Warning;
                } else if (keyword == "HACK" || keyword == "XXX") {
                    finding.severity = Severity::Warning;
                } else {
                    finding.severity = Severity::Info;
                }
                
                finding.confidence = 1.0f;
                finding.file_path = hunk.file_path;
                finding.line = line_num;
                finding.message = keyword + " comment added: " + match[2].str();
                finding.evidence = line;
                findings.push_back(finding);
            }
            line_num++;
        }
    }
    
    return findings;
}

// ============================================================================
// Large File Detector
// ============================================================================

std::vector<HeuristicFinding> LargeFileDetector::check(const ParsedDiff& diff) {
    std::vector<HeuristicFinding> findings;
    
    // Count additions per file
    std::unordered_map<std::string, size_t> file_additions;
    for (const auto& hunk : diff.hunks) {
        file_additions[hunk.file_path] += hunk.added_lines.size();
    }
    
    for (const auto& [file_path, additions] : file_additions) {
        if (additions > threshold_) {
            HeuristicFinding finding;
            finding.id = "large-file-" + std::to_string(findings.size());
            finding.category = CheckCategory::Other;
            finding.severity = Severity::Info;
            finding.confidence = 1.0f;
            finding.file_path = file_path;
            finding.message = "Large file addition: " + std::to_string(additions) + " lines added";
            finding.suggestion = "Consider breaking this into smaller, focused changes";
            findings.push_back(finding);
        }
    }
    
    return findings;
}

// ============================================================================
// Test Coverage Hint
// ============================================================================

std::vector<HeuristicFinding> TestCoverageHint::check(const ParsedDiff& diff) {
    std::vector<HeuristicFinding> findings;
    
    // Patterns for test files
    std::vector<std::string> test_patterns = {
        "test", "tests", "spec", "__tests__", "_test.", ".test.", ".spec."
    };
    
    std::unordered_set<std::string> modified_source_files;
    std::unordered_set<std::string> modified_test_files;
    
    for (const auto& file : diff.changed_files) {
        bool is_test = false;
        std::string lower_path = file.path;
        std::transform(lower_path.begin(), lower_path.end(), lower_path.begin(), ::tolower);
        
        for (const auto& pattern : test_patterns) {
            if (lower_path.find(pattern) != std::string::npos) {
                is_test = true;
                break;
            }
        }
        
        if (is_test) {
            modified_test_files.insert(file.path);
        } else {
            // Only count significant source files
            if (file.path.find(".") != std::string::npos &&
                file.path.find("config") == std::string::npos &&
                file.path.find(".md") == std::string::npos) {
                modified_source_files.insert(file.path);
            }
        }
    }
    
    // Check if source files were modified without corresponding test changes
    if (!modified_source_files.empty() && modified_test_files.empty()) {
        HeuristicFinding finding;
        finding.id = "test-coverage-hint";
        finding.category = CheckCategory::Testing;
        finding.severity = Severity::Info;
        finding.confidence = 0.7f;
        finding.message = std::to_string(modified_source_files.size()) + 
                         " source file(s) modified without corresponding test changes";
        finding.suggestion = "Consider adding or updating tests for the modified code";
        findings.push_back(finding);
    }
    
    return findings;
}

// ============================================================================
// Config Change Detector
// ============================================================================

std::vector<HeuristicFinding> ConfigChangeDetector::check(const ParsedDiff& diff) {
    std::vector<HeuristicFinding> findings;
    
    std::vector<std::pair<std::string, std::string>> config_patterns = {
        {"Dockerfile", "Docker configuration"},
        {"docker-compose", "Docker Compose configuration"},
        {".yml", "YAML configuration"},
        {".yaml", "YAML configuration"},
        {".env", "Environment configuration"},
        {"config.", "Configuration file"},
        {".conf", "Configuration file"},
        {"settings.", "Settings file"},
        {"package.json", "Package dependencies"},
        {"requirements.txt", "Python dependencies"},
        {"Gemfile", "Ruby dependencies"},
        {"pom.xml", "Maven configuration"},
        {"build.gradle", "Gradle configuration"},
        {".github/workflows", "GitHub Actions workflow"},
        {".gitlab-ci", "GitLab CI configuration"},
        {"Jenkinsfile", "Jenkins pipeline"},
        {"terraform", "Infrastructure configuration"},
        {"k8s", "Kubernetes configuration"},
        {"kubernetes", "Kubernetes configuration"},
    };
    
    for (const auto& file : diff.changed_files) {
        for (const auto& [pattern, description] : config_patterns) {
            if (file.path.find(pattern) != std::string::npos) {
                HeuristicFinding finding;
                finding.id = "config-change-" + std::to_string(findings.size());
                finding.category = CheckCategory::Architecture;
                finding.severity = Severity::Info;
                finding.confidence = 1.0f;
                finding.file_path = file.path;
                finding.message = description + " changed: " + file.path;
                finding.suggestion = "Verify this change is intentional and won't affect deployments";
                findings.push_back(finding);
                break;
            }
        }
    }
    
    return findings;
}

} // namespace heuristics

// ============================================================================
// Review Signals
// ============================================================================

ReviewSignals::ReviewSignals() = default;
ReviewSignals::~ReviewSignals() = default;

void ReviewSignals::registerCheck(std::unique_ptr<HeuristicCheck> check) {
    checks_.push_back(std::move(check));
}

std::vector<HeuristicFinding> ReviewSignals::runAllChecks(const ParsedDiff& diff) {
    std::vector<HeuristicFinding> all_findings;
    
    for (auto& check : checks_) {
        auto findings = check->check(diff);
        all_findings.insert(all_findings.end(), findings.begin(), findings.end());
    }
    
    return all_findings;
}

std::vector<HeuristicFinding> ReviewSignals::runChecks(
    const ParsedDiff& diff,
    const std::vector<CheckCategory>& categories
) {
    std::unordered_set<CheckCategory> category_set(categories.begin(), categories.end());
    std::vector<HeuristicFinding> filtered_findings;
    
    for (auto& check : checks_) {
        if (category_set.find(check->getCategory()) != category_set.end()) {
            auto findings = check->check(diff);
            filtered_findings.insert(filtered_findings.end(), findings.begin(), findings.end());
        }
    }
    
    return filtered_findings;
}

std::vector<std::string> ReviewSignals::getRegisteredChecks() const {
    std::vector<std::string> ids;
    for (const auto& check : checks_) {
        ids.push_back(check->getId());
    }
    return ids;
}

std::unique_ptr<ReviewSignals> ReviewSignals::createWithDefaults() {
    auto signals = std::make_unique<ReviewSignals>();
    
    signals->registerCheck(std::make_unique<heuristics::SecretsDetector>());
    signals->registerCheck(std::make_unique<heuristics::RiskyApiDetector>());
    signals->registerCheck(std::make_unique<heuristics::TodoDetector>());
    signals->registerCheck(std::make_unique<heuristics::LargeFileDetector>());
    signals->registerCheck(std::make_unique<heuristics::TestCoverageHint>());
    signals->registerCheck(std::make_unique<heuristics::ConfigChangeDetector>());
    
    return signals;
}

} // namespace aipr
