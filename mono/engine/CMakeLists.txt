cmake_minimum_required(VERSION 3.20)

# Set CMP0135 policy for FetchContent timestamp handling
if(POLICY CMP0135)
    cmake_policy(SET CMP0135 NEW)
endif()

# =============================================================================
# Version detection: git tag > mono/VERSION > fallback 0.0.0
# =============================================================================
# In CI/release, git describe finds the tag (e.g., v0.2.3 → "0.2.3").
# For local dev without tags, reads mono/VERSION.
# This means version bumps happen automatically via release.yml — no manual
# edits needed.

execute_process(
    COMMAND git describe --tags --match "v*" --abbrev=0
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
    OUTPUT_VARIABLE GIT_TAG_VERSION
    OUTPUT_STRIP_TRAILING_WHITESPACE
    ERROR_QUIET
    RESULT_VARIABLE GIT_TAG_RESULT
)

if(GIT_TAG_RESULT EQUAL 0 AND GIT_TAG_VERSION MATCHES "^v([0-9]+\\.[0-9]+\\.[0-9]+)")
    # Strip leading 'v' from tag
    string(REGEX REPLACE "^v" "" DETECTED_VERSION "${GIT_TAG_VERSION}")
    message(STATUS "Version from git tag: ${DETECTED_VERSION}")
elseif(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/../VERSION")
    file(READ "${CMAKE_CURRENT_SOURCE_DIR}/../VERSION" DETECTED_VERSION)
    string(STRIP "${DETECTED_VERSION}" DETECTED_VERSION)
    message(STATUS "Version from mono/VERSION: ${DETECTED_VERSION}")
else()
    set(DETECTED_VERSION "0.0.0")
    message(STATUS "Version fallback: ${DETECTED_VERSION}")
endif()

project(aipr-engine
    VERSION ${DETECTED_VERSION}
    DESCRIPTION "AI PR Reviewer - Core Engine"
    LANGUAGES C CXX
)

# =============================================================================
# Version header generation (from project VERSION + git describe)
# =============================================================================
# AIPR_GIT_HASH: short commit hash for identifying exact build
# AIPR_GIT_DESCRIBE: full describe string (e.g., "v0.1.0-5-g3d29406")
#   — useful for dev builds to see distance from last release
execute_process(
    COMMAND git rev-parse --short HEAD
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
    OUTPUT_VARIABLE AIPR_GIT_HASH
    OUTPUT_STRIP_TRAILING_WHITESPACE
    ERROR_QUIET
)
if(NOT AIPR_GIT_HASH)
    set(AIPR_GIT_HASH "unknown")
endif()

execute_process(
    COMMAND git describe --tags --always --match "v*"
    WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
    OUTPUT_VARIABLE AIPR_GIT_DESCRIBE
    OUTPUT_STRIP_TRAILING_WHITESPACE
    ERROR_QUIET
)
if(NOT AIPR_GIT_DESCRIBE)
    set(AIPR_GIT_DESCRIBE "${PROJECT_VERSION}")
endif()

string(TIMESTAMP AIPR_BUILD_DATE "%Y-%m-%d")
configure_file(
    ${CMAKE_CURRENT_SOURCE_DIR}/include/version.h.in
    ${CMAKE_CURRENT_BINARY_DIR}/generated/version.h
    @ONLY
)

# C++ Standard
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Export compile commands for IDE support
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# Options
option(AIPR_BUILD_TESTS "Build tests" ON)
option(AIPR_BUILD_TOOLS "Build utility tools" ON)
option(AIPR_BUILD_SERVER "Build gRPC server executable" ON)
option(AIPR_USE_FAISS "Use FAISS for vector search" ON)
option(AIPR_USE_TREE_SITTER "Use tree-sitter for AST parsing" ON)

# Output directories
set(CMAKE_ARCHIVE_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/lib)
set(CMAKE_LIBRARY_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/lib)
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/bin)

# Compiler warnings
if(MSVC)
    add_compile_options(/W4 /WX-)
else()
    add_compile_options(-Wall -Wextra -Wpedantic)
endif()

# Find dependencies
find_package(Threads REQUIRED)

# FetchContent for downloading dependencies (MUST be before any FetchContent_Declare)
include(FetchContent)

# =============================================================================
# libcurl for HTTP embedding API calls
# =============================================================================
if(WIN32)
    # On Windows, CURL may come from vcpkg - set hints
    if(DEFINED ENV{VCPKG_ROOT})
        set(CURL_DIR "$ENV{VCPKG_ROOT}/installed/x64-windows/share/curl")
    endif()
endif()

find_package(CURL REQUIRED)
if(CURL_FOUND)
    message(STATUS "Found libcurl: ${CURL_VERSION_STRING}")
    add_definitions(-DAIPR_HAS_CURL)
else()
    message(FATAL_ERROR "libcurl is required for HTTP embedding API calls. Install with:\n"
                        "  Ubuntu/Debian: sudo apt-get install libcurl4-openssl-dev\n"
                        "  macOS: brew install curl (or use system curl)\n"
                        "  Windows: vcpkg install curl:x64-windows")
endif()

# =============================================================================
# nlohmann/json for JSON parsing
# =============================================================================
FetchContent_Declare(
    nlohmann_json
    GIT_REPOSITORY https://github.com/nlohmann/json.git
    GIT_TAG v3.11.3
    GIT_SHALLOW TRUE
)
FetchContent_MakeAvailable(nlohmann_json)
message(STATUS "nlohmann/json downloaded")

# =============================================================================
# miniz for gzip compression of LTM chunks.bin (optional)
# =============================================================================
option(AIPR_USE_MINIZ "Use miniz for LTM index compression" ON)

if(AIPR_USE_MINIZ)
    FetchContent_Declare(
        miniz
        GIT_REPOSITORY https://github.com/richgel999/miniz.git
        GIT_TAG 3.0.2
        GIT_SHALLOW TRUE
    )
    FetchContent_MakeAvailable(miniz)
    message(STATUS "miniz downloaded for LTM compression")
    add_definitions(-DAIPR_HAS_MINIZ)
endif()

# =============================================================================
# SQLite3 amalgamation for Knowledge Graph (optional)
# =============================================================================
option(AIPR_USE_SQLITE "Use SQLite for Knowledge Graph" ON)

if(AIPR_USE_SQLITE)
    FetchContent_Declare(
        sqlite3
        URL https://www.sqlite.org/2024/sqlite-amalgamation-3450100.zip
        URL_HASH SHA256=5592243caf28b2cdef41e6ab58d25d653dfc53deded8450eb66072c929f030c4
    )
    FetchContent_MakeAvailable(sqlite3)

    # Build the amalgamation as a static library
    add_library(sqlite3_lib STATIC
        ${sqlite3_SOURCE_DIR}/sqlite3.c
    )
    target_include_directories(sqlite3_lib PUBLIC ${sqlite3_SOURCE_DIR})
    target_compile_definitions(sqlite3_lib PRIVATE
        SQLITE_THREADSAFE=1
        SQLITE_ENABLE_WAL=1
    )
    if(UNIX)
        target_link_libraries(sqlite3_lib PUBLIC dl pthread)
    endif()
    message(STATUS "SQLite3 amalgamation downloaded for Knowledge Graph")
    add_definitions(-DAIPR_HAS_SQLITE)
endif()

# =============================================================================
# ONNX Runtime for local embeddings (optional fallback)
# =============================================================================
option(AIPR_USE_ONNX "Use ONNX Runtime for local embeddings" ON)

if(AIPR_USE_ONNX)
    # Try to find system ONNX Runtime first
    find_package(onnxruntime QUIET)
    
    if(onnxruntime_FOUND)
        message(STATUS "Found system ONNX Runtime")
        set(ONNX_LIBRARIES onnxruntime::onnxruntime)
        add_definitions(-DAIPR_HAS_ONNX)
    else()
        message(STATUS "ONNX Runtime not found, downloading via FetchContent...")
        
        # Download pre-built ONNX Runtime
        set(ONNXRUNTIME_VERSION "1.16.3")
        
        if(WIN32)
            set(ONNX_URL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-win-x64-${ONNXRUNTIME_VERSION}.zip")
        elseif(APPLE)
            if(CMAKE_SYSTEM_PROCESSOR MATCHES "arm64")
                set(ONNX_URL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-osx-arm64-${ONNXRUNTIME_VERSION}.tgz")
            else()
                set(ONNX_URL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-osx-x86_64-${ONNXRUNTIME_VERSION}.tgz")
            endif()
        else()
            if(CMAKE_SYSTEM_PROCESSOR MATCHES "aarch64|arm64")
                set(ONNX_URL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-aarch64-${ONNXRUNTIME_VERSION}.tgz")
            else()
                set(ONNX_URL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz")
            endif()
        endif()
        
        FetchContent_Declare(
            onnxruntime
            URL ${ONNX_URL}
        )
        FetchContent_MakeAvailable(onnxruntime)
        
        set(ONNX_INCLUDE_DIR "${onnxruntime_SOURCE_DIR}/include")
        
        if(WIN32)
            set(ONNX_LIBRARIES "${onnxruntime_SOURCE_DIR}/lib/onnxruntime.lib")
            set(ONNX_DLL "${onnxruntime_SOURCE_DIR}/lib/onnxruntime.dll")
        elseif(APPLE)
            set(ONNX_LIBRARIES "${onnxruntime_SOURCE_DIR}/lib/libonnxruntime.dylib")
        else()
            set(ONNX_LIBRARIES "${onnxruntime_SOURCE_DIR}/lib/libonnxruntime.so")
        endif()
        
        add_definitions(-DAIPR_HAS_ONNX)
        message(STATUS "ONNX Runtime downloaded: ${onnxruntime_SOURCE_DIR}")
    endif()
else()
    message(STATUS "ONNX Runtime disabled, local embeddings not available")
    set(ONNX_LIBRARIES "")
endif()

# =============================================================================
# FAISS for Production Vector Search
# =============================================================================
# FAISS requires BLAS (OpenBLAS or MKL) and OpenMP
# On macOS: brew install faiss (provides system package)
# On Ubuntu/Windows: Built from source in CI, installed to system

if(AIPR_USE_FAISS)
    # First, try to find system/pre-built FAISS
    find_package(faiss QUIET CONFIG)
    if(NOT faiss_FOUND)
        find_package(faiss QUIET)
    endif()
    
    if(faiss_FOUND)
        message(STATUS "FAISS found (system/pre-built installation)")
        add_definitions(-DAIPR_HAS_FAISS)
        set(FAISS_LIBRARIES faiss)
    else()
        message(STATUS "FAISS not found, attempting to build from source...")
        
        # Try to find OpenMP
        find_package(OpenMP QUIET)
        
        # BLAS detection: Try MKL first, then OpenBLAS, then Apple Accelerate
        set(BLAS_FOUND FALSE)
        set(BLAS_TYPE "none")
        
        # 1. Try Intel MKL
        find_package(MKL QUIET)
        if(MKL_FOUND)
            set(BLAS_FOUND TRUE)
            set(BLAS_TYPE "MKL")
            set(BLAS_LIBRARIES ${MKL_LIBRARIES})
            set(BLAS_INCLUDE_DIRS ${MKL_INCLUDE_DIRS})
            message(STATUS "Found Intel MKL for BLAS")
        endif()
        
        # 2. Try OpenBLAS if MKL not found
        if(NOT BLAS_FOUND)
            find_package(OpenBLAS QUIET)
            if(OpenBLAS_FOUND)
                set(BLAS_FOUND TRUE)
                set(BLAS_TYPE "OpenBLAS")
                set(BLAS_LIBRARIES ${OpenBLAS_LIBRARIES})
                set(BLAS_INCLUDE_DIRS ${OpenBLAS_INCLUDE_DIRS})
                message(STATUS "Found OpenBLAS for BLAS")
            else()
                # Try pkg-config for OpenBLAS
                find_package(PkgConfig QUIET)
                if(PkgConfig_FOUND)
                    pkg_check_modules(OPENBLAS QUIET openblas)
                    if(OPENBLAS_FOUND)
                        set(BLAS_FOUND TRUE)
                        set(BLAS_TYPE "OpenBLAS")
                        set(BLAS_LIBRARIES ${OPENBLAS_LIBRARIES})
                        set(BLAS_INCLUDE_DIRS ${OPENBLAS_INCLUDE_DIRS})
                        message(STATUS "Found OpenBLAS via pkg-config")
                    endif()
                endif()
            endif()
        endif()
        
        # 3. Try generic BLAS
        if(NOT BLAS_FOUND)
            find_package(BLAS QUIET)
            if(BLAS_FOUND)
                set(BLAS_TYPE "Generic")
                message(STATUS "Found generic BLAS")
            endif()
        endif()
        
        # 4. Apple Accelerate on macOS
        if(NOT BLAS_FOUND AND APPLE)
            find_library(ACCELERATE_FRAMEWORK Accelerate)
            if(ACCELERATE_FRAMEWORK)
                set(BLAS_FOUND TRUE)
                set(BLAS_TYPE "Accelerate")
                set(BLAS_LIBRARIES ${ACCELERATE_FRAMEWORK})
                message(STATUS "Found Apple Accelerate framework for BLAS")
            endif()
        endif()
        
        # Build FAISS if we have BLAS
        if(BLAS_FOUND)
            message(STATUS "Using ${BLAS_TYPE} for FAISS BLAS backend")
            
            FetchContent_Declare(
                faiss
                GIT_REPOSITORY https://github.com/facebookresearch/faiss.git
                GIT_TAG v1.7.4
                GIT_SHALLOW TRUE
            )
            
            # Configure FAISS build options
            set(FAISS_ENABLE_GPU OFF CACHE BOOL "" FORCE)
            set(FAISS_ENABLE_PYTHON OFF CACHE BOOL "" FORCE)
            set(FAISS_ENABLE_C_API ON CACHE BOOL "" FORCE)
            set(BUILD_TESTING OFF CACHE BOOL "" FORCE)
            set(BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
            
            # Set BLAS type for FAISS
            if(BLAS_TYPE STREQUAL "MKL")
                set(BLA_VENDOR Intel10_64lp_seq CACHE STRING "" FORCE)
            elseif(BLAS_TYPE STREQUAL "OpenBLAS")
                set(BLA_VENDOR OpenBLAS CACHE STRING "" FORCE)
            elseif(BLAS_TYPE STREQUAL "Accelerate")
                set(BLA_VENDOR Apple CACHE STRING "" FORCE)
            endif()
            
            FetchContent_MakeAvailable(faiss)
            
            add_definitions(-DAIPR_HAS_FAISS)
            set(FAISS_LIBRARIES faiss)
            message(STATUS "FAISS v1.7.4 configured with ${BLAS_TYPE}")
        else()
            message(WARNING "FAISS requires BLAS library. Install one of:")
            message(WARNING "  Ubuntu/Debian: sudo apt-get install libopenblas-dev")
            message(WARNING "  RHEL/CentOS:   sudo yum install openblas-devel")
            message(WARNING "  macOS:         brew install openblas (or use Accelerate)")
            message(WARNING "  Windows:       vcpkg install openblas")
            message(WARNING "Using fallback brute-force vector search")
            set(FAISS_LIBRARIES "")
        endif()
        
        if(NOT OpenMP_FOUND)
            message(STATUS "OpenMP not found - FAISS will use single-threaded mode")
            message(STATUS "  Ubuntu/Debian: sudo apt-get install libomp-dev")
            message(STATUS "  macOS:         brew install libomp")
        endif()
    endif()
else()
    message(STATUS "FAISS disabled (brute-force fallback enabled)")
    set(FAISS_LIBRARIES "")
endif()

# =============================================================================
# Tree-sitter for AST-based Code Chunking
# =============================================================================

if(AIPR_USE_TREE_SITTER)
    message(STATUS "Configuring tree-sitter for AST parsing...")
    
    # Fetch tree-sitter core library
    # Note: We don't use SOURCE_SUBDIR or the upstream CMakeLists.txt because
    # tree-sitter's CMakeLists.txt uses file(GLOB) with relative paths that break
    # under FetchContent. Instead we build the amalgamated lib/src/lib.c ourselves.
    FetchContent_Declare(
        tree_sitter
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter.git
        GIT_TAG v0.22.6
        GIT_SHALLOW TRUE
    )
    
    # Fetch language grammars
    FetchContent_Declare(
        tree_sitter_c
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-c.git
        GIT_TAG v0.21.0
        GIT_SHALLOW TRUE
    )
    
    FetchContent_Declare(
        tree_sitter_cpp
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-cpp.git
        GIT_TAG v0.22.0
        GIT_SHALLOW TRUE
    )
    
    FetchContent_Declare(
        tree_sitter_java
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-java.git
        GIT_TAG v0.21.0
        GIT_SHALLOW TRUE
    )
    
    FetchContent_Declare(
        tree_sitter_python
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-python.git
        GIT_TAG v0.21.0
        GIT_SHALLOW TRUE
    )
    
    FetchContent_Declare(
        tree_sitter_javascript
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-javascript.git
        GIT_TAG v0.21.0
        GIT_SHALLOW TRUE
    )
    
    FetchContent_Declare(
        tree_sitter_typescript
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-typescript.git
        GIT_TAG v0.21.0
        GIT_SHALLOW TRUE
    )
    
    FetchContent_Declare(
        tree_sitter_go
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-go.git
        GIT_TAG v0.21.0
        GIT_SHALLOW TRUE
    )
    
    FetchContent_Declare(
        tree_sitter_rust
        GIT_REPOSITORY https://github.com/tree-sitter/tree-sitter-rust.git
        GIT_TAG v0.21.2
        GIT_SHALLOW TRUE
    )
    
    # Make available - populate source dirs without running upstream CMakeLists.txt
    # We use FetchContent_Populate since these repos don't have CMakeLists.txt
    # at the tags we use, and we build them manually anyway.
    FetchContent_GetProperties(tree_sitter)
    if(NOT tree_sitter_POPULATED)
        FetchContent_Populate(tree_sitter)
    endif()

    set(_TS_GRAMMARS
        tree_sitter_c
        tree_sitter_cpp
        tree_sitter_java
        tree_sitter_python
        tree_sitter_javascript
        tree_sitter_typescript
        tree_sitter_go
        tree_sitter_rust
    )
    foreach(_grammar ${_TS_GRAMMARS})
        FetchContent_GetProperties(${_grammar})
        if(NOT ${_grammar}_POPULATED)
            FetchContent_Populate(${_grammar})
        endif()
    endforeach()

    # Build tree-sitter core as a static library from the amalgamated source
    add_library(tree-sitter STATIC
        ${tree_sitter_SOURCE_DIR}/lib/src/lib.c
    )
    target_include_directories(tree-sitter
        PUBLIC  ${tree_sitter_SOURCE_DIR}/lib/include
        PRIVATE ${tree_sitter_SOURCE_DIR}/lib/src
    )
    set_target_properties(tree-sitter PROPERTIES
        C_STANDARD 11
        C_STANDARD_REQUIRED ON
        POSITION_INDEPENDENT_CODE ON
    )
    target_compile_definitions(tree-sitter PRIVATE
        _POSIX_C_SOURCE=200112L
        _DEFAULT_SOURCE
        _BSD_SOURCE
        _DARWIN_C_SOURCE
    )
    
    # Build tree-sitter grammars as a static library
    add_library(tree_sitter_grammars STATIC
        ${tree_sitter_c_SOURCE_DIR}/src/parser.c
        ${tree_sitter_cpp_SOURCE_DIR}/src/parser.c
        ${tree_sitter_cpp_SOURCE_DIR}/src/scanner.c
        ${tree_sitter_java_SOURCE_DIR}/src/parser.c
        ${tree_sitter_python_SOURCE_DIR}/src/parser.c
        ${tree_sitter_python_SOURCE_DIR}/src/scanner.c
        ${tree_sitter_javascript_SOURCE_DIR}/src/parser.c
        ${tree_sitter_javascript_SOURCE_DIR}/src/scanner.c
        ${tree_sitter_typescript_SOURCE_DIR}/typescript/src/parser.c
        ${tree_sitter_typescript_SOURCE_DIR}/typescript/src/scanner.c
        ${tree_sitter_go_SOURCE_DIR}/src/parser.c
        ${tree_sitter_rust_SOURCE_DIR}/src/parser.c
        ${tree_sitter_rust_SOURCE_DIR}/src/scanner.c
    )
    
    target_include_directories(tree_sitter_grammars PUBLIC
        ${tree_sitter_SOURCE_DIR}/lib/include
        ${tree_sitter_c_SOURCE_DIR}/src
        ${tree_sitter_cpp_SOURCE_DIR}/src
        ${tree_sitter_java_SOURCE_DIR}/src
        ${tree_sitter_python_SOURCE_DIR}/src
        ${tree_sitter_javascript_SOURCE_DIR}/src
        ${tree_sitter_typescript_SOURCE_DIR}/typescript/src
        ${tree_sitter_go_SOURCE_DIR}/src
        ${tree_sitter_rust_SOURCE_DIR}/src
    )
    
    target_link_libraries(tree_sitter_grammars PUBLIC tree-sitter)
    
    add_definitions(-DAIPR_HAS_TREE_SITTER)
    set(TREE_SITTER_LIBRARIES tree_sitter_grammars tree-sitter)
    set(TREE_SITTER_INCLUDE_DIRS ${tree_sitter_SOURCE_DIR}/lib/include)
    
    message(STATUS "Tree-sitter configured with grammars: C, C++, Java, Python, JavaScript, TypeScript, Go, Rust")
else()
    message(STATUS "Tree-sitter disabled (regex fallback enabled)")
    set(TREE_SITTER_LIBRARIES "")
    set(TREE_SITTER_INCLUDE_DIRS "")
endif()

# =============================================================================
# gRPC Dependencies (for server build)
# =============================================================================

if(AIPR_BUILD_SERVER)
    message(STATUS "Building gRPC server - fetching dependencies...")
    
    # Try to find system gRPC first
    find_package(gRPC CONFIG QUIET)
    find_package(Protobuf CONFIG QUIET)
    
    if(gRPC_FOUND AND Protobuf_FOUND)
        message(STATUS "Found system gRPC: ${gRPC_VERSION}")
        message(STATUS "Found system Protobuf: ${Protobuf_VERSION}")
        
        set(GRPC_LIBRARIES gRPC::grpc++ gRPC::grpc++_reflection)
        set(PROTOBUF_LIBRARIES protobuf::libprotobuf)
        
        # Get the grpc_cpp_plugin location
        get_target_property(GRPC_CPP_PLUGIN gRPC::grpc_cpp_plugin LOCATION)
        get_target_property(PROTOC protobuf::protoc LOCATION)
        
    else()
        message(STATUS "gRPC not found, downloading via FetchContent (this may take a while)...")
        
        # Abseil (required by gRPC)
        set(ABSL_PROPAGATE_CXX_STD ON)
        set(ABSL_ENABLE_INSTALL ON)
        FetchContent_Declare(
            abseil-cpp
            GIT_REPOSITORY https://github.com/abseil/abseil-cpp.git
            GIT_TAG 20240116.1
            GIT_SHALLOW TRUE
        )
        
        # gRPC
        set(gRPC_BUILD_TESTS OFF CACHE BOOL "" FORCE)
        set(gRPC_BUILD_CSHARP_EXT OFF CACHE BOOL "" FORCE)
        set(gRPC_BUILD_GRPC_CSHARP_PLUGIN OFF CACHE BOOL "" FORCE)
        set(gRPC_BUILD_GRPC_NODE_PLUGIN OFF CACHE BOOL "" FORCE)
        set(gRPC_BUILD_GRPC_OBJECTIVE_C_PLUGIN OFF CACHE BOOL "" FORCE)
        set(gRPC_BUILD_GRPC_PHP_PLUGIN OFF CACHE BOOL "" FORCE)
        set(gRPC_BUILD_GRPC_PYTHON_PLUGIN OFF CACHE BOOL "" FORCE)
        set(gRPC_BUILD_GRPC_RUBY_PLUGIN OFF CACHE BOOL "" FORCE)
        set(gRPC_INSTALL ON CACHE BOOL "" FORCE)
        
        FetchContent_Declare(
            grpc
            GIT_REPOSITORY https://github.com/grpc/grpc.git
            GIT_TAG v1.60.0
            GIT_SHALLOW TRUE
        )
        
        FetchContent_MakeAvailable(abseil-cpp grpc)
        
        set(GRPC_LIBRARIES grpc++ grpc++_reflection)
        set(PROTOBUF_LIBRARIES libprotobuf)
        set(GRPC_CPP_PLUGIN $<TARGET_FILE:grpc_cpp_plugin>)
        set(PROTOC $<TARGET_FILE:protoc>)
        
        message(STATUS "gRPC downloaded and configured")
    endif()
    
    # Proto file location
    set(PROTO_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../proto")
    set(PROTO_FILE "${PROTO_DIR}/engine.proto")
    set(PROTO_GEN_DIR "${CMAKE_CURRENT_BINARY_DIR}/proto-gen")
    
    # Create output directory
    file(MAKE_DIRECTORY ${PROTO_GEN_DIR})
    
    # Generated file names
    set(PROTO_GENERATED_SRCS
        "${PROTO_GEN_DIR}/engine.pb.cc"
        "${PROTO_GEN_DIR}/engine.grpc.pb.cc"
    )
    set(PROTO_GENERATED_HDRS
        "${PROTO_GEN_DIR}/engine.pb.h"
        "${PROTO_GEN_DIR}/engine.grpc.pb.h"
    )
    
    # Custom command to generate protobuf/grpc code
    add_custom_command(
        OUTPUT ${PROTO_GENERATED_SRCS} ${PROTO_GENERATED_HDRS}
        COMMAND ${PROTOC}
            --proto_path=${PROTO_DIR}
            --cpp_out=${PROTO_GEN_DIR}
            --grpc_out=${PROTO_GEN_DIR}
            --plugin=protoc-gen-grpc=${GRPC_CPP_PLUGIN}
            ${PROTO_FILE}
        DEPENDS ${PROTO_FILE}
        COMMENT "Generating C++ protobuf/gRPC sources from engine.proto"
        VERBATIM
    )
    
    # Mark generated files
    set_source_files_properties(${PROTO_GENERATED_SRCS} ${PROTO_GENERATED_HDRS}
        PROPERTIES GENERATED TRUE
    )
    
    message(STATUS "Proto generation configured: ${PROTO_FILE}")
endif()

# Include directories
include_directories(${CMAKE_CURRENT_SOURCE_DIR}/include)
include_directories(${CMAKE_CURRENT_SOURCE_DIR}/include/tms)

# Source files - Indexing
set(INDEXING_SOURCES
    src/indexing/repo_scanner.cpp
    src/indexing/language_detect.cpp
    src/indexing/chunker_ast.cpp
    src/indexing/chunker_fallback.cpp
    src/indexing/manifest.cpp
    src/indexing/embed_client.cpp
    src/indexing/hierarchy_builder.cpp
    src/indexing/chunk_prefixer.cpp
)

# Source files - Retrieval
set(RETRIEVAL_SOURCES
    src/retrieval/lexical_index.cpp
    src/retrieval/vector_index.cpp
    src/retrieval/graph_index.cpp
    src/retrieval/ranker.cpp
)

# Source files - Review
set(REVIEW_SOURCES
    src/review/diff_parser.cpp
    src/review/touched_symbols.cpp
    src/review/context_builder.cpp
    src/review/heuristics.cpp
)

# Source files - Storage
set(STORAGE_SOURCES
    src/storage/local_storage.cpp
    src/storage/cloud_storage.cpp
    src/storage/migration.cpp
)

# Source files - Memory System (Brain-inspired RAG)
set(MEMORY_SOURCES
    src/memory/long_term_memory.cpp
    src/memory/short_term_memory.cpp
    src/memory/meta_task_memory.cpp
    src/memory/cross_memory_attention.cpp
    src/memory/memory_system.cpp
)

# Source files - TMS Cognitive Memory System (Production Brain-RAG)
set(TMS_SOURCES
    src/tms/core/tms_memory_system.cpp
    src/tms/core/ltm_faiss.cpp
    src/tms/core/stm.cpp
    src/tms/core/mtm_graph.cpp
    src/tms/core/compute_controller.cpp
    src/tms/core/cross_memory_attention.cpp
    src/tms/core/memory_accounts.cpp
    src/tms/ingestion/embedding_engine.cpp
    src/tms/ingestion/repo_parser.cpp
    src/tms/retrieval/graph_rag.cpp
    src/tms/retrieval/multi_vector_index.cpp
)

# Source files - Cache
set(CACHE_SOURCES
    src/cache/merkle_cache.cpp
)

# Source files - Knowledge Graph
set(GRAPH_SOURCES
    src/graph/knowledge_graph.cpp
)

# Source files - Utilities
set(UTIL_SOURCES
    src/util/json.cpp
    src/util/hashing.cpp
    src/util/fs.cpp
    src/util/perf_timer.cpp
    src/util/metrics.cpp
    src/util/config_validator.cpp
)

# Main engine library
add_library(aipr-engine STATIC
    src/engine.cpp
    ${INDEXING_SOURCES}
    ${RETRIEVAL_SOURCES}
    ${REVIEW_SOURCES}
    ${STORAGE_SOURCES}
    ${MEMORY_SOURCES}
    ${TMS_SOURCES}
    ${GRAPH_SOURCES}
    ${CACHE_SOURCES}
    ${UTIL_SOURCES}
)

target_include_directories(aipr-engine PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_BINARY_DIR}/generated>
    $<INSTALL_INTERFACE:include>
)

target_link_libraries(aipr-engine PUBLIC
    Threads::Threads
    CURL::libcurl
    nlohmann_json::nlohmann_json
)

# MerkleCache uses OpenSSL/BoringSSL SHA-256 (from gRPC's bundled BoringSSL)
if(TARGET crypto)
    target_link_libraries(aipr-engine PUBLIC crypto)
else()
    find_package(OpenSSL QUIET)
    if(OpenSSL_FOUND)
        target_link_libraries(aipr-engine PUBLIC OpenSSL::Crypto)
    endif()
endif()

if(AIPR_USE_MINIZ)
    target_link_libraries(aipr-engine PUBLIC miniz)
endif()

if(AIPR_USE_SQLITE)
    target_link_libraries(aipr-engine PUBLIC sqlite3_lib)
endif()

if(FAISS_LIBRARIES)
    target_link_libraries(aipr-engine PUBLIC ${FAISS_LIBRARIES})
endif()

if(TREE_SITTER_LIBRARIES)
    target_include_directories(aipr-engine PUBLIC ${TREE_SITTER_INCLUDE_DIRS})
    target_link_libraries(aipr-engine PUBLIC ${TREE_SITTER_LIBRARIES})
endif()

if(AIPR_USE_ONNX AND ONNX_LIBRARIES)
    # Use PRIVATE to avoid exporting build-tree paths
    target_include_directories(aipr-engine PRIVATE ${ONNX_INCLUDE_DIR})
    target_link_libraries(aipr-engine PRIVATE ${ONNX_LIBRARIES})
endif()

# Shared library version
add_library(aipr-engine-shared SHARED
    ${INDEXING_SOURCES}
    ${RETRIEVAL_SOURCES}
    ${REVIEW_SOURCES}
    ${UTIL_SOURCES}
)

target_include_directories(aipr-engine-shared PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:include>
)

target_link_libraries(aipr-engine-shared PUBLIC
    Threads::Threads
    CURL::libcurl
    nlohmann_json::nlohmann_json
)

if(FAISS_LIBRARIES)
    target_link_libraries(aipr-engine-shared PUBLIC ${FAISS_LIBRARIES})
endif()

if(TREE_SITTER_LIBRARIES)
    target_include_directories(aipr-engine-shared PUBLIC ${TREE_SITTER_INCLUDE_DIRS})
    target_link_libraries(aipr-engine-shared PUBLIC ${TREE_SITTER_LIBRARIES})
endif()

if(AIPR_USE_ONNX AND ONNX_LIBRARIES)
    # Use PRIVATE to avoid exporting build-tree paths
    target_include_directories(aipr-engine-shared PRIVATE ${ONNX_INCLUDE_DIR})
    target_link_libraries(aipr-engine-shared PRIVATE ${ONNX_LIBRARIES})
endif()

set_target_properties(aipr-engine-shared PROPERTIES
    VERSION ${PROJECT_VERSION}
    SOVERSION 0
    OUTPUT_NAME aipr-engine
)

# Tools
if(AIPR_BUILD_TOOLS)
    add_executable(aipr-engine-bench tools/aipr-engine-bench.cpp)
    target_link_libraries(aipr-engine-bench PRIVATE aipr-engine)
    set_target_properties(aipr-engine-bench PROPERTIES OUTPUT_NAME rtvortex-bench)
    
    add_executable(aipr-engine-doctor tools/aipr-engine-doctor.cpp)
    target_link_libraries(aipr-engine-doctor PRIVATE aipr-engine)
    set_target_properties(aipr-engine-doctor PROPERTIES OUTPUT_NAME rtvortex-doctor)
    
    # TMS Demo - Brain-Inspired RAG System
    add_executable(tms-demo examples/tms_demo.cpp)
    target_link_libraries(tms-demo PRIVATE aipr-engine)
    target_include_directories(tms-demo PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/include)
endif()

# =============================================================================
# gRPC Server Executable
# =============================================================================

if(AIPR_BUILD_SERVER)
    message(STATUS "Building aipr-engine gRPC server executable...")
    
    # Server source files
    set(SERVER_SOURCES
        src/server/main.cpp
        src/server/engine_service_impl.cpp
        ${PROTO_GENERATED_SRCS}
    )
    
    # Windows: embed application icon and version info via .rc resource file
    if(WIN32 AND EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/rtvortex.rc")
        list(APPEND SERVER_SOURCES rtvortex.rc)
    endif()
    
    # Create the server executable
    add_executable(aipr-engine-server ${SERVER_SOURCES})
    
    # Set output name
    set_target_properties(aipr-engine-server PROPERTIES
        OUTPUT_NAME rtvortex
        RUNTIME_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/bin
    )
    
    # Include directories for generated proto files
    target_include_directories(aipr-engine-server PRIVATE
        ${CMAKE_CURRENT_SOURCE_DIR}/include
        ${PROTO_GEN_DIR}
    )
    
    # Link dependencies
    target_link_libraries(aipr-engine-server PRIVATE
        aipr-engine
        ${GRPC_LIBRARIES}
        ${PROTOBUF_LIBRARIES}
        Threads::Threads
    )
    
    # Windows-specific: link ws2_32 for sockets
    if(WIN32)
        target_link_libraries(aipr-engine-server PRIVATE ws2_32)
    endif()
    
    # Install the server executable
    install(TARGETS aipr-engine-server
        RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
    )
    
    message(STATUS "Server executable will be: ${CMAKE_BINARY_DIR}/bin/rtvortex${CMAKE_EXECUTABLE_SUFFIX}")

    # ── Copy to rt_home (shared build output) ────────────────────────────
    set(RT_HOME_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../rt_home")
    add_custom_command(TARGET aipr-engine-server POST_BUILD
        COMMAND ${CMAKE_COMMAND} -E make_directory "${RT_HOME_DIR}/bin"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${RT_HOME_DIR}/config"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${RT_HOME_DIR}/data"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${RT_HOME_DIR}/logs"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${RT_HOME_DIR}/tmp"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${RT_HOME_DIR}/models"
        COMMAND ${CMAKE_COMMAND} -E copy "$<TARGET_FILE:aipr-engine-server>" "${RT_HOME_DIR}/bin/"
        COMMENT "Copying rtvortex to rt_home/bin/"
    )

    # ---------------------------------------------------------------------------
    # Local-dev convenience: symlink models/, config/, logs/ into the build tree
    # so the binary can find them without setting RTVORTEX_HOME.
    # On Windows cmake -E create_symlink may need developer mode; fall back to copy.
    # ---------------------------------------------------------------------------
    set(_ENGINE_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR})
    set(_MODELS_SRC_DIR "${_ENGINE_SRC_DIR}/models")

    # ── Auto-download MiniLM ONNX model into models/minilm/ ───────────────
    # MiniLM (~87 MB) is downloaded at build time — it's the lightweight
    # fallback model. BGE-M3 (~2.3 GB) is downloaded at engine runtime on
    # first use to avoid bloating the build.
    set(_MINILM_DIR "${_MODELS_SRC_DIR}/minilm")
    set(_MINILM_MODEL "${_MINILM_DIR}/model.onnx")
    if(NOT EXISTS "${_MINILM_MODEL}")
        set(_HF_BASE "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main")

        file(MAKE_DIRECTORY "${_MINILM_DIR}")

        message(STATUS "Downloading ONNX embedding model (all-MiniLM-L6-v2, ~87 MB)...")
        file(DOWNLOAD
            "${_HF_BASE}/onnx/model.onnx"
            "${_MINILM_MODEL}"
            SHOW_PROGRESS
            STATUS _dl_status
        )
        list(GET _dl_status 0 _dl_code)
        if(NOT _dl_code EQUAL 0)
            message(WARNING "Failed to download MiniLM ONNX model (status: ${_dl_status}). "
                            "Server will start but local embeddings will be unavailable. "
                            "Download manually from: ${_HF_BASE}/onnx/model.onnx")
            file(REMOVE "${_MINILM_MODEL}")
        else()
            message(STATUS "MiniLM model downloaded: ${_MINILM_MODEL}")
        endif()

        foreach(_f tokenizer.json vocab.txt)
            if(NOT EXISTS "${_MINILM_DIR}/${_f}")
                message(STATUS "Downloading ${_f}...")
                file(DOWNLOAD
                    "${_HF_BASE}/${_f}"
                    "${_MINILM_DIR}/${_f}"
                    STATUS _dl2_status
                )
            endif()
        endforeach()
    endif()

    # Create empty bge-m3 directory structure (model downloaded at runtime).
    file(MAKE_DIRECTORY "${_MODELS_SRC_DIR}/bge-m3")

    # models/ → mono/engine/models  (symlink to avoid duplicating ~90 MB ONNX file)
    if(EXISTS "${_ENGINE_SRC_DIR}/models" AND NOT EXISTS "${CMAKE_BINARY_DIR}/models")
        file(CREATE_LINK
            "${_ENGINE_SRC_DIR}/models"
            "${CMAKE_BINARY_DIR}/models"
            COPY_ON_ERROR
            SYMBOLIC
        )
        message(STATUS "Linked build/models → ${_ENGINE_SRC_DIR}/models")
    endif()

    # config/ → mono/config  (so default.yml is found)
    if(EXISTS "${_ENGINE_SRC_DIR}/../config" AND NOT EXISTS "${CMAKE_BINARY_DIR}/config")
        file(CREATE_LINK
            "${_ENGINE_SRC_DIR}/../config"
            "${CMAKE_BINARY_DIR}/config"
            COPY_ON_ERROR
            SYMBOLIC
        )
        message(STATUS "Linked build/config → ${_ENGINE_SRC_DIR}/../config")
    endif()

    # logs/ and data/ — just create the directories (no source to link)
    file(MAKE_DIRECTORY "${CMAKE_BINARY_DIR}/logs")
    file(MAKE_DIRECTORY "${CMAKE_BINARY_DIR}/data")
endif()

# Tests
if(AIPR_BUILD_TESTS)
    enable_testing()
    add_subdirectory(tests)
endif()

# Installation
# Note: We only install the server executable and skip library installation
# to avoid export set issues with FetchContent dependencies (nlohmann_json, tree_sitter)
include(GNUInstallDirs)

# Install server executable
if(AIPR_BUILD_SERVER)
    install(TARGETS aipr-engine-server
        RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
    )
endif()

# Install headers (for development)
install(DIRECTORY include/
    DESTINATION ${CMAKE_INSTALL_INCLUDEDIR}/aipr-engine
    FILES_MATCHING PATTERN "*.h"
)

# Package configuration
include(CMakePackageConfigHelpers)

configure_package_config_file(
    ${CMAKE_CURRENT_SOURCE_DIR}/cmake/aipr-engine-config.cmake.in
    ${CMAKE_CURRENT_BINARY_DIR}/aipr-engine-config.cmake
    INSTALL_DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/aipr-engine
)

write_basic_package_version_file(
    ${CMAKE_CURRENT_BINARY_DIR}/aipr-engine-config-version.cmake
    VERSION ${PROJECT_VERSION}
    COMPATIBILITY SameMajorVersion
)

install(FILES
    ${CMAKE_CURRENT_BINARY_DIR}/aipr-engine-config.cmake
    ${CMAKE_CURRENT_BINARY_DIR}/aipr-engine-config-version.cmake
    DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/aipr-engine
)

# Summary
message(STATUS "")
message(STATUS "AI PR Reviewer Engine Configuration:")
message(STATUS "  Version:         ${PROJECT_VERSION}")
message(STATUS "  Build tests:     ${AIPR_BUILD_TESTS}")
message(STATUS "  Build tools:     ${AIPR_BUILD_TOOLS}")
message(STATUS "  Build server:    ${AIPR_BUILD_SERVER}")
message(STATUS "  Use FAISS:       ${AIPR_USE_FAISS}")
message(STATUS "  Use tree-sitter: ${AIPR_USE_TREE_SITTER}")
message(STATUS "")
message(STATUS "  TMS Cognitive Memory System: ENABLED")
message(STATUS "    - LTM (FAISS): Long-term code memory")
message(STATUS "    - STM: Session context ring buffer")
message(STATUS "    - MTM: Pattern/strategy graph")
message(STATUS "    - Cross-Memory Attention: Multi-head fusion")
message(STATUS "    - Compute Controller: Adaptive strategy selection")
if(AIPR_BUILD_SERVER)
    message(STATUS "")
    message(STATUS "  Server binary:   ${CMAKE_BINARY_DIR}/bin/aipr-engine${CMAKE_EXECUTABLE_SUFFIX}")
    message(STATUS "  Default port:    50051")
endif()
message(STATUS "")
