/**
 * Tests for ChunkPrefixer
 */

#include <gtest/gtest.h>
#include "chunk_prefixer.h"

class ChunkPrefixerTest : public ::testing::Test {
protected:
    aipr::RepoManifest manifest_;

    void SetUp() override {
        manifest_.repo_root = "/tmp/repo";
        manifest_.build_system = "cmake";
        manifest_.file_to_module["src/engine.cpp"] = "ENGINE_SOURCES";
    }
};

TEST_F(ChunkPrefixerTest, BasicPrefixGeneration) {
    aipr::ChunkPrefixer prefixer;

    aipr::tms::CodeChunk chunk;
    chunk.file_path = "src/engine.cpp";
    chunk.language = "cpp";
    chunk.type = "function";
    chunk.name = "processQuery";
    chunk.parent_name = "Engine";

    std::string prefix = prefixer.buildPrefix(chunk, "myrepo", manifest_);

    EXPECT_NE(prefix.find("[repo:myrepo]"), std::string::npos);
    EXPECT_NE(prefix.find("[module:ENGINE_SOURCES]"), std::string::npos);
    EXPECT_NE(prefix.find("[file:src/engine.cpp]"), std::string::npos);
    EXPECT_NE(prefix.find("[lang:cpp]"), std::string::npos);
    EXPECT_NE(prefix.find("[parent:Engine]"), std::string::npos);
    EXPECT_NE(prefix.find("[kind:function]"), std::string::npos);
}

TEST_F(ChunkPrefixerTest, PrefixRespectsTokenLimit) {
    aipr::ChunkPrefixer prefixer(8);  // very small budget: ~32 chars

    aipr::tms::CodeChunk chunk;
    chunk.file_path = "some/very/long/deeply/nested/path/to/source/file.cpp";
    chunk.language = "cpp";
    chunk.type = "function";
    chunk.name = "someVeryLongFunctionNameThatExceedsBudget";
    chunk.parent_name = "AnotherVeryLongClassName";

    std::string prefix = prefixer.buildPrefix(chunk, "repository_with_long_name", manifest_);

    // Should be truncated to roughly 8*4=32 chars
    EXPECT_LE(prefix.size(), 40u);  // small tolerance
}

TEST_F(ChunkPrefixerTest, ApplyPrefixesMutatesContent) {
    aipr::ChunkPrefixer prefixer;

    std::vector<aipr::tms::CodeChunk> chunks(3);
    for (int i = 0; i < 3; ++i) {
        chunks[i].file_path = "src/file" + std::to_string(i) + ".cpp";
        chunks[i].language = "cpp";
        chunks[i].type = "function";
        chunks[i].content = "void f" + std::to_string(i) + "() {}";
    }

    size_t prefixed = prefixer.applyPrefixes(chunks, "repo", manifest_);

    EXPECT_EQ(prefixed, 3u);

    for (const auto& c : chunks) {
        // Content should now start with a prefix line
        EXPECT_NE(c.content.find("[repo:repo]"), std::string::npos);
        // Original content should still be there
        EXPECT_NE(c.content.find("void f"), std::string::npos);
    }
}

TEST_F(ChunkPrefixerTest, AvgPrefixLengthTracking) {
    aipr::ChunkPrefixer prefixer;

    std::vector<aipr::tms::CodeChunk> chunks(5);
    for (int i = 0; i < 5; ++i) {
        chunks[i].file_path = "f.cpp";
        chunks[i].language = "cpp";
        chunks[i].type = "function";
        chunks[i].content = "code";
    }

    prefixer.applyPrefixes(chunks, "r", manifest_);

    EXPECT_GT(prefixer.avgPrefixLength(), 0.0);
}

TEST_F(ChunkPrefixerTest, NoModuleWhenNotInManifest) {
    aipr::ChunkPrefixer prefixer;

    aipr::tms::CodeChunk chunk;
    chunk.file_path = "unknown/file.py";
    chunk.language = "python";
    chunk.type = "function";

    std::string prefix = prefixer.buildPrefix(chunk, "repo", manifest_);

    EXPECT_EQ(prefix.find("[module:"), std::string::npos);
    EXPECT_NE(prefix.find("[file:unknown/file.py]"), std::string::npos);
}

TEST_F(ChunkPrefixerTest, EmptyRepoIdOmitsRepoTag) {
    aipr::ChunkPrefixer prefixer;

    aipr::tms::CodeChunk chunk;
    chunk.file_path = "main.go";
    chunk.language = "go";
    chunk.type = "function";

    std::string prefix = prefixer.buildPrefix(chunk, "", manifest_);

    EXPECT_EQ(prefix.find("[repo:"), std::string::npos);
    EXPECT_NE(prefix.find("[file:main.go]"), std::string::npos);
}
