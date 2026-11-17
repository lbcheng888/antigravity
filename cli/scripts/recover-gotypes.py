#!/usr/bin/env python3
"""L3-B: Go runtime reflection type & struct recovery for /Users/mac/.local/bin/agy.

Walks the whole ``__DATA_CONST.__go_type`` section of the Mach-O binary (Go
``ego1.28`` toolchain), rebuilds every ``abi.Type`` (struct / interface / ptr /
slice / array / chan / map / func / basic kinds) including ``UncommonType``
method tables, and emits

  * ``cli/reconstructed/go-types/<pkg_path>/types_recovered.go``  (one per package)
  * ``cli/metadata/recovered-gotypes.json``  (summary, per-package index,
    per-type detail and a ``va_to_type`` lookup table for decompile-workbench.py)

Only the Python 3 standard library is used.  Nothing is hard-coded about the
section position: it is read from ``cli/metadata/macho-segments.json``.

abi.Type layout (48 bytes, ``<QQIBBBBQQiI``)::

    +0x00 Size_  +0x08 PtrBytes  +0x10 Hash(u32)  +0x14 TFlag  +0x15 Align_
    +0x16 FieldAlign_  +0x17 Kind_  +0x18 Equal  +0x20 GCData
    +0x28 Str (nameOff, relative to __go_type start)
    +0x2c PtrToThis (typeOff, relative to __go_type start)

Kind specific payload follows at +0x30; ``UncommonType`` (TFlag&1) follows the
kind specific payload.  ``nameOff``/``typeOff`` are relative to the start of
``__go_type`` while ``StructField.Typ`` / ``Elem`` / ``Key`` / ``PkgPath`` are
absolute virtual addresses.  ``textOff`` (Method.Ifn/Tfn) is relative to
``moduledata.text`` (= start of ``__TEXT.__text``).

Usage::

    python3 cli/scripts/recover-gotypes.py                  # full run
    python3 cli/scripts/recover-gotypes.py --dump nativeswarm.Member
    python3 cli/scripts/recover-gotypes.py --stats          # scan only, print stats
"""

from __future__ import annotations

import argparse
import json
import mmap
import re
import shutil
import struct
import sys
import time
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Optional

DEFAULT_BINARY = Path("/Users/mac/.local/bin/agy")
CLI_ROOT = Path(__file__).resolve().parent.parent

# --------------------------------------------------------------------------- #
# Constants
# --------------------------------------------------------------------------- #

K_BOOL, K_INT, K_INT8, K_INT16, K_INT32, K_INT64 = 1, 2, 3, 4, 5, 6
K_UINT, K_UINT8, K_UINT16, K_UINT32, K_UINT64, K_UINTPTR = 7, 8, 9, 10, 11, 12
K_FLOAT32, K_FLOAT64, K_COMPLEX64, K_COMPLEX128 = 13, 14, 15, 16
K_ARRAY, K_CHAN, K_FUNC, K_INTERFACE, K_MAP, K_PTR, K_SLICE = 17, 18, 19, 20, 21, 22, 23
K_STRING, K_STRUCT, K_UNSAFEPTR = 24, 25, 26

KIND_NAMES = {
    1: "bool", 2: "int", 3: "int8", 4: "int16", 5: "int32", 6: "int64",
    7: "uint", 8: "uint8", 9: "uint16", 10: "uint32", 11: "uint64", 12: "uintptr",
    13: "float32", 14: "float64", 15: "complex64", 16: "complex128",
    17: "array", 18: "chan", 19: "func", 20: "interface", 21: "map", 22: "ptr",
    23: "slice", 24: "string", 25: "struct", 26: "unsafe.Pointer",
}
# Go spelling of the underlying basic kinds (used for `type X int32` style decls).
BASIC_GO = {
    1: "bool", 2: "int", 3: "int8", 4: "int16", 5: "int32", 6: "int64",
    7: "uint", 8: "uint8", 9: "uint16", 10: "uint32", 11: "uint64", 12: "uintptr",
    13: "float32", 14: "float64", 15: "complex64", 16: "complex128",
    24: "string", 26: "unsafe.Pointer",
}
# Fixed sizes for kinds whose size is architecture-invariant on arm64.
KIND_SIZE = {
    1: 1, 2: 8, 3: 1, 4: 2, 5: 4, 6: 8, 7: 8, 8: 1, 9: 2, 10: 4, 11: 8, 12: 8,
    13: 4, 14: 8, 15: 8, 16: 16, 18: 8, 19: 8, 20: 16, 21: 8, 22: 8, 23: 24,
    24: 16, 26: 8,
}

TFLAG_UNCOMMON = 0x1
TFLAG_EXTRASTAR = 0x2
TFLAG_NAMED = 0x4
TFLAG_REGULAR_MEMORY = 0x8

NAME_EXPORTED = 0x1
NAME_HAS_TAG = 0x2
NAME_HAS_PKGPATH = 0x4
NAME_EMBEDDED = 0x8

# Size of the kind-specific struct (header + kind payload), i.e. where
# UncommonType starts for types that carry one.
KIND_PAYLOAD_END = {
    K_PTR: 56, K_SLICE: 56, K_ARRAY: 72, K_CHAN: 64, K_FUNC: 56,
    K_STRUCT: 80, K_INTERFACE: 80,
}
MAP_PAYLOAD_END_CANDIDATES = (136, 112, 88)  # ego1.28 swiss map (observed: 136), go1.24 swiss, classic hmap

HEADER = struct.Struct("<QQIBBBBQQiI")
U64 = struct.Struct("<Q")
U32 = struct.Struct("<I")
I32 = struct.Struct("<i")
U16 = struct.Struct("<H")
FIELD = struct.Struct("<QQQ")
IMETHOD = struct.Struct("<II")
METHOD = struct.Struct("<iiii")

COMPOSITE_PREFIXES = (
    "*", "[", "map[", "chan ", "chan<-", "<-chan", "func(", "func ", "struct {",
    "struct{", "interface {", "interface{", "go:", "unsafe.Pointer", "map.group[", "map.",
)

IDENT_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


# --------------------------------------------------------------------------- #
# Binary image helpers
# --------------------------------------------------------------------------- #


class Image:
    """Read-only mmap of the Mach-O file with VA -> file offset translation."""

    def __init__(self, path: Path, layout: dict[str, Any]):
        self.path = path
        self._fh = open(path, "rb")
        self.m = mmap.mmap(self._fh.fileno(), 0, access=mmap.ACCESS_READ)
        self.spans: list[tuple[int, int, int]] = []
        self.sections: dict[tuple[str, str], dict[str, Any]] = {}
        for seg in layout["segments"]:
            if seg["filesize"] > 0:
                self.spans.append((seg["vmaddr_int"], seg["vmaddr_int"] + seg["filesize"], seg["fileoff"]))
            for sec in seg["sections"]:
                sec = dict(sec)
                sec["vmaddr_int"] = int(sec["vmaddr"], 16)
                self.sections[(sec["segname"], sec["sectname"])] = sec

    def va2off(self, va: int) -> Optional[int]:
        for lo, hi, fo in self.spans:
            if lo <= va < hi:
                return fo + (va - lo)
        return None

    def close(self) -> None:
        self.m.close()
        self._fh.close()


def read_uvarint(buf: Any, i: int) -> tuple[int, int]:
    n = 0
    shift = 0
    while True:
        b = buf[i]
        i += 1
        n |= (b & 0x7F) << shift
        if b < 0x80:
            return n, i
        shift += 7
        if shift > 35:
            raise ValueError("uvarint too long")


# --------------------------------------------------------------------------- #
# Type table
# --------------------------------------------------------------------------- #


class GoTypeTable:
    def __init__(self, img: Image, gt_va: int, gt_off: int, gt_size: int, text_va: int):
        self.img = img
        self.gt_va = gt_va
        self.gt_off = gt_off
        self.gt_size = gt_size
        self.gt_end = gt_va + gt_size
        self.text_va = text_va
        self.d: bytes = img.m[gt_off:gt_off + gt_size]
        self.types: dict[int, dict[str, Any]] = {}   # section offset -> type record
        self._names: dict[int, Optional[dict[str, Any]]] = {}
        self.stats: Counter = Counter()

    # -- names ------------------------------------------------------------- #

    def _name_buf(self, va: int) -> tuple[Any, int]:
        if self.gt_va <= va < self.gt_end:
            return self.d, va - self.gt_va
        off = self.img.va2off(va)
        if off is None:
            raise ValueError("name VA unmapped")
        return self.img.m, off

    def name_at_va(self, va: int) -> Optional[dict[str, Any]]:
        if va == 0:
            return None
        if va in self._names:
            return self._names[va]
        try:
            buf, i = self._name_buf(va)
            res = self._decode_name(buf, i)
        except (ValueError, IndexError, UnicodeDecodeError):
            res = None
        self._names[va] = res
        return res

    def name_at_off(self, off: int) -> Optional[dict[str, Any]]:
        """Decode abi.Name at nameOff (relative to __go_type start)."""
        if off <= 0 or off >= self.gt_size:
            return None
        return self.name_at_va(self.gt_va + off)

    @staticmethod
    def _decode_name(buf: Any, i: int) -> dict[str, Any]:
        flags = buf[i]
        if flags & 0xF0:
            raise ValueError("bad name flags")
        n, j = read_uvarint(buf, i + 1)
        if n > 1 << 16:
            raise ValueError("name too long")
        name = bytes(buf[j:j + n]).decode("utf-8")
        j += n
        tag = ""
        if flags & NAME_HAS_TAG:
            tn, j = read_uvarint(buf, j)
            if tn > 1 << 16:
                raise ValueError("tag too long")
            tag = bytes(buf[j:j + tn]).decode("utf-8")
            j += tn
        pkg_off = 0
        if flags & NAME_HAS_PKGPATH:
            pkg_off = struct.unpack_from("<i", buf, j)[0]
            j += 4
        return {
            "flags": flags, "name": name, "tag": tag, "pkg_off": pkg_off,
            "exported": bool(flags & NAME_EXPORTED), "embedded": bool(flags & NAME_EMBEDDED),
        }

    # -- scanning ---------------------------------------------------------- #

    def scan(self) -> None:
        d = self.d
        size = self.gt_size
        unpack = HEADER.unpack_from
        cands: dict[int, dict[str, Any]] = {}
        tflag_hist: Counter = Counter()
        for p in range(0, size - 48, 8):
            kb = d[p + 23]
            k = kb & 31
            if k == 0 or k > 26:
                continue
            al = d[p + 21]
            fa = d[p + 22]
            if al not in (1, 2, 4, 8, 16, 32, 64) or fa not in (1, 2, 4, 8, 16, 32, 64):
                continue
            tf = d[p + 20]
            if tf & 0xC0:
                continue
            sz, pb, hsh, _tf, _al, _fa, _k, eq, gc, s, ptt = unpack(d, p)
            if hsh == 0 or pb > sz or not (0 < s < size - 4):
                continue
            if ptt != 0 and not (0 < ptt < size):
                continue
            fixed = KIND_SIZE.get(k)
            if fixed is not None and sz != fixed:
                continue
            nm = self.name_at_off(s)
            if nm is None or not nm["name"] or nm["flags"] & (NAME_HAS_TAG | NAME_EMBEDDED):
                continue
            text = nm["name"]
            if not text.isprintable():
                continue
            own = text[1:] if tf & TFLAG_EXTRASTAR else text
            if tf & TFLAG_EXTRASTAR and not text.startswith("*"):
                continue
            if not own:
                continue
            if not (tf & TFLAG_NAMED) and not own.startswith(COMPOSITE_PREFIXES):
                continue
            t = {
                "off": p, "size": sz, "ptrbytes": pb, "hash": hsh, "tflag": tf, "align": al,
                "falign": fa, "kind": k, "equal": eq, "gcdata": gc, "str": text, "own": own,
                "ptr_to_this": ptt, "refs": [],
            }
            if not self._parse_kind(t):
                continue
            tflag_hist[tf] += 1
            cands[p] = t
        self.stats["raw_candidates"] = len(cands)
        self.stats["tflag_hist"] = dict(tflag_hist)
        self._prune(cands)
        self.types = cands
        self._resolve_overlaps()

    def _va_ok(self, va: int) -> bool:
        return self.gt_va <= va < self.gt_end and (va - self.gt_va) % 8 == 0

    def _parse_kind(self, t: dict[str, Any]) -> bool:
        p, k, d = t["off"], t["kind"], self.d
        size = self.gt_size
        refs: list[int] = []
        if k in (K_PTR, K_SLICE):
            elem = U64.unpack_from(d, p + 48)[0]
            if not self._va_ok(elem):
                return False
            t["elem"] = elem
            refs.append(elem)
        elif k == K_ARRAY:
            elem, sl, ln = struct.unpack_from("<QQQ", d, p + 48)
            if not self._va_ok(elem) or not self._va_ok(sl) or ln > 1 << 40:
                return False
            t["elem"], t["slice"], t["len"] = elem, sl, ln
            refs += [elem, sl]
        elif k == K_CHAN:
            elem, dr = struct.unpack_from("<QQ", d, p + 48)
            if not self._va_ok(elem) or not 1 <= dr <= 3:
                return False
            t["elem"], t["dir"] = elem, dr
            refs.append(elem)
        elif k == K_MAP:
            key, elem = struct.unpack_from("<QQ", d, p + 48)
            if not self._va_ok(key) or not self._va_ok(elem):
                return False
            t["key"], t["elem"] = key, elem
            refs += [key, elem]
        elif k == K_FUNC:
            inc, outc = struct.unpack_from("<HH", d, p + 48)
            variadic = bool(outc & 0x8000)
            outc &= 0x7FFF
            if inc > 512 or outc > 512:
                return False
            t["in_count"], t["out_count"], t["variadic"] = inc, outc, variadic
        elif k == K_STRUCT:
            pkg, fdata, flen, fcap = struct.unpack_from("<QQQQ", d, p + 48)
            if flen > fcap or flen > 20000:
                return False
            if pkg and self.name_at_va(pkg) is None:
                return False
            t["pkg_va"] = pkg
            t["fields_va"], t["nfields"] = fdata, flen
            if flen:
                fo = self._buf_off(fdata, flen * 24)
                if fo is None:
                    return False
                buf, base = fo
                last = 0
                fields = []
                for i in range(flen):
                    nva, tva, off = FIELD.unpack_from(buf, base + i * 24)
                    if not self._va_ok(tva) or off > t["size"] or off < last:
                        return False
                    nm = self.name_at_va(nva)
                    if nm is None or (not nm["name"]):
                        return False
                    last = off
                    refs.append(tva)
                    fields.append((nva, tva, off))
                t["_fields_raw"] = fields
            else:
                t["_fields_raw"] = []
        elif k == K_INTERFACE:
            pkg, mdata, mlen, mcap = struct.unpack_from("<QQQQ", d, p + 48)
            if mlen > mcap or mlen > 5000:
                return False
            if pkg and self.name_at_va(pkg) is None:
                return False
            t["pkg_va"] = pkg
            t["methods_va"], t["nmethods_iface"] = mdata, mlen
            ims = []
            if mlen:
                fo = self._buf_off(mdata, mlen * 8)
                if fo is None:
                    return False
                buf, base = fo
                for i in range(mlen):
                    noff, toff = IMETHOD.unpack_from(buf, base + i * 8)
                    if self.name_at_off(noff) is None or not (0 < toff < size) or toff % 8:
                        return False
                    ims.append((noff, toff))
                    refs.append(self.gt_va + toff)
            t["_imethods_raw"] = ims
        else:
            pass  # basic kinds, string, unsafe.Pointer
        # UncommonType
        t["uncommon"] = None
        if t["tflag"] & TFLAG_UNCOMMON:
            if k == K_MAP:
                un = None
                for end in MAP_PAYLOAD_END_CANDIDATES:
                    un = self._parse_uncommon(p + end)
                    if un is not None:
                        break
            else:
                end = KIND_PAYLOAD_END.get(k, 48)
                un = self._parse_uncommon(p + end)
            if un is None:
                return False
            t["uncommon"] = un
        elif k == K_FUNC:
            pass
        if k == K_FUNC:
            base = p + 56 + (16 if t["uncommon"] else 0)
            n = t["in_count"] + t["out_count"]
            if base + 8 * n > size:
                return False
            params = []
            for i in range(n):
                v = U64.unpack_from(d, base + 8 * i)[0]
                if not self._va_ok(v):
                    return False
                params.append(v)
            t["params"] = params
            refs += params
        t["refs"] = refs
        return True

    def _buf_off(self, va: int, nbytes: int) -> Optional[tuple[Any, int]]:
        if va == 0:
            return None
        if self.gt_va <= va and va + nbytes <= self.gt_end:
            return self.d, va - self.gt_va
        off = self.img.va2off(va)
        if off is None or off + nbytes > len(self.img.m):
            return None
        return self.img.m, off

    def _parse_uncommon(self, pos: int) -> Optional[dict[str, Any]]:
        d = self.d
        if pos + 16 > self.gt_size:
            return None
        pkg_off, mcount, xcount, moff, _ = struct.unpack_from("<iHHII", d, pos)
        if xcount > mcount or mcount > 8000:
            return None
        pkg = ""
        if pkg_off != 0:
            nm = self.name_at_off(pkg_off)
            if nm is None or not nm["name"]:
                return None
            pkg = nm["name"]
            if not pkg.isprintable():
                return None
        methods: list[tuple[int, int, int, int]] = []
        if mcount:
            mpos = pos + moff
            if moff == 0 or mpos + 16 * mcount > self.gt_size:
                return None
            for i in range(mcount):
                methods.append(METHOD.unpack_from(d, mpos + 16 * i))
            for noff, _m, _i, _t in methods:
                if self.name_at_off(noff) is None:
                    return None
        return {"pkg_path": pkg, "pkg_off": pkg_off, "mcount": mcount, "xcount": xcount,
                "moff": moff, "pos": pos, "_methods_raw": methods}

    def _prune(self, cands: dict[int, dict[str, Any]]) -> None:
        """Drop candidates that reference something that is not itself a type."""
        gt_va = self.gt_va
        changed = True
        rounds = 0
        dropped = 0
        while changed:
            changed = False
            rounds += 1
            for off in list(cands):
                t = cands[off]
                for r in t["refs"]:
                    if (r - gt_va) not in cands:
                        del cands[off]
                        dropped += 1
                        changed = True
                        break
        self.stats["pruned"] = dropped
        self.stats["prune_rounds"] = rounds

    def _resolve_overlaps(self) -> None:
        """Count (not delete) candidates lying inside another candidate's payload."""
        offs = sorted(self.types)
        overlaps = 0
        prev_end = 0
        for off in offs:
            t = self.types[off]
            if off < prev_end:
                overlaps += 1
            end = off + KIND_PAYLOAD_END.get(t["kind"], 48)
            prev_end = max(prev_end, end)
        self.stats["overlaps"] = overlaps

    # -- accessors --------------------------------------------------------- #

    def type_at_va(self, va: int) -> Optional[dict[str, Any]]:
        return self.types.get(va - self.gt_va)

    def type_at_off(self, off: int) -> Optional[dict[str, Any]]:
        return self.types.get(off)

    def va_of(self, t: dict[str, Any]) -> int:
        return self.gt_va + t["off"]

    def pkg_path_of(self, t: dict[str, Any]) -> str:
        un = t.get("uncommon")
        if un and un["pkg_path"]:
            return un["pkg_path"]
        if t["kind"] in (K_STRUCT, K_INTERFACE) and t.get("pkg_va"):
            nm = self.name_at_va(t["pkg_va"])
            if nm and nm["name"]:
                return nm["name"]
        return ""

    def is_named(self, t: dict[str, Any]) -> bool:
        return bool(t["tflag"] & TFLAG_NAMED)

    # -- type expressions -------------------------------------------------- #

    def func_sig(self, ft: dict[str, Any], own_pkg: str = "") -> str:
        params = [self.type_at_va(v) for v in ft["params"]]
        ins = params[: ft["in_count"]]
        outs = params[ft["in_count"]:]

        def nm(t: Optional[dict[str, Any]]) -> str:
            return self.q(t["own"], own_pkg) if t else "?"

        in_s = [nm(t) for t in ins]
        if ft["variadic"] and in_s and in_s[-1].startswith("[]"):
            in_s[-1] = "..." + in_s[-1][2:]
        res = "(" + ", ".join(in_s) + ")"
        out_s = [nm(t) for t in outs]
        if len(out_s) == 1:
            res += " " + out_s[0]
        elif out_s:
            res += " (" + ", ".join(out_s) + ")"
        return res

    @staticmethod
    def q(expr: str, own_pkg: str) -> str:
        """Strip the package qualifier of the package currently being emitted."""
        if not own_pkg:
            return expr
        return re.sub(r"(?<![\w.\"/])" + re.escape(own_pkg) + r"\.(?=[A-Za-z_])", "", expr)

    def underlying_expr(self, t: dict[str, Any], own_pkg: str = "") -> str:
        k = t["kind"]
        e = lambda va: self._elem_expr(va, own_pkg)  # noqa: E731
        if k in BASIC_GO:
            return BASIC_GO[k]
        if k == K_PTR:
            return "*" + e(t["elem"])
        if k == K_SLICE:
            return "[]" + e(t["elem"])
        if k == K_ARRAY:
            return f"[{t['len']}]" + e(t["elem"])
        if k == K_MAP:
            return f"map[{e(t['key'])}]{e(t['elem'])}"
        if k == K_CHAN:
            pre = {1: "<-chan ", 2: "chan<- ", 3: "chan "}[t["dir"]]
            return pre + e(t["elem"])
        if k == K_FUNC:
            return "func" + self.func_sig(t, own_pkg)
        if k == K_STRUCT:
            return "struct{ /* %d fields */ }" % t["nfields"]
        if k == K_INTERFACE:
            return "interface{ /* %d methods */ }" % t["nmethods_iface"]
        return "?"

    def _elem_expr(self, va: int, own_pkg: str) -> str:
        et = self.type_at_va(va)
        return self.q(et["own"], own_pkg) if et else "?"

    # -- decoded members ---------------------------------------------------- #

    def fields_of(self, t: dict[str, Any]) -> list[dict[str, Any]]:
        out = []
        for nva, tva, off in t["_fields_raw"]:
            nm = self.name_at_va(nva) or {}
            ft = self.type_at_va(tva)
            out.append({
                "name": nm.get("name", "?"),
                "type": ft["own"] if ft else "?",
                "type_va": tva,
                "offset": off,
                "size": ft["size"] if ft else None,
                "tag": nm.get("tag", ""),
                "embedded": bool(nm.get("embedded")),
                "exported": bool(nm.get("exported")),
            })
        return out

    def iface_methods_of(self, t: dict[str, Any], own_pkg: str = "") -> list[dict[str, Any]]:
        out = []
        for noff, toff in t["_imethods_raw"]:
            nm = self.name_at_off(noff)
            ft = self.type_at_off(toff)
            out.append({
                "name": nm["name"] if nm else "?",
                "exported": bool(nm and nm["exported"]),
                "sig": self.func_sig(ft, own_pkg) if ft and ft["kind"] == K_FUNC else "?",
            })
        return out

    def methods_of(self, t: dict[str, Any], own_pkg: str = "") -> list[dict[str, Any]]:
        un = t.get("uncommon")
        if not un:
            return []
        out = []
        for noff, mtyp, ifn, tfn in un["_methods_raw"]:
            nm = self.name_at_off(noff)
            ft = self.type_at_off(mtyp) if 0 < mtyp < self.gt_size else None
            out.append({
                "name": nm["name"] if nm else "?",
                "exported": bool(nm and nm["exported"]),
                "sig": self.func_sig(ft, own_pkg) if ft and ft["kind"] == K_FUNC else None,
                "ifn": (self.text_va + ifn) if ifn >= 0 else None,
                "tfn": (self.text_va + tfn) if tfn >= 0 else None,
            })
        return out


# --------------------------------------------------------------------------- #
# Package grouping & code generation
# --------------------------------------------------------------------------- #


def is_target_pkg(pkg_path: str) -> bool:
    low = pkg_path.lower()
    if "jetski" in low or "antigravity" in low:
        return True
    for comp in low.split("/"):
        if comp == "exa" or comp.startswith("exa_") or comp.startswith("exa-"):
            return True
    return False


def go_ident(name: str, va: int, dup: bool) -> str:
    base = name
    if not IDENT_RE.match(base):
        base = re.sub(r"[^A-Za-z0-9_]", "_", base)
        if not base or not (base[0].isalpha() or base[0] == "_"):
            base = "_" + base
        base += f"_G{va & 0xFFFFFF:06x}"
    elif dup:
        base += f"_{va & 0xFFFFFF:06x}"
    return base


def split_named(own: str) -> tuple[str, str]:
    """'nativeswarm.Member' -> ('nativeswarm', 'Member'); 'int' -> ('', 'int')."""
    if own.startswith("go:"):
        return "", own
    i = own.find(".")
    if i < 0:
        return "", own
    return own[:i], own[i + 1:]


class Recovery:
    def __init__(self, tbl: GoTypeTable, func_syms: dict[int, str]):
        self.tbl = tbl
        self.syms = func_syms
        self.pkgs: dict[str, dict[str, Any]] = {}
        self.named: list[dict[str, Any]] = []

    def group(self) -> None:
        tbl = self.tbl
        by_pkg: dict[str, list[dict[str, Any]]] = defaultdict(list)
        anon_by_pkg: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for t in tbl.types.values():
            if not tbl.is_named(t):
                if t["kind"] in (K_STRUCT, K_INTERFACE) and t.get("pkg_va"):
                    ap = tbl.pkg_path_of(t)
                    if ap:
                        va = tbl.va_of(t)
                        t["_pkg_path"] = ap
                        t["_pkg_name"] = ""
                        t["_local"] = f"anon_{va & 0xFFFFFF:06x}"
                        t["_ident"] = t["_local"]
                        t["_anonymous"] = True
                        anon_by_pkg[ap].append(t)
                continue
            pkg = tbl.pkg_path_of(t)
            pname, lname = split_named(t["own"])
            t["_pkg_path"] = pkg
            t["_pkg_name"] = pname
            t["_local"] = lname
            by_pkg[pkg].append(t)
        for pkg, ts in by_pkg.items():
            names = Counter(t["_pkg_name"] for t in ts if t["_pkg_name"])
            pkg_name = names.most_common(1)[0][0] if names else (pkg.rsplit("/", 1)[-1] or "builtin")
            self.pkgs[pkg] = {"pkg_path": pkg, "pkg_name": pkg_name, "anon": [],
                              "types": sorted(ts, key=lambda x: (x["_local"], x["off"]))}
            dup = Counter(t["_local"] for t in ts)
            for t in ts:
                t["_ident"] = go_ident(t["_local"], tbl.va_of(t), dup[t["_local"]] > 1)
        for ap, ats in anon_by_pkg.items():
            if ap not in self.pkgs:
                self.pkgs[ap] = {"pkg_path": ap, "pkg_name": ap.rsplit("/", 1)[-1], "anon": [], "types": []}
            self.pkgs[ap]["anon"] = sorted(ats, key=lambda x: x["off"])
        # A named type belongs to the package in UncommonType.PkgPath, but StructType/InterfaceType.PkgPath
        # (declaring package of the unexported members) may differ; remember both for target filtering.
        for t in tbl.types.values():
            t["_alt_pkg"] = ""
            if t["kind"] in (K_STRUCT, K_INTERFACE) and t.get("pkg_va") and tbl.is_named(t):
                nm = tbl.name_at_va(t["pkg_va"])
                if nm and nm["name"] != t.get("_pkg_path"):
                    t["_alt_pkg"] = nm["name"]

    # -- Go source emission ----------------------------------------------- #

    def _find_ptr_type(self, t: dict[str, Any]) -> Optional[dict[str, Any]]:
        ptt = t["ptr_to_this"]
        if not ptt:
            return None
        pt = self.tbl.type_at_off(ptt)
        if pt and pt["kind"] == K_PTR and pt.get("elem") == self.tbl.va_of(t):
            return pt
        return None

    def method_set(self, t: dict[str, Any], own_pkg: str) -> list[dict[str, Any]]:
        """Methods recorded on T plus pointer-receiver methods recorded on *T."""
        tbl = self.tbl
        val = tbl.methods_of(t, own_pkg)
        for m in val:
            m["recv"] = "value"
        pt = self._find_ptr_type(t)
        if pt:
            have = {m["name"] for m in val}
            for m in tbl.methods_of(pt, own_pkg):
                if m["name"] not in have:
                    m["recv"] = "ptr"
                    val.append(m)
        for m in val:
            m["tfn_sym"] = self.syms.get(m["tfn"]) if m["tfn"] else None
            m["ifn_sym"] = self.syms.get(m["ifn"]) if m["ifn"] else None
        return val

    @staticmethod
    def _fmt_va(v: Optional[int]) -> str:
        return "pruned" if v is None else f"0x{v:x}"

    def emit_method_comments(self, t: dict[str, Any], ms: list[dict[str, Any]], ident: str) -> list[str]:
        lines = []
        if not ms:
            return lines
        lines.append(f"// Recovered method set of {ident} ({len(ms)} methods; Ifn/Tfn from UncommonType):")
        for m in ms:
            recv = f"(*{ident})" if m["recv"] == "ptr" else f"({ident})"
            sym = m.get("tfn_sym") or m.get("ifn_sym") or ""
            sym_s = f" | {sym.rsplit('/', 1)[-1]}" if sym else ""
            sig = m["sig"] if m["sig"] is not None else " /* signature pruned by linker (Mtyp=-1) */"
            lines.append(
                f"//   func {recv} {m['name']}{sig}  // Ifn={self._fmt_va(m['ifn'])} Tfn={self._fmt_va(m['tfn'])}{sym_s}"
            )
        return lines

    def emit_type(self, t: dict[str, Any], pkg_name: str) -> str:
        tbl = self.tbl
        va = tbl.va_of(t)
        ident = t["_ident"]
        k = t["kind"]
        lines: list[str] = []
        lines.append(
            f"// Binary VA: 0x{va:x} | Size: {t['size']} B | Kind: {KIND_NAMES[k]} | "
            f"Align: {t['align']} | PtrBytes: {t['ptrbytes']} | Hash: 0x{t['hash']:08x} | Str: {t['own']}"
        )
        if ident != t["_local"]:
            lines.append(f"// NOTE: identifier sanitised / disambiguated from reflected name {t['_local']!r}")
        ms = self.method_set(t, pkg_name)
        if k == K_STRUCT:
            fields = tbl.fields_of(t)
            lines.append(f"type {ident} struct {{")
            for f in fields:
                ty = tbl.q(f["type"], pkg_name)
                tag = f" `{f['tag']}`" if f["tag"] else ""
                if "`" in f["tag"]:
                    tag = " " + json.dumps(f["tag"])
                size = f"size={f['size']}" if f["size"] is not None else "size=?"
                cmt = f"// offset=0x{f['offset']:x} {size}"
                if f["embedded"]:
                    lines.append(f"\t{ty}{tag} {cmt} (embedded)")
                else:
                    lines.append(f"\t{f['name']} {ty}{tag} {cmt}")
            lines.append("}")
        elif k == K_INTERFACE:
            lines.append(f"type {ident} interface {{")
            for m in tbl.iface_methods_of(t, pkg_name):
                lines.append(f"\t{m['name']}{m['sig']}")
            lines.append("}")
        else:
            lines.append(f"type {ident} {tbl.underlying_expr(t, pkg_name)}")
        lines += self.emit_method_comments(t, ms, ident)
        return "\n".join(lines) + "\n"

    def emit_package(self, pkg: dict[str, Any]) -> str:
        tbl = self.tbl
        ts = pkg["types"]
        hdr = [
            "// Code generated by cli/scripts/recover-gotypes.py from /Users/mac/.local/bin/agy (v1.5.10). DO NOT EDIT.",
            "//",
            "// Reconstructed from __DATA_CONST.__go_type reflection data (abi.Type / UncommonType / abi.Name).",
            "// Declarations are *recovered*, not original source: field order, offsets, sizes, JSON/YAML tags and",
            "// method signatures are exact; imports are omitted, so this file documents rather than compiles.",
            f"// Package path: {pkg['pkg_path'] or '(builtin)'}",
            f"// Types: {len(ts)}  (structs={sum(1 for t in ts if t['kind'] == K_STRUCT)}, "
            f"interfaces={sum(1 for t in ts if t['kind'] == K_INTERFACE)}), anonymous struct/interface types: {len(pkg['anon'])}",
        ]
        refs: dict[str, str] = {}
        for t in ts:
            for r in self._ref_closure(t):
                rp = tbl.pkg_path_of(r)
                rn, _ = split_named(r["own"])
                if rp and rn and rp != pkg["pkg_path"]:
                    refs.setdefault(rn, rp)
        if refs:
            hdr.append("//")
            hdr.append("// Referenced packages (qualifier -> package path):")
            for rn in sorted(refs):
                hdr.append(f"//   {rn} = {refs[rn]}")
        out = ["\n".join(hdr), "", f"package {self._pkg_ident(pkg['pkg_name'])}", ""]
        for t in ts:
            out.append(self.emit_type(t, pkg["pkg_name"]))
        if pkg["anon"]:
            out.append(
                "// ---- Anonymous (unnamed) struct/interface types whose unexported members belong to this package ----\n"
                "// Reflected `Str` is shown in the comment; identifiers are synthetic."
            )
            for t in pkg["anon"]:
                out.append(f"// Str: {t['own']}")
                out.append(self.emit_type(t, pkg["pkg_name"]))
        return "\n".join(out)

    @staticmethod
    def _pkg_ident(name: str) -> str:
        n = re.sub(r"[^A-Za-z0-9_]", "_", name)
        return n if n and (n[0].isalpha() or n[0] == "_") else "p_" + n

    def _ref_closure(self, t: dict[str, Any], limit: int = 64) -> list[dict[str, Any]]:
        """Named types directly referenced by a type (through composite wrappers)."""
        tbl = self.tbl
        seen: set[int] = set()
        out: list[dict[str, Any]] = []
        stack: list[int] = []
        if t["kind"] == K_STRUCT:
            stack = [tva for _, tva, _ in t["_fields_raw"]]
        elif t["kind"] == K_INTERFACE:
            stack = []
            for _, toff in t["_imethods_raw"]:
                ft = tbl.type_at_off(toff)
                if ft:
                    stack += ft.get("params", [])
        else:
            stack = list(t["refs"])
        while stack and len(seen) < limit * 4:
            va = stack.pop()
            if va in seen:
                continue
            seen.add(va)
            r = tbl.type_at_va(va)
            if r is None:
                continue
            if tbl.is_named(r):
                out.append(r)
            else:
                stack += r["refs"]
        return out

    # -- JSON ----------------------------------------------------------------- #

    def type_detail(self, t: dict[str, Any], pkg_name: str) -> dict[str, Any]:
        tbl = self.tbl
        k = t["kind"]
        d: dict[str, Any] = {
            "va": f"0x{tbl.va_of(t):x}",
            "name": t["own"],
            "ident": t["_ident"],
            "pkg_path": t["_pkg_path"],
            "kind": KIND_NAMES[k],
            "size": t["size"],
            "align": t["align"],
            "ptrbytes": t["ptrbytes"],
            "hash": f"0x{t['hash']:08x}",
            "tflag": t["tflag"],
        }
        if t.get("_anonymous"):
            d["anonymous"] = True
        pt = self._find_ptr_type(t)
        if pt:
            d["ptr_type_va"] = f"0x{tbl.va_of(pt):x}"
        if k == K_STRUCT:
            d["fields"] = [
                {
                    "name": f["name"], "type": f["type"], "type_va": f"0x{f['type_va']:x}",
                    "offset": f["offset"], "size": f["size"],
                    **({"tag": f["tag"]} if f["tag"] else {}),
                    **({"embedded": True} if f["embedded"] else {}),
                }
                for f in tbl.fields_of(t)
            ]
        elif k == K_INTERFACE:
            d["methods"] = [{"name": m["name"], "sig": m["sig"]} for m in tbl.iface_methods_of(t)]
        else:
            d["underlying"] = tbl.underlying_expr(t)
        ms = self.method_set(t, "")
        if ms:
            d["method_set"] = [
                {
                    "name": m["name"], "recv": m["recv"], "sig": m["sig"],
                    "ifn": f"0x{m['ifn']:x}" if m["ifn"] else None,
                    "tfn": f"0x{m['tfn']:x}" if m["tfn"] else None,
                    **({"symbol": m["tfn_sym"]} if m.get("tfn_sym") else {}),
                }
                for m in ms
            ]
        return d


# --------------------------------------------------------------------------- #
# Driver
# --------------------------------------------------------------------------- #


def load_func_symbols(cli_root: Path) -> dict[int, str]:
    path = cli_root / "metadata" / "jetski-functions.jsonl"
    syms: dict[int, str] = {}
    if not path.exists():
        return syms
    with path.open("r", encoding="utf-8") as fh:
        for line in fh:
            try:
                rec = json.loads(line)
                syms[int(rec["vmaddr"], 16)] = rec["name"]
            except (ValueError, KeyError):
                continue
    return syms


def load_layout(cli_root: Path) -> dict[str, Any]:
    return json.loads((cli_root / "metadata" / "macho-segments.json").read_text())


def build(args: argparse.Namespace) -> int:
    cli_root = Path(args.cli_root).resolve()
    binary = Path(args.binary)
    t0 = time.time()
    layout = load_layout(cli_root)
    img = Image(binary, layout)
    sec = img.sections[("__DATA_CONST", "__go_type")]
    text = img.sections[("__TEXT", "__text")]
    tbl = GoTypeTable(img, sec["vmaddr_int"], sec["fileoff"], sec["size"], text["vmaddr_int"])
    tbl.scan()
    print(
        f"[scan] {len(tbl.types)} types validated "
        f"(raw candidates {tbl.stats['raw_candidates']}, pruned {tbl.stats['pruned']} "
        f"in {tbl.stats['prune_rounds']} rounds, overlaps {tbl.stats['overlaps']}) "
        f"in {time.time() - t0:.1f}s"
    )
    if args.stats:
        by_kind = Counter(KIND_NAMES[t["kind"]] for t in tbl.types.values())
        print(json.dumps({"by_kind": by_kind, "tflags": tbl.stats["tflag_hist"]}, indent=1, default=dict))
        return 0

    syms = load_func_symbols(cli_root)
    rec = Recovery(tbl, syms)
    rec.group()

    if args.dump:
        return dump_types(rec, args.dump)

    def pkg_is_target(p: str, v: dict[str, Any]) -> bool:
        if is_target_pkg(p):
            return True
        # named type declared elsewhere but whose struct/interface PkgPath points into a target package
        return any(is_target_pkg(t.get("_alt_pkg", "")) for t in v["types"])

    target_pkgs = {p: v for p, v in rec.pkgs.items() if (args.all_packages or pkg_is_target(p, v))}
    if args.pkg_regex:
        rx = re.compile(args.pkg_regex)
        target_pkgs = {p: v for p, v in target_pkgs.items() if rx.search(p)}

    # ---- Go sources ----
    out_root = cli_root / "reconstructed" / "go-types"
    expected = (cli_root / "reconstructed" / "go-types").resolve()
    if out_root.resolve() != expected:
        raise RuntimeError("refusing to write outside reconstructed/go-types")
    if out_root.exists():
        shutil.rmtree(out_root)
    out_root.mkdir(parents=True, exist_ok=True)
    files_written = 0
    for pkg_path, pkg in sorted(target_pkgs.items()):
        if not pkg["types"] and not pkg["anon"]:
            continue
        rel = pkg_path if pkg_path else "builtin"
        # defend against odd characters in package paths
        rel = re.sub(r"[^A-Za-z0-9_./\-@+]", "_", rel).strip("/")
        d = out_root / rel
        d.mkdir(parents=True, exist_ok=True)
        (d / "types_recovered.go").write_text(rec.emit_package(pkg), encoding="utf-8")
        pkg["_go_file"] = f"reconstructed/go-types/{rel}/types_recovered.go"
        files_written += 1

    # ---- JSON ----
    all_types = tbl.types.values()
    by_kind = Counter(KIND_NAMES[t["kind"]] for t in all_types)
    va_to_type: dict[str, dict[str, Any]] = {}
    for off in sorted(tbl.types):
        t = tbl.types[off]
        e: dict[str, Any] = {"n": t["own"], "k": KIND_NAMES[t["kind"]], "s": t["size"]}
        if t["tflag"] & TFLAG_NAMED:
            e["p"] = t.get("_pkg_path", "")
        elif t["kind"] in (K_PTR, K_SLICE, K_ARRAY, K_CHAN) and t.get("elem"):
            e["e"] = f"0x{t['elem']:x}"
        va_to_type[f"0x{tbl.gt_va + off:x}"] = e

    packages_json: dict[str, Any] = {}
    detail: dict[str, Any] = {}
    type_index: dict[str, str] = {}
    tot = Counter()
    for pkg_path, pkg in sorted(target_pkgs.items()):
        entries = []
        c = Counter()
        for t in pkg["types"] + pkg["anon"]:
            d = rec.type_detail(t, pkg["pkg_name"])
            va_s = d["va"]
            detail[va_s] = d
            if not t.get("_anonymous"):
                type_index[f"{pkg_path}.{t['_local']}"] = va_s
            else:
                c["anonymous"] += 1
            c[d["kind"]] += 1
            if d["kind"] == "struct":
                c["fields"] += len(d["fields"])
            nm = len(d.get("method_set", [])) + (len(d["methods"]) if d["kind"] == "interface" else 0)
            c["methods"] += nm
            entries.append({
                "name": t["_local"], "ident": t["_ident"], "va": va_s, "kind": d["kind"], "size": t["size"],
                **({"fields": len(d["fields"])} if d["kind"] == "struct" else {}),
                **({"methods": nm} if nm else {}),
                **({"anonymous": True} if t.get("_anonymous") else {}),
            })
        tot.update(c)
        packages_json[pkg_path] = {
            "pkg_name": pkg["pkg_name"],
            "go_file": pkg.get("_go_file"),
            "type_count": len(pkg["types"]),
            "anonymous_count": len(pkg["anon"]),
            "counts": dict(c),
            "types": entries,
        }

    sym_total = 0
    sym_match = 0
    for dd in detail.values():
        for m in dd.get("method_set", []):
            if m.get("symbol"):
                sym_total += 1
                if m["symbol"].endswith("." + m["name"]):
                    sym_match += 1

    all_pkg_counts = {p: len(v["types"]) for p, v in sorted(rec.pkgs.items())}
    info = {}
    info_path = cli_root / "bin" / "agy-info.json"
    if info_path.exists():
        info = json.loads(info_path.read_text())
    summary = {
        "total_types": len(tbl.types),
        "by_kind": dict(by_kind),
        "named_types": sum(1 for t in all_types if t["tflag"] & TFLAG_NAMED),
        "total_packages": len(rec.pkgs),
        "target_packages": len(packages_json),
        "target_named_types": sum(p["type_count"] for p in packages_json.values()),
        "target_anonymous_types": tot["anonymous"],
        "target_structs": tot["struct"],
        "target_interfaces": tot["interface"],
        "target_other_named": sum(p["type_count"] for p in packages_json.values())
        - (tot["struct"] + tot["interface"] - tot["anonymous"]),
        "target_struct_fields": tot["fields"],
        "target_methods": tot["methods"],
        "go_files_written": files_written,
        "raw_candidates": tbl.stats["raw_candidates"],
        "pruned_candidates": tbl.stats["pruned"],
        "overlapping_candidates": tbl.stats["overlaps"],
        "methods_resolved_to_jetski_symbol": sym_total,
        "methods_symbol_name_matches": sym_match,
    }
    meta = {
        "generator": "cli/scripts/recover-gotypes.py",
        "binary": str(binary),
        "version": info.get("version"),
        "sha256": info.get("sha256"),
        "go_version": (info.get("go_buildinfo") or {}).get("go_version"),
        "go_type_section": {
            "vmaddr": f"0x{tbl.gt_va:x}", "fileoff": tbl.gt_off, "size": tbl.gt_size,
            "end_vmaddr": f"0x{tbl.gt_end:x}",
        },
        "text_vmaddr": f"0x{tbl.text_va:x}",
        "notes": {
            "va_to_type": "key = absolute VA of abi.Type; n = Go type string (extra '*' stripped), k = kind, "
                          "s = size, p = package path (named types), e = elem VA (ptr/slice/array/chan)",
            "types": "full detail (fields with offsets/tags, interface methods, method sets with Ifn/Tfn VAs) for "
                     "named types of jetski/antigravity/exa packages, keyed by VA",
            "type_index": "'<pkg_path>.<TypeName>' -> VA (keys of 'types')",
        },
    }
    result = {
        "meta": meta,
        "summary": summary,
        "kind_counts": dict(by_kind),
        "package_type_counts": all_pkg_counts,
        "packages": packages_json,
        "type_index": type_index,
        "types": detail,
        "va_to_type": va_to_type,
    }
    out_json = cli_root / "metadata" / "recovered-gotypes.json"
    out_json.write_text(json.dumps(result, indent=None, separators=(",", ":"), ensure_ascii=False), encoding="utf-8")
    print(json.dumps(summary, indent=2))
    print(f"[done] wrote {out_json} ({out_json.stat().st_size / 1e6:.1f} MB) and {files_written} Go files "
          f"in {time.time() - t0:.1f}s")
    img.close()
    return 0


def dump_types(rec: Recovery, needle: str) -> int:
    tbl = rec.tbl
    shown = 0
    for t in sorted(tbl.types.values(), key=lambda x: x["off"]):
        if not tbl.is_named(t) or needle not in t["own"] and needle not in t.get("_pkg_path", ""):
            continue
        if t["kind"] != K_STRUCT and t["kind"] != K_INTERFACE:
            continue
        va = tbl.va_of(t)
        print(f"== {t['own']} @0x{va:x} (section off 0x{t['off']:x}) pkg={t['_pkg_path']} size={t['size']} "
              f"tflag=0x{t['tflag']:x}")
        print("   raw header:", tbl.d[t["off"]:t["off"] + 48].hex())
        if t["kind"] == K_STRUCT:
            for f in tbl.fields_of(t):
                print(f"   +0x{f['offset']:<4x} {f['name']:<28} {f['type']:<40} {f['tag']!r}")
        else:
            for m in tbl.iface_methods_of(t):
                print(f"   {m['name']}{m['sig']}")
        shown += 1
        if shown >= 20:
            break
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--binary", default=str(DEFAULT_BINARY))
    ap.add_argument("--cli-root", default=str(CLI_ROOT))
    ap.add_argument("--stats", action="store_true", help="scan and print statistics only")
    ap.add_argument("--dump", metavar="SUBSTR", help="print raw+decoded view of matching structs/interfaces")
    ap.add_argument("--all-packages", action="store_true",
                    help="also emit Go files for every package (stdlib / third-party); default: jetski/antigravity/exa")
    ap.add_argument("--pkg-regex", help="restrict output packages to those whose path matches this regex")
    args = ap.parse_args()
    return build(args)


if __name__ == "__main__":
    sys.exit(main())
