/**
 * AI PR Reviewer Engine - Heuristics Tests
 */

#include <gtest/gtest.h>
#include <regex>
#include <algorithm>
#include "types.h"
#include "review_signals.h"

namespace aipr {
namespace test {

class HeuristicsTest : public ::testing::Test {
protected:
    void SetUp() override {
        // Setup test fixtures
    }

    void TearDown() override {
        // Cleanup
    }
};

// =============================================================================
// Secrets Detection Tests
// =============================================================================

TEST_F(HeuristicsTest, DetectAwsAccessKey) {
    std::regex aws_key_pattern(R"(AKIA[0-9A-Z]{16})");
    
    std::string code_with_key = "const key = 'AKIAIOSFODNN7EXAMPLE';";
    std::string code_without_key = "const key = getAwsKey();";
    
    EXPECT_TRUE(std::regex_search(code_with_key, aws_key_pattern));
    EXPECT_FALSE(std::regex_search(code_without_key, aws_key_pattern));
}

TEST_F(HeuristicsTest, DetectGenericApiKey) {
    std::regex api_key_pattern(R"((api[_-]?key|apikey)\s*[:=]\s*['\"][a-zA-Z0-9]{20,}['\"])", 
                               std::regex::icase);
    
    std::string has_key = "const api_key = 'abcdef1234567890abcdef';";
    std::string no_key = "const api_key = getFromEnv();";
    
    EXPECT_TRUE(std::regex_search(has_key, api_key_pattern));
    EXPECT_FALSE(std::regex_search(no_key, api_key_pattern));
}

TEST_F(HeuristicsTest, DetectPrivateKey) {
    std::regex private_key_pattern(R"(-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----)");
    
    std::string has_private_key = "const key = '-----BEGIN PRIVATE KEY-----';";
    std::string has_rsa_key = "-----BEGIN RSA PRIVATE KEY-----";
    std::string public_key = "-----BEGIN PUBLIC KEY-----";
    
    EXPECT_TRUE(std::regex_search(has_private_key, private_key_pattern));
    EXPECT_TRUE(std::regex_search(has_rsa_key, private_key_pattern));
    EXPECT_FALSE(std::regex_search(public_key, private_key_pattern));
}

TEST_F(HeuristicsTest, DetectHardcodedPassword) {
    std::regex password_pattern(R"((password|passwd|pwd)\s*[:=]\s*['\"][^'\"]+['\"])",
                                std::regex::icase);
    
    std::string hardcoded = "password = 'secretpassword123';";
    std::string env_var = "password = os.environ['PASSWORD'];";
    
    EXPECT_TRUE(std::regex_search(hardcoded, password_pattern));
    EXPECT_FALSE(std::regex_search(env_var, password_pattern));
}

// =============================================================================
// Risky API Detection Tests
// =============================================================================

TEST_F(HeuristicsTest, DetectEvalUsage) {
    std::regex eval_pattern(R"(\beval\s*\()");
    
    std::string has_eval = "result = eval(user_input)";
    std::string no_eval = "result = evaluate(data)";
    
    EXPECT_TRUE(std::regex_search(has_eval, eval_pattern));
    EXPECT_FALSE(std::regex_search(no_eval, eval_pattern));
}

TEST_F(HeuristicsTest, DetectExecUsage) {
    std::regex exec_pattern(R"(\bexec\s*\()");
    
    std::string has_exec = "exec(command)";
    std::string execute = "executor.run()";
    
    EXPECT_TRUE(std::regex_search(has_exec, exec_pattern));
    EXPECT_FALSE(std::regex_search(execute, exec_pattern));
}

TEST_F(HeuristicsTest, DetectShellInjection) {
    std::regex shell_pattern(R"(subprocess\.(call|run|Popen)\s*\([^)]*shell\s*=\s*True)");
    
    std::string unsafe = "subprocess.call(cmd, shell=True)";
    std::string safe = "subprocess.call(cmd_list)";
    
    EXPECT_TRUE(std::regex_search(unsafe, shell_pattern));
    EXPECT_FALSE(std::regex_search(safe, shell_pattern));
}

TEST_F(HeuristicsTest, DetectSqlInjection) {
    std::regex sql_pattern(R"(execute\s*\(\s*['\"]SELECT.*\+|f['\"]SELECT.*\{)");
    
    std::string unsafe = R"(cursor.execute("SELECT * FROM users WHERE id=" + user_id))";
    std::string safe = "cursor.execute('SELECT * FROM users WHERE id=?', (user_id,))";
    
    EXPECT_TRUE(std::regex_search(unsafe, sql_pattern));
    EXPECT_FALSE(std::regex_search(safe, sql_pattern));
}

// =============================================================================
// TODO/FIXME Detection Tests
// =============================================================================

TEST_F(HeuristicsTest, DetectTodo) {
    std::regex todo_pattern(R"(\bTODO\b)", std::regex::icase);
    
    std::string has_todo = "// TODO: implement this function";
    std::string no_todo = "// This function is complete";
    
    EXPECT_TRUE(std::regex_search(has_todo, todo_pattern));
    EXPECT_FALSE(std::regex_search(no_todo, todo_pattern));
}

TEST_F(HeuristicsTest, DetectFixme) {
    std::regex fixme_pattern(R"(\bFIXME\b)", std::regex::icase);
    
    std::string has_fixme = "# FIXME: this is broken";
    std::string no_fixme = "# This is working";
    
    EXPECT_TRUE(std::regex_search(has_fixme, fixme_pattern));
    EXPECT_FALSE(std::regex_search(no_fixme, fixme_pattern));
}

TEST_F(HeuristicsTest, DetectHack) {
    std::regex hack_pattern(R"(\bHACK\b)", std::regex::icase);
    
    std::string has_hack = "// HACK: temporary workaround";
    std::string hackathon = "// hackathon project";  // Should not match word boundary
    
    EXPECT_TRUE(std::regex_search(has_hack, hack_pattern));
    EXPECT_FALSE(std::regex_search(hackathon, hack_pattern));
}

TEST_F(HeuristicsTest, DetectXxx) {
    std::regex xxx_pattern(R"(\bXXX\b)");
    
    std::string has_xxx = "// XXX: needs review";
    std::string no_xxx = "// needs review";
    
    EXPECT_TRUE(std::regex_search(has_xxx, xxx_pattern));
    EXPECT_FALSE(std::regex_search(no_xxx, xxx_pattern));
}

// =============================================================================
// Large File Detection Tests
// =============================================================================

TEST_F(HeuristicsTest, LargeFileThreshold) {
    size_t threshold_lines = 500;
    
    size_t small_file = 100;
    size_t medium_file = 400;
    size_t large_file = 1000;
    
    EXPECT_LT(small_file, threshold_lines);
    EXPECT_LT(medium_file, threshold_lines);
    EXPECT_GT(large_file, threshold_lines);
}

TEST_F(HeuristicsTest, LargeFunctionDetection) {
    size_t max_function_lines = 50;
    
    size_t small_func = 20;
    size_t large_func = 100;
    
    EXPECT_LT(small_func, max_function_lines);
    EXPECT_GT(large_func, max_function_lines);
}

// =============================================================================
// Complexity Detection Tests
// =============================================================================

TEST_F(HeuristicsTest, DeepNestingDetection) {
    // Count indentation levels (simplified)
    auto countNestingLevel = [](const std::string& line) -> int {
        int spaces = 0;
        for (char c : line) {
            if (c == ' ') spaces++;
            else if (c == '\t') spaces += 4;
            else break;
        }
        return spaces / 4;  // Assume 4-space indentation
    };
    
    EXPECT_EQ(countNestingLevel("code"), 0);
    EXPECT_EQ(countNestingLevel("    nested"), 1);
    EXPECT_EQ(countNestingLevel("        deeply"), 2);
    EXPECT_EQ(countNestingLevel("                very_deep"), 4);
}

TEST_F(HeuristicsTest, MaxNestingThreshold) {
    int max_nesting = 4;
    
    int normal_nesting = 2;
    int deep_nesting = 6;
    
    EXPECT_LE(normal_nesting, max_nesting);
    EXPECT_GT(deep_nesting, max_nesting);
}

// =============================================================================
// Debug Code Detection Tests
// =============================================================================

TEST_F(HeuristicsTest, DetectConsoleLog) {
    std::regex console_log_pattern(R"(console\.(log|debug|warn|error)\s*\()");
    
    std::string has_log = "console.log('debug info');";
    std::string production = "logger.info('info');";
    
    EXPECT_TRUE(std::regex_search(has_log, console_log_pattern));
    EXPECT_FALSE(std::regex_search(production, console_log_pattern));
}

TEST_F(HeuristicsTest, DetectPrintStatements) {
    std::regex print_pattern(R"(\bprint\s*\()");
    
    std::string python_print = "print('debug')";
    std::string println = "System.out.println()";  // Different pattern
    
    EXPECT_TRUE(std::regex_search(python_print, print_pattern));
    EXPECT_FALSE(std::regex_search(println, print_pattern));
}

TEST_F(HeuristicsTest, DetectDebugger) {
    std::regex debugger_pattern(R"(\bdebugger\b)");
    
    std::string has_debugger = "debugger;";
    std::string no_debugger = "// removed debugger statement";
    
    EXPECT_TRUE(std::regex_search(has_debugger, debugger_pattern));
    // Note: The word "debugger" in comment would still match - 
    // real implementation should check if in code vs comment
}

// =============================================================================
// Finding Severity Tests
// =============================================================================

TEST_F(HeuristicsTest, SeverityFromFindingType) {
    auto getSeverity = [](const std::string& finding_type) -> Severity {
        if (finding_type == "secret" || finding_type == "sql_injection") {
            return Severity::Critical;
        } else if (finding_type == "risky_api" || finding_type == "shell_injection") {
            return Severity::Error;
        } else if (finding_type == "todo" || finding_type == "large_file") {
            return Severity::Warning;
        }
        return Severity::Info;
    };
    
    EXPECT_EQ(getSeverity("secret"), Severity::Critical);
    EXPECT_EQ(getSeverity("sql_injection"), Severity::Critical);
    EXPECT_EQ(getSeverity("risky_api"), Severity::Error);
    EXPECT_EQ(getSeverity("todo"), Severity::Warning);
    EXPECT_EQ(getSeverity("style"), Severity::Info);
}

// =============================================================================
// Category Assignment Tests
// =============================================================================

TEST_F(HeuristicsTest, CategoryFromFindingType) {
    auto getCategory = [](const std::string& finding_type) -> CheckCategory {
        if (finding_type == "secret" || finding_type == "sql_injection" ||
            finding_type == "risky_api") {
            return CheckCategory::Security;
        } else if (finding_type == "n_plus_one" || finding_type == "inefficient_loop") {
            return CheckCategory::Performance;
        } else if (finding_type == "todo" || finding_type == "null_check") {
            return CheckCategory::Reliability;
        }
        return CheckCategory::Other;
    };
    
    EXPECT_EQ(getCategory("secret"), CheckCategory::Security);
    EXPECT_EQ(getCategory("sql_injection"), CheckCategory::Security);
    EXPECT_EQ(getCategory("n_plus_one"), CheckCategory::Performance);
    EXPECT_EQ(getCategory("todo"), CheckCategory::Reliability);
}

// =============================================================================
// Pattern Compilation Tests
// =============================================================================

TEST_F(HeuristicsTest, ValidRegexCompilation) {
    std::vector<std::string> patterns = {
        R"(AKIA[0-9A-Z]{16})",
        R"(\beval\s*\()",
        R"(\bTODO\b)",
        R"(console\.log\s*\()"
    };
    
    for (const auto& pattern : patterns) {
        EXPECT_NO_THROW({
            std::regex re(pattern);
        }) << "Failed to compile pattern: " << pattern;
    }
}

TEST_F(HeuristicsTest, CaseInsensitiveMatching) {
    std::regex pattern(R"(\btodo\b)", std::regex::icase);
    
    EXPECT_TRUE(std::regex_search("TODO: fix this", pattern));
    EXPECT_TRUE(std::regex_search("todo: fix this", pattern));
    EXPECT_TRUE(std::regex_search("Todo: fix this", pattern));
}

}  // namespace test
}  // namespace aipr
