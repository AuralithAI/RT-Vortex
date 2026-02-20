cmake_minimum_required(VERSION 3.16)

# Enable testing
enable_testing()

# Find test dependencies
find_package(GTest QUIET)

if(NOT GTest_FOUND)
    # Use FetchContent to download GoogleTest
    include(FetchContent)
    FetchContent_Declare(
        googletest
        GIT_REPOSITORY https://github.com/google/googletest.git
        GIT_TAG        v1.14.0
    )
    # Prevent overriding the parent project's compiler/linker settings on Windows
    set(gtest_force_shared_crt ON CACHE BOOL "" FORCE)
    FetchContent_MakeAvailable(googletest)
endif()

# Test sources
set(TEST_SOURCES
    test_main.cpp
    test_indexer.cpp
    test_retriever.cpp
    test_parser.cpp
    test_heuristics.cpp
)

# Create test executable
add_executable(aipr-engine-tests ${TEST_SOURCES})

target_link_libraries(aipr-engine-tests PRIVATE
    aipr-engine
    GTest::gtest
    GTest::gtest_main
)

target_include_directories(aipr-engine-tests PRIVATE
    ${CMAKE_SOURCE_DIR}/include
)

# Register tests with CTest
include(GoogleTest)
gtest_discover_tests(aipr-engine-tests)

# Add custom target to run tests
add_custom_target(run-tests
    COMMAND ${CMAKE_CTEST_COMMAND} --output-on-failure
    DEPENDS aipr-engine-tests
)
