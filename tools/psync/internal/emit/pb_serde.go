package emit

import (
	"fmt"
	"strings"

	"property_sync/psync/internal/ir"
	"property_sync/psync/internal/wire"
)

// renderPbMemberDecls: declarations injected into the class body.
func renderPbMemberDecls(cls *ir.ClassDef, pbCppNs string) string {
	snap := pbCppNs + "::" + cls.Name
	var b strings.Builder
	fmt.Fprintf(&b, "\n#if PROPERTY_SYNC_WITH_PROTOBUF\n")
	fmt.Fprintf(&b, "public:\n")
	fmt.Fprintf(&b, "\tvoid to_pb(spiritsaway::property::property_flags flag, bool ignore_default,\n")
	fmt.Fprintf(&b, "\t\tstd::uint32_t schema_version, %s& dst) const;\n", snap)
	fmt.Fprintf(&b, "\tbool from_pb(const %s& src);\n", snap)
	if cls.Kind == "slot_item" {
		slots := pbCppNs + "::" + cls.Name + "Slots"
		fmt.Fprintf(&b, "\tstatic void to_pb_slots(const spiritsaway::property::property_slots<%s>& src,\n", cls.Name)
		fmt.Fprintf(&b, "\t\tspiritsaway::property::property_flags flag, bool ignore_default,\n")
		fmt.Fprintf(&b, "\t\tstd::uint32_t schema_version, %s& dst);\n", slots)
		fmt.Fprintf(&b, "\tstatic bool from_pb_slots(const %s& src, spiritsaway::property::property_slots<%s>& dst);\n", slots, cls.Name)
	}
	fmt.Fprintf(&b, "#endif // PROPERTY_SYNC_WITH_PROTOBUF\n")
	return b.String()
}

// renderPbMemberImpl: definitions appended to Class.cpp.
func renderPbMemberImpl(cls *ir.ClassDef, pbCppNs string) string {
	ns := wire.NsCpp(cls.Namespace)
	snap := pbCppNs + "::" + cls.Name
	var c strings.Builder

	fmt.Fprintf(&c, "\n#if PROPERTY_SYNC_WITH_PROTOBUF\n")
	fmt.Fprintf(&c, "#include \"%s.pb.h\"\n", ProtoFileStem(cls.Name))
	seenInc := map[string]bool{}
	for _, f := range cls.Fields {
		if (f.Type.Kind == "bag" || f.Type.Kind == "slots" || f.Type.Kind == "vec" || f.Type.Kind == "object") && f.Type.Name != nil {
			inc := ProtoFileStem(*f.Type.Name) + ".pb.h"
			if !seenInc[inc] {
				seenInc[inc] = true
				fmt.Fprintf(&c, "#include \"%s\"\n", inc)
			}
		}
	}
	fmt.Fprintf(&c, "#include <algorithm>\n")
	fmt.Fprintf(&c, "#include <memory>\n\n")
	fmt.Fprintf(&c, "namespace %s\n{\n", ns)

	fmt.Fprintf(&c, "void %s::to_pb(spiritsaway::property::property_flags flag, bool ignore_default,\n", cls.Name)
	fmt.Fprintf(&c, "\tstd::uint32_t schema_version, %s& dst) const\n{\n", snap)
	fmt.Fprintf(&c, "\tdst.Clear();\n")
	fmt.Fprintf(&c, "\tdst.set_schema_version(schema_version);\n")
	if cls.Kind == "bag_item" || cls.Kind == "slot_item" {
		fmt.Fprintf(&c, "\tdst.set_id(m_id);\n")
	}
	if cls.Kind == "slot_item" {
		fmt.Fprintf(&c, "\tdst.set_slot(m_slot);\n")
	}
	for _, f := range cls.Fields {
		writeMemberToPbField(&c, cls, f)
	}
	fmt.Fprintf(&c, "}\n\n")

	fmt.Fprintf(&c, "bool %s::from_pb(const %s& src)\n{\n", cls.Name, snap)
	fmt.Fprintf(&c, "\t*this = %s{};\n", cls.Name)
	if cls.Kind == "bag_item" || cls.Kind == "slot_item" {
		fmt.Fprintf(&c, "\tm_id = static_cast<decltype(m_id)>(src.id());\n")
	}
	if cls.Kind == "slot_item" {
		fmt.Fprintf(&c, "\tm_slot = src.slot();\n")
	}
	for _, f := range cls.Fields {
		writeMemberFromPbField(&c, f)
	}
	fmt.Fprintf(&c, "\treturn true;\n}\n")

	if cls.Kind == "slot_item" {
		slots := pbCppNs + "::" + cls.Name + "Slots"
		fmt.Fprintf(&c, "\nvoid %s::to_pb_slots(const spiritsaway::property::property_slots<%s>& src,\n", cls.Name, cls.Name)
		fmt.Fprintf(&c, "\tspiritsaway::property::property_flags flag, bool ignore_default,\n")
		fmt.Fprintf(&c, "\tstd::uint32_t schema_version, %s& dst)\n{\n", slots)
		fmt.Fprintf(&c, "\tdst.Clear();\n")
		fmt.Fprintf(&c, "\tdst.set_sz(src.capacity());\n")
		fmt.Fprintf(&c, "\tfor (std::uint32_t i = 0; i < src.capacity(); ++i) {\n")
		fmt.Fprintf(&c, "\t\tconst auto* item = src.get_slot(i);\n")
		fmt.Fprintf(&c, "\t\tif (!item) continue;\n")
		fmt.Fprintf(&c, "\t\titem->to_pb(flag, ignore_default, schema_version, *dst.add_data());\n")
		fmt.Fprintf(&c, "\t}\n}\n\n")
		fmt.Fprintf(&c, "bool %s::from_pb_slots(const %s& src, spiritsaway::property::property_slots<%s>& dst)\n{\n", cls.Name, slots, cls.Name)
		fmt.Fprintf(&c, "\tdst.clear();\n")
		fmt.Fprintf(&c, "\tdst.resize_slots(src.sz());\n")
		fmt.Fprintf(&c, "\tfor (const auto& one : src.data()) {\n")
		fmt.Fprintf(&c, "\t\t%s item;\n", cls.Name)
		fmt.Fprintf(&c, "\t\tif (!item.from_pb(one)) return false;\n")
		fmt.Fprintf(&c, "\t\tif (!dst.insert_item(std::make_unique<%s>(std::move(item)))) return false;\n", cls.Name)
		fmt.Fprintf(&c, "\t}\n")
		fmt.Fprintf(&c, "\treturn true;\n}\n")
	}

	fmt.Fprintf(&c, "} // namespace %s\n", ns)
	fmt.Fprintf(&c, "#endif // PROPERTY_SYNC_WITH_PROTOBUF\n")
	return c.String()
}

func writeMemberToPbField(c *strings.Builder, cls *ir.ClassDef, f ir.FieldDef) {
	name := f.Name
	flagConst := "flag_for_" + name
	mem := "m_" + name
	fmt.Fprintf(c, "\tif ((%s & flag.value) == flag.value) {\n", flagConst)

	switch f.Type.Kind {
	case "scalar":
		sn := deref(f.Type.Name)
		fmt.Fprintf(c, "\t\tif (!ignore_default || !spiritsaway::property::has_default_value<decltype(%s)>()(%s)) {\n", mem, mem)
		switch sn {
		case "string", "bool", "float", "double":
			fmt.Fprintf(c, "\t\t\tdst.set_%s(%s);\n", name, mem)
		default:
			fmt.Fprintf(c, "\t\t\tdst.set_%s(static_cast<std::int64_t>(%s));\n", name, mem)
		}
		fmt.Fprintf(c, "\t\t}\n")
	case "array", "list":
		fmt.Fprintf(c, "\t\tif (!ignore_default || !spiritsaway::property::has_default_value<decltype(%s)>()(%s)) {\n", mem, mem)
		fmt.Fprintf(c, "\t\t\tdst.clear_%s();\n", name)
		fmt.Fprintf(c, "\t\t\tfor (const auto& v : %s) dst.add_%s(v);\n", mem, name)
		fmt.Fprintf(c, "\t\t}\n")
	case "dict":
		fmt.Fprintf(c, "\t\tif (!ignore_default || !spiritsaway::property::has_default_value<decltype(%s)>()(%s)) {\n", mem, mem)
		fmt.Fprintf(c, "\t\t\tauto* m = dst.mutable_%s();\n", name)
		fmt.Fprintf(c, "\t\t\tm->clear();\n")
		fmt.Fprintf(c, "\t\t\tfor (const auto& kv : %s) (*m)[kv.first] = static_cast<std::int64_t>(kv.second);\n", mem)
		fmt.Fprintf(c, "\t\t}\n")
	case "bag":
		fmt.Fprintf(c, "\t\tdst.clear_%s();\n", name)
		fmt.Fprintf(c, "\t\tfor (const auto& ptr : %s.data()) {\n", mem)
		fmt.Fprintf(c, "\t\t\tif (!ptr) continue;\n")
		fmt.Fprintf(c, "\t\t\tptr->to_pb(flag, ignore_default, schema_version, *dst.add_%s());\n", name)
		fmt.Fprintf(c, "\t\t}\n")
	case "vec":
		fmt.Fprintf(c, "\t\tdst.clear_%s();\n", name)
		fmt.Fprintf(c, "\t\tfor (std::uint32_t i = 0; i < %s.size(); ++i) {\n", mem)
		fmt.Fprintf(c, "\t\t\tconst auto* ptr = %s.get(i);\n", mem)
		fmt.Fprintf(c, "\t\t\tif (!ptr) continue;\n")
		fmt.Fprintf(c, "\t\t\tptr->to_pb(flag, ignore_default, schema_version, *dst.add_%s());\n", name)
		fmt.Fprintf(c, "\t\t}\n")
	case "slots":
		item := deref(f.Type.Name)
		fmt.Fprintf(c, "\t\t%s::to_pb_slots(%s, flag, ignore_default, schema_version, *dst.mutable_%s());\n", item, mem, name)
	case "object":
		fmt.Fprintf(c, "\t\t%s.to_pb(flag, ignore_default, schema_version, *dst.mutable_%s());\n", mem, name)
	default:
		fmt.Fprintf(c, "\t\t// unsupported kind %q for field %s\n", f.Type.Kind, name)
	}
	fmt.Fprintf(c, "\t}\n")
	_ = cls
}

func writeMemberFromPbField(c *strings.Builder, f ir.FieldDef) {
	name := f.Name
	mem := "m_" + name

	switch f.Type.Kind {
	case "scalar":
		sn := deref(f.Type.Name)
		switch sn {
		case "string", "bool", "float", "double":
			fmt.Fprintf(c, "\t%s = src.%s();\n", mem, name)
		default:
			fmt.Fprintf(c, "\t%s = static_cast<decltype(%s)>(src.%s());\n", mem, mem, name)
		}
	case "array":
		fmt.Fprintf(c, "\t{\n")
		fmt.Fprintf(c, "\t\tconst int n = std::min(src.%s_size(), static_cast<int>(%s.size()));\n", name, mem)
		fmt.Fprintf(c, "\t\tfor (int i = 0; i < n; ++i) %s[static_cast<std::size_t>(i)] = src.%s(i);\n", mem, name)
		fmt.Fprintf(c, "\t}\n")
	case "list":
		fmt.Fprintf(c, "\t%s.clear();\n", mem)
		fmt.Fprintf(c, "\t%s.reserve(static_cast<std::size_t>(src.%s_size()));\n", mem, name)
		fmt.Fprintf(c, "\tfor (int i = 0; i < src.%s_size(); ++i) %s.push_back(src.%s(i));\n", name, mem, name)
	case "dict":
		fmt.Fprintf(c, "\t%s.clear();\n", mem)
		fmt.Fprintf(c, "\tfor (const auto& kv : src.%s()) {\n", name)
		fmt.Fprintf(c, "\t\t%s[kv.first] = static_cast<decltype(%s)::mapped_type>(kv.second);\n", mem, mem)
		fmt.Fprintf(c, "\t}\n")
	case "bag":
		item := deref(f.Type.Name)
		fmt.Fprintf(c, "\t%s.clear();\n", mem)
		fmt.Fprintf(c, "\tfor (const auto& one : src.%s()) {\n", name)
		fmt.Fprintf(c, "\t\t%s item;\n", item)
		fmt.Fprintf(c, "\t\tif (!item.from_pb(one)) return false;\n")
		fmt.Fprintf(c, "\t\t%s.insert_item(std::move(item));\n", mem)
		fmt.Fprintf(c, "\t}\n")
	case "vec":
		item := deref(f.Type.Name)
		fmt.Fprintf(c, "\t%s.clear();\n", mem)
		fmt.Fprintf(c, "\tfor (const auto& one : src.%s()) {\n", name)
		fmt.Fprintf(c, "\t\tauto item = std::make_unique<%s>();\n", item)
		fmt.Fprintf(c, "\t\tif (!item->from_pb(one)) return false;\n")
		fmt.Fprintf(c, "\t\t%s.emplace_back(std::move(item));\n", mem)
		fmt.Fprintf(c, "\t}\n")
	case "slots":
		item := deref(f.Type.Name)
		fmt.Fprintf(c, "\tif (!%s::from_pb_slots(src.%s(), %s)) return false;\n", item, name, mem)
	case "object":
		fmt.Fprintf(c, "\tif (!%s.from_pb(src.%s())) return false;\n", mem, name)
	default:
		fmt.Fprintf(c, "\t// unsupported kind %q for field %s\n", f.Type.Kind, name)
	}
}
