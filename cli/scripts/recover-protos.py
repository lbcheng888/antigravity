#!/usr/bin/env python3
"""L3-A Protobuf recovery for /Users/mac/.local/bin/agy (Python 3.9+ stdlib only).

Sources
  (a) gzip-compressed FileDescriptorProtos found anywhere in the binary,
  (b) uncompressed `rawDesc` Go string literals (no NUL terminator, glued to
      neighbouring string-pool entries) found by a strict recursive
      FileDescriptorProto wire-format validator,
  (c) cross-check: `*_proto_init` / `*_rawDescGZIP(.funcN)` machine code is
      scanned with a tiny ARM64 decoder (adrp/add/movz/movk/orr) to recover the
      exact (pointer, length) pair of every rawDesc and compare it with (b).

Outputs
  cli/reconstructed/proto/<proto_path>   readable .proto text
  cli/metadata/recovered-protos.json     comprehensive index + metrics
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import struct
import sys
import zlib
from collections import OrderedDict, defaultdict
from pathlib import Path

DEFAULT_BINARY = Path("/Users/mac/.local/bin/agy")
CLI_ROOT = Path(__file__).resolve().parent.parent
MAX_DEPTH = 48


class PErr(Exception):
    pass


# ----------------------------------------------------------------------------
# descriptor.proto schema (enough to strictly validate + render)
# ----------------------------------------------------------------------------
ENUMS = {
    "Label": {1: "LABEL_OPTIONAL", 2: "LABEL_REQUIRED", 3: "LABEL_REPEATED"},
    "Type": {1: "TYPE_DOUBLE", 2: "TYPE_FLOAT", 3: "TYPE_INT64", 4: "TYPE_UINT64", 5: "TYPE_INT32",
             6: "TYPE_FIXED64", 7: "TYPE_FIXED32", 8: "TYPE_BOOL", 9: "TYPE_STRING", 10: "TYPE_GROUP",
             11: "TYPE_MESSAGE", 12: "TYPE_BYTES", 13: "TYPE_UINT32", 14: "TYPE_ENUM", 15: "TYPE_SFIXED32",
             16: "TYPE_SFIXED64", 17: "TYPE_SINT32", 18: "TYPE_SINT64"},
    "OptimizeMode": {1: "SPEED", 2: "CODE_SIZE", 3: "LITE_RUNTIME"},
    "CType": {0: "STRING", 1: "CORD", 2: "STRING_PIECE"},
    "JSType": {0: "JS_NORMAL", 1: "JS_STRING", 2: "JS_NUMBER"},
    "Retention": {0: "RETENTION_UNKNOWN", 1: "RETENTION_RUNTIME", 2: "RETENTION_SOURCE"},
    "Target": {0: "TARGET_TYPE_UNKNOWN", 1: "TARGET_TYPE_FILE", 2: "TARGET_TYPE_EXTENSION_RANGE",
               3: "TARGET_TYPE_MESSAGE", 4: "TARGET_TYPE_FIELD", 5: "TARGET_TYPE_ONEOF", 6: "TARGET_TYPE_ENUM",
               7: "TARGET_TYPE_ENUM_ENTRY", 8: "TARGET_TYPE_SERVICE", 9: "TARGET_TYPE_METHOD"},
    "Idempotency": {0: "IDEMPOTENCY_UNKNOWN", 1: "NO_SIDE_EFFECTS", 2: "IDEMPOTENT"},
    "Edition": {0: "EDITION_UNKNOWN", 1: "EDITION_1_TEST_ONLY", 2: "EDITION_2_TEST_ONLY", 900: "EDITION_LEGACY",
                998: "EDITION_PROTO2", 999: "EDITION_PROTO3", 1000: "EDITION_2023", 1001: "EDITION_2024",
                9999: "EDITION_UNSTABLE", 99997: "EDITION_99997_TEST_ONLY", 99998: "EDITION_99998_TEST_ONLY",
                99999: "EDITION_99999_TEST_ONLY", 2147483647: "EDITION_MAX"},
    "FieldPresence": {0: "FIELD_PRESENCE_UNKNOWN", 1: "EXPLICIT", 2: "IMPLICIT", 3: "LEGACY_REQUIRED"},
    "EnumType": {0: "ENUM_TYPE_UNKNOWN", 1: "OPEN", 2: "CLOSED"},
    "RepEnc": {0: "REPEATED_FIELD_ENCODING_UNKNOWN", 1: "PACKED", 2: "EXPANDED"},
    "Utf8": {0: "UTF8_VALIDATION_UNKNOWN", 2: "VERIFY", 3: "NONE"},
    "MsgEnc": {0: "MESSAGE_ENCODING_UNKNOWN", 1: "LENGTH_PREFIXED", 2: "DELIMITED"},
    "JsonFmt": {0: "JSON_FORMAT_UNKNOWN", 1: "ALLOW", 2: "LEGACY_BEST_EFFORT"},
    "Naming": {0: "ENFORCE_NAMING_STYLE_UNKNOWN", 1: "STYLE2024", 2: "STYLE_LEGACY"},
    "DefVis": {0: "DEFAULT_SYMBOL_VISIBILITY_UNKNOWN", 1: "EXPORT_ALL", 2: "EXPORT_TOP_LEVEL", 3: "LOCAL_ALL",
               4: "STRICT"},
    "Visibility": {0: "VISIBILITY_UNSET", 1: "LOCAL", 2: "EXPORT"},
    "Verification": {0: "DECLARATION", 1: "UNVERIFIED"},
}
STRICT_ENUMS = {"Label", "Type"}

# type codes: s free string, id identifier, tn type name, path, pkg, b bool, i32 i64 u32 u64 enum bytes dbl flt msg
SCHEMA = {}


def _S(name, *fields):
    d = {}
    for f in fields:
        num, fname, t = f[0], f[1], f[2]
        rep = len(f) > 3 and f[3]
        ref = f[4] if len(f) > 4 else None
        d[num] = (fname, t, bool(rep), ref)
    SCHEMA[name] = d


_S("FileDescriptorProto",
   (1, "name", "path"), (2, "package", "pkg"), (3, "dependency", "path", True),
   (4, "message_type", "msg", True, "DescriptorProto"), (5, "enum_type", "msg", True, "EnumDescriptorProto"),
   (6, "service", "msg", True, "ServiceDescriptorProto"), (7, "extension", "msg", True, "FieldDescriptorProto"),
   (8, "options", "msg", False, "FileOptions"), (9, "source_code_info", "raw"),
   (10, "public_dependency", "i32", True), (11, "weak_dependency", "i32", True),
   (12, "syntax", "s"), (14, "edition", "enum", False, "Edition"), (15, "option_dependency", "path", True))
_S("DescriptorProto",
   (1, "name", "id"), (2, "field", "msg", True, "FieldDescriptorProto"),
   (3, "nested_type", "msg", True, "DescriptorProto"), (4, "enum_type", "msg", True, "EnumDescriptorProto"),
   (5, "extension_range", "msg", True, "ExtensionRange"), (6, "extension", "msg", True, "FieldDescriptorProto"),
   (7, "options", "msg", False, "MessageOptions"), (8, "oneof_decl", "msg", True, "OneofDescriptorProto"),
   (9, "reserved_range", "msg", True, "ReservedRange"), (10, "reserved_name", "id", True),
   (11, "visibility", "enum", False, "Visibility"))
_S("ExtensionRange", (1, "start", "i32"), (2, "end", "i32"), (3, "options", "msg", False, "ExtensionRangeOptions"))
_S("ReservedRange", (1, "start", "i32"), (2, "end", "i32"))
_S("EnumReservedRange", (1, "start", "i32"), (2, "end", "i32"))
_S("FieldDescriptorProto",
   (1, "name", "id"), (2, "extendee", "tn"), (3, "number", "i32"), (4, "label", "enum", False, "Label"),
   (5, "type", "enum", False, "Type"), (6, "type_name", "tn"), (7, "default_value", "s"),
   (8, "options", "msg", False, "FieldOptions"), (9, "oneof_index", "i32"), (10, "json_name", "s"),
   (17, "proto3_optional", "b"))
_S("OneofDescriptorProto", (1, "name", "id"), (2, "options", "msg", False, "OneofOptions"))
_S("EnumDescriptorProto",
   (1, "name", "id"), (2, "value", "msg", True, "EnumValueDescriptorProto"),
   (3, "options", "msg", False, "EnumOptions"), (4, "reserved_range", "msg", True, "EnumReservedRange"),
   (5, "reserved_name", "id", True), (6, "visibility", "enum", False, "Visibility"))
_S("EnumValueDescriptorProto", (1, "name", "id"), (2, "number", "i32"),
   (3, "options", "msg", False, "EnumValueOptions"))
_S("ServiceDescriptorProto", (1, "name", "id"), (2, "method", "msg", True, "MethodDescriptorProto"),
   (3, "options", "msg", False, "ServiceOptions"))
_S("MethodDescriptorProto",
   (1, "name", "id"), (2, "input_type", "tn"), (3, "output_type", "tn"),
   (4, "options", "msg", False, "MethodOptions"), (5, "client_streaming", "b"), (6, "server_streaming", "b"))
_S("FileOptions",
   (1, "java_package", "s"), (8, "java_outer_classname", "s"), (9, "optimize_for", "enum", False, "OptimizeMode"),
   (10, "java_multiple_files", "b"), (11, "go_package", "s"), (16, "cc_generic_services", "b"),
   (17, "java_generic_services", "b"), (18, "py_generic_services", "b"),
   (20, "java_generate_equals_and_hash", "b"), (23, "deprecated", "b"), (27, "java_string_check_utf8", "b"),
   (31, "cc_enable_arenas", "b"), (36, "objc_class_prefix", "s"), (37, "csharp_namespace", "s"),
   (39, "swift_prefix", "s"), (40, "php_class_prefix", "s"), (41, "php_namespace", "s"),
   (42, "php_generic_services", "b"), (44, "php_metadata_namespace", "s"), (45, "ruby_package", "s"),
   (50, "features", "msg", False, "FeatureSet"), (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("MessageOptions",
   (1, "message_set_wire_format", "b"), (2, "no_standard_descriptor_accessor", "b"), (3, "deprecated", "b"),
   (7, "map_entry", "b"), (11, "deprecated_legacy_json_field_conflicts", "b"),
   (12, "features", "msg", False, "FeatureSet"), (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("FieldOptions",
   (1, "ctype", "enum", False, "CType"), (2, "packed", "b"), (3, "deprecated", "b"), (5, "lazy", "b"),
   (6, "jstype", "enum", False, "JSType"), (10, "weak", "b"), (15, "unverified_lazy", "b"),
   (16, "debug_redact", "b"), (17, "retention", "enum", False, "Retention"),
   (19, "targets", "enum", True, "Target"), (20, "edition_defaults", "msg", True, "EditionDefault"),
   (21, "features", "msg", False, "FeatureSet"), (22, "feature_support", "msg", False, "FeatureSupport"),
   (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("EditionDefault", (3, "edition", "enum", False, "Edition"), (2, "value", "s"))
_S("FeatureSupport", (1, "edition_introduced", "enum", False, "Edition"),
   (2, "edition_deprecated", "enum", False, "Edition"), (3, "deprecation_warning", "s"),
   (4, "edition_removed", "enum", False, "Edition"))
_S("OneofOptions", (1, "features", "msg", False, "FeatureSet"),
   (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("ExtensionRangeOptions",
   (2, "declaration", "msg", True, "ExtDeclaration"), (3, "verification", "enum", False, "Verification"),
   (50, "features", "msg", False, "FeatureSet"), (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("ExtDeclaration", (1, "number", "i32"), (2, "full_name", "s"), (3, "type", "s"), (5, "reserved", "b"),
   (6, "repeated", "b"))
_S("EnumOptions",
   (2, "allow_alias", "b"), (3, "deprecated", "b"), (6, "deprecated_legacy_json_field_conflicts", "b"),
   (7, "features", "msg", False, "FeatureSet"), (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("EnumValueOptions",
   (1, "deprecated", "b"), (2, "features", "msg", False, "FeatureSet"), (3, "debug_redact", "b"),
   (4, "feature_support", "msg", False, "FeatureSupport"),
   (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("ServiceOptions", (33, "deprecated", "b"), (34, "features", "msg", False, "FeatureSet"),
   (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("MethodOptions",
   (33, "deprecated", "b"), (34, "idempotency_level", "enum", False, "Idempotency"),
   (35, "features", "msg", False, "FeatureSet"), (999, "uninterpreted_option", "msg", True, "UninterpretedOption"))
_S("FeatureSet",
   (1, "field_presence", "enum", False, "FieldPresence"), (2, "enum_type", "enum", False, "EnumType"),
   (3, "repeated_field_encoding", "enum", False, "RepEnc"), (4, "utf8_validation", "enum", False, "Utf8"),
   (5, "message_encoding", "enum", False, "MsgEnc"), (6, "json_format", "enum", False, "JsonFmt"),
   (7, "enforce_naming_style", "enum", False, "Naming"),
   (8, "default_symbol_visibility", "enum", False, "DefVis"))
_S("UninterpretedOption",
   (2, "name", "msg", True, "NamePart"), (3, "identifier_value", "s"), (4, "positive_int_value", "u64"),
   (5, "negative_int_value", "i64"), (6, "double_value", "dbl"), (7, "string_value", "bytes"),
   (8, "aggregate_value", "s"))
_S("NamePart", (1, "name_part", "s"), (2, "is_extension", "b"))

LENIENT_EXTRA = {"FeatureSupport", "EditionDefault", "ExtDeclaration"}
EXT_OK = {"FileOptions", "MessageOptions", "FieldOptions", "OneofOptions", "ExtensionRangeOptions",
          "EnumOptions", "EnumValueOptions", "ServiceOptions", "MethodOptions", "FeatureSet"}
OPTION_FULLNAME = {k: "google.protobuf." + k for k in EXT_OK}

RE_ID = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
RE_PKG = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$")
RE_TN = re.compile(r"^\.?[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$")
RE_PATH = re.compile(r"^[A-Za-z0-9_./+\-@]+\.proto$")


# ----------------------------------------------------------------------------
# strict wire-format reader / validator
# ----------------------------------------------------------------------------
def read_varint(buf, pos, end):
    r = 0
    shift = 0
    while True:
        if pos >= end:
            raise PErr("varint overruns buffer")
        b = buf[pos]
        pos += 1
        r |= (b & 0x7F) << shift
        if b < 0x80:
            return r, pos
        shift += 7
        if shift > 63:
            raise PErr("varint too long")


def check_utf8(raw, allow_ws=True):
    try:
        s = raw.decode("utf-8")
    except UnicodeDecodeError:
        raise PErr("invalid utf-8")
    for ch in s:
        o = ord(ch)
        if o == 0 or (o < 0x20 and not (allow_ws and ch in "\n\t\r")) or o == 0x7F:
            raise PErr("control char in string")
    return s


def _signed(v, bits):
    if v >= 1 << 63:
        v -= 1 << 64
    if bits == 32 and not (-(1 << 31) <= v < (1 << 31)):
        raise PErr("int32 out of range")
    return v


def _decode_scalar(t, ref, v, wt):
    if t == "b":
        if v not in (0, 1):
            raise PErr("bad bool")
        return bool(v)
    if t == "i32":
        return _signed(v, 32)
    if t == "i64":
        return _signed(v, 64)
    if t == "u32":
        if v >= 1 << 32:
            raise PErr("u32 overflow")
        return v
    if t == "u64":
        return v
    if t == "enum":
        tab = ENUMS[ref]
        val = _signed(v, 32)
        if val not in tab and (ref in STRICT_ENUMS or not (0 <= val < (1 << 31))):
            raise PErr("bad enum value for " + ref)
        return val
    raise PErr("bad scalar type")


VARINT_TYPES = {"b", "i32", "i64", "u32", "u64", "enum"}


def parse_field(buf, pos, end, mt, out, depth):
    """Parse one field of message type `mt` at pos; returns (fieldno, newpos)."""
    tag, p = read_varint(buf, pos, end)
    fn = tag >> 3
    wt = tag & 7
    if fn == 0 or wt in (3, 4, 6, 7):
        raise PErr("bad tag")
    spec = SCHEMA[mt].get(fn)
    if spec is None:
        if (mt in EXT_OK or mt in LENIENT_EXTRA) and 1 <= fn <= 536870911:
            if wt == 0:
                v, p = read_varint(buf, p, end)
            elif wt == 1:
                if p + 8 > end:
                    raise PErr("fixed64 overrun")
                v = bytes(buf[p:p + 8])
                p += 8
            elif wt == 5:
                if p + 4 > end:
                    raise PErr("fixed32 overrun")
                v = bytes(buf[p:p + 4])
                p += 4
            else:
                ln, p = read_varint(buf, p, end)
                if p + ln > end:
                    raise PErr("ext length overrun")
                v = bytes(buf[p:p + ln])
                p += ln
            out.setdefault("_ext", []).append((fn, wt, v))
            return fn, p
        raise PErr("unknown field %d in %s" % (fn, mt))
    name, t, rep, ref = spec
    vals = []
    if t in VARINT_TYPES:
        if wt == 0:
            v, p = read_varint(buf, p, end)
            vals.append(_decode_scalar(t, ref, v, wt))
        elif wt == 2 and rep:
            ln, p = read_varint(buf, p, end)
            if p + ln > end:
                raise PErr("packed overrun")
            q = p + ln
            while p < q:
                v, p = read_varint(buf, p, q)
                vals.append(_decode_scalar(t, ref, v, 0))
        else:
            raise PErr("wire type mismatch")
    elif t == "dbl":
        if wt != 1 or p + 8 > end:
            raise PErr("bad double")
        vals.append(struct.unpack_from("<d", buf, p)[0])
        p += 8
    else:
        if wt != 2:
            raise PErr("wire type mismatch")
        ln, p = read_varint(buf, p, end)
        if p + ln > end:
            raise PErr("length overrun")
        raw = bytes(buf[p:p + ln])
        q = p + ln
        if t == "msg":
            if depth >= MAX_DEPTH:
                raise PErr("too deep")
            sub = {}
            sp = p
            while sp < q:
                _, sp = parse_field(buf, sp, q, ref, sub, depth + 1)
            vals.append(sub)
        elif t == "bytes" or t == "raw":
            vals.append(raw)
        else:
            s = check_utf8(raw, allow_ws=(t == "s"))
            if t == "id" and not RE_ID.match(s):
                raise PErr("bad identifier")
            if t == "pkg" and not RE_PKG.match(s):
                raise PErr("bad package")
            if t == "tn" and not RE_TN.match(s):
                raise PErr("bad type name")
            if t == "path" and not RE_PATH.match(s):
                raise PErr("bad path")
            vals.append(s)
        p = q
    if rep:
        out.setdefault(name, []).extend(vals)
    else:
        out[name] = vals[-1]
    return fn, p


SYNTAXES = {"proto2", "proto3", "editions"}


def scan_file(buf, start, limit=None):
    """Parse a FileDescriptorProto starting at `start`; stop at the last valid top-level field boundary.

    Returns dict(tree,end,boundary,reason,last_field) or None when not even a name parses.
    """
    if limit is None:
        limit = len(buf)
    pos = start
    tree = {}
    last = 0
    reason = "end of buffer"
    while pos < limit:
        try:
            tag, _ = read_varint(buf, pos, limit)
            fn = tag >> 3
            if last == 0 and fn != 1:
                raise PErr("first field is not name")
            if fn < last:
                raise PErr("top-level field order regression %d<%d" % (fn, last))
            if last == 14 and fn != 15:
                raise PErr("field after edition")
            if last == 15 and fn != 15:
                raise PErr("field after option_dependency")
            if fn == 14 and tree.get("syntax") != "editions":
                raise PErr("edition without syntax=editions")
            if fn == 15 and tree.get("syntax") != "editions":
                raise PErr("option_dependency without editions")
            if last == 12 and fn not in (14, 15):
                raise PErr("only edition may follow syntax")
            if fn in (10, 11) and (tag & 7) != 0:
                raise PErr("packed public/weak dependency")
            trial = {}
            _, npos = parse_field(buf, pos, limit, "FileDescriptorProto", trial, 0)
            if fn in (10, 11) and not (0 <= trial[SCHEMA["FileDescriptorProto"][fn][0]][0] < len(tree.get("dependency", []))):
                raise PErr("dependency index out of range")
            if fn == 14 and trial["edition"] not in ENUMS["Edition"]:
                raise PErr("unknown edition")
            if fn == 12 and trial["syntax"] not in SYNTAXES:
                raise PErr("bad syntax string")
            if fn == 1 and not trial["name"].endswith(".proto"):
                raise PErr("name not .proto")
        except PErr as e:
            reason = str(e)
            break
        for k, v in trial.items():
            if isinstance(v, list):
                tree.setdefault(k, []).extend(v)
            else:
                tree[k] = v
        pos = npos
        last = fn
    if last == 0:
        return None
    if last == 14:
        boundary = "edition_end"
    elif last == 12:
        boundary = "syntax_end"
    elif last == 15:
        boundary = "option_dependency_end"
    else:
        boundary = "field_boundary"
    return {"tree": tree, "end": pos, "boundary": boundary, "reason": reason, "last_field": last}


# ----------------------------------------------------------------------------
# binary access helpers
# ----------------------------------------------------------------------------
class Binary:
    def __init__(self, path, segments_json):
        self.path = Path(path)
        self.data = self.path.read_bytes()
        meta = json.loads(Path(segments_json).read_text())
        self.text_va = meta["text_section_vmaddr"]
        self.segments = [s for s in meta["segments"] if s["filesize"] > 0]
        self.sections = []
        for s in meta["segments"]:
            for sec in s["sections"]:
                if sec["fileoff"]:
                    self.sections.append((sec["fileoff"], sec["fileoff"] + sec["size"],
                                          s["segname"] + "." + sec["sectname"], int(sec["vmaddr"], 16)))
        self.sections.sort()

    def va_to_off(self, va):
        for s in self.segments:
            lo = s["vmaddr_int"]
            if lo <= va < lo + s["filesize"]:
                return s["fileoff"] + (va - lo)
        return None

    def off_to_va(self, off):
        for s in self.segments:
            if s["fileoff"] <= off < s["fileoff"] + s["filesize"]:
                return s["vmaddr_int"] + (off - s["fileoff"])
        return None

    def section_of(self, off):
        for lo, hi, name, _ in self.sections:
            if lo <= off < hi:
                return name
        return "?"


RE_CAND = re.compile(rb"(?=\x0a([\x05-\x7f])([A-Za-z0-9_./+\-@]{4,127}\.proto))")
RE_CAND_LONG = re.compile(rb"(?=\x0a([\x80-\xff])([\x01-\x7f])([A-Za-z0-9_./+\-@]{128,300}\.proto))")


def find_candidates(data):
    cands = []
    for m in RE_CAND.finditer(data):
        if len(m.group(2)) == m.group(1)[0]:
            cands.append(m.start())
    for m in RE_CAND_LONG.finditer(data):
        if len(m.group(3)) == (m.group(1)[0] & 0x7F) | (m.group(2)[0] << 7):
            cands.append(m.start())
    return sorted(set(cands))


def accept_scan(res):
    """A scan is accepted as a real descriptor when it carries structure, not just a name."""
    t = res["tree"]
    if "syntax" in t:
        return True
    return any(k in t for k in ("message_type", "enum_type", "service", "extension"))


def discover_raw(b):
    data = b.data
    cands = find_candidates(data)
    found = []
    rejected = []
    covered_until = -1
    for off in cands:
        if off < covered_until:
            continue
        res = scan_file(data, off, min(len(data), off + 16 * 1024 * 1024))
        if res is None or not accept_scan(res):
            rejected.append({"offset": off, "reason": (res or {}).get("reason", "no name"),
                             "name": data[off + 2:off + 2 + data[off + 1]].decode("ascii", "replace")
                             if data[off + 1] < 128 else "?"})
            continue
        res["start"] = off
        found.append(res)
        covered_until = res["end"]
    return found, rejected, len(cands)


def discover_gzip(b):
    data = b.data
    found = []
    for m in re.finditer(b"\x1f\x8b\x08", data):
        goff = m.start()
        try:
            d0 = zlib.decompressobj(16 + zlib.MAX_WBITS)
            head = d0.decompress(data[goff:goff + 64], 4)
        except Exception:
            continue
        if len(head) < 2 or head[0] != 0x0A:
            continue
        try:
            dec = zlib.decompressobj(16 + zlib.MAX_WBITS)
            out = dec.decompress(data[goff:goff + 4_000_000]) + dec.flush()
            if not dec.eof:
                continue
        except Exception:
            continue
        res = scan_file(out, 0)
        if res is None or not accept_scan(res):
            continue
        res["gzip_offset"] = goff
        res["raw"] = out
        res["gzip_len"] = len(data[goff:goff + 4_000_000]) - len(dec.unused_data)
        res["full"] = res["end"] == len(out)
        found.append(res)
    return found


# ----------------------------------------------------------------------------
# gopclntab (names only) + tiny ARM64 decoder -> rawDesc (ptr,len) cross-check
# ----------------------------------------------------------------------------
RE_PROTO_FN = re.compile(r"(_proto_init|_proto_rawDescGZIP|_proto_rawDescGZIP\.func\d+|_proto_rawDescData)$")


def parse_pcln_funcs(b, name_filter):
    data = b.data
    base = data.find(b"\xf1\xff\xff\xff\x00\x00")
    if base < 0:
        return []
    (_, _, _, _, nfunc, _, _, funcname_off, _, _, _, pcln_off) = struct.unpack_from("<IHBBQQQQQQQQ", data, base)
    ftab = base + pcln_off
    fnames = base + funcname_off
    funcs = []
    for i in range(nfunc):
        entry, foff = struct.unpack_from("<II", data, ftab + i * 8)
        nentry, _ = struct.unpack_from("<II", data, ftab + (i + 1) * 8)
        name_off = struct.unpack_from("<i", data, ftab + foff + 4)[0]
        p = fnames + name_off
        e = data.find(b"\x00", p)
        nm = data[p:e].decode("utf-8", "replace")
        if name_filter(nm):
            funcs.append({"name": nm, "va": b.text_va + entry, "size": nentry - entry})
    return funcs


def _bitmask(n, imms, immr, size):
    ln = ((n << 6) | (~imms & 0x3F)).bit_length() - 1
    if ln < 1:
        return None
    esize = 1 << ln
    if esize > size:
        return None
    levels = esize - 1
    s = imms & levels
    r = immr & levels
    if s == levels:
        return None
    w = (1 << (s + 1)) - 1
    emask = (1 << esize) - 1
    elem = ((w >> r) | (w << (esize - r))) & emask
    val = 0
    for i in range(size // esize):
        val |= elem << (i * esize)
    return val


def arm64_scan(code, va):
    """Yield (kind, idx, reg, value): 'ptr' for adrp+add results, 'const' for mov immediates."""
    regs = {}
    events = []
    for i in range(len(code) // 4):
        w = struct.unpack_from("<I", code, i * 4)[0]
        pc = va + i * 4
        rd = w & 31
        if (w & 0x9F000000) == 0x90000000:  # adrp
            immlo = (w >> 29) & 3
            immhi = (w >> 5) & 0x7FFFF
            imm = (immhi << 2) | immlo
            if imm & (1 << 20):
                imm -= 1 << 21
            regs[rd] = (pc & ~0xFFF) + (imm << 12)
        elif (w & 0xFF800000) == 0x91000000:  # add xd, xn, #imm
            rn = (w >> 5) & 31
            imm = (w >> 10) & 0xFFF
            if (w >> 22) & 1:
                imm <<= 12
            if rn in regs and rn != 31 and rd != 31:
                regs[rd] = regs[rn] + imm
                events.append(("ptr", i, rd, regs[rd]))
            else:
                regs.pop(rd, None)
        elif (w & 0x7F800000) == 0x52800000:  # movz
            val = ((w >> 5) & 0xFFFF) << (16 * ((w >> 21) & 3))
            regs[rd] = val
            events.append(("const", i, rd, val))
        elif (w & 0x7F800000) == 0x72800000:  # movk
            sh = 16 * ((w >> 21) & 3)
            val = (regs.get(rd, 0) & ~(0xFFFF << sh)) | (((w >> 5) & 0xFFFF) << sh)
            regs[rd] = val
            events.append(("const", i, rd, val))
        elif (w & 0x7F800000) == 0x32000000 and ((w >> 5) & 31) == 31:  # orr rd, wzr/xzr, #bitmask
            sf = w >> 31
            val = _bitmask((w >> 22) & 1, (w >> 10) & 0x3F, (w >> 16) & 0x3F, 64 if sf else 32)
            if val is None:
                regs.pop(rd, None)
            else:
                regs[rd] = val
                events.append(("const", i, rd, val))
        elif (w & 0xFFE0FFE0) == 0xAA0003E0:  # mov xd, xm
            rm = (w >> 16) & 31
            if rm in regs:
                regs[rd] = regs[rm]
            else:
                regs.pop(rd, None)
        elif rd in regs and (w & 0x1F000000) not in (0x0B000000,):
            # conservative: any other writer to rd that we do not model drops knowledge only for obvious ALU/load forms
            if (w & 0xFF000000) in (0xF9000000, 0xB9000000, 0x39000000, 0xAA000000, 0xCB000000, 0x8B000000):
                if (w & 0xFF000000) in (0xF9000000, 0xB9000000, 0x39000000):
                    if w & 0x00400000:  # load
                        regs.pop(rd, None)
                else:
                    regs.pop(rd, None)
    return events


def asm_crosscheck(b, raw_found):
    """Return (per-start-offset list of asm records, list of proto_init functions without matching candidate)."""
    funcs = parse_pcln_funcs(b, lambda n: bool(RE_PROTO_FN.search(n)))
    starts = {r["start"]: r for r in raw_found}
    by_start = defaultdict(list)
    unmatched = []
    for f in funcs:
        off = b.va_to_off(f["va"])
        if off is None or f["size"] <= 0:
            continue
        code = b.data[off:off + f["size"]]
        ev = arm64_scan(code, f["va"])
        matched = False
        for kind, idx, reg, val in ev:
            if kind != "ptr":
                continue
            poff = b.va_to_off(val)
            if poff is None or poff not in starts:
                continue
            lens = sorted({v for k, j, r, v in ev
                           if k == "const" and abs(j - idx) <= 10 and 16 <= v <= 8_000_000})
            by_start[poff].append({"function": f["name"], "function_va": hex(f["va"]),
                                   "rodata_va": hex(val), "length_candidates": lens})
            matched = True
        if not matched and f["name"].endswith("_proto_init"):
            unmatched.append(f["name"])
    return by_start, unmatched, len(funcs)


# ----------------------------------------------------------------------------
# registry of everything recovered (symbols, messages, enums, extensions)
# ----------------------------------------------------------------------------
SCALAR_NAMES = {1: "double", 2: "float", 3: "int64", 4: "uint64", 5: "int32", 6: "fixed64", 7: "fixed32",
                8: "bool", 9: "string", 12: "bytes", 13: "uint32", 15: "sfixed32", 16: "sfixed64",
                17: "sint32", 18: "sint64"}
EDITION_TEXT = {1000: "2023", 1001: "2024", 9999: "UNSTABLE", 998: "2023", 999: "2023"}
MAX_FIELD = 536870911


class Registry:
    def __init__(self):
        self.files = OrderedDict()
        self.syms = set()
        self.msgs = {}
        self.enums = {}
        self.exts = defaultdict(dict)

    @staticmethod
    def _join(scope, name):
        return scope + "." + name if scope else name

    def add_file(self, tree):
        pkg = tree.get("package", "")
        self.files[tree["name"]] = tree
        parts = pkg.split(".") if pkg else []
        for i in range(1, len(parts) + 1):
            self.syms.add(".".join(parts[:i]))
        for m in tree.get("message_type", []):
            self._walk_msg(m, pkg)
        for e in tree.get("enum_type", []):
            full = self._join(pkg, e["name"])
            self.syms.add(full)
            self.enums[full] = e
        for s in tree.get("service", []):
            self.syms.add(self._join(pkg, s["name"]))
        for x in tree.get("extension", []):
            self._add_ext(x, pkg)

    def _add_ext(self, x, scope):
        ext = x.get("extendee", "").lstrip(".")
        if ext and "number" in x:
            self.exts[ext][x["number"]] = (self._join(scope, x["name"]), x)

    def _walk_msg(self, m, scope):
        full = self._join(scope, m["name"])
        self.syms.add(full)
        self.msgs[full] = m
        for n in m.get("nested_type", []):
            self._walk_msg(n, full)
        for e in m.get("enum_type", []):
            ef = self._join(full, e["name"])
            self.syms.add(ef)
            self.enums[ef] = e
        for x in m.get("extension", []):
            self._add_ext(x, full)

    def resolve(self, name, scope):
        first = name.split(".")[0]
        sc = scope
        while True:
            base = self._join(sc, first)
            if base in self.syms:
                return self._join(sc, name)
            if not sc:
                return None
            sc = sc.rpartition(".")[0]

    def short(self, target, scope):
        target = target.lstrip(".")
        parts = target.split(".")
        for i in range(1, len(parts) + 1):
            self.syms.add(".".join(parts[:i])) if target not in self.msgs and target not in self.enums else None
        for k in range(len(parts) - 1, -1, -1):
            cand = ".".join(parts[k:])
            if self.resolve(cand, scope) == target:
                return cand
        return "." + target


# ----------------------------------------------------------------------------
# text helpers
# ----------------------------------------------------------------------------
def q_str(s):
    out = []
    for ch in s:
        o = ord(ch)
        if ch == "\\":
            out.append("\\\\")
        elif ch == '"':
            out.append('\\"')
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\r":
            out.append("\\r")
        elif ch == "\t":
            out.append("\\t")
        elif o < 0x20 or o == 0x7F:
            out.append("\\%03o" % o)
        else:
            out.append(ch)
    return '"' + "".join(out) + '"'


def q_bytes(b):
    out = []
    for c in b:
        if c == 0x5C:
            out.append("\\\\")
        elif c == 0x22:
            out.append('\\"')
        elif c == 0x0A:
            out.append("\\n")
        elif c == 0x0D:
            out.append("\\r")
        elif c == 0x09:
            out.append("\\t")
        elif 0x20 <= c < 0x7F:
            out.append(chr(c))
        else:
            out.append("\\%03o" % c)
    return '"' + "".join(out) + '"'


def json_name_default(name):
    out = []
    up = False
    for ch in name:
        if ch == "_":
            up = True
        elif up:
            out.append(ch.upper())
            up = False
        else:
            out.append(ch)
    return "".join(out)


def zz(v):
    return (v >> 1) ^ -(v & 1)


def _bool_s(v):
    return "true" if v else "false"


def _scalar_text(t, ref, v):
    if t == "b":
        return _bool_s(v)
    if t == "s":
        return q_str(v)
    if t == "bytes":
        return q_bytes(v)
    if t == "enum":
        return ENUMS[ref].get(v, str(v))
    return str(v)


def aggregate(mt, d):
    """Text-format aggregate of a schema-parsed message (used for non-flattened option sub-messages)."""
    parts = []
    for fn in sorted(SCHEMA[mt]):
        name, t, rep, ref = SCHEMA[mt][fn]
        if name not in d:
            continue
        vals = d[name] if rep else [d[name]]
        for v in vals:
            if t == "msg":
                parts.append("%s %s" % (name, aggregate(ref, v)))
            else:
                parts.append("%s: %s" % (name, _scalar_text(t, ref, v)))
    for fn, wt, v in d.get("_ext", []):
        parts.append("%d: %s" % (fn, _raw_value(wt, v)))
    return "{ " + " ".join(parts) + " }" if parts else "{}"


def _raw_value(wt, v):
    if wt == 0:
        return str(v)
    if wt == 2:
        return q_bytes(v)
    return "0x" + bytes(reversed(v)).hex()


# ----------------------------------------------------------------------------
# generic wire decoding of extension values / option aggregates
# ----------------------------------------------------------------------------
def wire_fields(raw):
    pos = 0
    end = len(raw)
    out = []
    while pos < end:
        tag, pos = read_varint(raw, pos, end)
        fn, wt = tag >> 3, tag & 7
        if wt == 0:
            v, pos = read_varint(raw, pos, end)
        elif wt == 1:
            v = raw[pos:pos + 8]
            pos += 8
        elif wt == 5:
            v = raw[pos:pos + 4]
            pos += 4
        elif wt == 2:
            ln, pos = read_varint(raw, pos, end)
            v = raw[pos:pos + ln]
            pos += ln
        else:
            raise PErr("group in aggregate")
        if pos > end or fn == 0:
            raise PErr("bad aggregate")
        out.append((fn, wt, v))
    return out


def decode_typed(reg, ftype, tname, wt, v):
    """Decode a single wire value as proto field type `ftype`; returns list of text values."""
    if wt == 2 and ftype in (1, 2, 3, 4, 5, 6, 7, 8, 13, 14, 15, 16, 17, 18):  # packed repeated
        res = []
        if ftype in (1, 6, 16):
            for i in range(0, len(v) - 7, 8):
                res += decode_typed(reg, ftype, tname, 1, v[i:i + 8])
        elif ftype in (2, 7, 15):
            for i in range(0, len(v) - 3, 4):
                res += decode_typed(reg, ftype, tname, 5, v[i:i + 4])
        else:
            p = 0
            while p < len(v):
                x, p = read_varint(v, p, len(v))
                res += decode_typed(reg, ftype, tname, 0, x)
        return res
    if ftype == 11 or ftype == 10:
        return [message_text(reg, tname.lstrip("."), v)]
    if ftype == 9:
        return [q_str(v.decode("utf-8", "replace"))]
    if ftype == 12:
        return [q_bytes(v)]
    if ftype == 8:
        return [_bool_s(v)]
    if ftype == 14:
        e = reg.enums.get(tname.lstrip("."))
        if e:
            for ev in e.get("value", []):
                if ev.get("number", 0) == _signed(v, 32):
                    return [ev["name"]]
        return [str(_signed(v, 32))]
    if ftype in (3, 5):
        return [str(_signed(v, 64 if ftype == 3 else 32))]
    if ftype in (4, 13):
        return [str(v)]
    if ftype in (17, 18):
        return [str(zz(v))]
    if ftype == 1:
        return [repr(struct.unpack("<d", v)[0])]
    if ftype == 2:
        return [repr(struct.unpack("<f", v)[0])]
    if ftype in (6,):
        return [str(struct.unpack("<Q", v)[0])]
    if ftype in (16,):
        return [str(struct.unpack("<q", v)[0])]
    if ftype in (7,):
        return [str(struct.unpack("<I", v)[0])]
    if ftype in (15,):
        return [str(struct.unpack("<i", v)[0])]
    return [_raw_value(wt, v)]


def message_text(reg, full, raw):
    m = reg.msgs.get(full)
    by_num = {}
    if m:
        for f in m.get("field", []):
            by_num[f.get("number")] = f
    try:
        items = wire_fields(raw)
    except PErr:
        return q_bytes(raw)
    parts = []
    for fn, wt, v in items:
        f = by_num.get(fn)
        if f is None:
            parts.append("%d: %s" % (fn, _raw_value(wt, v)))
            continue
        ft = f.get("type", 11 if f.get("type_name") else 9)
        try:
            vals = decode_typed(reg, ft, f.get("type_name", ""), wt, v)
        except Exception:
            vals = [_raw_value(wt, v)]
        for s in vals:
            if ft in (10, 11):
                parts.append("%s %s" % (f["name"], s))
            else:
                parts.append("%s: %s" % (f["name"], s))
    return "{ " + " ".join(parts) + " }" if parts else "{}"


def render_option_ext(reg, mt, d):
    """Return [(name, valuetext)] for unknown/extension fields of an options message."""
    res = []
    known = reg.exts.get(OPTION_FULLNAME.get(mt, ""), {})
    for fn, wt, v in d.get("_ext", []):
        ent = known.get(fn)
        if ent is None:
            res.append(("(unresolved_option_%d)" % fn, _raw_value(wt, v)))
            continue
        full, f = ent
        ft = f.get("type", 11 if f.get("type_name") else 9)
        try:
            vals = decode_typed(reg, ft, f.get("type_name", ""), wt, v)
        except Exception:
            vals = [_raw_value(wt, v)]
        for s in vals:
            res.append(("(%s)" % full, s))
    return res


def render_options(reg, mt, d, skip=()):
    """Flatten an options message into [(name, valuetext)]."""
    res = []
    if not d:
        return res
    for fn in sorted(SCHEMA[mt]):
        name, t, rep, ref = SCHEMA[mt][fn]
        if name not in d or name in skip or name == "uninterpreted_option":
            continue
        vals = d[name] if rep else [d[name]]
        for v in vals:
            if t == "msg" and ref == "FeatureSet":
                res.extend(("features." + n, s) for n, s in render_options(reg, "FeatureSet", v))
            elif t == "msg":
                res.append((name, aggregate(ref, v)))
            else:
                res.append((name, _scalar_text(t, ref, v)))
    res.extend(render_option_ext(reg, mt, d))
    return res


# ----------------------------------------------------------------------------
# .proto renderer
# ----------------------------------------------------------------------------
class FileRenderer:
    def __init__(self, tree, reg, header):
        self.t = tree
        self.reg = reg
        self.header = header
        self.pkg = tree.get("package", "")
        syn = tree.get("syntax", "proto2")
        self.kind = "editions" if syn == "editions" else syn
        self.edition = EDITION_TEXT.get(tree.get("edition", 1000), "2023") if self.kind == "editions" else None
        self.lines = []

    def emit(self, ind, text):
        self.lines.append(("  " * ind + text).rstrip())

    def tname(self, name, scope):
        return self.reg.short(name, scope)

    def reserved_names(self, names):
        if self.kind == "editions":
            return ", ".join(names)
        return ", ".join(q_str(n) for n in names)

    def ranges(self, rs, exclusive_end):
        out = []
        for r in rs:
            s = r.get("start", 0)
            e = r.get("end", 0)
            if exclusive_end:
                e -= 1
            if e == s:
                out.append(str(s))
            elif e >= MAX_FIELD:
                out.append("%d to max" % s)
            else:
                out.append("%d to %d" % (s, e))
        return ", ".join(out)

    def opt_stmts(self, ind, mt, d, skip=()):
        for n, v in render_options(self.reg, mt, d, skip):
            self.emit(ind, "option %s = %s;" % (n, v))

    def render(self):
        t = self.t
        L = self.emit
        for h in self.header:
            L(0, "// " + h)
        L(0, "")
        if self.kind == "editions":
            L(0, 'edition = "%s";' % self.edition)
        else:
            L(0, 'syntax = "%s";' % self.kind)
        L(0, "")
        if self.pkg:
            L(0, "package %s;" % self.pkg)
            L(0, "")
        deps = t.get("dependency", [])
        pub = set(t.get("public_dependency", []))
        weak = set(t.get("weak_dependency", []))
        for i, d in enumerate(deps):
            L(0, 'import %s"%s";' % ("public " if i in pub else "weak " if i in weak else "", d))
        for d in t.get("option_dependency", []):
            L(0, 'import option "%s";' % d)
        if deps or t.get("option_dependency"):
            L(0, "")
        opts = t.get("options")
        if opts:
            n0 = len(self.lines)
            self.opt_stmts(0, "FileOptions", opts)
            if len(self.lines) > n0:
                L(0, "")
        for e in t.get("enum_type", []):
            self.enum(e, 0, self.pkg)
            L(0, "")
        for m in t.get("message_type", []):
            self.message(m, 0, self.pkg)
            L(0, "")
        self.extends(t.get("extension", []), 0, self.pkg)
        for s in t.get("service", []):
            self.service(s, 0)
            L(0, "")
        while self.lines and self.lines[-1] == "":
            self.lines.pop()
        return "\n".join(self.lines) + "\n"

    # -- enum ---------------------------------------------------------------
    def enum(self, e, ind, scope):
        L = self.emit
        vis = {1: "local ", 2: "export "}.get(e.get("visibility", 0), "") if self.kind == "editions" else ""
        L(ind, "%senum %s {" % (vis, e["name"]))
        self.opt_stmts(ind + 1, "EnumOptions", e.get("options"))
        if e.get("reserved_range"):
            L(ind + 1, "reserved %s;" % self.ranges(e["reserved_range"], False))
        if e.get("reserved_name"):
            L(ind + 1, "reserved %s;" % self.reserved_names(e["reserved_name"]))
        for v in e.get("value", []):
            fo = ["%s = %s" % nv for nv in render_options(self.reg, "EnumValueOptions", v.get("options"))]
            L(ind + 1, "%s = %d%s;" % (v["name"], v.get("number", 0), " [" + ", ".join(fo) + "]" if fo else ""))
        L(ind, "}")

    # -- extend -------------------------------------------------------------
    def extends(self, exts, ind, scope):
        groups = OrderedDict()
        for x in exts:
            groups.setdefault(x.get("extendee", ""), []).append(x)
        for ext, fields in groups.items():
            self.emit(ind, "extend %s {" % self.tname(ext, scope))
            for f in fields:
                self.field(f, ind + 1, scope, None, None)
            self.emit(ind, "}")
            self.emit(ind, "")
        if groups:
            while self.lines and self.lines[-1] == "":
                self.lines.pop()
            self.emit(0, "")

    # -- message ------------------------------------------------------------
    def message(self, m, ind, scope):
        L = self.emit
        full = scope + "." + m["name"] if scope else m["name"]
        vis = {1: "local ", 2: "export "}.get(m.get("visibility", 0), "") if self.kind == "editions" else ""
        L(ind, "%smessage %s {" % (vis, m["name"]))
        self.opt_stmts(ind + 1, "MessageOptions", m.get("options"), skip=("map_entry",))
        if m.get("reserved_range"):
            L(ind + 1, "reserved %s;" % self.ranges(m["reserved_range"], True))
        if m.get("reserved_name"):
            L(ind + 1, "reserved %s;" % self.reserved_names(m["reserved_name"]))
        for er in m.get("extension_range", []):
            eo = ["%s = %s" % nv for nv in render_options(self.reg, "ExtensionRangeOptions", er.get("options"))]
            L(ind + 1, "extensions %s%s;" % (self.ranges([er], True), " [" + ", ".join(eo) + "]" if eo else ""))

        nested = {n["name"]: n for n in m.get("nested_type", [])}
        maps = {}
        groups = {}
        for f in m.get("field", []):
            tn = f.get("type_name", "").lstrip(".")
            nn = tn.rpartition(".")[2]
            if f.get("type") in (10, 11) and tn == full + "." + nn and nn in nested:
                ne = nested[nn]
                if f.get("label") == 3 and (ne.get("options") or {}).get("map_entry"):
                    maps[f["name"]] = ne
                elif f.get("type") == 10 and self.kind != "editions":
                    groups[f["name"]] = ne
        hidden = set(n["name"] for n in list(maps.values()) + list(groups.values()))

        oneofs = {}
        for f in m.get("field", []):
            if "oneof_index" in f and not f.get("proto3_optional"):
                oneofs.setdefault(f["oneof_index"], []).append(f)
        done = set()
        decls = m.get("oneof_decl", [])
        for f in m.get("field", []):
            oi = f.get("oneof_index")
            if oi is not None and not f.get("proto3_optional") and oi < len(decls):
                if oi in done:
                    continue
                done.add(oi)
                L(ind + 1, "oneof %s {" % decls[oi]["name"])
                self.opt_stmts(ind + 2, "OneofOptions", decls[oi].get("options"))
                for g in oneofs[oi]:
                    self.field(g, ind + 2, full, maps.get(g["name"]), groups.get(g["name"]), in_oneof=True)
                L(ind + 1, "}")
            else:
                self.field(f, ind + 1, full, maps.get(f["name"]), groups.get(f["name"]))
        for e in m.get("enum_type", []):
            L(ind + 1, "")
            self.enum(e, ind + 1, full)
        for n in m.get("nested_type", []):
            if n["name"] in hidden:
                continue
            L(ind + 1, "")
            self.message(n, ind + 1, full)
        if m.get("extension"):
            L(ind + 1, "")
            self.extends(m["extension"], ind + 1, full)
            while self.lines and self.lines[-1] == "":
                self.lines.pop()
        L(ind, "}")

    def field(self, f, ind, scope, map_entry, group_msg, in_oneof=False):
        L = self.emit
        ft = f.get("type", 11 if f.get("type_name") else 9)
        label = f.get("label", 1)
        opts = []
        fopts = f.get("options")
        extra = []
        lbl = ""
        if in_oneof:
            lbl = ""
        elif self.kind == "proto2":
            lbl = {1: "optional ", 2: "required ", 3: "repeated "}.get(label, "")
        elif self.kind == "proto3":
            lbl = "repeated " if label == 3 else ("optional " if f.get("proto3_optional") else "")
        else:
            lbl = "repeated " if label == 3 else ""
            if label == 2:
                extra.append("features.field_presence = LEGACY_REQUIRED")
            if ft == 10:
                extra.append("features.message_encoding = DELIMITED")
        if map_entry is not None:
            kf = [x for x in map_entry.get("field", []) if x.get("number") == 1]
            vf = [x for x in map_entry.get("field", []) if x.get("number") == 2]
            full = scope
            kt = self._ftype(kf[0], full) if kf else "string"
            vt = self._ftype(vf[0], full) if vf else "string"
            tstr = "map<%s, %s>" % (kt, vt)
            lbl = ""
        elif ft == 10 and group_msg is not None:
            tstr = "group"
        else:
            tstr = self._ftype(f, scope)
        if "default_value" in f:
            dv = f["default_value"]
            if ft == 9:
                dv = q_str(dv)
            elif ft == 12:
                dv = '"%s"' % dv
            opts.append("default = %s" % dv)
        jn = f.get("json_name")
        if jn is not None and jn != json_name_default(f["name"]):
            opts.append("json_name = %s" % q_str(jn))
        opts.extend("%s = %s" % nv for nv in render_options(self.reg, "FieldOptions", fopts))
        opts.extend(extra)
        oc = " [" + ", ".join(opts) + "]" if opts else ""
        if group_msg is not None and tstr == "group":
            L(ind, "%sgroup %s = %d%s {" % (lbl, group_msg["name"], f.get("number", 0), oc))
            sub = FileRenderer(self.t, self.reg, [])
            sub.kind = self.kind
            sub.pkg = self.pkg
            sub.lines = []
            full = scope + "." + group_msg["name"] if scope else group_msg["name"]
            sub.message(group_msg, 0, scope)
            body = sub.lines[1:-1]
            for ln in body:
                L(ind, ln)
            L(ind, "}")
        else:
            L(ind, "%s%s %s = %d%s;" % (lbl, tstr, f["name"], f.get("number", 0), oc))

    def _ftype(self, f, scope):
        ft = f.get("type", 11 if f.get("type_name") else 9)
        if ft in SCALAR_NAMES:
            return SCALAR_NAMES[ft]
        return self.tname(f.get("type_name", ""), scope)

    # -- service ------------------------------------------------------------
    def service(self, s, ind):
        L = self.emit
        full = self.pkg + "." + s["name"] if self.pkg else s["name"]
        L(ind, "service %s {" % s["name"])
        self.opt_stmts(ind + 1, "ServiceOptions", s.get("options"))
        for mth in s.get("method", []):
            ins = ("stream " if mth.get("client_streaming") else "") + self.tname(mth.get("input_type", ""), full)
            outs = ("stream " if mth.get("server_streaming") else "") + self.tname(mth.get("output_type", ""), full)
            mo = render_options(self.reg, "MethodOptions", mth.get("options"))
            if mo:
                L(ind + 1, "rpc %s(%s) returns (%s) {" % (mth["name"], ins, outs))
                for n, v in mo:
                    L(ind + 2, "option %s = %s;" % (n, v))
                L(ind + 1, "}")
            else:
                L(ind + 1, "rpc %s(%s) returns (%s);" % (mth["name"], ins, outs))
        L(ind, "}")


def brace_balance(text):
    """Return (balanced, open_minus_close) ignoring braces inside strings and comments."""
    depth = 0
    i = 0
    n = len(text)
    while i < n:
        c = text[i]
        if c == "/" and text.startswith("//", i):
            j = text.find("\n", i)
            i = n if j < 0 else j
            continue
        if c == '"':
            i += 1
            while i < n and text[i] != '"':
                i += 2 if text[i] == "\\" else 1
        elif c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth < 0:
                return False, depth
        i += 1
    return depth == 0, depth


def count_tree(tree):
    cnt = {"messages": 0, "fields": 0, "enums": 0, "enum_values": 0, "services": 0, "methods": 0,
           "extensions": 0, "oneofs": 0, "map_fields": 0}

    def walk_msg(m):
        cnt["messages"] += 1
        cnt["fields"] += len(m.get("field", []))
        cnt["oneofs"] += len(m.get("oneof_decl", []))
        cnt["extensions"] += len(m.get("extension", []))
        for n in m.get("nested_type", []):
            if (n.get("options") or {}).get("map_entry"):
                cnt["map_fields"] += 1
                cnt["messages"] += 0
            else:
                walk_msg(n)
        for e in m.get("enum_type", []):
            cnt["enums"] += 1
            cnt["enum_values"] += len(e.get("value", []))

    for m in tree.get("message_type", []):
        walk_msg(m)
    for e in tree.get("enum_type", []):
        cnt["enums"] += 1
        cnt["enum_values"] += len(e.get("value", []))
    for s in tree.get("service", []):
        cnt["services"] += 1
        cnt["methods"] += len(s.get("method", []))
    cnt["extensions"] += len(tree.get("extension", []))
    return cnt


# ----------------------------------------------------------------------------
# orchestration
# ----------------------------------------------------------------------------
def sha256(b):
    return hashlib.sha256(b).hexdigest()


def family_of(name, pkg):
    if "jetski" in name or pkg.startswith("exa.") and "cortex" in pkg:
        return "jetski"
    if "cider" in name or "cider" in pkg:
        return "cider"
    if pkg == "exa" or pkg.startswith("exa.") or "/exa/" in name:
        return "exa"
    if "businessaicode" in name or "gca/aicode" in name:
        return "businessaicode"
    if name.startswith("google/protobuf/") or name.endswith("descriptor.proto"):
        return "protobuf"
    if name.startswith("google/"):
        return "google"
    return "other"


def prose_preview(raw):
    return "".join(chr(c) if 32 <= c < 127 else "." for c in raw)


def main():
    ap = argparse.ArgumentParser(description="Recover FileDescriptorProtos from agy and emit .proto files")
    ap.add_argument("--binary", type=Path, default=DEFAULT_BINARY)
    ap.add_argument("--cli-root", type=Path, default=CLI_ROOT)
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    cli = args.cli_root
    out_dir = cli / "reconstructed" / "proto"
    index_path = cli / "metadata" / "recovered-protos.json"
    b = Binary(args.binary, cli / "metadata" / "macho-segments.json")
    data = b.data
    log = (lambda *a: print(*a, file=sys.stderr)) if args.verbose else (lambda *a: None)

    # ---- discovery -------------------------------------------------------
    raw_found, rejected, ncands = discover_raw(b)
    gz_found = discover_gzip(b)
    asm_by_start, unmatched_inits, nfuncs = asm_crosscheck(b, raw_found)
    log("raw", len(raw_found), "gzip", len(gz_found), "asm-matched", len(asm_by_start))

    entries = OrderedDict()  # proto name -> entry

    def put(name, entry):
        if name not in entries:
            entries[name] = entry
            return
        cur = entries[name]
        if cur["bytes"] == entry["bytes"]:
            cur["sources"].extend(entry["sources"])
            for k in ("asm", "gzip"):
                if entry.get(k) and not cur.get(k):
                    cur[k] = entry[k]
            return
        # conflicting content for the same proto name: keep the asm-confirmed / longer one, record the other
        better = entry if (entry["asm_confirmed"], len(entry["bytes"])) > (cur["asm_confirmed"], len(cur["bytes"])) else cur
        worse = cur if better is entry else entry
        better.setdefault("alternates", []).append({"sources": worse["sources"], "length": len(worse["bytes"]),
                                                     "sha256": sha256(worse["bytes"])})
        entries[name] = better

    for r in raw_found:
        start, end = r["start"], r["end"]
        asm = asm_by_start.get(start, [])
        length = end - start
        confirmed = any(length in a["length_candidates"] for a in asm)
        boundary = r["boundary"]
        override = None
        if asm and not confirmed:
            cands = sorted({v for a in asm for v in a["length_candidates"]})
            for c in cands:
                res = scan_file(data, start, start + c)
                if res and res["end"] == start + c and accept_scan(res):
                    r, end, length, confirmed, boundary = res, start + c, c, True, "asm_length_override"
                    r["start"] = start
                    override = {"validator_length": length, "asm_length": c}
                    break
        tree = r["tree"]
        trailing = data[end:end + 40]
        entry = {
            "tree": tree, "bytes": data[start:end], "asm_confirmed": confirmed,
            "sources": [{"kind": "rawDesc", "file_offset": start, "va": hex(b.off_to_va(start) or 0),
                         "section": b.section_of(start), "length": end - start}],
            "boundary": boundary, "boundary_reason": r["reason"], "trailing": trailing,
            "asm": [{k: v for k, v in a.items()} for a in asm], "override": override,
        }
        put(tree["name"], entry)
    for g in gz_found:
        tree = g["tree"]
        blob = g["raw"][:g["end"]]
        entry = {
            "tree": tree, "bytes": blob, "asm_confirmed": False,
            "sources": [{"kind": "gzip", "file_offset": g["gzip_offset"], "va": hex(b.off_to_va(g["gzip_offset"]) or 0),
                         "section": b.section_of(g["gzip_offset"]), "compressed_length": g["gzip_len"],
                         "length": len(blob), "fully_consumed": g["full"]}],
            "boundary": g["boundary"], "boundary_reason": "gzip stream end" if g["full"] else g["reason"],
            "trailing": g["raw"][g["end"]:g["end"] + 40], "asm": [], "override": None,
        }
        put(tree["name"], entry)

    # ---- registry --------------------------------------------------------
    reg = Registry()
    for e in entries.values():
        reg.add_file(e["tree"])

    # ---- render ----------------------------------------------------------
    if out_dir.exists():
        shutil.rmtree(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    index_protos = []
    services = []
    packages = defaultdict(list)
    fam_counts = defaultdict(int)
    totals = defaultdict(int)
    problems = []
    for name, e in sorted(entries.items()):
        tree = e["tree"]
        rel = Path(name)
        if rel.is_absolute() or ".." in rel.parts:
            rel = Path("_unsafe") / name.replace("/", "_").replace("..", "_")
        pkg = tree.get("package", "")
        src0 = e["sources"][0]
        header = [
            "Recovered from agy v1.5.10 (/Users/mac/.local/bin/agy) by cli/scripts/recover-protos.py",
            "source: %s @ file offset 0x%x (va %s, %s), descriptor length %d B, sha256 %s"
            % (src0["kind"], src0["file_offset"], src0["va"], src0["section"], len(e["bytes"]),
               sha256(e["bytes"])[:16]),
            "boundary: %s%s" % (e["boundary"], "; machine-code length cross-check OK" if e["asm_confirmed"] else ""),
        ]
        try:
            text = FileRenderer(tree, reg, header).render()
        except Exception as ex:  # keep going, report loudly
            problems.append({"proto": name, "error": "render: %r" % ex})
            text = "// RENDER FAILED: %r\n" % ex
        balanced, depth = brace_balance(text)
        target = out_dir / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding="utf-8")
        cnt = count_tree(tree)
        for k, v in cnt.items():
            totals[k] += v
        fam = family_of(name, pkg)
        fam_counts[fam] += 1
        packages[pkg].append(name)
        deps = tree.get("dependency", [])
        pub = set(tree.get("public_dependency", []))
        weak = set(tree.get("weak_dependency", []))
        for s in tree.get("service", []):
            full = (pkg + "." if pkg else "") + s["name"]
            services.append({"proto": name, "service": s["name"], "full_name": full,
                             "methods": [{"name": m["name"], "input": m.get("input_type", "").lstrip("."),
                                          "output": m.get("output_type", "").lstrip("."),
                                          "client_streaming": bool(m.get("client_streaming")),
                                          "server_streaming": bool(m.get("server_streaming"))}
                                         for m in s.get("method", [])]})
        # exactness self-check: descriptor bytes parse to the very end and nothing else
        chk = scan_file(e["bytes"], 0)
        exact = bool(chk and chk["end"] == len(e["bytes"]))
        edition = tree.get("edition")
        trailing = e["trailing"]
        rec = {
            "proto_path": name, "package": pkg, "family": fam,
            "syntax": tree.get("syntax", "proto2"),
            "edition": ENUMS["Edition"].get(edition) if edition is not None else None,
            "go_package": (tree.get("options") or {}).get("go_package"),
            "imports": [{"path": d, "public": i in pub, "weak": i in weak} for i, d in enumerate(deps)],
            "option_imports": tree.get("option_dependency", []),
            "counts": cnt,
            "descriptor_length": len(e["bytes"]), "descriptor_sha256": sha256(e["bytes"]),
            "sources": e["sources"],
            "boundary": e["boundary"], "boundary_reason": e["boundary_reason"],
            "boundary_exact_reparse": exact,
            "asm_confirmed": e["asm_confirmed"],
            "asm_functions": e["asm"], "asm_override": e["override"],
            "trailing_after_end_ascii": prose_preview(trailing),
            "output": str(Path("reconstructed/proto") / rel),
            "output_bytes": len(text.encode("utf-8")), "braces_balanced": balanced,
            "alternates": e.get("alternates", []),
        }
        if not balanced or not exact:
            problems.append({"proto": name, "braces_balanced": balanced, "exact_reparse": exact})
        index_protos.append(rec)

    gz_names = sorted(n for n, e in entries.items() if any(s["kind"] == "gzip" for s in e["sources"]))
    both = sorted(n for n, e in entries.items()
                  if {"gzip", "rawDesc"} <= set(s["kind"] for s in e["sources"]))
    asm_ok = sum(1 for r in index_protos if r["asm_confirmed"])
    unconfirmed = [r["proto_path"] for r in index_protos if not r["asm_confirmed"]]
    fam_total_core = sum(fam_counts[k] for k in ("jetski", "exa", "cider", "businessaicode"))
    summary = {
        "total_protos": len(index_protos),
        "from_rawdesc": sum(1 for e in entries.values() if any(s["kind"] == "rawDesc" for s in e["sources"])),
        "from_gzip": len(gz_names),
        "in_both_rawdesc_and_gzip": len(both),
        "gzip_only": sorted(set(gz_names) - set(both)),
        "candidates_scanned": ncands, "candidates_rejected": len(rejected),
        "asm_functions_scanned": nfuncs,
        "asm_confirmed_exact_length": asm_ok,
        "asm_unconfirmed": len(unconfirmed),
        "proto_init_without_matching_rawdesc": len(unmatched_inits),
        "boundary_kinds": {k: sum(1 for r in index_protos if r["boundary"] == k)
                           for k in sorted({r["boundary"] for r in index_protos})},
        "families": dict(sorted(fam_counts.items())),
        "jetski_exa_cider_businessaicode": fam_total_core,
        "braces_balanced": sum(1 for r in index_protos if r["braces_balanced"]),
        "exact_reparse_ok": sum(1 for r in index_protos if r["boundary_exact_reparse"]),
        "totals": dict(totals),
        "services": len(services),
        "rpc_methods": sum(len(s["methods"]) for s in services),
        "problems": problems,
    }
    index = {
        "tool": "cli/scripts/recover-protos.py",
        "binary": {"path": str(args.binary), "size": len(data), "sha256": sha256(data)},
        "summary": summary,
        "unconfirmed_by_machine_code": unconfirmed,
        "rejected_candidates": rejected,
        "proto_init_without_matching_rawdesc": unmatched_inits,
        "packages": {k: sorted(v) for k, v in sorted(packages.items())},
        "services": services,
        "protos": index_protos,
    }
    index_path.write_text(json.dumps(index, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({k: v for k, v in summary.items() if k != "gzip_only"}, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
