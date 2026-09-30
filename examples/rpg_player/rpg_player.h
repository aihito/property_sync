#pragma once

#include "rpg_items.h"
#include <array>
#include <string>
#include <unordered_map>
#include <vector>

namespace spiritsaway::rpg_example
{
	/// RPG 玩家根属性：覆盖基础值 / STL / bag / slots / vec / flag 可见性
	class Meta(property) Player
	{
	protected:
		// --- 基础值（property_stl）---
		Meta(property(sync_clients)) std::string m_nickname;
		Meta(property(sync_clients)) int m_hp = 100;
		Meta(property(sync_clients)) int m_level = 1;
		/// 仅存库：周围观察者不应看到
		Meta(property(save_db)) int m_gold = 0;

		// --- STL 容器 ---
		Meta(property(sync_clients)) std::array<float, 3> m_pos{};           // 坐标 xyz
		Meta(property(sync_clients)) std::vector<std::string> m_tags;        // 标签列表
		Meta(property(sync_clients)) std::unordered_map<std::string, int> m_attrs; // 战斗属性表

		// --- 三种背包抽象 ---
		Meta(property(save_db, sync_clients)) Inventory m_inventory;     // bag: 按 id
		Meta(property(save_db, sync_clients)) BuffBag m_buffs;         // bag: 按 id
		Meta(property(save_db, sync_clients)) Equipment m_equipment;     // slots: 按格子
		Meta(property(save_db, sync_clients)) LoginHistory m_login_history; // vec: 有序记录

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
