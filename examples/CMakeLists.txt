option(WITH_RPG_EXAMPLE "Build RPG player property sync example" ON)

if(WITH_RPG_EXAMPLE)
	add_subdirectory(rpg_player)
endif()
