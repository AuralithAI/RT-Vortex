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
)

if(FAISS_LIBRARIES)
    target_link_libraries(aipr-engine PUBLIC ${FAISS_LIBRARIES})
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
)

if(FAISS_LIBRARIES)
    target_link_libraries(aipr-engine-shared PUBLIC ${FAISS_LIBRARIES})
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
message(STATUS "  Version:      ${PROJECT_VERSION}")
message(STATUS "  Build tests:  ${AIPR_BUILD_TESTS}")
message(STATUS "  Build tools:  ${AIPR_BUILD_TOOLS}")
message(STATUS "  Use FAISS:    ${AIPR_USE_FAISS}")
message(STATUS "  Use tree-sitter: ${AIPR_USE_TREE_SITTER}")
message(STATUS "")
