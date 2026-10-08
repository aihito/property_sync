/**
 * Replay mutate JSON onto a Player (C++ Rep) and write sync_clients view.
 *
 * Usage:
 *   channel_matrix_replay_json <mutates.json> <out_view.json>
 */

#include <fstream>
#include <iostream>
#include <string>

#include "Player.h"

using namespace spiritsaway::serialize;
using namespace spiritsaway::property;
using namespace spiritsaway::rpg_example;

namespace
{

json load_json(const char* path)
{
	std::ifstream ifs(path);
	if (!ifs) {
		std::cerr << "cannot open " << path << "\n";
		std::exit(2);
	}
	json j;
	ifs >> j;
	return j;
}

void write_json(const char* path, const json& j)
{
	std::ofstream ofs(path);
	if (!ofs) {
		std::cerr << "cannot write " << path << "\n";
		std::exit(2);
	}
	ofs << j.dump(2) << "\n";
}

} // namespace

int main(int argc, char** argv)
{
	if (argc < 3) {
		std::cerr << "usage: " << argv[0] << " <mutates.json> <out_view.json>\n";
		return 2;
	}

	const json batch = load_json(argv[1]);
	if (!batch.is_array()) {
		std::cerr << "mutates.json must be a JSON array\n";
		return 1;
	}

	Player player;
	prop_replay_proxy<Player> replay(player);

	std::size_t i = 0;
	for (const auto& msg : batch) {
		++i;
		const auto offset = property_replay_offset{msg.at("offset").get<std::uint64_t>()};
		const auto cmd = static_cast<property_cmd>(msg.at("cmd").get<std::uint8_t>());
		json data = json();
		if (msg.contains("data") && !msg.at("data").is_null()) {
			data = msg.at("data");
		}
		if (!replay.replay(offset, cmd, data)) {
			std::cerr << "[FAIL] C++ replay #" << i << " offset=" << offset.value()
					  << " cmd=" << static_cast<int>(cmd) << " data=" << data.dump() << "\n";
			return 1;
		}
	}

	json view = player.encode_with_flag(property_flags{rpg_property_flags::sync_clients}, true, false);
	write_json(argv[2], view);
	return 0;
}

#include "Player.generated.incpp"
#include "Item.generated.incpp"
#include "Buff.generated.incpp"
#include "EquipItem.generated.incpp"
#include "LoginRecord.generated.incpp"
