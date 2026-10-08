/**
 * channel_matrix M1: C++ Record → Client sync queue + DB save_db snapshot.
 *
 * Exports (argv[1] = output directory):
 *   client_mutates.json / client_view.json / db_view.json
 * DB PB: lua pb_archive_cli.lua save db_view.json player_db.pb
 *
 * C++: DSL psync emit → generated/cpp/{Player.h, *.inch, *.incpp, PropFlags.h}
 */

#include <array>
#include <fstream>
#include <iostream>
#include <string>
#include <vector>

#include "Player.h"

using namespace spiritsaway::serialize;
using namespace spiritsaway::property;
using namespace spiritsaway::rpg_example;

namespace
{

int g_fail = 0;
std::vector<json> g_client_mutates;

void write_json(const std::string& path, const json& j)
{
	std::ofstream ofs(path);
	if (!ofs) {
		std::cerr << "cannot write " << path << "\n";
		std::exit(2);
	}
	ofs << j.dump(2) << "\n";
}

void drain_client(top_msg_queue& queue, prop_replay_proxy<Player>& client)
{
	while (!queue.empty()) {
		auto msg = queue.front();
		queue.pop_front();
		json exported;
		exported["offset"] = msg.offset.to_replay_offset().value();
		exported["offset_is_record"] = false;
		exported["cmd"] = static_cast<std::uint8_t>(msg.cmd);
		exported["flag"] = msg.flag.value;
		exported["data"] = msg.data;
		g_client_mutates.push_back(std::move(exported));
		if (!client.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data)) {
			std::cerr << "[FAIL] client replay\n";
			++g_fail;
		}
	}
}

} // namespace

int main(int argc, char** argv)
{
	const std::string out_dir = argc > 1 ? argv[1] : ".";

	std::vector<property_flags> need_flags;
	need_flags.push_back(property_flags{rpg_property_flags::sync_clients});
	top_msg_queue sync_queue(need_flags, /*ignore_default=*/true, /*with_array=*/true);

	Player server;
	Player client;
	prop_record_proxy<Player> sp(server, sync_queue, property_record_offset{},
								 property_flags{rpg_property_flags::mask_all});
	prop_replay_proxy<Player> cp(client);

	sp.nickname().set("Alice");
	sp.level().set(5);
	sp.hp().set(100);
	sp.gold().set(999); // save_db only
	drain_client(sync_queue, cp);

	{
		auto pos = sp.pos();
		pos.set(std::array<float, 3>{10.f, 0.f, 20.f});
		drain_client(sync_queue, cp);
		pos.item_change(1, 3.5f);
	}
	sp.tags().set(std::vector<std::string>{"warrior", "pvp"});
	sp.tags().push_back("guild");
	sp.attrs().insert("atk", 100);
	drain_client(sync_queue, cp);

	{
		auto inv = sp.inventory();
		json potion;
		potion["id"] = 1001;
		potion["count"] = 1;
		potion["name"] = "HP Potion";
		inv.insert(potion);
		drain_client(sync_queue, cp);
		if (auto item = inv.get(1001)) {
			item->count().set(5);
			drain_client(sync_queue, cp);
			item->name().set("Greater HP Potion"); // save_db — empty client queue
			if (!sync_queue.empty()) {
				std::cerr << "[FAIL] Item.name should not enqueue sync_clients\n";
				++g_fail;
				while (!sync_queue.empty()) {
					sync_queue.pop_front();
				}
			}
		}
	}

	{
		auto eq = sp.equipment();
		eq.resize(4);
		drain_client(sync_queue, cp);
		json sword;
		sword["id"] = 501;
		sword["slot"] = 0;
		sword["enhance"] = 0;
		sword["name"] = "Iron Sword";
		eq.insert(sword);
		drain_client(sync_queue, cp);
		if (auto s = eq.get(501)) {
			s->enhance().set(3);
			drain_client(sync_queue, cp);
		}
	}

	{
		auto hist = sp.login_history();
		json rec;
		rec["login_ts"] = 100.f;
		rec["logout_ts"] = 200.f;
		rec["ip"] = "10.0.0.1";
		hist.push_back(rec);
		drain_client(sync_queue, cp);
		if (auto row = hist.get(0)) {
			row->ip().set("10.0.0.8"); // save_db
			if (!sync_queue.empty()) {
				std::cerr << "[FAIL] LoginRecord.ip should not enqueue sync_clients\n";
				++g_fail;
				while (!sync_queue.empty()) {
					sync_queue.pop_front();
				}
			}
		}
	}

	const auto sync_flag = property_flags{rpg_property_flags::sync_clients};
	const auto db_flag = property_flags{rpg_property_flags::save_db};

	json client_view = server.encode_with_flag(sync_flag, true, false);
	client_view["schema_version"] = 1;
	json client_mirror = client.encode_with_flag(sync_flag, true, false);
	client_mirror["schema_version"] = 1;
	if (client_view != client_mirror) {
		std::cerr << "[FAIL] client view != server sync_clients view\n";
		++g_fail;
	} else {
		std::cout << "[PASS] C++ Client Replay matches sync_clients view\n";
	}

	json db_view = server.encode_with_flag(db_flag, true, false);
	// Value is SCHEMA_VERSION (1); proto field *number* is 1000 — do not confuse the two.
	db_view["schema_version"] = 1;

	if (!db_view.contains("gold") || db_view.at("gold") != 999) {
		std::cerr << "[FAIL] db_view missing gold\n";
		++g_fail;
	}
	if (client_view.contains("gold")) {
		std::cerr << "[FAIL] client_view must not contain gold\n";
		++g_fail;
	}

	write_json(out_dir + "/client_mutates.json", json(g_client_mutates));
	write_json(out_dir + "/client_view.json", client_view);
	write_json(out_dir + "/db_view.json", db_view);

	std::cout << "[PASS] wrote client_mutates.json (" << g_client_mutates.size() << ")\n";
	std::cout << "[PASS] wrote client_view.json / db_view.json → " << out_dir << "\n";

	if (g_fail) {
		std::cerr << "[FAIL] channel_matrix cpp fail=" << g_fail << "\n";
		return 1;
	}
	std::cout << "[PASS] channel_matrix cpp M1 scenario\n";
	return 0;
}

#include "Player.generated.incpp"
#include "Item.generated.incpp"
#include "Buff.generated.incpp"
#include "EquipItem.generated.incpp"
#include "LoginRecord.generated.incpp"
