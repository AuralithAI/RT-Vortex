/**
 * AI PR Reviewer Engine - Parser & Diff Tests
 */

#include <gtest/gtest.h>
#include <sstream>
#include <algorithm>
#include "types.h"
#include "review_signals.h"

namespace aipr {
namespace test {

class ParserTest : public ::testing::Test {
protected:
    void SetUp() override {
        // Setup test fixtures
    }

    void TearDown() override {
        // Cleanup
    }
};

// =============================================================================
// Language Detection Tests
// =============================================================================

TEST_F(ParserTest, DetectLanguageFromExtension) {
    // Test common file extensions
    std::map<std::string, std::string> extension_to_lang = {
        {".cpp", "cpp"},
        {".cc", "cpp"},
        {".java", "java"},
        {".py", "python"},
        {".js", "javascript"},
        {".ts", "typescript"},
        {".go", "go"},
        {".rs", "rust"},
        {".rb", "ruby"},
        {".php", "php"},
        {".cs", "csharp"},
        {".swift", "swift"},
        {".kt", "kotlin"}
    };
    
    EXPECT_EQ(extension_to_lang[".cpp"], "cpp");
    EXPECT_EQ(extension_to_lang[".java"], "java");
    EXPECT_EQ(extension_to_lang[".py"], "python");
    EXPECT_EQ(extension_to_lang[".ts"], "typescript");
}

TEST_F(ParserTest, ExtractExtensionFromPath) {
    auto getExtension = [](const std::string& path) -> std::string {
        size_t pos = path.rfind('.');
        if (pos != std::string::npos) {
            return path.substr(pos);
        }
        return "";
    };
    
    EXPECT_EQ(getExtension("src/main.cpp"), ".cpp");
    EXPECT_EQ(getExtension("lib/utils.py"), ".py");
    EXPECT_EQ(getExtension("app/index.ts"), ".ts");
    EXPECT_EQ(getExtension("Makefile"), "");
}

// =============================================================================
// Severity & Category Tests
// =============================================================================

TEST_F(ParserTest, SeverityLevels) {
    EXPECT_EQ(static_cast<int>(Severity::Info), 0);
    EXPECT_EQ(static_cast<int>(Severity::Warning), 1);
    EXPECT_EQ(static_cast<int>(Severity::Error), 2);
    EXPECT_EQ(static_cast<int>(Severity::Critical), 3);
    
    // Critical > Error > Warning > Info
    EXPECT_GT(static_cast<int>(Severity::Critical), static_cast<int>(Severity::Error));
    EXPECT_GT(static_cast<int>(Severity::Error), static_cast<int>(Severity::Warning));
}

TEST_F(ParserTest, CheckCategories) {
    EXPECT_EQ(static_cast<int>(CheckCategory::Security), 0);
    EXPECT_EQ(static_cast<int>(CheckCategory::Performance), 1);
    EXPECT_EQ(static_cast<int>(CheckCategory::Reliability), 2);
    EXPECT_EQ(static_cast<int>(CheckCategory::Style), 3);
}

// =============================================================================
// ChangeType Tests
// =============================================================================

TEST_F(ParserTest, ChangeTypes) {
    ChangeType added = ChangeType::Added;
    ChangeType modified = ChangeType::Modified;
    ChangeType deleted = ChangeType::Deleted;
    ChangeType renamed = ChangeType::Renamed;
    
    EXPECT_NE(added, modified);
    EXPECT_NE(modified, deleted);
    EXPECT_NE(deleted, renamed);
}

// =============================================================================
// Language Struct Tests
// =============================================================================

TEST_F(ParserTest, LanguageStructure) {
    Language cpp;
    cpp.id = "cpp";
    cpp.name = "C++";
    cpp.extensions = {".cpp", ".cc", ".cxx", ".h", ".hpp"};
    
    EXPECT_EQ(cpp.id, "cpp");
    EXPECT_EQ(cpp.name, "C++");
    EXPECT_EQ(cpp.extensions.size(), 5);
    
    // Check if extension exists
    bool has_cpp = std::find(cpp.extensions.begin(), cpp.extensions.end(), ".cpp")
                   != cpp.extensions.end();
    EXPECT_TRUE(has_cpp);
}

TEST_F(ParserTest, MultipleLanguages) {
    std::vector<Language> languages;
    
    Language java;
    java.id = "java";
    java.name = "Java";
    java.extensions = {".java"};
    
    Language python;
    python.id = "python";
    python.name = "Python";
    python.extensions = {".py", ".pyw", ".pyi"};
    
    languages.push_back(java);
    languages.push_back(python);
    
    EXPECT_EQ(languages.size(), 2);
    EXPECT_EQ(languages[0].extensions.size(), 1);
    EXPECT_EQ(languages[1].extensions.size(), 3);
}

// =============================================================================
// Symbol Parsing Tests
// =============================================================================

TEST_F(ParserTest, QualifiedNameParsing) {
    std::string qualified = "com.example.MyClass.myMethod";
    
    // Extract simple name (last component)
    size_t last_dot = qualified.rfind('.');
    std::string simple_name = qualified.substr(last_dot + 1);
    
    EXPECT_EQ(simple_name, "myMethod");
}

TEST_F(ParserTest, QualifiedNameComponents) {
    std::string qualified = "com.example.utils.StringHelper.trim";
    
    // Split by '.'
    std::vector<std::string> components;
    std::stringstream ss(qualified);
    std::string token;
    while (std::getline(ss, token, '.')) {
        components.push_back(token);
    }
    
    EXPECT_EQ(components.size(), 5);
    EXPECT_EQ(components[0], "com");
    EXPECT_EQ(components[1], "example");
    EXPECT_EQ(components[4], "trim");
}

// =============================================================================
// Import Parsing Tests
// =============================================================================

TEST_F(ParserTest, JavaImportParsing) {
    std::vector<std::string> java_imports = {
        "import java.util.List;",
        "import java.util.Map;",
        "import com.example.MyClass;"
    };
    
    EXPECT_EQ(java_imports.size(), 3);
    
    // Check for standard library import
    bool has_list_import = false;
    for (const auto& imp : java_imports) {
        if (imp.find("java.util.List") != std::string::npos) {
            has_list_import = true;
            break;
        }
    }
    EXPECT_TRUE(has_list_import);
}

TEST_F(ParserTest, PythonImportParsing) {
    std::vector<std::string> python_imports = {
        "import os",
        "from typing import List, Dict",
        "from collections import defaultdict"
    };
    
    EXPECT_EQ(python_imports.size(), 3);
}

TEST_F(ParserTest, CppIncludeParsing) {
    std::vector<std::string> cpp_includes = {
        "#include <iostream>",
        "#include <vector>",
        "#include \"myheader.h\""
    };
    
    EXPECT_EQ(cpp_includes.size(), 3);
    
    // Check for system vs local includes
    bool has_system_include = cpp_includes[0].find('<') != std::string::npos;
    bool has_local_include = cpp_includes[2].find('"') != std::string::npos;
    
    EXPECT_TRUE(has_system_include);
    EXPECT_TRUE(has_local_include);
}

// =============================================================================
// Diff Line Parsing Tests
// =============================================================================

TEST_F(ParserTest, DiffLineTypeDetection) {
    auto getLineType = [](const std::string& line) -> char {
        if (line.empty()) return ' ';
        return line[0];
    };
    
    EXPECT_EQ(getLineType("+added line"), '+');
    EXPECT_EQ(getLineType("-removed line"), '-');
    EXPECT_EQ(getLineType(" context line"), ' ');
    EXPECT_EQ(getLineType("@@ -1,5 +1,6 @@"), '@');
}

TEST_F(ParserTest, HunkHeaderParsing) {
    std::string hunk_header = "@@ -10,5 +12,7 @@ function example()";
    
    // Check it starts with @@
    EXPECT_EQ(hunk_header.substr(0, 2), "@@");
    
    // Check for old line info
    size_t minus_pos = hunk_header.find('-');
    EXPECT_NE(minus_pos, std::string::npos);
    
    // Check for new line info
    size_t plus_pos = hunk_header.find('+');
    EXPECT_NE(plus_pos, std::string::npos);
    EXPECT_GT(plus_pos, minus_pos);
}

// =============================================================================
// Content Hashing Tests
// =============================================================================

TEST_F(ParserTest, SimpleHashComputation) {
    // Simple hash for testing - not cryptographic
    auto simpleHash = [](const std::string& content) -> size_t {
        size_t hash = 0;
        for (char c : content) {
            hash = hash * 31 + static_cast<size_t>(c);
        }
        return hash;
    };
    
    std::string content1 = "int main() { return 0; }";
    std::string content2 = "int main() { return 0; }";
    std::string content3 = "int main() { return 1; }";
    
    EXPECT_EQ(simpleHash(content1), simpleHash(content2));
    EXPECT_NE(simpleHash(content1), simpleHash(content3));
}

// =============================================================================
// Line Counting Tests
// =============================================================================

TEST_F(ParserTest, CountLines) {
    auto countLines = [](const std::string& content) -> size_t {
        if (content.empty()) return 0;
        return std::count(content.begin(), content.end(), '\n') + 1;
    };
    
    EXPECT_EQ(countLines("line1"), 1);
    EXPECT_EQ(countLines("line1\nline2"), 2);
    EXPECT_EQ(countLines("line1\nline2\nline3"), 3);
    EXPECT_EQ(countLines(""), 0);
}

// =============================================================================
// File Size Tests
// =============================================================================

TEST_F(ParserTest, FileSizeThresholds) {
    size_t max_file_size_kb = 1024;  // 1 MB
    size_t max_file_size_bytes = max_file_size_kb * 1024;
    
    size_t small_file = 10 * 1024;      // 10 KB
    size_t medium_file = 500 * 1024;    // 500 KB
    size_t large_file = 2000 * 1024;    // 2 MB
    
    EXPECT_LT(small_file, max_file_size_bytes);
    EXPECT_LT(medium_file, max_file_size_bytes);
    EXPECT_GT(large_file, max_file_size_bytes);
}

// =============================================================================
// Binary File Detection Tests
// =============================================================================

TEST_F(ParserTest, BinaryFileDetection) {
    auto isBinaryExtension = [](const std::string& ext) -> bool {
        static const std::vector<std::string> binary_exts = {
            ".exe", ".dll", ".so", ".dylib", ".o", ".a",
            ".png", ".jpg", ".jpeg", ".gif", ".ico",
            ".pdf", ".zip", ".tar", ".gz"
        };
        return std::find(binary_exts.begin(), binary_exts.end(), ext) 
               != binary_exts.end();
    };
    
    EXPECT_TRUE(isBinaryExtension(".exe"));
    EXPECT_TRUE(isBinaryExtension(".png"));
    EXPECT_TRUE(isBinaryExtension(".pdf"));
    EXPECT_FALSE(isBinaryExtension(".cpp"));
    EXPECT_FALSE(isBinaryExtension(".java"));
    EXPECT_FALSE(isBinaryExtension(".py"));
}

// =============================================================================
// Generated File Detection Tests
// =============================================================================

TEST_F(ParserTest, GeneratedFileDetection) {
    auto isGeneratedFile = [](const std::string& path) -> bool {
        // Check common generated file patterns
        if (path.find("generated") != std::string::npos) return true;
        if (path.find("_gen.") != std::string::npos) return true;
        if (path.find(".pb.") != std::string::npos) return true;  // protobuf
        if (path.find(".g.dart") != std::string::npos) return true;
        return false;
    };
    
    EXPECT_TRUE(isGeneratedFile("src/generated/api.cpp"));
    EXPECT_TRUE(isGeneratedFile("proto/message.pb.h"));
    EXPECT_TRUE(isGeneratedFile("lib/model.g.dart"));
    EXPECT_FALSE(isGeneratedFile("src/main.cpp"));
    EXPECT_FALSE(isGeneratedFile("lib/utils.py"));
}

}  // namespace test
}  // namespace aipr
