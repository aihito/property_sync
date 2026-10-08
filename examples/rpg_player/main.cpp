/**
 * RPG 玩家属性同步——细粒度演示
 *
 * 覆盖：
 *   1. 基础值 set/clear
 *   2. array / vector / map（property_stl）
 *   3. property_bag（道具、Buff）
 *   4. property_slots（装备栏：resize/换位/挪格）
 *   5. property_vec（登录记录：有序 push/改字段/中间插入）
 *   6. flag 过滤（save_db 不同步给观察者）
 *
 * Server record → mutate_msg 队列 → Client replay（内存模拟网络）
 */

#include <fstream>
#include <iostream>
#include <string>
#include <vector>

#include "rpg_player.h"

using namespace spiritsaway::serialize;
using namespace spiritsaway::property;
using namespace spiritsaway::rpg_example;

namespace
{
const char* cmd_name(property_cmd cmd)
{
    switch (cmd) {
        case property_cmd::clear:
            return "clear";
        case property_cmd::set:
            return "set";
        case property_cmd::add:
            return "add";
        case property_cmd::erase:
            return "erase";
        case property_cmd::push:
            return "push";
        case property_cmd::pop:
            return "pop";
        case property_cmd::item_change:
            return "item_change";
        case property_cmd::slot_swap:
            return "slot_swap";
        case property_cmd::slot_resize:
            return "slot_resize";
        case property_cmd::slot_move:
            return "slot_move";
        case property_cmd::update_fields:
            return "update_fields";
        default:
            return "other";
    }
}

void section(const char* title)
{
    std::cout << "\n========== " << title << " ==========\n";
}

void note(const char* text)
{
    std::cout << "  // " << text << "\n";
}

int g_fail = 0;

/// Collected mutate batch for pure-Lua 对拍 (offset = replay offset value).
std::vector<json> g_mutate_batch;
/// Index into g_mutate_batch after STL sections — start of bag/slots/vec delta.
std::size_t g_checkpoint_mutate_index = 0;
json g_checkpoint_snapshot;

void drain_and_replay(top_msg_queue& queue, prop_replay_proxy<Player>& client, bool quiet = false)
{
    while (!queue.empty()) {
        auto msg = queue.front();
        queue.pop_front();
        if (!quiet) {
            std::cout << "  [sync] " << cmd_name(msg.cmd)
                      << " flag=" << msg.flag.value
                      << " data=" << msg.data.dump() << "\n";
        }
        json exported;
        exported["offset"] = msg.offset.to_replay_offset().value();
        exported["offset_is_record"] = false;
        exported["cmd"] = static_cast<std::uint8_t>(msg.cmd);
        exported["flag"] = msg.flag.value;
        exported["data"] = msg.data;
        g_mutate_batch.push_back(std::move(exported));

        if (!client.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data)) {
            std::cout << "  [error] replay failed\n";
            ++g_fail;
        }
    }
}

void expect_empty(top_msg_queue& queue, const char* reason)
{
    if (queue.empty()) {
        std::cout << "  [ok] 队列为空 — " << reason << "\n";
    }
    else {
        std::cout << "  [fail] 预期空队列但仍有 " << queue.queue().size() << " 条 — " << reason << "\n";
        ++g_fail;
        while (!queue.empty()) {
            queue.pop_front();
        }
    }
}

bool same_visible(const Player& a, const Player& b)
{
    const auto flag = property_flags{rpg_property_flags::sync_clients};
    return a.encode_with_flag(flag, true, false) == b.encode_with_flag(flag, true, false);
}

void check_synced(const Player& server, const Player& client, const char* after)
{
    if (same_visible(server, client)) {
        std::cout << "  [ok] 可见字段已同步 — " << after << "\n";
    }
    else {
        std::cout << "  [fail] 可见字段不一致 — " << after << "\n";
        ++g_fail;
    }
}
} // namespace

int main()
{
    std::vector<property_flags> need_flags;
    need_flags.push_back(property_flags{rpg_property_flags::sync_clients});
    top_msg_queue sync_queue(need_flags, /*ignore_default=*/true, /*with_array=*/true);

    Player server;
    Player client; // 观察者客户端上的镜像
    prop_record_proxy<Player> sp(server, sync_queue, property_record_offset{}, property_flags{rpg_property_flags::mask_all});
    prop_replay_proxy<Player> cp(client);

    // ------------------------------------------------------------------
    section("1) 基础值：昵称 / 等级 / 血量（property_stl 标量）");
    note("set → 周围看到新名字与战斗数值");
    sp.nickname().set("Alice");
    sp.level().set(5);
    sp.hp().set(80);
    drain_and_replay(sync_queue, cp);
    check_synced(server, client, "基础值 set");

    note("clear HP → 回到默认 0（演示 clear 命令）");
    sp.hp().clear();
    drain_and_replay(sync_queue, cp);
    sp.hp().set(80);
    drain_and_replay(sync_queue, cp);
    check_synced(server, client, "clear + 再 set");

    // ------------------------------------------------------------------
    section("2) array：坐标 pos[3]");
    note("整表 set，再改某一个分量（item_change）");
    {
        auto pos = sp.pos();
        pos.set(std::array<float, 3>{10.f, 0.f, 20.f});
        drain_and_replay(sync_queue, cp);
        pos.item_change(1, 3.5f); // y = 3.5
        drain_and_replay(sync_queue, cp);
    }
    check_synced(server, client, "array");

    // ------------------------------------------------------------------
    section("3) vector：标签 tags（简单元素，非整 item 结构）");
    note("set → push_back → pop_back → 按段 erase");
    {
        auto tags = sp.tags();
        tags.set(std::vector<std::string>{"newbie", "warrior"});
        drain_and_replay(sync_queue, cp);
        tags.push_back("vip");
        drain_and_replay(sync_queue, cp);
        tags.pop_back();
        drain_and_replay(sync_queue, cp);
        tags.push_back("pvp");
        tags.push_back("guild");
        drain_and_replay(sync_queue, cp);
        tags.erase_multi(0, 1); // 删掉 newbie
        drain_and_replay(sync_queue, cp);
    }
    check_synced(server, client, "vector tags");

    // ------------------------------------------------------------------
    section("4) map：战斗属性 attrs");
    note("insert / 覆盖同 key / erase / clear");
    {
        auto attrs = sp.attrs();
        attrs.insert("atk", 100);
        drain_and_replay(sync_queue, cp);
        attrs.insert("atk", 120); // 覆盖
        drain_and_replay(sync_queue, cp);
        attrs.insert("def", 50);
        drain_and_replay(sync_queue, cp);
        attrs.erase("def");
        drain_and_replay(sync_queue, cp);
    }
    check_synced(server, client, "map attrs");

    // P4 checkpoint: full sync snapshot + subsequent mutates = mixed 对拍
    {
        const auto sync_flag = property_flags{rpg_property_flags::sync_clients};
        g_checkpoint_snapshot = server.encode_with_flag(sync_flag, true, false);
        g_checkpoint_snapshot["schema_version"] = 1;
        g_checkpoint_mutate_index = g_mutate_batch.size();
        std::cout << "  [checkpoint] snapshot after STL; mutate_index="
                  << g_checkpoint_mutate_index << "\n";
    }

    // ------------------------------------------------------------------
    section("5) property_bag：道具背包 inventory（按 id）");
    note("插入药水 id=1001 → 只改 count 字段 → 再插入材料 → erase");
    {
        auto inv = sp.inventory();
        json potion;
        potion["id"] = 1001;
        potion["count"] = 1;
        potion["name"] = "HP Potion";
        inv.insert(potion);
        drain_and_replay(sync_queue, cp);

        if (auto item = inv.get(1001)) {
            note("字段级：count 1→5，name(save_db) 变更不会进 sync 队列");
            item->count().set(5);
            drain_and_replay(sync_queue, cp);
            item->name().set("Greater HP Potion");
            expect_empty(sync_queue, "Item.name 仅 save_db");
        }

        json ore;
        ore["id"] = 2001;
        ore["count"] = 10;
        ore["name"] = "Iron Ore";
        inv.insert(ore);
        drain_and_replay(sync_queue, cp);

        note("get_insert：没有则创建，有则返回已有");
        auto created = inv.get_insert(3001);
        created.count().set(2);
        drain_and_replay(sync_queue, cp);

        inv.erase(2001);
        drain_and_replay(sync_queue, cp);
    }
    check_synced(server, client, "bag inventory");

    // ------------------------------------------------------------------
    section("6) property_bag：Buff 列表（叠层 / 到期）");
    {
        auto buffs = sp.buffs();
        json buff;
        buff["id"] = 200;
        buff["level"] = 1;
        buff["expire_ts"] = 1000.f;
        buffs.insert(buff);
        drain_and_replay(sync_queue, cp);

        if (auto b = buffs.get(200)) {
            note("叠层 level 1→3，刷新 expire_ts");
            b->level().set(3);
            drain_and_replay(sync_queue, cp);
            b->expire_ts().set(9999.f);
            drain_and_replay(sync_queue, cp);
        }
    }
    check_synced(server, client, "bag buffs");

    // ------------------------------------------------------------------
    section("7) property_slots：装备栏 equipment（有格子）");
    note("必须先 resize 才有格子；再 insert / 改强化 / swap / move / 按格删除");
    {
        auto eq = sp.equipment();

        note("未 resize 时 insert 不会产生同步（与 test 行为一致）");
        json sword_early;
        sword_early["id"] = 1;
        sword_early["slot"] = 0;
        sword_early["enhance"] = 0;
        sword_early["name"] = "Wood Sword";
        eq.insert(sword_early);
        expect_empty(sync_queue, "未 resize 前 insert 不同步");

        eq.resize(6); // 6 格装备栏
        drain_and_replay(sync_queue, cp);

        json sword;
        sword["id"] = 501;
        sword["slot"] = 0;
        sword["enhance"] = 0;
        sword["name"] = "Iron Sword";
        eq.insert(sword);
        drain_and_replay(sync_queue, cp);

        json shield;
        shield["id"] = 502;
        shield["slot"] = eq.data().get_first_empty_slot();
        shield["enhance"] = 1;
        shield["name"] = "Wood Shield";
        eq.insert(shield);
        drain_and_replay(sync_queue, cp);

        if (auto s = eq.get(501)) {
            note("字段级：强化 +1");
            s->enhance().set(3);
            drain_and_replay(sync_queue, cp);
        }

        note("swap_slot(0,1)：交换两格外观位置");
        eq.swap_slot(0, 1);
        drain_and_replay(sync_queue, cp);

        note("move_slot：把某格挪到空位（演示用 1→3）");
        eq.move_slot(1, 3);
        drain_and_replay(sync_queue, cp);

        if (auto at3 = eq.get_slot(3)) {
            note("按 slot 访问成功");
            (void)at3;
        }

        eq.erase(502);
        drain_and_replay(sync_queue, cp);
    }
    check_synced(server, client, "slots equipment");

    // ------------------------------------------------------------------
    section("8) property_vec：登录记录 login_history（有序）");
    note("顺序即语义：push 两条 → 改当前条 logout → 中间 insert → 删旧记录");
    {
        auto hist = sp.login_history();

        json r0;
        r0["login_ts"] = 100.f;
        r0["logout_ts"] = 200.f;
        r0["ip"] = "10.0.0.1";
        hist.push_back(r0);
        drain_and_replay(sync_queue, cp);

        json r1;
        r1["login_ts"] = 300.f;
        r1["logout_ts"] = 0.f; // 还在线
        r1["ip"] = "10.0.0.1";
        hist.push_back(r1);
        drain_and_replay(sync_queue, cp);

        if (auto cur = hist.get(1)) {
            note("字段级：补写登出时间；ip 仅 save_db 不同步");
            cur->logout_ts().set(400.f);
            drain_and_replay(sync_queue, cp);
            cur->ip().set("10.0.0.2");
            expect_empty(sync_queue, "LoginRecord.ip 仅 save_db");
        }

        json mid;
        mid["login_ts"] = 250.f;
        mid["logout_ts"] = 280.f;
        mid["ip"] = "10.0.0.8";
        note("insert(1, ...)：在中间插入一条，后面下标后移");
        hist.insert(1, mid);
        drain_and_replay(sync_queue, cp);

        note("erase_multi(0,1)：删掉最旧一条");
        hist.erase_multi(0, 1);
        drain_and_replay(sync_queue, cp);

        hist.push_back(r1);
        drain_and_replay(sync_queue, cp);
        hist.pop_back();
        drain_and_replay(sync_queue, cp);
    }
    check_synced(server, client, "vec login_history");

    // ------------------------------------------------------------------
    section("9) flag 过滤：金币只存库，不进观察者同步队列");
    sp.gold().set(99999);
    expect_empty(sync_queue, "Player.gold 仅 save_db");

    std::cout << "\n--- encode 视图对比 ---\n";
    std::cout << "sync_clients:\n"
              << server.encode_with_flag(property_flags{rpg_property_flags::sync_clients}, true, false).dump(2)
              << "\n";
    std::cout << "save_db（含 gold / name / ip 等）:\n"
              << server.encode_with_flag(property_flags{rpg_property_flags::save_db}, true, false).dump(2)
              << "\n";

    // ------------------------------------------------------------------
    section("10) 最终校验");
    std::cout << "client sync_clients 视图:\n"
              << client.encode_with_flag(property_flags{rpg_property_flags::sync_clients}, true, false).dump(2)
              << "\n";

    if (!same_visible(server, client)) {
        std::cout << "[FAIL] 最终可见字段不一致\n";
        ++g_fail;
    }

    // ------------------------------------------------------------------
    section("11) 导出 Lua 对拍产物");
    {
        const auto sync_flag = property_flags{rpg_property_flags::sync_clients};
        json sync_view = server.encode_with_flag(sync_flag, true, false);
        sync_view["schema_version"] = 1;

        json delta = json::array();
        for (std::size_t i = g_checkpoint_mutate_index; i < g_mutate_batch.size(); ++i) {
            delta.push_back(g_mutate_batch[i]);
        }

        const char* mutate_path = "lua_mutates.json";
        const char* view_path = "lua_sync_view.json";
        const char* snap_path = "lua_checkpoint_snapshot.json";
        const char* delta_path = "lua_mutates_after_checkpoint.json";
        const char* full_snap_path = "lua_final_snapshot.json";
        {
            std::ofstream ofs(mutate_path);
            ofs << json(g_mutate_batch).dump(2) << "\n";
        }
        {
            std::ofstream ofs(view_path);
            ofs << sync_view.dump(2) << "\n";
        }
        {
            std::ofstream ofs(snap_path);
            ofs << g_checkpoint_snapshot.dump(2) << "\n";
        }
        {
            std::ofstream ofs(delta_path);
            ofs << delta.dump(2) << "\n";
        }
        {
            std::ofstream ofs(full_snap_path);
            ofs << sync_view.dump(2) << "\n";
        }
        std::cout << "  wrote " << mutate_path << " (" << g_mutate_batch.size() << " msgs)\n";
        std::cout << "  wrote " << view_path << "\n";
        std::cout << "  wrote " << snap_path << " + " << delta_path
                  << " (" << delta.size() << " delta msgs)\n";
        std::cout << "  wrote " << full_snap_path << "\n";
    }

    if (g_fail == 0) {
        std::cout << "[PASS] 全部场景通过（基础值 / STL / bag / slots / vec / flag）\n";
        return 0;
    }
    std::cout << "[FAIL] 失败计数=" << g_fail << "\n";
    return 1;
}

#include "Player.generated.incpp"
#include "Item.generated.incpp"
#include "Buff.generated.incpp"
#include "EquipItem.generated.incpp"
#include "LoginRecord.generated.incpp"
