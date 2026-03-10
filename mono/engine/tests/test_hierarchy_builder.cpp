/**
 * Tests for HierarchyBuilder
 */

#include <gtest/gtest.h>
#include "hierarchy_builder.h"
#include <filesystem>
#include <fstream>

namespace fs = std::filesystem;

class HierarchyBuilderTest : public ::testing::Test {
protected:
    std::string test_dir_;

    void SetUp() override {
        test_dir_ = fs::temp_directory_path().string() + "/hierarchy_test_" +
                     std::to_string(std::chrono::steady_clock::now().time_since_epoch().count());
        fs::create_directories(test_dir_);
    }

    void TearDown() override {
        fs::remove_all(test_dir_);
    }

    void writeFile(const std::string& relative, const std::string& content) {
        auto full = fs::path(test_dir_) / relative;
        fs::create_directories(full.parent_path());
        std::ofstream f(full.string());
        f << content;
    }
};

TEST_F(HierarchyBuilderTest, DetectsCMakeBuildSystem) {
    writeFile("CMakeLists.txt",
        "cmake_minimum_required(VERSION 3.16)\n"
        "project(mylib)\n"
        "add_library(mylib STATIC src/foo.cpp src/bar.cpp)\n"
        "add_executable(myapp main.cpp)\n");
    writeFile("src/foo.cpp", "void foo() {}");
    writeFile("src/bar.cpp", "void bar() {}");
    writeFile("main.cpp", "int main() {}");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "cmake");
    EXPECT_GE(manifest.targets.size(), 1u);
}

TEST_F(HierarchyBuilderTest, DetectsGoMod) {
    writeFile("go.mod",
        "module github.com/example/myproject\n"
        "\n"
        "go 1.21\n");
    writeFile("main.go", "package main\nfunc main() {}");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "go");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "github.com/example/myproject");
}

TEST_F(HierarchyBuilderTest, DetectsPackageJson) {
    writeFile("package.json",
        R"({"name": "my-app", "version": "1.0.0"})");
    writeFile("index.js", "console.log('hello');");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "npm");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "my-app");
}

TEST_F(HierarchyBuilderTest, UnknownBuildSystem) {
    writeFile("readme.md", "Just a readme");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "unknown");
    EXPECT_TRUE(manifest.targets.empty());
}

TEST_F(HierarchyBuilderTest, SummarizeFileCollectsSymbols) {
    aipr::HierarchyBuilder hb;
    aipr::RepoManifest manifest;
    manifest.repo_root = test_dir_;
    manifest.file_to_module["src/foo.cpp"] = "INDEXING_SOURCES";

    std::vector<aipr::tms::CodeChunk> chunks;
    {
        aipr::tms::CodeChunk c;
        c.name = "processData";
        c.end_line = 50;
        c.dependencies.push_back("<vector>");
        chunks.push_back(c);
    }
    {
        aipr::tms::CodeChunk c;
        c.name = "writeOutput";
        c.end_line = 80;
        c.dependencies.push_back("<fstream>");
        chunks.push_back(c);
    }

    auto summary = hb.summarizeFile("src/foo.cpp", "cpp", chunks, manifest);
    EXPECT_EQ(summary.module, "INDEXING_SOURCES");
    EXPECT_EQ(summary.top_symbols.size(), 2u);
    EXPECT_EQ(summary.imports.size(), 2u);
    EXPECT_EQ(summary.line_count, 80u);
}

TEST_F(HierarchyBuilderTest, BuildFileSummaryChunkContent) {
    aipr::HierarchyBuilder hb;
    aipr::FileSummary s;
    s.file_path = "src/main.cpp";
    s.language = "cpp";
    s.module = "ENGINE";
    s.line_count = 200;
    s.top_symbols = {"main", "init"};
    s.imports = {"<iostream>"};

    auto chunk = hb.buildFileSummaryChunk(s);

    EXPECT_EQ(chunk.type, "file_summary");
    EXPECT_EQ(chunk.file_path, "src/main.cpp");
    EXPECT_NE(chunk.content.find("Module: ENGINE"), std::string::npos);
    EXPECT_NE(chunk.content.find("main"), std::string::npos);
    EXPECT_GT(chunk.importance_score, 0.5);
}

// ── New build-system tests ─────────────────────────────────────────────────

TEST_F(HierarchyBuilderTest, DetectsCargoToml) {
    writeFile("Cargo.toml",
        "[package]\n"
        "name = \"my-crate\"\n"
        "version = \"0.1.0\"\n"
        "\n"
        "[workspace]\n"
        "members = [\n"
        "    \"core\",\n"
        "    \"cli\",\n"
        "]\n");
    writeFile("src/main.rs", "fn main() {}");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "cargo");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "my-crate");
    EXPECT_EQ(manifest.targets[0].type, "package");
    // Workspace members discovered
    EXPECT_TRUE(manifest.module_to_files.count("core"));
    EXPECT_TRUE(manifest.module_to_files.count("cli"));
}

TEST_F(HierarchyBuilderTest, DetectsGradleKts) {
    writeFile("build.gradle.kts",
        "plugins {\n"
        "    id(\"java\")\n"
        "}\n"
        "group = \"com.example\"\n");
    writeFile("settings.gradle.kts",
        "rootProject.name = \"my-gradle-app\"\n"
        "include(\":api\")\n"
        "include(\":server\")\n");
    writeFile("src/main/java/App.java", "class App {}");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "gradle");
    ASSERT_GE(manifest.targets.size(), 1u);
    // rootProject.name should be extracted
    bool found_root = false;
    for (auto& t : manifest.targets) {
        if (t.name == "my-gradle-app") found_root = true;
    }
    EXPECT_TRUE(found_root);
    // Subprojects from settings.gradle.kts
    EXPECT_TRUE(manifest.module_to_files.count("api"));
    EXPECT_TRUE(manifest.module_to_files.count("server"));
}

TEST_F(HierarchyBuilderTest, DetectsPyProjectToml) {
    writeFile("pyproject.toml",
        "[project]\n"
        "name = \"my-python-lib\"\n"
        "version = \"1.0.0\"\n"
        "\n"
        "[tool.setuptools]\n"
        "packages = [\"src\", \"tests\"]\n");
    writeFile("src/__init__.py", "");
    writeFile("src/main.py", "def main(): pass");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "python");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "my-python-lib");
    EXPECT_EQ(manifest.targets[0].type, "package");
}

TEST_F(HierarchyBuilderTest, DetectsSetupPy) {
    writeFile("setup.py",
        "from setuptools import setup\n"
        "setup(\n"
        "    name='legacy-pkg',\n"
        "    version='2.0',\n"
        ")\n");
    writeFile("legacy_pkg/__init__.py", "");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "python");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "legacy-pkg");
}

TEST_F(HierarchyBuilderTest, DetectsSwiftPackage) {
    writeFile("Package.swift",
        "// swift-tools-version:5.5\n"
        "import PackageDescription\n"
        "let package = Package(\n"
        "    name: \"MySwiftLib\",\n"
        "    targets: [\n"
        "        .target(name: \"Core\"),\n"
        "        .executableTarget(name: \"CLI\"),\n"
        "        .testTarget(name: \"CoreTests\"),\n"
        "    ]\n"
        ")\n");
    writeFile("Sources/Core/main.swift", "print(\"hello\")");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "swift");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "MySwiftLib");
    EXPECT_TRUE(manifest.module_to_files.count("Core"));
    EXPECT_TRUE(manifest.module_to_files.count("CLI"));
    EXPECT_TRUE(manifest.module_to_files.count("CoreTests"));
}

TEST_F(HierarchyBuilderTest, DetectsMixExs) {
    writeFile("mix.exs",
        "defmodule MyApp.MixProject do\n"
        "  use Mix.Project\n"
        "  def project do\n"
        "    [\n"
        "      app: :my_app,\n"
        "      version: \"0.1.0\"\n"
        "    ]\n"
        "  end\n"
        "end\n");
    writeFile("lib/my_app.ex", "defmodule MyApp do end");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "mix");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "my_app");
    EXPECT_EQ(manifest.targets[0].type, "package");
}

TEST_F(HierarchyBuilderTest, DetectsDotnetCsproj) {
    writeFile("MyProject.csproj",
        "<Project Sdk=\"Microsoft.NET.Sdk\">\n"
        "  <PropertyGroup>\n"
        "    <AssemblyName>MyProject</AssemblyName>\n"
        "    <TargetFramework>net8.0</TargetFramework>\n"
        "  </PropertyGroup>\n"
        "</Project>\n");
    writeFile("Program.cs", "Console.WriteLine(\"Hello\");");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "dotnet");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "MyProject");
    EXPECT_EQ(manifest.targets[0].type, "module");
}

TEST_F(HierarchyBuilderTest, DetectsBazelWorkspace) {
    writeFile("WORKSPACE",
        "workspace(name = \"my_bazel_repo\")\n"
        "load(\"@bazel_tools//tools/build_defs/repo:http.bzl\", \"http_archive\")\n");
    writeFile("lib/BUILD", "cc_library(name = \"mylib\")");
    writeFile("app/BUILD.bazel", "cc_binary(name = \"myapp\")");
    writeFile("lib/foo.cc", "void foo() {}");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "bazel");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "my_bazel_repo");
    // BUILD files should discover packages
    EXPECT_TRUE(manifest.module_to_files.count("lib"));
    EXPECT_TRUE(manifest.module_to_files.count("app"));
}

TEST_F(HierarchyBuilderTest, DetectsMavenPom) {
    writeFile("pom.xml",
        "<project>\n"
        "  <artifactId>my-java-app</artifactId>\n"
        "  <groupId>com.example</groupId>\n"
        "</project>\n");
    writeFile("src/main/java/Main.java", "class Main {}");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.build_system, "maven");
    ASSERT_GE(manifest.targets.size(), 1u);
    EXPECT_EQ(manifest.targets[0].name, "my-java-app");
}

TEST_F(HierarchyBuilderTest, PrimaryLanguageDetection) {
    writeFile("src/main.rs", "fn main() {}");
    writeFile("src/lib.rs", "pub fn foo() {}");
    writeFile("src/util.rs", "pub fn bar() {}");
    writeFile("README.md", "# Hello");
    writeFile("Cargo.toml", "[package]\nname = \"test\"");

    aipr::HierarchyBuilder hb;
    auto manifest = hb.buildRepoManifest(test_dir_);

    EXPECT_EQ(manifest.primary_language, "rust");
}
