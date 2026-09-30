#pragma once

#include <property.h>
#include "macro.h"
#include "prop_flags.h"

using namespace spiritsaway::serialize;
using namespace spiritsaway::property;

namespace spiritsaway::rpg_example
{
	/// 道具：按 id 索引的背包元素
	class Meta(property) Item : public property_bag_item<int>
	{
	public:
		Meta(property(sync_clients)) int m_count = 0;
		Meta(property(save_db)) std::string m_name;
#ifndef __meta_parse__
#include "Item.generated.inch"
#endif
	};

	/// Buff：按 buff_id 索引，支持字段级增量同步（叠层、到期时间）
	class Meta(property) Buff : public property_bag_item<int>
	{
	public:
		Meta(property(sync_clients)) int m_level = 0;
		Meta(property(sync_clients)) float m_expire_ts = 0.f;
#ifndef __meta_parse__
#include "Buff.generated.inch"
#endif
	};

	using Inventory = property_bag<Item>;
	using BuffBag = property_bag<Buff>;
}

namespace spiritsaway::property
{
#ifndef __meta_parse__
#include "Item.proxy.inch"
#include "Buff.proxy.inch"
#endif
}
