#include <cstdint>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <sstream>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <utility>
#include <vector>

#include <meta/parser/nodes/class.h>
#include <meta/parser/clang_utils.h>
#include <meta/parser/generator.h>

using namespace spiritsaway::meta;
using namespace spiritsaway::meta::generator;

namespace
{

// ---------------------------------------------------------------------------
// Config / IO
// ---------------------------------------------------------------------------

json load_json_file(const std::string& filename)
{
	std::ifstream t(filename);
	std::stringstream buffer;
	buffer << t.rdbuf();
	const auto cur_file_content = buffer.str();
	if (!json::accept(cur_file_content))
	{
		return {};
	}
	return json::parse(cur_file_content);
}

std::string read_file_text(const std::filesystem::path& path)
{
	std::ifstream ifs(path);
	if (!ifs)
	{
		return {};
	}
	return std::string((std::istreambuf_iterator<char>(ifs)), std::istreambuf_iterator<char>());
}

mustache::mustache load_mustache(const std::string& mustache_folder, const std::string& file_name)
{
	return mustache::mustache(read_file_text(std::filesystem::path(mustache_folder) / file_name));
}

// ---------------------------------------------------------------------------
// String / type helpers
// ---------------------------------------------------------------------------

std::string short_type_name(const std::string& qualified)
{
	const auto pos = qualified.rfind(':');
	return (pos == std::string::npos) ? qualified : qualified.substr(pos + 1);
}

/// Content inside the outermost `<...>` of a C++ type spelling, or empty.
std::string template_inner(const std::string& cpp_type)
{
	const auto lt = cpp_type.find('<');
	const auto gt = cpp_type.rfind('>');
	if (lt == std::string::npos || gt == std::string::npos || gt <= lt)
	{
		return {};
	}
	return cpp_type.substr(lt + 1, gt - lt - 1);
}

/// Split `K, V` at depth-0 comma (handles nested templates).
std::pair<std::string, std::string> split_template_args_2(const std::string& inside)
{
	int depth = 0;
	std::size_t comma = std::string::npos;
	for (std::size_t i = 0; i < inside.size(); ++i)
	{
		const char c = inside[i];
		if (c == '<')
		{
			++depth;
		}
		else if (c == '>')
		{
			--depth;
		}
		else if (c == ',' && depth == 0)
		{
			comma = i;
			break;
		}
	}
	if (comma == std::string::npos)
	{
		return {inside, {}};
	}
	std::string key = inside.substr(0, comma);
	std::string val = inside.substr(comma + 1);
	auto trim_front = [](std::string& s) {
		while (!s.empty() && (s.front() == ' ' || s.front() == '\t'))
		{
			s.erase(s.begin());
		}
	};
	trim_front(key);
	trim_front(val);
	return {key, val};
}

bool contains_any(const std::string& hay, std::initializer_list<const char*> needles)
{
	for (const char* n : needles)
	{
		if (hay.find(n) != std::string::npos)
		{
			return true;
		}
	}
	return false;
}

std::string map_proto_type(const std::string& cpp_type)
{
	const auto inside = template_inner(cpp_type);
	if (inside.empty())
	{
		return "map<string, string>";
	}
	const auto [key_ty, val_ty] = split_template_args_2(inside);
	(void)key_ty;
	if (val_ty.empty())
	{
		return "map<string, string>";
	}
	if (contains_any(val_ty, {"float", "double"}))
	{
		return "map<string, float>";
	}
	if (contains_any(val_ty, {"int", "uint", "long", "short"}))
	{
		return "map<string, int64>";
	}
	if (val_ty.find("bool") != std::string::npos)
	{
		return "map<string, bool>";
	}
	if (contains_any(val_ty, {"string"}))
	{
		return "map<string, string>";
	}
	return "map<string, string>";
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

enum class property_item_type
{
	none = 0,
	bag,
	slot,
	vec
};

struct FieldClassify
{
	std::string wire_kind = "other";
	std::string proto_type = "bytes";
	std::string item_class; // template item or nested property class
	bool has_property_interface = false;
};

struct FieldModel
{
	std::string name;
	std::size_t index = 0;
	std::string cpp_type;
	FieldClassify classify;
	std::string item_sync_module;
	std::string item_sync_local;
	std::string field_flags; // e.g. Flags::sync_clients|Flags::save_db
	std::vector<std::string> flag_names;
	std::vector<std::pair<std::string, std::string>> annotate_kvs; // annotate_* → value
};

struct ClassModel
{
	std::string class_name;
	std::string class_full_name;
	std::string class_namespace;
	std::string schema_version = "1";
	property_item_type item_type = property_item_type::none;
	bool has_base_class = false;
	bool is_property_item_direct_subclass = false;
	std::string base_class_name;
	std::size_t property_idx_begin = 0;
	std::size_t property_idx_max = 0;
	std::vector<FieldModel> fields;
	std::vector<std::string> proto_imports; // e.g. Item.proto
	std::vector<std::pair<std::string, std::string>> annotate_kvs;
};

// ---------------------------------------------------------------------------
// Item subtype / field classification
// ---------------------------------------------------------------------------

property_item_type subtype_of_property_item(const class_node* one_class, bool check_parent)
{
	static const std::string k_bag = "spiritsaway::property::property_bag_item";
	static const std::string k_slot = "spiritsaway::property::property_slot_item";
	static const std::string k_vec = "spiritsaway::property::property_vec_item";

	if (check_parent)
	{
		for (const auto* one_sub_class : one_class->base_classes())
		{
			const auto temp = subtype_of_property_item(one_sub_class, check_parent);
			if (temp != property_item_type::none)
			{
				return temp;
			}
		}
	}

	for (const auto* one_base : one_class->bases())
	{
		const auto& qn = one_base->qualified_name();
		if (qn.rfind(k_bag, 0) == 0)
		{
			return property_item_type::bag;
		}
		if (qn.rfind(k_slot, 0) == 0)
		{
			return property_item_type::slot;
		}
		if (qn.rfind(k_vec, 0) == 0)
		{
			return property_item_type::vec;
		}
	}
	return property_item_type::none;
}

FieldClassify classify_field(const std::string& cpp_type, const class_node* related_class)
{
	FieldClassify out;

	auto set_item_from_template = [&](const char* wire, const std::string& proto_prefix, const std::string& proto_suffix) {
		out.has_property_interface = true;
		out.wire_kind = wire;
		out.item_class = template_inner(cpp_type);
		if (!out.item_class.empty())
		{
			out.proto_type = proto_prefix + short_type_name(out.item_class) + proto_suffix;
		}
	};

	if (cpp_type.find("property_bag") != std::string::npos)
	{
		out.proto_type = "repeated bytes";
		set_item_from_template("bag", "repeated ", "Snapshot");
		if (out.item_class.empty())
		{
			out.proto_type = "repeated bytes";
		}
		return out;
	}
	if (cpp_type.find("property_slots") != std::string::npos)
	{
		out.proto_type = "bytes";
		set_item_from_template("slots", "", "SlotsSnapshot");
		if (out.item_class.empty())
		{
			out.proto_type = "bytes";
		}
		return out;
	}
	if (cpp_type.find("property_vec") != std::string::npos)
	{
		out.proto_type = "repeated bytes";
		set_item_from_template("vec", "repeated ", "Snapshot");
		if (out.item_class.empty())
		{
			out.proto_type = "repeated bytes";
		}
		return out;
	}
	if (cpp_type.find("std::vector") != std::string::npos)
	{
		out.wire_kind = "vector";
		out.proto_type = "repeated string"; // schema keeps cpp_type for detail
		return out;
	}
	if (cpp_type.find("std::unordered_map") != std::string::npos || cpp_type.find("std::map") != std::string::npos)
	{
		out.wire_kind = "map";
		out.proto_type = map_proto_type(cpp_type);
		return out;
	}
	if (cpp_type.find("std::array") != std::string::npos)
	{
		out.wire_kind = "array";
		out.proto_type = "repeated float";
		return out;
	}
	if (contains_any(cpp_type, {"basic_string", "std::string"}))
	{
		out.wire_kind = "string";
		out.proto_type = "string";
		return out;
	}
	if (cpp_type == "bool")
	{
		out.wire_kind = "bool";
		out.proto_type = "bool";
		return out;
	}
	if (contains_any(cpp_type, {"float", "double"}))
	{
		out.wire_kind = "number";
		out.proto_type = "float";
		return out;
	}
	if (contains_any(cpp_type, {"int", "uint", "long", "short"}))
	{
		out.wire_kind = "number";
		out.proto_type = "int64";
		return out;
	}

	if (related_class)
	{
		const auto& ann = related_class->annotations();
		if (ann.find("property") != ann.end())
		{
			out.has_property_interface = true;
			out.wire_kind = "object";
			out.proto_type = related_class->unqualified_name() + "Snapshot";
			out.item_class = related_class->qualified_name();
			return out;
		}
	}

	out.wire_kind = "other";
	out.proto_type = "bytes";
	return out;
}

bool is_container_item_wire(const std::string& wire)
{
	return wire == "bag" || wire == "slots" || wire == "vec";
}

// ---------------------------------------------------------------------------
// Parse clang class → ClassModel
// ---------------------------------------------------------------------------

ClassModel parse_class_model(const class_node* one_class, const std::string& flag_class)
{
	auto& the_logger = utils::get_logger();
	ClassModel model;
	model.class_name = one_class->unqualified_name();
	model.class_full_name = one_class->qualified_name();
	model.class_namespace = one_class->get_resident_ns()->qualified_name;
	model.schema_version = "1";

	auto property_fields = one_class->query_fields_with_pred([](const variable_node& node) {
		return node.unqualified_name().rfind("m_", 0) == 0 && filter_with_annotation<variable_node>("property", node);
	});
	auto property_fields_with_base = one_class->query_fields_with_pred_recursive([](const variable_node& node) {
		return node.unqualified_name().rfind("m_", 0) == 0 && filter_with_annotation<variable_node>("property", node);
	});

	model.property_idx_begin = property_fields_with_base.size() - property_fields.size();
	model.property_idx_max = property_fields_with_base.size();

	const auto base_classes = one_class->base_classes();
	const auto bases = one_class->bases();
	if (base_classes.size() > 1)
	{
		the_logger.error("cant generate property for class {} with {} base classes", model.class_full_name, base_classes.size());
		return model;
	}

	model.item_type = subtype_of_property_item(one_class, true);
	if (model.item_type == property_item_type::bag)
	{
		model.property_idx_begin += 1;
		model.property_idx_max += 1;
	}
	else if (model.item_type == property_item_type::slot)
	{
		model.property_idx_begin += 2;
		model.property_idx_max += 2;
	}

	if (base_classes.size() == 1 && base_classes[0]->unqualified_name() != "property_vec_item")
	{
		model.has_base_class = true;
		model.base_class_name = base_classes[0]->unqualified_name();
	}
	else if (bases.size() == 1)
	{
		if (subtype_of_property_item(one_class, false) != property_item_type::none)
		{
			model.is_property_item_direct_subclass = true;
			model.has_base_class = true;
			model.base_class_name = bases[0]->pretty_name();
		}
	}

	std::size_t field_index = model.property_idx_begin;
	std::unordered_set<std::string> imported_protos;

	for (const auto* one_field : property_fields)
	{
		FieldModel field;
		field.name = one_field->unqualified_name().substr(2); // strip m_
		field.index = field_index++;
		field.cpp_type = one_field->decl_type()->qualified_name();
		field.classify = classify_field(field.cpp_type, one_field->decl_type()->related_class());

		if (!field.classify.item_class.empty() && is_container_item_wire(field.classify.wire_kind))
		{
			const auto short_item = short_type_name(field.classify.item_class);
			field.item_sync_module = short_item + "_meta";
			field.item_sync_local = short_item + "Meta";
		}

		// Proto imports from container items / nested property objects
		if (!field.classify.item_class.empty()
			&& (is_container_item_wire(field.classify.wire_kind) || field.classify.wire_kind == "object"))
		{
			if (field.classify.wire_kind != "object"
				|| short_type_name(field.classify.item_class) != model.class_name)
			{
				const std::string file = short_type_name(field.classify.item_class) + ".proto";
				if (imported_protos.insert(file).second)
				{
					model.proto_imports.push_back(file);
				}
			}
		}

		const auto prop_ann_it = one_field->annotations().find("property");
		if (prop_ann_it != one_field->annotations().end())
		{
			for (const auto& one_flag_pair : prop_ann_it->second)
			{
				field.flag_names.push_back(one_flag_pair.first);
				if (field.field_flags.empty())
				{
					field.field_flags = flag_class + one_flag_pair.first;
				}
				else
				{
					field.field_flags += "|" + flag_class + one_flag_pair.first;
				}
			}
		}
		if (field.field_flags.empty())
		{
			field.field_flags = "0";
		}

		for (const auto& one_pair : one_field->annotations())
		{
			for (const auto& detail_pair : one_pair.second)
			{
				field.annotate_kvs.emplace_back("annotate_" + one_pair.first + "_" + detail_pair.first, detail_pair.second);
			}
		}

		model.fields.push_back(std::move(field));
	}

	for (const auto& one_pair : one_class->annotations())
	{
		for (const auto& detail_pair : one_pair.second)
		{
			model.annotate_kvs.emplace_back("annotate_" + one_pair.first + "_" + detail_pair.first, detail_pair.second);
		}
	}

	return model;
}

// ---------------------------------------------------------------------------
// ClassModel → mustache::data (stable key contract for templates)
// ---------------------------------------------------------------------------

void set_wire_kind_flags(mustache::data& field_data, const std::string& wire)
{
	static const char* kinds[] = {
		"bag", "slots", "vec", "map", "vector", "array", "string", "number", "bool", "object", "other"};
	for (const char* k : kinds)
	{
		field_data.set(std::string("is_wire_") + k, wire == k);
	}
}

mustache::data field_to_mustache(const FieldModel& field, bool first_field)
{
	mustache::data d;
	d.set("field_name", field.name);
	d.set("first_field", first_field);
	d.set("field_index", std::to_string(field.index));
	d.set("proto_field_number", std::to_string(field.index + 1));
	d.set("has_property_interface", field.classify.has_property_interface);
	d.set("field_cpp_type", field.cpp_type);
	d.set("wire_kind", field.classify.wire_kind);
	d.set("item_class", field.classify.item_class);
	d.set("proto_type", field.classify.proto_type);
	set_wire_kind_flags(d, field.classify.wire_kind);

	d.set("has_item_meta", !field.item_sync_module.empty());
	d.set("item_meta_module", field.item_sync_module);
	d.set("item_meta_local", field.item_sync_local);
	d.set("field_flags", field.field_flags);

	mustache::data flag_name_list{mustache::data::type::list};
	bool first_flag = true;
	for (const auto& name : field.flag_names)
	{
		mustache::data one;
		one.set("flag_name", name);
		one.set("first", first_flag);
		first_flag = false;
		flag_name_list << one;
	}
	d.set("flag_names", flag_name_list);

	for (const auto& kv : field.annotate_kvs)
	{
		d.set(kv.first, kv.second);
	}
	return d;
}

mustache::data class_model_to_mustache(const ClassModel& model)
{
	mustache::data render_args;
	render_args.set("is_property_item", static_cast<int>(model.item_type));
	render_args.set("property_idx_begin", std::to_string(model.property_idx_begin));
	render_args.set("property_idx_max", std::to_string(model.property_idx_max));
	render_args.set("has_base_class", model.has_base_class);
	render_args.set("is_property_item_direct_subclass", model.is_property_item_direct_subclass);
	render_args.set("base_class_name", model.base_class_name);
	render_args.set("class_name", model.class_name);
	render_args.set("class_full_name", model.class_full_name);
	render_args.set("class_namespace", model.class_namespace);
	render_args.set("schema_version", model.schema_version);
	render_args.set("is_bag_item", model.item_type == property_item_type::bag);
	render_args.set("is_slot_item", model.item_type == property_item_type::slot);
	render_args.set("is_vec_item", model.item_type == property_item_type::vec);

	mustache::data field_list{mustache::data::type::list};
	bool first = true;
	for (const auto& field : model.fields)
	{
		field_list << field_to_mustache(field, first);
		first = false;
	}
	render_args.set("property_fields", field_list);

	mustache::data proto_imports{mustache::data::type::list};
	for (const auto& file : model.proto_imports)
	{
		mustache::data one;
		one.set("import_file", file);
		proto_imports << one;
	}
	render_args.set("proto_imports", proto_imports);

	for (const auto& kv : model.annotate_kvs)
	{
		render_args.set(kv.first, kv.second);
	}
	return render_args;
}

// ---------------------------------------------------------------------------
// Emit all artifacts
// ---------------------------------------------------------------------------

std::unordered_map<std::string, std::string> generate_property(
	const std::string& generated_folder,
	const std::string& mustache_folder,
	const std::string& flag_class,
	const std::string& base_namespace)
{
	auto& the_logger = utils::get_logger();

	auto all_property_classes = language::type_db::instance().get_class_with_pred([](const language::class_node& cur_node) {
		return language::filter_with_annotation<language::class_node>("property", cur_node);
	});
	for (const auto& one_class : all_property_classes)
	{
		the_logger.info("class {} has annotation property with info {}", one_class->name(), json(one_class->annotations()).dump(4));
	}

	std::unordered_map<std::string, std::string> result;

	auto property_proxy_mustache = load_mustache(mustache_folder, "property_proxy_h.mustache");
	auto property_h_mustache = load_mustache(mustache_folder, "property_h.mustache");
	auto property_cpp_mustache = load_mustache(mustache_folder, "property_cpp.mustache");
	auto property_schema_mustache = load_mustache(mustache_folder, "property_schema.mustache");
	auto property_proto_mustache = load_mustache(mustache_folder, "property_proto.mustache");
	auto property_lua_mustache = load_mustache(mustache_folder, "property_lua_meta.mustache");
	auto property_mutate_proto_mustache = load_mustache(mustache_folder, "property_mutate_proto.mustache");
	auto property_cmd_lua_mustache = load_mustache(mustache_folder, "property_cmd_lua.mustache");

	mustache::data empty_args;
	const auto gen_root = std::filesystem::path(generated_folder);
	std::filesystem::create_directories(gen_root / "schema");
	std::filesystem::create_directories(gen_root / "proto");
	std::filesystem::create_directories(gen_root / "lua");

	generator::append_output_to_stream(
		result, (gen_root / "proto" / "property_mutate.proto").string(), property_mutate_proto_mustache.render(empty_args));
	generator::append_output_to_stream(
		result, (gen_root / "lua" / "property_cmd.lua").string(), property_cmd_lua_mustache.render(empty_args));

	{
		const auto runtime_dir = std::filesystem::path(mustache_folder).parent_path() / "lua_runtime";
		for (const char* name : {"property_runtime.lua", "property_record.lua", "json.lua"})
		{
			const auto runtime_src = runtime_dir / name;
			const auto content = read_file_text(runtime_src);
			if (!content.empty())
			{
				generator::append_output_to_stream(result, (gen_root / "lua" / name).string(), content);
			}
			else
			{
				the_logger.error("missing lua runtime file at {}", runtime_src.string());
			}
		}
	}

	for (auto* one_class : all_property_classes)
	{
		const auto generated_folder_path = spiritsaway::meta::utils::create_dir_for_sub_namespace(
			base_namespace, one_class->get_resident_ns()->qualified_name, generated_folder);

		const ClassModel model = parse_class_model(one_class, flag_class);
		const mustache::data render_args = class_model_to_mustache(model);

		const auto stem = one_class->unqualified_name();
		generator::append_output_to_stream(
			result, (generated_folder_path / (stem + ".proxy.inch")).string(), property_proxy_mustache.render(render_args));
		generator::append_output_to_stream(
			result, (generated_folder_path / (stem + ".generated.inch")).string(), property_h_mustache.render(render_args));
		generator::append_output_to_stream(
			result, (generated_folder_path / (stem + ".generated.incpp")).string(), property_cpp_mustache.render(render_args));
		generator::append_output_to_stream(
			result, (gen_root / "schema" / (stem + ".schema.json")).string(), property_schema_mustache.render(render_args));
		generator::append_output_to_stream(
			result, (gen_root / "proto" / (stem + ".proto")).string(), property_proto_mustache.render(render_args));
		generator::append_output_to_stream(
			result, (gen_root / "lua" / (stem + "_meta.lua")).string(), property_lua_mustache.render(render_args));
	}
	return result;
}

struct GeneratorConfig
{
	std::vector<std::string> include_dirs;
	std::vector<std::string> compile_definitions;
	std::string src_file;
	std::string property_namespace;
	std::string mustache_folder;
	std::string generated_folder;
	std::string flag_class;
};

bool load_generator_config(const std::filesystem::path& json_path, GeneratorConfig& cfg, std::string& err)
{
	const auto cur_json_content = load_json_file(json_path.string());
	const auto file_folder = json_path.parent_path();
	try
	{
		cur_json_content.at("include_dirs").get_to(cfg.include_dirs);
		cur_json_content.at("definitions").get_to(cfg.compile_definitions);
		cur_json_content.at("src_file").get_to(cfg.src_file);
		cur_json_content.at("namespace").get_to(cfg.property_namespace);
		cur_json_content.at("mustache_folder").get_to(cfg.mustache_folder);
		cur_json_content.at("generated_folder").get_to(cfg.generated_folder);
		cur_json_content.at("flag_class").get_to(cfg.flag_class);
	}
	catch (const std::exception& e)
	{
		err = std::string("fail to parse json content with exception ") + e.what();
		return false;
	}

	if (cfg.src_file.empty())
	{
		err = "empty src file";
		return false;
	}
	if (cfg.flag_class.empty())
	{
		err = "flag class is empty";
		return false;
	}
	cfg.flag_class += "::";
	cfg.src_file = (file_folder / cfg.src_file).string();

	auto require_dot_relative = [&](std::string& folder, const char* label) -> bool {
		if (folder.empty())
		{
			err = std::string(label) + " is empty";
			return false;
		}
		if (folder[0] != '.')
		{
			err = std::string(label) + " " + folder + " should begin with .";
			return false;
		}
		folder = (file_folder / folder).string();
		return true;
	};
	if (!require_dot_relative(cfg.mustache_folder, "mustache folder"))
	{
		return false;
	}
	if (!require_dot_relative(cfg.generated_folder, "generated_folder"))
	{
		return false;
	}

	for (auto& one_include_dir : cfg.include_dirs)
	{
		if (one_include_dir.empty())
		{
			continue;
		}
		if (one_include_dir[0] == '.')
		{
			one_include_dir = (file_folder / one_include_dir).string();
		}
	}
	return true;
}

int run_generator(const GeneratorConfig& cfg)
{
	auto& the_logger = utils::get_logger();
	std::filesystem::create_directories(cfg.generated_folder);

	std::vector<std::string> arguments;
	for (const auto& one_include_dir : cfg.include_dirs)
	{
		arguments.push_back("-I" + one_include_dir);
	}
	for (const auto& one_definition : cfg.compile_definitions)
	{
		arguments.push_back(one_definition);
	}
	arguments.push_back("-x");
	arguments.push_back("c++");
	arguments.push_back("-std=c++17");
	arguments.push_back("-D__meta_parse__");

	std::vector<const char*> cstr_arguments;
	cstr_arguments.reserve(arguments.size());
	for (const auto& i : arguments)
	{
		cstr_arguments.push_back(i.c_str());
	}

	CXIndex m_index = clang_createIndex(true, true);
	CXTranslationUnit m_translationUnit = clang_createTranslationUnitFromSourceFile(
		m_index, cfg.src_file.c_str(), static_cast<int>(cstr_arguments.size()), cstr_arguments.data(), 0, nullptr);
	auto cursor = clang_getTranslationUnitCursor(m_translationUnit);
	the_logger.info("the root cursor is {}", utils::to_string(cursor));

	auto& cur_type_db = language::type_db::instance();
	cur_type_db.create_from_translate_unit(cursor);
	cur_type_db.build_class_under_namespace("std");
	cur_type_db.build_class_under_namespace("spiritsaway::property");
	cur_type_db.build_class_under_namespace(cfg.property_namespace);

	auto cur_namespace_classes = language::type_db::instance().get_class_with_pred([&](const language::class_node& temp_class) {
		return temp_class.qualified_name().rfind(cfg.property_namespace, 0) == 0;
	});
	json type_dump;
	for (const auto& one_class : cur_namespace_classes)
	{
		type_dump.push_back(json(*one_class));
	}
	{
		std::ofstream json_out("type_info.json");
		json_out << std::setw(4) << type_dump << std::endl;
	}

	std::unordered_map<std::string, std::string> file_content;
	generator::merge_file_content(
		file_content, generate_property(cfg.generated_folder, cfg.mustache_folder, cfg.flag_class, cfg.property_namespace));
	generator::write_content_to_file(file_content);
	clang_disposeTranslationUnit(m_translationUnit);
	return 0;
}

} // namespace

int main(int argc, const char** argv)
{
	if (argc != 2)
	{
		std::cout << "please specify the json file path" << std::endl;
		return 1;
	}
	const std::string json_file_path = argv[1];
	if (json_file_path.empty())
	{
		std::cout << "empty json file path" << std::endl;
		return 1;
	}

	GeneratorConfig cfg;
	std::string err;
	if (!load_generator_config(json_file_path, cfg, err))
	{
		std::cout << err << std::endl;
		return 1;
	}
	return run_generator(cfg);
}
