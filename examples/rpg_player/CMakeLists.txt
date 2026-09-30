# RPG player example — property sync demo

set(RPG_EXAMPLE_GEN_DIR ${CMAKE_CURRENT_BINARY_DIR}/generated)
file(MAKE_DIRECTORY ${RPG_EXAMPLE_GEN_DIR})

# mustache_folder must be relative and start with '.' (generator requirement)
file(RELATIVE_PATH RPG_MUSTACHE_FOLDER_REL
	${CMAKE_CURRENT_BINARY_DIR}
	${CMAKE_SOURCE_DIR}/meta/mustache
)
if(NOT RPG_MUSTACHE_FOLDER_REL MATCHES "^\\.")
	set(RPG_MUSTACHE_FOLDER_REL "./${RPG_MUSTACHE_FOLDER_REL}")
endif()

# Clang resource-dir for libclang (needed so base classes parse correctly)
set(RPG_CLANG_RESOURCE_DIR "")
if(DEFINED ENV{CLANG_RESOURCE_DIR})
	set(RPG_CLANG_RESOURCE_DIR $ENV{CLANG_RESOURCE_DIR})
endif()
if(NOT RPG_CLANG_RESOURCE_DIR)
	foreach(_clang_bin clang++ clang++-21 clang++-20 clang)
		execute_process(
			COMMAND ${_clang_bin} -print-resource-dir
			OUTPUT_VARIABLE RPG_CLANG_RESOURCE_DIR
			OUTPUT_STRIP_TRAILING_WHITESPACE
			ERROR_QUIET
			RESULT_VARIABLE _clang_rd_rc
		)
		if(_clang_rd_rc EQUAL 0 AND RPG_CLANG_RESOURCE_DIR)
			break()
		endif()
		set(RPG_CLANG_RESOURCE_DIR "")
	endforeach()
endif()
if(NOT RPG_CLANG_RESOURCE_DIR AND EXISTS "/usr/lib/clang/21")
	set(RPG_CLANG_RESOURCE_DIR "/usr/lib/clang/21")
endif()
if(RPG_CLANG_RESOURCE_DIR)
	message(STATUS "rpg_player example: clang resource-dir = ${RPG_CLANG_RESOURCE_DIR}")
else()
	message(WARNING "rpg_player example: clang resource-dir not found; meta codegen may produce wrong proxies")
endif()

set(RPG_INCLUDE_DIRS_JSON
	"\"${CMAKE_SOURCE_DIR}/include\""
	"\"${CMAKE_CURRENT_SOURCE_DIR}\""
	"\"/usr/include\""
	"\"/usr/local/include\""
	"\"${RPG_EXAMPLE_GEN_DIR}\""
)
if(RPG_CLANG_RESOURCE_DIR)
	list(APPEND RPG_INCLUDE_DIRS_JSON "\"${RPG_CLANG_RESOURCE_DIR}/include\"")
endif()
foreach(_prefix IN LISTS CMAKE_PREFIX_PATH)
	if(EXISTS "${_prefix}/include")
		list(APPEND RPG_INCLUDE_DIRS_JSON "\"${_prefix}/include\"")
	endif()
endforeach()
set(RPG_INCLUDE_DIRS_CSV "")
set(_rpg_first TRUE)
foreach(_inc IN LISTS RPG_INCLUDE_DIRS_JSON)
	if(_rpg_first)
		set(RPG_INCLUDE_DIRS_CSV "${_inc}")
		set(_rpg_first FALSE)
	else()
		set(RPG_INCLUDE_DIRS_CSV "${RPG_INCLUDE_DIRS_CSV},\n\t\t${_inc}")
	endif()
endforeach()

set(RPG_DEFINITIONS_JSON "\"-x\", \"c++\", \"-std=c++17\", \"-fparse-all-comments\"")
if(RPG_CLANG_RESOURCE_DIR)
	set(RPG_DEFINITIONS_JSON "${RPG_DEFINITIONS_JSON}, \"-resource-dir\", \"${RPG_CLANG_RESOURCE_DIR}\"")
endif()

configure_file(
	${CMAKE_CURRENT_SOURCE_DIR}/config.json.in
	${CMAKE_CURRENT_BINARY_DIR}/config.json
	@ONLY
)

set(RPG_GENERATED_FILES
	${RPG_EXAMPLE_GEN_DIR}/Item.generated.inch
	${RPG_EXAMPLE_GEN_DIR}/Item.generated.incpp
	${RPG_EXAMPLE_GEN_DIR}/Item.proxy.inch
	${RPG_EXAMPLE_GEN_DIR}/Buff.generated.inch
	${RPG_EXAMPLE_GEN_DIR}/Buff.generated.incpp
	${RPG_EXAMPLE_GEN_DIR}/Buff.proxy.inch
	${RPG_EXAMPLE_GEN_DIR}/Player.generated.inch
	${RPG_EXAMPLE_GEN_DIR}/Player.generated.incpp
	${RPG_EXAMPLE_GEN_DIR}/Player.proxy.inch
)

add_custom_command(
	OUTPUT ${RPG_GENERATED_FILES}
	COMMAND $<TARGET_FILE:generate_property_sync> ${CMAKE_CURRENT_BINARY_DIR}/config.json
	WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR}
	DEPENDS
		generate_property_sync
		${CMAKE_CURRENT_BINARY_DIR}/config.json
		${CMAKE_CURRENT_SOURCE_DIR}/generate_entry.cpp
		${CMAKE_CURRENT_SOURCE_DIR}/rpg_player.h
		${CMAKE_CURRENT_SOURCE_DIR}/rpg_items.h
		${CMAKE_CURRENT_SOURCE_DIR}/prop_flags.h
		${CMAKE_CURRENT_SOURCE_DIR}/macro.h
	COMMENT "Generating RPG player property sync code"
	VERBATIM
)

add_custom_target(rpg_player_generate DEPENDS ${RPG_GENERATED_FILES})

add_executable(rpg_player_example
	main.cpp
	${RPG_GENERATED_FILES}
)

add_dependencies(rpg_player_example rpg_player_generate)

target_include_directories(rpg_player_example PRIVATE
	${CMAKE_CURRENT_SOURCE_DIR}
	${RPG_EXAMPLE_GEN_DIR}
	${CMAKE_SOURCE_DIR}/include
)

target_link_libraries(rpg_player_example PRIVATE any_container)

target_compile_features(rpg_player_example PRIVATE cxx_std_17)
