/**
 * MemoryAccountClassifier tests — domain-aware chunk classification
 */

#include <gtest/gtest.h>
#include "tms/memory_accounts.h"

using namespace aipr::tms;

namespace {

// ── Helper: build a CodeChunk with specific path and content ───────────────

static CodeChunk makeChunk(const std::string& path,
                           const std::string& content = "",
                           const std::string& type = "function") {
    CodeChunk c;
    c.id = "r:" + path + ":sym";
    c.file_path = path;
    c.content = content;
    c.type = type;
    c.name = "sym";
    c.language = "cpp";
    return c;
}

// ── OPS classification ─────────────────────────────────────────────────────

TEST(MemoryAccounts, DockerfileIsOps) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("Dockerfile", "FROM ubuntu:22.04");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::OPS);
}

TEST(MemoryAccounts, GithubWorkflowIsOps) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk(".github/workflows/ci.yml", "name: CI\non: push");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::OPS);
}

TEST(MemoryAccounts, MakefileIsOps) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("Makefile", "all: build");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::OPS);
}

TEST(MemoryAccounts, KubernetesYamlIsOps) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("deploy/service.yaml",
                           "apiVersion: v1\nkind: Service\nkubernetes container");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::OPS);
}

TEST(MemoryAccounts, JenkinsfileIsOps) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("Jenkinsfile", "pipeline { stage('build') {} }");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::OPS);
}

// ── SECURITY classification ────────────────────────────────────────────────

TEST(MemoryAccounts, AuthFileIsSecurity) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("src/auth/login.cpp",
                           "bool authenticate(token, password) {}");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::SECURITY);
}

TEST(MemoryAccounts, CryptoContentIsSecurity) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("src/utils.cpp",
                           "encrypt(data, key); decrypt(cipher, key);");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::SECURITY);
}

// ── HISTORY classification ─────────────────────────────────────────────────

TEST(MemoryAccounts, ChangelogIsHistory) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("CHANGELOG.md", "## 1.0.0\n- Initial release");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::HISTORY);
}

TEST(MemoryAccounts, MigrationIsHistory) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("db/migration_001.sql", "ALTER TABLE users ADD ...");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::HISTORY);
}

// ── DEV classification (default) ───────────────────────────────────────────

TEST(MemoryAccounts, RegularCodeIsDev) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("src/engine/parser.cpp",
                           "void Parser::parse(const std::string& input) {}");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::DEV);
}

TEST(MemoryAccounts, TestFileIsDev) {
    MemoryAccountClassifier cls;
    auto chunk = makeChunk("tests/test_parser.cpp",
                           "TEST(Parser, BasicParse) { EXPECT_TRUE(...); }");
    EXPECT_EQ(cls.classify(chunk), MemoryAccount::DEV);
}

// ── Query classification ───────────────────────────────────────────────────

TEST(MemoryAccounts, DockerQueryRoutesToOps) {
    MemoryAccountClassifier cls;
    auto ranked = cls.classifyQuery("how does the docker build work?");
    ASSERT_GE(ranked.size(), 2u);
    EXPECT_EQ(ranked[0], MemoryAccount::OPS);
}

TEST(MemoryAccounts, AuthQueryRoutesToSecurity) {
    MemoryAccountClassifier cls;
    auto ranked = cls.classifyQuery("explain the authentication token flow");
    ASSERT_GE(ranked.size(), 2u);
    EXPECT_EQ(ranked[0], MemoryAccount::SECURITY);
}

TEST(MemoryAccounts, ChangelogQueryRoutesToHistory) {
    MemoryAccountClassifier cls;
    auto ranked = cls.classifyQuery("what changed in the changelog?");
    ASSERT_GE(ranked.size(), 2u);
    EXPECT_EQ(ranked[0], MemoryAccount::HISTORY);
}

TEST(MemoryAccounts, GenericQueryRoutesToDev) {
    MemoryAccountClassifier cls;
    auto ranked = cls.classifyQuery("how does the parser work?");
    ASSERT_GE(ranked.size(), 2u);
    EXPECT_EQ(ranked[0], MemoryAccount::DEV);
}

// ── accountTag helper ──────────────────────────────────────────────────────

TEST(MemoryAccounts, AccountTagFormat) {
    EXPECT_EQ(MemoryAccountClassifier::accountTag(MemoryAccount::DEV), "account:dev");
    EXPECT_EQ(MemoryAccountClassifier::accountTag(MemoryAccount::OPS), "account:ops");
    EXPECT_EQ(MemoryAccountClassifier::accountTag(MemoryAccount::SECURITY), "account:security");
    EXPECT_EQ(MemoryAccountClassifier::accountTag(MemoryAccount::HISTORY), "account:history");
}

// ── accountName helper ─────────────────────────────────────────────────────

TEST(MemoryAccounts, AccountNameStrings) {
    EXPECT_STREQ(accountName(MemoryAccount::DEV), "dev");
    EXPECT_STREQ(accountName(MemoryAccount::OPS), "ops");
    EXPECT_STREQ(accountName(MemoryAccount::SECURITY), "security");
    EXPECT_STREQ(accountName(MemoryAccount::HISTORY), "history");
}

} // namespace
