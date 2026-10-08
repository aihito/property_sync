option(WITH_RPG_EXAMPLE "Build RPG player property sync example (C++ Meta)" ON)
option(WITH_CHANNEL_MATRIX "Build client/DB channel_matrix example" ON)
option(WITH_LUA_RECORD "Build Lua Record/Replay example" ON)

if(WITH_RPG_EXAMPLE)
	add_subdirectory(rpg_player)
endif()

if(WITH_CHANNEL_MATRIX)
	add_subdirectory(channel_matrix)
endif()

if(WITH_LUA_RECORD)
	add_subdirectory(lua_record)
endif()
