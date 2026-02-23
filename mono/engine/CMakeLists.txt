cmake_minimum_required(VERSION 3.20)

project(aipr-engine
    VERSION 0.1.0
    DESCRIPTION "AI PR Reviewer - Core Engine"
    LANGUAGES CXX
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

# =============================================================================
# libcurl for HTTP embedding API calls
# =============================================================================
find_package(CURL REQUIRED)
if(CURL_FOUND)
    message(STATUS "Found libcurl: ${CURL_VERSION_STRING}")
else()
    message(FATAL_ERROR "libcurl is required for embedding API calls. Install with:\n"
                        "  Ubuntu/Debian: sudo apt-get install libcurl4-openssl-dev\n"
                        "  macOS: brew install curl\n"
                        "  Windows: vcpkg install curl")
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

# FetchContent for downloading dependencies
include(FetchContent)

# Optional: FAISS for vector search
if(AIPR_USE_FAISS)
    find_package(faiss QUIET)
    if(faiss_FOUND)
        message(STATUS "FAISS found (system)")
        add_definitions(-DAIPR_HAS_FAISS)
        set(FAISS_LIBRARIES faiss)
    else()
        message(STATUS "FAISS not found, downloading via FetchContent...")
        
        # FAISS requires OpenMP and BLAS
        find_package(OpenMP QUIET)
        find_package(BLAS QUIET)
        
        if(OpenMP_FOUND AND BLAS_FOUND)
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
            
            FetchContent_MakeAvailable(faiss)
            
            add_definitions(-DAIPR_HAS_FAISS)
            set(FAISS_LIBRARIES faiss)
            message(STATUS "FAISS downloaded and configured")
        else()
            message(WARNING "FAISS requires OpenMP and BLAS. Install them or FAISS will be disabled.")
            message(WARNING "  Ubuntu/Debian: sudo apt-get install libomp-dev libopenblas-dev")
            message(WARNING "  macOS: brew install libomp openblas")
            message(WARNING "Using fallback vector search")
            set(FAISS_LIBRARIES "")
        endif()
    endif()
else()
    set(FAISS_LIBRARIES "")
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

if(AIPR_USE_ONNX AND ONNX_LIBRARIES)
    target_include_directories(aipr-engine PUBLIC ${ONNX_INCLUDE_DIR})
    target_link_libraries(aipr-engine PUBLIC ${ONNX_LIBRARIES})
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

if(AIPR_USE_ONNX AND ONNX_LIBRARIES)
    target_include_directories(aipr-engine-shared PUBLIC ${ONNX_INCLUDE_DIR})
    target_link_libraries(aipr-engine-shared PUBLIC ${ONNX_LIBRARIES})
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
include(GNUInstallDirs)

install(TARGETS aipr-engine aipr-engine-shared
    EXPORT aipr-engine-targets
    ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
    LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
    RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
)

install(DIRECTORY include/
    DESTINATION ${CMAKE_INSTALL_INCLUDEDIR}
)

install(EXPORT aipr-engine-targets
    FILE aipr-engine-targets.cmake
    NAMESPACE aipr::
    DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/aipr-engine
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
if(AIPR_BUILD_SERVER)
    message(STATUS "")
    message(STATUS "  Server binary:   ${CMAKE_BINARY_DIR}/bin/aipr-engine${CMAKE_EXECUTABLE_SUFFIX}")
    message(STATUS "  Default port:    50051")
endif()
message(STATUS "")
