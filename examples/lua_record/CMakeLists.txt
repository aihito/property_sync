# lua_record — Pure Lua Record/Replay + optional C++ cross (DSL-only)
#
# Truth source: dsl/*.psync
# Go psync → generated/{cpp,schema,lua,proto}

set(LR_ROOT ${CMAKE_CURRENT_SOURCE_DIR})
set(LR_GEN ${LR_ROOT}/generated)
set(LR_CPP ${LR_GEN}/cpp)
set(LR_FIX ${CMAKE_CURRENT_BINARY_DIR}/fixtures)
file(MAKE_DIRECTORY ${LR_GEN} ${LR_CPP} ${LR_FIX})

find_program(LUA_EXECUTABLE NAMES lua lua5.4 lua5.3)
if(NOT TARGET psync_tool)
	message(FATAL_ERROR "lua_record requires Go psync_tool")
endif()

set(LR_CPP_IMPL
	${LR_CPP}/Item.cpp
	${LR_CPP}/Buff.cpp
	${LR_CPP}/EquipItem.cpp
	${LR_CPP}/LoginRecord.cpp
	${LR_CPP}/Player.cpp
)
set(LR_CPP_GENERATED
	${LR_CPP}/PropFlags.h
	${LR_CPP}/Item.h
	${LR_CPP}/Buff.h
	${LR_CPP}/EquipItem.h
	${LR_CPP}/LoginRecord.h
	${LR_CPP}/Player.h
	${LR_CPP_IMPL}
)
set(LR_EMIT_MARKER ${LR_GEN}/schema/Player.schema.json)

add_custom_command(
	OUTPUT ${LR_CPP_GENERATED} ${LR_EMIT_MARKER}
	COMMAND ${PSYNC_BIN} emit
		${LR_ROOT}/dsl/player.psync
		-o ${LR_GEN}
		--root ${CMAKE_SOURCE_DIR}
	DEPENDS
		psync_tool ${PSYNC_BIN}
		${LR_ROOT}/dsl/player.psync
		${LR_ROOT}/dsl/items.psync
		${LR_ROOT}/dsl/flags.psync
		${CMAKE_SOURCE_DIR}/meta/mustache/property_h.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/property_proxy_h.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/property_cpp.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/class_header_full.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/class_cpp_full.mustache
		${CMAKE_SOURCE_DIR}/meta/mustache/flags_h.mustache
	COMMENT "lua_record: Go psync emit → ${LR_GEN}"
	VERBATIM
)
add_custom_target(lua_record_generate DEPENDS ${LR_CPP_GENERATED} ${LR_EMIT_MARKER})

add_executable(lua_record_cpp cpp/main.cpp ${LR_CPP_IMPL})
add_dependencies(lua_record_cpp lua_record_generate)
set_source_files_properties(${LR_CPP_IMPL} PROPERTIES GENERATED TRUE)
target_include_directories(lua_record_cpp PRIVATE ${LR_CPP} ${CMAKE_SOURCE_DIR}/include)
target_link_libraries(lua_record_cpp PRIVATE any_container)
target_compile_features(lua_record_cpp PRIVATE cxx_std_17)

add_executable(lua_record_replay_json cpp/replay_from_json.cpp ${LR_CPP_IMPL})
add_dependencies(lua_record_replay_json lua_record_generate)
set_source_files_properties(${LR_CPP_IMPL} PROPERTIES GENERATED TRUE)
target_include_directories(lua_record_replay_json PRIVATE ${LR_CPP} ${CMAKE_SOURCE_DIR}/include)
target_link_libraries(lua_record_replay_json PRIVATE any_container)
target_compile_features(lua_record_replay_json PRIVATE cxx_std_17)

if(LUA_EXECUTABLE)
	set(LR_LUA_PATH "${LR_GEN}/lua/?.lua;${LR_ROOT}/lua/?.lua;;")

	add_custom_target(lua_record_test
		COMMAND $<TARGET_FILE:lua_record_cpp>
		COMMAND ${CMAKE_COMMAND} -E env "LUA_PATH=${LR_LUA_PATH}"
			${LUA_EXECUTABLE} ${LR_ROOT}/lua/lua_record_test.lua
			${LR_GEN}/lua
			${CMAKE_CURRENT_BINARY_DIR}/lua_mutates.json
		WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR}
		DEPENDS lua_record_cpp lua_record_generate
		COMMENT "lua_record: API matrix + S5/S6 vs C++ mutates"
		VERBATIM
	)

	add_custom_target(lua_record_replay
		COMMAND $<TARGET_FILE:lua_record_cpp>
		COMMAND ${CMAKE_COMMAND} -E env "LUA_PATH=${LR_LUA_PATH}"
			${LUA_EXECUTABLE} ${LR_ROOT}/lua/lua_replay.lua
			${LR_GEN}/lua --batch
			${CMAKE_CURRENT_BINARY_DIR}/lua_mutates.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_sync_view.json
		COMMAND ${CMAKE_COMMAND} -E env "LUA_PATH=${LR_LUA_PATH}"
			${LUA_EXECUTABLE} ${LR_ROOT}/lua/lua_replay.lua
			${LR_GEN}/lua --snapshot
			${CMAKE_CURRENT_BINARY_DIR}/lua_final_snapshot.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_sync_view.json
		COMMAND ${CMAKE_COMMAND} -E env "LUA_PATH=${LR_LUA_PATH}"
			${LUA_EXECUTABLE} ${LR_ROOT}/lua/lua_replay.lua
			${LR_GEN}/lua --mixed
			${CMAKE_CURRENT_BINARY_DIR}/lua_checkpoint_snapshot.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_mutates_after_checkpoint.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_sync_view.json
		WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR}
		DEPENDS lua_record_cpp
		COMMENT "lua_record: CppRec→LuaRep batch/snapshot/mixed"
		VERBATIM
	)

	add_custom_target(lua_record_cross
		COMMAND $<TARGET_FILE:lua_record_cpp>
		COMMAND ${CMAKE_COMMAND} -E env "LUA_PATH=${LR_LUA_PATH}"
			${LUA_EXECUTABLE} ${LR_ROOT}/lua/lua_replay.lua
			${LR_GEN}/lua --batch
			${CMAKE_CURRENT_BINARY_DIR}/lua_mutates.json
			${CMAKE_CURRENT_BINARY_DIR}/lua_sync_view.json
		COMMAND ${CMAKE_COMMAND} -E env "LUA_PATH=${LR_LUA_PATH}"
			${LUA_EXECUTABLE} ${LR_ROOT}/lua/lua_cross_matrix.lua
			${LR_GEN}/lua
			${CMAKE_CURRENT_BINARY_DIR}
			--cpp-replay $<TARGET_FILE:lua_record_replay_json>
		WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR}
		DEPENDS lua_record_cpp lua_record_replay_json
		COMMENT "lua_record: S7 cross matrix"
		VERBATIM
	)

	add_custom_target(lua_record_all)
	add_dependencies(lua_record_all lua_record_test lua_record_replay lua_record_cross)
endif()
