# Go psync binary — DSL → IR / schema / lua / proto / C++
find_program(GO_EXECUTABLE NAMES go REQUIRED)
if(NOT GO_EXECUTABLE)
	message(FATAL_ERROR "Go toolchain required to build tools/psync")
endif()

set(PSYNC_BIN ${CMAKE_BINARY_DIR}/bin/psync)
set(PSYNC_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR})

file(GLOB_RECURSE PSYNC_GO_SOURCES
	${PSYNC_SRC_DIR}/cmd/*.go
	${PSYNC_SRC_DIR}/internal/**/*.go
	${PSYNC_SRC_DIR}/go.mod
	${PSYNC_SRC_DIR}/go.sum
)

add_custom_command(
	OUTPUT ${PSYNC_BIN}
	COMMAND ${CMAKE_COMMAND} -E make_directory ${CMAKE_BINARY_DIR}/bin
	COMMAND ${CMAKE_COMMAND} -E env
		"GOPROXY=https://goproxy.cn,direct"
		${GO_EXECUTABLE} build -o ${PSYNC_BIN} ./cmd/psync
	WORKING_DIRECTORY ${PSYNC_SRC_DIR}
	DEPENDS ${PSYNC_GO_SOURCES}
	COMMENT "Building Go psync → ${PSYNC_BIN}"
	VERBATIM
)
add_custom_target(psync_tool DEPENDS ${PSYNC_BIN})
set(PSYNC_BIN ${PSYNC_BIN} PARENT_SCOPE)
