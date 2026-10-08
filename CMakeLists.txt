CMAKE_MINIMUM_REQUIRED(VERSION 3.4)

PROJECT(property_sync)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS}")
if(MSVC)
set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} /std:c++latest")
endif(MSVC)

INCLUDE_DIRECTORIES(${CMAKE_CURRENT_SOURCE_DIR}/include)
INCLUDE_DIRECTORIES(${CMAKE_CURRENT_SOURCE_DIR}/third_party/Mustache)


find_package(nlohmann_json CONFIG REQUIRED)

find_package(any_container CONFIG REQUIRED)

INCLUDE_DIRECTORIES(${any_container_include_dirs})

option(WITH_TEST "with test" ON)
option(WITH_EXAMPLES "with examples" ON)

add_library(${PROJECT_NAME} INTERFACE)
target_include_directories(${PROJECT_NAME} INTERFACE $<INSTALL_INTERFACE:include>)

target_link_libraries(${PROJECT_NAME} INTERFACE any_container)

target_precompile_headers(${PROJECT_NAME} INTERFACE [["property_sync/property.h"]])

# Codegen tools — must be available before examples that depend on them
add_subdirectory(meta)
add_subdirectory(tools/psync)

if(CMAKE_PROJECT_NAME STREQUAL PROJECT_NAME)
  set(IS_TOPLEVEL_PROJECT TRUE)
else()
  set(IS_TOPLEVEL_PROJECT FALSE)
endif()

if(IS_TOPLEVEL_PROJECT)

foreach(p LIB BIN INCLUDE)
	set(var CMAKE_INSTALL_${p}DIR)
	if(NOT IS_ABSOLUTE "${${var}}")
		string(TOLOWER ${p} lower_p)
        set(${var} "${CMAKE_INSTALL_PREFIX}/${lower_p}")
	endif()
endforeach()

export(TARGETS ${PROJECT_NAME}
     FILE ${CMAKE_BINARY_DIR}/${PROJECT_NAME}Targets.cmake)

file(WRITE
    ${CMAKE_BINARY_DIR}/${PROJECT_NAME}Config.cmake
    "include(\${CMAKE_CURRENT_LIST_DIR}/${PROJECT_NAME}Targets.cmake)\n")

install(FILES
        ${CMAKE_BINARY_DIR}/${PROJECT_NAME}Config.cmake
        DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/${PROJECT_NAME})

install(TARGETS ${PROJECT_NAME} EXPORT ${PROJECT_NAME}Targets
   ARCHIVE  DESTINATION lib
   LIBRARY DESTINATION lib
    RUNTIME DESTINATION bin
  )

install(DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}/include/ DESTINATION include/${PROJECT_NAME})

install(EXPORT ${PROJECT_NAME}Targets
        DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/${PROJECT_NAME})

if(WITH_TEST)
add_subdirectory(test)
endif(WITH_TEST)

if(WITH_EXAMPLES)
add_subdirectory(examples)
endif(WITH_EXAMPLES)

# One-shot: DSL unit tests + core/example acceptance targets
find_program(GO_EXECUTABLE NAMES go)
if(GO_EXECUTABLE AND EXISTS "${CMAKE_SOURCE_DIR}/tools/psync/go.mod")
	add_custom_target(psync_check
		COMMAND ${CMAKE_COMMAND} -E env "GOPROXY=https://goproxy.cn,direct"
			${GO_EXECUTABLE} test ./... -count=1
		WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}/tools/psync
		COMMENT "Go psync unit tests (IR golden + emit)"
		VERBATIM
	)
endif()

add_custom_target(check_all
	COMMENT "Run all property_sync checks (psync + examples)"
)
if(TARGET psync_check)
	add_dependencies(check_all psync_check)
endif()
if(TARGET property_test)
	add_custom_target(property_test_run
		COMMAND $<TARGET_FILE:property_test>
		DEPENDS property_test
		WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
		COMMENT "Run property_test"
		VERBATIM
	)
	add_dependencies(check_all property_test_run)
endif()
if(TARGET rpg_player_example)
	add_custom_target(rpg_player_example_run
		COMMAND $<TARGET_FILE:rpg_player_example>
		DEPENDS rpg_player_example
		WORKING_DIRECTORY ${CMAKE_BINARY_DIR}/examples/rpg_player
		COMMENT "Run rpg_player_example"
		VERBATIM
	)
	add_dependencies(check_all rpg_player_example_run)
endif()
if(TARGET channel_matrix_all)
	add_dependencies(check_all channel_matrix_all)
endif()
if(TARGET lua_record_all)
	add_dependencies(check_all lua_record_all)
endif()

endif(IS_TOPLEVEL_PROJECT)
