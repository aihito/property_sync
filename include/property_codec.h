#pragma once

/**
 * Snapshot codec: select JSON and/or field-level Protobuf (generated to_pb/from_pb).
 *
 * Incremental sync stays JSON mutate queues.
 * Protobuf path calls PropertyT::to_pb / from_pb (no JSON↔PB bridge).
 */

#include <cstdint>
#include <string>

#include <nlohmann/json.hpp>

#include "property_basic.h"

#ifndef PROPERTY_SYNC_WITH_PROTOBUF
#define PROPERTY_SYNC_WITH_PROTOBUF 0
#endif

namespace spiritsaway::property
{

enum class codec_kind
{
    json = 0,
    protobuf = 1,
    both = 2,
};

struct codec_blob
{
    nlohmann::json view = nlohmann::json::object();
    std::string json_text;
    std::string pb_bytes;
};

inline const char* codec_kind_name(codec_kind k)
{
    switch (k) {
        case codec_kind::json:
            return "json";
        case codec_kind::protobuf:
            return "protobuf";
        case codec_kind::both:
            return "both";
    }
    return "unknown";
}

template <typename PropertyT>
nlohmann::json encode_snapshot_view(const PropertyT& obj, property_flags flag, bool ignore_default = true, std::uint32_t schema_version = 1)
{
    nlohmann::json view = obj.encode_with_flag(flag, ignore_default, false);
    view["schema_version"] = schema_version;
    return view;
}

template <typename PropertyT>
bool apply_snapshot_view(PropertyT& obj, const nlohmann::json& view, std::string* err = nullptr)
{
    if (!obj.decode(view)) {
        if (err) {
            *err = "PropertyT::decode failed";
        }
        return false;
    }
    return true;
}

/// JSON-only encode (always available).
template <typename PropertyT>
bool encode_snapshot(const PropertyT& obj, property_flags flag, codec_kind kind, codec_blob& out, bool ignore_default = true, std::uint32_t schema_version = 1, std::string* err = nullptr)
{
    if (kind != codec_kind::json) {
        if (err) {
            *err =
                "protobuf/both require encode_snapshot<PropertyT, SnapshotMsg> "
                "(and PROPERTY_SYNC_WITH_PROTOBUF=1)";
        }
        return false;
    }
    out = codec_blob{};
    out.view = encode_snapshot_view(obj, flag, ignore_default, schema_version);
    out.json_text = out.view.dump();
    return true;
}

template <typename PropertyT>
bool decode_snapshot(const codec_blob& in, codec_kind kind, PropertyT& obj, std::string* err = nullptr)
{
    if (kind != codec_kind::json) {
        if (err) {
            *err =
                "protobuf/both require decode_snapshot<PropertyT, SnapshotMsg> "
                "(and PROPERTY_SYNC_WITH_PROTOBUF=1)";
        }
        return false;
    }
    nlohmann::json view;
    if (!in.json_text.empty()) {
        view = nlohmann::json::parse(in.json_text, nullptr, false);
        if (view.is_discarded()) {
            if (err) {
                *err = "invalid json_text";
            }
            return false;
        }
    }
    else if (!in.view.is_null()) {
        view = in.view;
    }
    else {
        if (err) {
            *err = "json codec: empty input";
        }
        return false;
    }
    return apply_snapshot_view(obj, view, err);
}

#if PROPERTY_SYNC_WITH_PROTOBUF

/// Field-level PB via PropertyT::to_pb / from_pb (generated into Class.cpp).
template <typename PropertyT, typename SnapshotMsg>
bool encode_snapshot(const PropertyT& obj, property_flags flag, codec_kind kind, codec_blob& out, bool ignore_default = true, std::uint32_t schema_version = 1, std::string* err = nullptr)
{
    out = codec_blob{};
    out.view = encode_snapshot_view(obj, flag, ignore_default, schema_version);
    out.json_text = out.view.dump();

    if (kind == codec_kind::json) {
        return true;
    }

    SnapshotMsg msg;
    obj.to_pb(flag, ignore_default, schema_version, msg);
    if (!msg.SerializeToString(&out.pb_bytes)) {
        if (err) {
            *err = "SerializeToString failed";
        }
        return false;
    }
    return true;
}

template <typename PropertyT, typename SnapshotMsg>
bool decode_snapshot(const codec_blob& in, codec_kind kind, PropertyT& obj, std::string* err = nullptr)
{
    if (kind == codec_kind::json) {
        return decode_snapshot<PropertyT>(in, codec_kind::json, obj, err);
    }
    if (in.pb_bytes.empty()) {
        if (err) {
            *err = "protobuf codec: empty pb_bytes";
        }
        return false;
    }
    SnapshotMsg msg;
    if (!msg.ParseFromString(in.pb_bytes)) {
        if (err) {
            *err = "ParseFromString failed";
        }
        return false;
    }
    if (!obj.from_pb(msg)) {
        if (err) {
            *err = "from_pb failed";
        }
        return false;
    }
    return true;
}

template <typename PropertyT, typename SnapshotMsg>
bool encode_snapshot_both_roundtrip_ok(const PropertyT& obj, property_flags flag, codec_blob& out, bool ignore_default = true, std::uint32_t schema_version = 1, std::string* err = nullptr)
{
    if (!encode_snapshot<PropertyT, SnapshotMsg>(obj, flag, codec_kind::both, out, ignore_default, schema_version, err)) {
        return false;
    }
    PropertyT mirror{};
    if (!decode_snapshot<PropertyT, SnapshotMsg>(out, codec_kind::protobuf, mirror, err)) {
        return false;
    }
    nlohmann::json back = encode_snapshot_view(mirror, flag, ignore_default, schema_version);
    out.view["schema_version"] = schema_version;
    back["schema_version"] = schema_version;
    if (out.view != back) {
        if (err) {
            *err = "both roundtrip view mismatch\nwant=" + out.view.dump() + "\ngot=" + back.dump();
        }
        return false;
    }
    return true;
}

#endif // PROPERTY_SYNC_WITH_PROTOBUF

} // namespace spiritsaway::property
