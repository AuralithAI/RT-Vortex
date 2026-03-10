/**
 * KnowledgeGraph tests — SQLite WAL knowledge graph
 */

#include <gtest/gtest.h>
#include "knowledge_graph.h"
#include <filesystem>
#include <fstream>

using namespace aipr;
using namespace aipr::tms;

namespace {

// Temporary DB path that auto-cleans
class KGTest : public ::testing::Test {
protected:
    std::string db_path;

    void SetUp() override {
        db_path = "/tmp/aipr_kg_test_" + std::to_string(::getpid()) + ".db";
        // Clean up any leftover from a prior crash
        cleanup();
    }

    void TearDown() override {
        cleanup();
    }

    void cleanup() {
        std::filesystem::remove(db_path);
        std::filesystem::remove(db_path + "-wal");
        std::filesystem::remove(db_path + "-shm");
    }

    std::vector<CodeChunk> makeChunks() {
        CodeChunk a;
        a.id = "repo1:src/main.cpp:main";
        a.type = "function";
        a.name = "main";
        a.file_path = "src/main.cpp";
        a.language = "cpp";
        a.symbols = {"main"};
        a.content = "int main() { return run(); }";

        CodeChunk b;
        b.id = "repo1:src/main.cpp:run";
        b.type = "function";
        b.name = "run";
        b.file_path = "src/main.cpp";
        b.language = "cpp";
        b.parent_chunk_id = "repo1:src/main.cpp:main";
        b.symbols = {"run"};
        b.content = "void run() { helper(); }";

        CodeChunk c;
        c.id = "repo1:src/helper.cpp:helper";
        c.type = "function";
        c.name = "helper";
        c.file_path = "src/helper.cpp";
        c.language = "cpp";
        c.symbols = {"helper"};
        c.dependencies = {"main.cpp"};
        c.content = "void helper() {}";

        CodeChunk d;
        d.id = "repo1:src/main.cpp:file_summary";
        d.type = "file_summary";
        d.name = "main.cpp";
        d.file_path = "src/main.cpp";
        d.language = "cpp";
        d.content = "Main entry point";

        return {a, b, c, d};
    }
};

// ── Basic open / close ─────────────────────────────────────────────────────

TEST_F(KGTest, OpenAndClose) {
    KnowledgeGraph kg(db_path);
    kg.open();
    EXPECT_TRUE(kg.isOpen());
    kg.close();
    EXPECT_FALSE(kg.isOpen());
    EXPECT_TRUE(std::filesystem::exists(db_path));
}

// ── buildFromChunks inserts nodes and edges ────────────────────────────────

TEST_F(KGTest, BuildFromChunksCreatesNodesAndEdges) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    EXPECT_EQ(kg.nodeCount("repo1"), 4u);
    EXPECT_GT(kg.edgeCount("repo1"), 0u);

    kg.close();
}

// ── getNode returns correct data ───────────────────────────────────────────

TEST_F(KGTest, GetNodeReturnsCorrectData) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    auto node = kg.getNode("repo1:src/main.cpp:main");
    ASSERT_TRUE(node.has_value());
    EXPECT_EQ(node->name, "main");
    EXPECT_EQ(node->node_type, "function");
    EXPECT_EQ(node->language, "cpp");
    EXPECT_EQ(node->repo_id, "repo1");

    auto missing = kg.getNode("nonexistent");
    EXPECT_FALSE(missing.has_value());

    kg.close();
}

// ── CONTAINS edges from parent_chunk_id ────────────────────────────────────

TEST_F(KGTest, InfersContainsEdges) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    // "run" has parent_chunk_id pointing to "main"
    auto edges = kg.neighbors("repo1:src/main.cpp:main", "CONTAINS");
    EXPECT_GE(edges.size(), 1u);

    bool found = false;
    for (const auto& e : edges) {
        if (e.dst_id == "repo1:src/main.cpp:run") found = true;
    }
    EXPECT_TRUE(found);

    kg.close();
}

// ── REFERENCES edges from symbol matching ──────────────────────────────────

TEST_F(KGTest, InfersReferenceEdges) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    // "main" calls "run()" — "run" is a symbol defined in chunk b,
    // and "main" content contains "run"
    auto edges = kg.neighbors("repo1:src/main.cpp:main");
    bool found_ref = false;
    for (const auto& e : edges) {
        if (e.edge_type == "REFERENCES" &&
            e.dst_id == "repo1:src/main.cpp:run") {
            found_ref = true;
        }
    }
    EXPECT_TRUE(found_ref);

    kg.close();
}

// ── IMPORTS edges from dependencies ────────────────────────────────────────

TEST_F(KGTest, InfersImportsEdges) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    // "helper" depends on "main.cpp" — should match file_summary node
    auto edges = kg.neighbors("repo1:src/helper.cpp:helper", "IMPORTS");
    bool found_import = false;
    for (const auto& e : edges) {
        if (e.edge_type == "IMPORTS") found_import = true;
    }
    EXPECT_TRUE(found_import);

    kg.close();
}

// ── linkFaissId updates the node ───────────────────────────────────────────

TEST_F(KGTest, LinkFaissIdUpdatesNode) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    kg.linkFaissId("repo1:src/main.cpp:main", 42);
    auto node = kg.getNode("repo1:src/main.cpp:main");
    ASSERT_TRUE(node.has_value());
    EXPECT_EQ(node->faiss_id, 42);

    kg.close();
}

// ── removeRepo deletes everything ──────────────────────────────────────────

TEST_F(KGTest, RemoveRepoClearsData) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);
    EXPECT_GT(kg.nodeCount("repo1"), 0u);

    kg.removeRepo("repo1");
    EXPECT_EQ(kg.nodeCount("repo1"), 0u);
    EXPECT_EQ(kg.edgeCount("repo1"), 0u);

    kg.close();
}

// ── Rebuild replaces old data ──────────────────────────────────────────────

TEST_F(KGTest, RebuildReplacesOldData) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);
    size_t first_nodes = kg.nodeCount("repo1");

    // Rebuild with fewer chunks
    std::vector<CodeChunk> fewer = {chunks[0]};
    kg.buildFromChunks("repo1", fewer);
    EXPECT_LT(kg.nodeCount("repo1"), first_nodes);

    kg.close();
}

// ── neighbors2 returns 2-hop neighborhood ──────────────────────────────────

TEST_F(KGTest, Neighbors2Returns2Hop) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    // 2-hop from "main" should include indirect neighbors
    auto edges = kg.neighbors2("repo1:src/main.cpp:main");
    // Should have at least the 1-hop edges
    EXPECT_GE(edges.size(), 1u);

    kg.close();
}

// ── getNodes returns all repo nodes ────────────────────────────────────────

TEST_F(KGTest, GetNodesReturnsAllRepoNodes) {
    KnowledgeGraph kg(db_path);
    kg.open();

    auto chunks = makeChunks();
    kg.buildFromChunks("repo1", chunks);

    auto nodes = kg.getNodes("repo1");
    EXPECT_EQ(nodes.size(), 4u);

    kg.close();
}

} // namespace
