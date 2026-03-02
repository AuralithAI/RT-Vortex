cmake_minimum_required(VERSION 3.20)

# Set CMP0135 policy for FetchContent timestamp handling
if(POLICY CMP0135)
    cmake_policy(SET CMP0135 NEW)
endif()

project(aipr-engine
    VERSION 0.1.0
    DESCRIPTION "AI PR Reviewer - Core Engine"
    LANGUAGES C CXX
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
            set(ONNX_URL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz")
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
    src/tms/ingestion/embedding_engine.cpp
    src/tms/ingestion/repo_parser.cpp
)

# Source files - Utilities
set(UTIL_SOURCES
    src/util/json.cpp
    src/util/hashing.cpp
    src/util/fs.cpp
    src/util/perf_timer.cpp
)

# Main engine library
add_library(aipr-engine STATIC
    ${INDEXING_SOURCES}
    ${RETRIEVAL_SOURCES}
    ${REVIEW_SOURCES}
    ${STORAGE_SOURCES}
    ${MEMORY_SOURCES}
    ${TMS_SOURCES}
    ${UTIL_SOURCES}
)

target_include_directories(aipr-engine PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:include>
)

target_link_libraries(aipr-engine PUBLIC
    Threads::Threads
    CURL::libcurl
    nlohmann_json::nlohmann_json
)

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
    
    add_executable(aipr-engine-doctor tools/aipr-engine-doctor.cpp)
    target_link_libraries(aipr-engine-doctor PRIVATE aipr-engine)
    
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
    
    # Create the server executable
    add_executable(aipr-engine-server ${SERVER_SOURCES})
    
    # Set output name (without -server suffix for cleaner binary name)
    set_target_properties(aipr-engine-server PROPERTIES
        OUTPUT_NAME aipr-engine
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
    
    message(STATUS "Server executable will be: ${CMAKE_BINARY_DIR}/bin/aipr-engine${CMAKE_EXECUTABLE_SUFFIX}")
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
