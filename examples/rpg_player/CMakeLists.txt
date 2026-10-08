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
	# normalize e.g. /usr/bin/../lib/clang/21 → absolute path
	get_filename_component(RPG_CLANG_RESOURCE_DIR "${RPG_CLANG_RESOURCE_DIR}" REALPATH)
	list(APPEND RPG_INCLUDE_DIRS_JSON "\"${RPG_CLANG_RESOURCE_DIR}/include\"")
endif()

# any_container / nlohmann headers — required for libclang to parse bag base classes & STL maps
foreach(_tgt any_container nlohmann_json::nlohmann_json)
	if(TARGET ${_tgt})
		get_target_property(_rpg_tgt_incs ${_tgt} INTERFACE_INCLUDE_DIRECTORIES)
		if(_rpg_tgt_incs)
			foreach(_inc IN LISTS _rpg_tgt_incs)
				# strip generator expressions like $<BUILD_INTERFACE:...>
				string(REGEX REPLACE "^\\$<BUILD_INTERFACE:(.*)>$" "\\1" _inc "${_inc}")
				string(REGEX REPLACE "^\\$<INSTALL_INTERFACE:(.*)>$" "" _inc "${_inc}")
				if(_inc AND EXISTS "${_inc}")
					list(APPEND RPG_INCLUDE_DIRS_JSON "\"${_inc}\"")
				endif()
			endforeach()
		endif()
	endif()
endforeach()

# CMAKE_PREFIX_PATH / DEPS fallbacks
foreach(_prefix IN LISTS CMAKE_PREFIX_PATH)
	if(EXISTS "${_prefix}/include")
		list(APPEND RPG_INCLUDE_DIRS_JSON "\"${_prefix}/include\"")
	endif()
endforeach()
if(DEFINED ENV{DEPS} AND EXISTS "$ENV{DEPS}/include")
	list(APPEND RPG_INCLUDE_DIRS_JSON "\"$ENV{DEPS}/include\"")
endif()
# common local install used in this workspace
if(EXISTS "/home/game/open-source/game-server/_deps/install/include")
	list(APPEND RPG_INCLUDE_DIRS_JSON "\"/home/game/open-source/game-server/_deps/install/include\"")
endif()

list(REMOVE_DUPLICATES RPG_INCLUDE_DIRS_JSON)
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
	${RPG_EXAMPLE_GEN_DIR}/EquipItem.generated.inch
	${RPG_EXAMPLE_GEN_DIR}/EquipItem.generated.incpp
	${RPG_EXAMPLE_GEN_DIR}/EquipItem.proxy.inch
	${RPG_EXAMPLE_GEN_DIR}/LoginRecord.generated.inch
	${RPG_EXAMPLE_GEN_DIR}/LoginRecord.generated.incpp
	${RPG_EXAMPLE_GEN_DIR}/LoginRecord.proxy.inch
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
		${CMAKE_SOURCE_DIR}/meta/mustache/property_lua_sync.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/property_proto.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/property_mutate_proto.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/property_schema.mustache
		${CMAKE_SOURCE_DIR}/meta/lua_runtime/property_runtime.lua
		${CMAKE_SOURCE_DIR}/meta/lua_runtime/json.lua
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

# P2: protoc compile-check generated Snapshot / Mutate protos
find_program(PROTOC_EXECUTABLE NAMES protoc)
if(PROTOC_EXECUTABLE)
	set(RPG_PROTO_DIR ${RPG_EXAMPLE_GEN_DIR}/proto)
	set(RPG_PROTO_DESC ${CMAKE_CURRENT_BINARY_DIR}/rpg_player_protos.pb)
	add_custom_command(
		OUTPUT ${RPG_PROTO_DESC}
		COMMAND ${PROTOC_EXECUTABLE}
			--descriptor_set_out=${RPG_PROTO_DESC}
			-I${RPG_PROTO_DIR}
			${RPG_PROTO_DIR}/property_mutate.proto
			${RPG_PROTO_DIR}/Item.proto
			${RPG_PROTO_DIR}/Buff.proto
			${RPG_PROTO_DIR}/EquipItem.proto
			${RPG_PROTO_DIR}/LoginRecord.proto
			${RPG_PROTO_DIR}/Player.proto
		DEPENDS rpg_player_generate
			${CMAKE_SOURCE_DIR}/meta/mustache/property_proto.mustache
			${CMAKE_SOURCE_DIR}/meta/mustache/property_mutate_proto.mustache
		COMMENT "protoc compile-check rpg_player protos"
		VERBATIM
	)
	add_custom_target(rpg_player_proto_check DEPENDS ${RPG_PROTO_DESC})
endif()

# P3/P4: Pure-Lua 对拍 — batch / snapshot / mixed
find_program(LUA_EXECUTABLE NAMES lua lua5.4 lua5.3)
if(LUA_EXECUTABLE)
	add_custom_target(rpg_player_lua_replay
		COMMAND $<TARGET_FILE:rpg_player_example>
		COMMAND ${LUA_EXECUTABLE}
			${CMAKE_CURRENT_SOURCE_DIR}/lua_replay.lua
			${RPG_EXAMPLE_GEN_DIR}/lua
			--batch
			${CMAKE_CURRENT_BINARY_DIR}/lua_mutates.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_sync_view.json
		COMMAND ${LUA_EXECUTABLE}
			${CMAKE_CURRENT_SOURCE_DIR}/lua_replay.lua
			${RPG_EXAMPLE_GEN_DIR}/lua
			--snapshot
			${CMAKE_CURRENT_BINARY_DIR}/lua_final_snapshot.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_sync_view.json
		COMMAND ${LUA_EXECUTABLE}
			${CMAKE_CURRENT_SOURCE_DIR}/lua_replay.lua
			${RPG_EXAMPLE_GEN_DIR}/lua
			--mixed
			${CMAKE_CURRENT_BINARY_DIR}/lua_checkpoint_snapshot.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_mutates_after_checkpoint.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_sync_view.json
		WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR}
		DEPENDS rpg_player_example
		COMMENT "C++/Lua batch+snapshot+mixed 对拍"
		VERBATIM
	)
endif()
