#pragma once

#include <property.h>
#include "macro.h"
#include "prop_flags.h"

using namespace spiritsaway::serialize;
using namespace spiritsaway::property;

namespace spiritsaway::rpg_example
{
/// 道具：property_bag —— 按 id 索引（材料/消耗品）
class Meta(property) Item : public property_bag_item<int>
{
public:
    Meta(property(sync_clients)) int m_count = 0;
    Meta(property(save_db)) std::string m_name;
#ifndef __meta_parse__
#include "Item.generated.inch"
#endif
};

/// Buff：property_bag —— 按 buff_id，字段级叠层/到期
class Meta(property) Buff : public property_bag_item<int>
{
public:
    Meta(property(sync_clients)) int m_level = 0;
    Meta(property(sync_clients)) float m_expire_ts = 0.f;
#ifndef __meta_parse__
#include "Buff.generated.inch"
#endif
};

/// 装备：property_slots —— 有格子号（武器/防具栏）
class Meta(property) EquipItem : public property_slot_item<int>
{
public:
    Meta(property(sync_clients)) int m_enhance = 0; // 强化等级
    Meta(property(sync_clients)) std::string m_name;
#ifndef __meta_parse__
#include "EquipItem.generated.inch"
#endif
};

/// 登录记录：property_vec —— 有序复杂记录（顺序即语义）
class Meta(property) LoginRecord : public property_vec_item
{
public:
    Meta(property(sync_clients)) float m_login_ts = 0.f;
    Meta(property(sync_clients)) float m_logout_ts = 0.f;
    Meta(property(save_db)) std::string m_ip;
#ifndef __meta_parse__
#include "LoginRecord.generated.inch"
#endif
};

using Inventory = property_bag<Item>;
using BuffBag = property_bag<Buff>;
using Equipment = property_slots<EquipItem>;
using LoginHistory = property_vec<LoginRecord>;
} // namespace spiritsaway::rpg_example

namespace spiritsaway::property
{
#ifndef __meta_parse__
#include "Item.proxy.inch"
#include "Buff.proxy.inch"
#include "EquipItem.proxy.inch"
#include "LoginRecord.proxy.inch"
#endif
} // namespace spiritsaway::property
