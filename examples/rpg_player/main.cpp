/**
 * RPG 玩家属性同步演示
 *
 * 模拟：Server 权威对象改属性 → mutate_msg 队列 → Client 镜像 replay
 * 真实项目中「队列 → 网络 → 对端」由宿主框架完成，这里用内存队列代替。
 *
 * 编译前需用 meta 工具生成：
 *   Item / Buff / Player 的 .generated.inch / .generated.incpp / .proxy.inch
 */

#include <iostream>
#include <iomanip>
#include <string>

#include "rpg_player.h"

using namespace spiritsaway::serialize;
using namespace spiritsaway::property;
using namespace spiritsaway::rpg_example;

namespace
{
	void drain_and_replay(top_msg_queue& queue, prop_replay_proxy<Player>& client)
	{
		while (!queue.empty())
		{
			auto msg = queue.front();
			queue.pop_front();
			std::cout << "  [sync] cmd=" << static_cast<int>(msg.cmd)
			          << " flag=" << msg.flag.value
			          << " data=" << msg.data.dump() << "\n";
			if (!client.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data))
			{
				std::cout << "  [error] replay failed\n";
			}
		}
	}

	bool same_visible(const Player& a, const Player& b)
	{
		// 用 sync_clients 视图比较：金币等仅存库字段不要求客户端一致
		const auto flag = property_flags{rpg_property_flags::sync_clients};
		return a.encode_with_flag(flag, true, false) == b.encode_with_flag(flag, true, false);
	}
}

int main()
{
	// 只订阅「同步给客户端」的变更；save_db 单独通道在真实项目里另开队列
	std::vector<property_flags> need_flags;
	need_flags.push_back(property_flags{rpg_property_flags::sync_clients});

	top_msg_queue sync_queue(need_flags, /*ignore_default=*/true, /*with_array=*/true);

	Player server;
	Player client_observer; // 周围某个玩家客户端上的 Alice 镜像

	prop_record_proxy<Player> sp(
		server,
		sync_queue,
		property_record_offset{},
		property_flags{rpg_property_flags::mask_all});
	prop_replay_proxy<Player> cp(client_observer);

	std::cout << "=== 1) 改昵称（周围应看到新名字）===\n";
	sp.nickname().set("Alice");
	drain_and_replay(sync_queue, cp);

	std::cout << "=== 2) 升级 + 扣血 ===\n";
	sp.level().set(5);
	sp.hp().set(80);
	drain_and_replay(sync_queue, cp);

	std::cout << "=== 3) 获得药水，再把数量改成 5（字段级增量）===\n";
	{
		json potion;
		potion["id"] = 1001;
		potion["count"] = 1;
		potion["name"] = "HP Potion";
		sp.inventory().insert(potion);
		drain_and_replay(sync_queue, cp);

		auto item = sp.inventory().get(1001);
		if (item)
		{
			item->count().set(5);
			drain_and_replay(sync_queue, cp);
		}
	}

	std::cout << "=== 4) 获得 Buff，再叠层 ===\n";
	{
		json buff;
		buff["id"] = 200;
		buff["level"] = 1;
		buff["expire_ts"] = 9999.0f;
		sp.buffs().insert(buff);
		drain_and_replay(sync_queue, cp);

		auto b = sp.buffs().get(200);
		if (b)
		{
			b->level().set(2);
			drain_and_replay(sync_queue, cp);
		}
	}

	std::cout << "=== 5) 改金币（仅 save_db，不应进入 sync_clients 队列）===\n";
	sp.gold().set(9999);
	if (sync_queue.empty())
	{
		std::cout << "  [ok] 金币变更未进入客户端同步队列\n";
	}
	else
	{
		std::cout << "  [unexpected] 队列非空\n";
		drain_and_replay(sync_queue, cp);
	}

	std::cout << "\n--- Server (sync_clients 视图) ---\n"
	          << server.encode_with_flag(property_flags{rpg_property_flags::sync_clients}, true, false).dump(2)
	          << "\n--- Client observer ---\n"
	          << client_observer.encode_with_flag(property_flags{rpg_property_flags::sync_clients}, true, false).dump(2)
	          << "\n";

	if (same_visible(server, client_observer))
	{
		std::cout << "[PASS] 观察者可见字段与服务器一致\n";
		return 0;
	}
	std::cout << "[FAIL] 可见字段不一致\n";
	return 1;
}

#include "Player.generated.incpp"
#include "Item.generated.incpp"
#include "Buff.generated.incpp"
