#pragma once

#include "rpg_items.h"

namespace spiritsaway::rpg_example
{
	/// RPG 玩家根属性：演示同步可见性与背包增量
	class Meta(property) Player
	{
	protected:
		Meta(property(sync_clients)) std::string m_nickname;
		Meta(property(sync_clients)) int m_hp = 100;
		Meta(property(sync_clients)) int m_level = 1;
		/// 仅存库：周围玩家不应看到金币变化
		Meta(property(save_db)) int m_gold = 0;
		Meta(property(save_db, sync_clients)) Inventory m_inventory;
		Meta(property(save_db, sync_clients)) BuffBag m_buffs;
#ifndef __meta_parse__
#include "Player.generated.inch"
#endif
	};
}

namespace spiritsaway::property
{
#ifndef __meta_parse__
#include "Player.proxy.inch"
#endif
}
