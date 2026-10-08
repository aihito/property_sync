# channel_matrix — Client sync + DB PB archive (lua-protobuf)
#
# Truth source: dsl/*.psync
# Go psync emit → generated/{cpp,schema,lua,proto}
# (Meta / rpg_player are NOT required for this example.)

set(CM_ROOT ${CMAKE_CURRENT_SOURCE_DIR})
set(CM_GEN ${CM_ROOT}/generated)
set(CM_CPP ${CM_GEN}/cpp)
set(CM_FIX ${CMAKE_CURRENT_BINARY_DIR}/fixtures)
file(MAKE_DIRECTORY ${CM_GEN} ${CM_CPP} ${CM_FIX})

find_program(LUA_EXECUTABLE NAMES lua lua5.4 lua5.3)
find_program(PROTOC_EXECUTABLE NAMES protoc)
find_program(LUAROCKS_EXECUTABLE NAMES luarocks)
if(NOT TARGET psync_tool)
	message(FATAL_ERROR "channel_matrix requires Go psync_tool (tools/psync)")
endif()

# --- luarocks lua-protobuf ---
set(CM_LUA_PATH "")
set(CM_LUA_CPATH "")
if(LUAROCKS_EXECUTABLE)
	execute_process(
		COMMAND ${LUAROCKS_EXECUTABLE} path --lr-path --lua-version 5.4
		OUTPUT_VARIABLE CM_LUA_PATH OUTPUT_STRIP_TRAILING_WHITESPACE ERROR_QUIET)
	execute_process(
		COMMAND ${LUAROCKS_EXECUTABLE} path --lr-cpath --lua-version 5.4
		OUTPUT_VARIABLE CM_LUA_CPATH OUTPUT_STRIP_TRAILING_WHITESPACE ERROR_QUIET)
endif()
set(CM_LUA_ENV_PATH "${CM_LUA_PATH};${CM_GEN}/lua/?.lua;${CM_ROOT}/lua/?.lua;;")
set(CM_LUA_ENV_CPATH "${CM_LUA_CPATH};;")

set(CM_CPP_IMPL
	${CM_CPP}/Item.cpp
	${CM_CPP}/Buff.cpp
	${CM_CPP}/EquipItem.cpp
	${CM_CPP}/LoginRecord.cpp
	${CM_CPP}/Player.cpp
)
set(CM_CPP_GENERATED_FILES
	${CM_CPP}/PropFlags.h
	${CM_CPP}/Item.h
	${CM_CPP}/Buff.h
	${CM_CPP}/EquipItem.h
	${CM_CPP}/LoginRecord.h
	${CM_CPP}/Player.h
	${CM_CPP_IMPL}
)

set(CM_EMIT_MARKER ${CM_GEN}/schema/Player.schema.json)
add_custom_command(
	OUTPUT ${CM_CPP_GENERATED_FILES} ${CM_EMIT_MARKER}
	COMMAND ${PSYNC_BIN} emit
		${CM_ROOT}/dsl/player.psync
		-o ${CM_GEN}
		--root ${CMAKE_SOURCE_DIR}
	DEPENDS
		psync_tool
		${PSYNC_BIN}
		${CM_ROOT}/dsl/player.psync
		${CM_ROOT}/dsl/items.psync
		${CM_ROOT}/dsl/flags.psync
		${CMAKE_SOURCE_DIR}/meta/mustache/property_h.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/property_proxy_h.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/property_cpp.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/class_header_full.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/class_cpp_full.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/flags_h.mustache
		${CMAKE_SOURCE_DIR}/meta/lua_runtime/property_runtime.lua
		${CMAKE_SOURCE_DIR}/meta/lua_runtime/property_record.lua
	COMMENT "channel_matrix: Go psync emit → ${CM_GEN}/{cpp,schema,lua,proto}"
	VERBATIM
)
add_custom_target(channel_matrix_generate DEPENDS ${CM_CPP_GENERATED_FILES} ${CM_EMIT_MARKER})

# --- protoc IDL check ---
if(PROTOC_EXECUTABLE)
	set(CM_PROTO_DESC ${CMAKE_CURRENT_BINARY_DIR}/channel_matrix_protos.pb)
	add_custom_command(
		OUTPUT ${CM_PROTO_DESC}
		COMMAND ${PROTOC_EXECUTABLE}
			--descriptor_set_out=${CM_PROTO_DESC}
			-I${CM_GEN}/proto
			${CM_GEN}/proto/Item.proto
			${CM_GEN}/proto/Buff.proto
			${CM_GEN}/proto/EquipItem.proto
			${CM_GEN}/proto/LoginRecord.proto
			${CM_GEN}/proto/Player.proto
		DEPENDS channel_matrix_generate
		COMMENT "channel_matrix: protoc descriptor check"
		VERBATIM
	)
	add_custom_target(channel_matrix_proto DEPENDS ${CM_PROTO_DESC})
endif()

# --- C++ M1 binary (generated *.cpp linked) ---
add_executable(channel_matrix_cpp cpp/main_dual_channel.cpp ${CM_CPP_IMPL})
add_dependencies(channel_matrix_cpp channel_matrix_generate)
set_source_files_properties(${CM_CPP_IMPL} PROPERTIES GENERATED TRUE)
target_include_directories(channel_matrix_cpp PRIVATE
	${CM_CPP}
	${CMAKE_SOURCE_DIR}/include
)
target_link_libraries(channel_matrix_cpp PRIVATE any_container)
target_compile_features(channel_matrix_cpp PRIVATE cxx_std_17)

# --- C++ Replay helper (Lua ↔ C++ cross check) ---
add_executable(channel_matrix_replay_json cpp/replay_from_json.cpp ${CM_CPP_IMPL})
add_dependencies(channel_matrix_replay_json channel_matrix_generate)
set_source_files_properties(${CM_CPP_IMPL} PROPERTIES GENERATED TRUE)
target_include_directories(channel_matrix_replay_json PRIVATE
	${CM_CPP}
	${CMAKE_SOURCE_DIR}/include
)
target_link_libraries(channel_matrix_replay_json PRIVATE any_container)
target_compile_features(channel_matrix_replay_json PRIVATE cxx_std_17)

# M1 + lua-pb
if(LUA_EXECUTABLE)
	add_custom_target(channel_matrix_cpp_test
		COMMAND $<TARGET_FILE:channel_matrix_cpp> ${CM_FIX}
		COMMAND ${CMAKE_COMMAND} -E env
			"LUA_PATH=${CM_LUA_ENV_PATH}" "LUA_CPATH=${CM_LUA_ENV_CPATH}"
			${LUA_EXECUTABLE} ${CM_ROOT}/lua/pb_archive_cli.lua
			${CM_GEN}/proto ${CM_GEN}/lua
			save ${CM_FIX}/db_view.json ${CM_FIX}/player_db.pb
		COMMAND ${CMAKE_COMMAND} -E env
			"LUA_PATH=${CM_LUA_ENV_PATH}" "LUA_CPATH=${CM_LUA_ENV_CPATH}"
			${LUA_EXECUTABLE} ${CM_ROOT}/lua/pb_archive_cli.lua
			${CM_GEN}/proto ${CM_GEN}/lua
			load ${CM_FIX}/player_db.pb ${CM_FIX}/db_view_from_pb.json
		COMMAND ${CMAKE_COMMAND} -E env
			"LUA_PATH=${CM_LUA_ENV_PATH}" "LUA_CPATH=${CM_LUA_ENV_CPATH}"
			${LUA_EXECUTABLE} ${CM_ROOT}/lua/pb_archive_cli.lua
			${CM_GEN}/proto ${CM_GEN}/lua
			roundtrip ${CM_FIX}/db_view.json
		WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR}
		DEPENDS channel_matrix_cpp channel_matrix_proto
		COMMENT "channel_matrix M1: C++ dual channel + lua-pb DB"
		VERBATIM
	)
endif()

if(LUA_EXECUTABLE)
	set(CM_LUA_ARGS
		${CM_ROOT}/lua/channel_matrix.lua
		${CM_GEN}/lua
		${CM_FIX}
		--proto-dir ${CM_GEN}/proto
		--cpp-replay $<TARGET_FILE:channel_matrix_replay_json>
	)
	add_custom_target(channel_matrix_lua_test
		COMMAND ${CMAKE_COMMAND} -E env
			"LUA_PATH=${CM_LUA_ENV_PATH}" "LUA_CPATH=${CM_LUA_ENV_CPATH}"
			${LUA_EXECUTABLE} ${CM_LUA_ARGS}
		WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR}
		DEPENDS channel_matrix_generate channel_matrix_proto channel_matrix_cpp_test channel_matrix_replay_json
		COMMENT "channel_matrix Lua Hub + lua-pb + CppRep"
		VERBATIM
	)
endif()

add_custom_target(channel_matrix_all)
if(TARGET channel_matrix_cpp_test)
	add_dependencies(channel_matrix_all channel_matrix_cpp_test)
endif()
if(TARGET channel_matrix_lua_test)
	add_dependencies(channel_matrix_all channel_matrix_lua_test)
endif()
