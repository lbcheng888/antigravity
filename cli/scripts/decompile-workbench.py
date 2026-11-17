#!/usr/bin/env python3
"""ARM64 static decompilation workbench for /Users/mac/.local/bin/agy (Go, Mach-O arm64).

Python 3 stdlib + /usr/bin/objdump only.  Nothing in the binary is modified.

What it does
------------
* Parses *all* functions of ``gopclntab`` (runtime / stdlib / third-party / jetski) so that every
  ``bl 0x...`` resolves to a real symbol name (``runtime.newobject``, ``fmt.Sprintf`` ...).
* Recovers the ``pcfile`` / ``pcln`` / ``PCDATA_InlTreeIndex`` state machines so every instruction
  maps to an exact source file, line number and (if inlined) the inlined callee chain.
* Builds a cross-package Caller/Callee callgraph for jetski code (``--build-callgraph``).
* Annotates ``adrp``+``add`` pairs: strings in ``__TEXT.__rodata`` are sliced with the adjacent
  length register (``mov``/``orr``/``movz``), types in ``__DATA_CONST.__go_type`` are resolved via
  ``recovered-gotypes.json`` when present and by parsing ``abi.Type`` / ``abi.Name`` otherwise,
  itabs / funcvals / globals are classified as well.
* Emits basic-block annotated pseudo-Go listings for ``--dump-func`` / ``--dump-package``.

CLI
---
  decompile-workbench.py --build-callgraph
  decompile-workbench.py --dump-func <substr|0xVA> [--limit N] [--pseudo-only]
  decompile-workbench.py --dump-package <pkg_substr> --out <dir> [--limit N]
"""

from __future__ import annotations

import argparse
import bisect
import json
import mmap
import os
import re
import struct
import subprocess
import sys
import time
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Iterable

DEFAULT_BINARY = Path("/Users/mac/.local/bin/agy")
CLI_ROOT = Path(__file__).resolve().parent.parent
META_DIR = CLI_ROOT / "metadata"
OBJDUMP = "/usr/bin/objdump"
CALLGRAPH_PATH = META_DIR / "callgraph-summary.json"
GOTYPES_PATH = META_DIR / "recovered-gotypes.json"
SCHEMA_VERSION = 1

KIND_NAMES = {
    1: "bool", 2: "int", 3: "int8", 4: "int16", 5: "int32", 6: "int64", 7: "uint", 8: "uint8",
    9: "uint16", 10: "uint32", 11: "uint64", 12: "uintptr", 13: "float32", 14: "float64",
    15: "complex64", 16: "complex128", 17: "array", 18: "chan", 19: "func", 20: "interface",
    21: "map", 22: "ptr", 23: "slice", 24: "string", 25: "struct", 26: "unsafe.Pointer",
}

G_FIELDS = {0x00: "g.stack.lo", 0x08: "g.stack.hi", 0x10: "g.stackguard0", 0x18: "g.stackguard1",
            0x20: "g._panic", 0x28: "g._defer", 0x30: "g.m"}


# ---------------------------------------------------------------------------------------------
# varints / names
# ---------------------------------------------------------------------------------------------
def read_uvarint(buf: Any, pos: int) -> tuple[int, int]:
    v = 0
    shift = 0
    while True:
        b = buf[pos]
        pos += 1
        v |= (b & 0x7F) << shift
        if not (b & 0x80):
            return v, pos
        shift += 7


def split_func_name(name: str) -> tuple[str, str]:
    """'google3/x/y/pkg.(*T).M' -> ('google3/x/y/pkg', '(*T).M'); handles generics."""
    base = name.split("[", 1)[0]
    slash = base.rfind("/")
    dot = base.find(".", slash + 1)
    if dot < 0:
        return name, ""
    return name[:dot], name[dot + 1:]


def is_jetski_name(name: str) -> bool:
    # Same predicate as scripts/extract-agy.py so node sets line up with jetski-functions.jsonl.
    return "jetski" in name or "antigravity" in name


def jstr(s: str, limit: int = 160) -> str:
    extra = ""
    if len(s) > limit:
        extra = f"…(+{len(s) - limit})"
        s = s[:limit]
    return json.dumps(s, ensure_ascii=False) + extra


# ---------------------------------------------------------------------------------------------
# Mach-O
# ---------------------------------------------------------------------------------------------
class MachO:
    def __init__(self, path: Path) -> None:
        self.path = path
        self._fh = open(path, "rb")
        self.mm = mmap.mmap(self._fh.fileno(), 0, access=mmap.ACCESS_READ)
        magic, _cpu, _sub, _ft, ncmds, _sz, _fl, _res = struct.unpack_from("<IIIIIIII", self.mm, 0)
        if magic != 0xFEEDFACF:
            raise SystemExit(f"{path}: not a 64-bit little-endian Mach-O (magic {magic:#x})")
        self.segments: list[dict[str, Any]] = []
        self.sections: dict[str, dict[str, int]] = {}
        off = 32
        for _ in range(ncmds):
            cmd, cmdsize = struct.unpack_from("<II", self.mm, off)
            if cmd == 0x19:
                segname = bytes(self.mm[off + 8:off + 24]).split(b"\0", 1)[0].decode()
                vmaddr, vmsize, fileoff, filesize, _mp, _ip, nsects, _fl2 = struct.unpack_from("<QQQQIIII", self.mm, off + 24)
                self.segments.append({"name": segname, "va": vmaddr, "vmsize": vmsize, "off": fileoff, "filesize": filesize})
                so = off + 72
                for _s in range(nsects):
                    sect = bytes(self.mm[so:so + 16]).split(b"\0", 1)[0].decode()
                    saddr, ssize, soff = struct.unpack_from("<QQI", self.mm, so + 32)
                    self.sections[f"{segname}.{sect}"] = {"va": saddr, "size": ssize, "off": soff}
                    so += 80
            off += cmdsize
        self._backed = [(s["va"], s["va"] + s["filesize"], s["off"]) for s in self.segments if s["filesize"] > 0]
        self._sec_list = sorted(
            ((v["va"], v["va"] + v["size"], k) for k, v in self.sections.items()), key=lambda t: t[0])
        self._sec_starts = [t[0] for t in self._sec_list]

    def va_to_off(self, va: int) -> int | None:
        for lo, hi, fo in self._backed:
            if lo <= va < hi:
                return fo + (va - lo)
        return None

    def section_of(self, va: int) -> str | None:
        i = bisect.bisect_right(self._sec_starts, va) - 1
        if i >= 0:
            lo, hi, name = self._sec_list[i]
            if lo <= va < hi:
                return name
        return None

    def read(self, va: int, n: int) -> bytes | None:
        off = self.va_to_off(va)
        if off is None or off + n > len(self.mm):
            return None
        return bytes(self.mm[off:off + n])

    def u32(self, va: int) -> int | None:
        b = self.read(va, 4)
        return struct.unpack("<I", b)[0] if b else None

    def u64(self, va: int) -> int | None:
        b = self.read(va, 8)
        return struct.unpack("<Q", b)[0] if b else None

    def sec(self, name: str) -> dict[str, int]:
        return self.sections[name]


# ---------------------------------------------------------------------------------------------
# gopclntab
# ---------------------------------------------------------------------------------------------
class Pclntab:
    """Complete Go 1.20+ pclntab reader (all functions, files, pcfile/pcln/inline tree)."""

    def __init__(self, macho: MachO) -> None:
        self.m = macho
        mm = macho.mm
        sec = macho.sec("__TEXT.__lrodata_gopcln")
        base = sec["off"]
        magic = struct.unpack_from("<I", mm, base)[0]
        if magic != 0xFFFFFFF1:
            raise SystemExit(f"gopclntab magic mismatch at section start: {magic:#x}")
        (_magic, _pad, self.min_lc, self.ptr_size, self.nfunc, self.nfiles, text_start,
         funcname_off, cu_off, filetab_off, pctab_off, pcln_off) = struct.unpack_from("<IHBBQQQQQQQQ", mm, base)
        self.base = base
        self.text_va = macho.sec("__TEXT.__text")["va"]
        self.text_off = macho.sec("__TEXT.__text")["off"]
        self.text_size = macho.sec("__TEXT.__text")["size"]
        self.funcname_base = base + funcname_off
        self.cu_base = base + cu_off
        self.filetab_base = base + filetab_off
        self.pctab_base = base + pctab_off
        self.ftab_base = base + pcln_off
        self.text_start = text_start + self.text_va  # runtime text base used for entryOff

        # moduledata: gofunc base (funcdata) and type bounds
        self.types_va = self.etypes_va = self.gofunc_va = 0
        try:
            md = macho.sec("__DATA.__go_module")
            md_bytes = bytes(mm[md["off"]:md["off"] + md["size"]])
            # ego1.28 moduledata layout (verified on v1.5.10): +296 types (VA), +312 gofunc (VA, == __go_func),
            # +320 etypes as a *byte length* from `types` (types+len == first itab).
            self.types_va = struct.unpack_from("<Q", md_bytes, 296)[0]
            self.gofunc_va = struct.unpack_from("<Q", md_bytes, 344)[0]
            self.etypes_va = self.types_va + struct.unpack_from("<Q", md_bytes, 320)[0]
        except (KeyError, struct.error):
            pass
        gt = macho.sec("__DATA_CONST.__go_type")
        gt_end = gt["va"] + gt["size"]
        if not (gt["va"] <= self.types_va < self.etypes_va <= gt_end):
            self.types_va = gt["va"]
            self.etypes_va = gt_end
        if not (self.m.va_to_off(self.gofunc_va) is not None):
            self.gofunc_va = 0

        # function table
        self.entry_va: list[int] = []
        self._func_pos: list[int] = []
        ftab = self.ftab_base
        unpack_II = struct.Struct("<II").unpack_from
        for i in range(self.nfunc + 1):
            entry_off, func_off = unpack_II(mm, ftab + i * 8)
            self.entry_va.append(self.text_va + entry_off)
            self._func_pos.append(ftab + func_off)
        self.end_va = self.entry_va[self.nfunc]
        self.nfunc_real = self.nfunc
        self._hdr = struct.Struct("<IiiiiIIIIi")
        self._names: list[str | None] = [None] * self.nfunc
        self._va_index = {va: i for i, va in enumerate(self.entry_va[:self.nfunc])}
        self._file_cache: dict[int, str] = {}
        self._lt_cache: dict[int, "LineTable"] = {}

    # -- basic accessors --------------------------------------------------------------------
    def name(self, i: int) -> str:
        n = self._names[i]
        if n is None:
            (_e, name_off, *_rest) = self._hdr.unpack_from(self.m.mm, self._func_pos[i])
            n = self.cstr(self.funcname_base + name_off)
            self._names[i] = n
        return n

    def cstr(self, off: int) -> str:
        mm = self.m.mm
        end = mm.find(b"\x00", off)
        return bytes(mm[off:end]).decode("utf-8", "replace")

    def size(self, i: int) -> int:
        return self.entry_va[i + 1] - self.entry_va[i]

    def raw(self, i: int) -> tuple[Any, ...]:
        return self._hdr.unpack_from(self.m.mm, self._func_pos[i])

    def start_line(self, i: int) -> int:
        return self.raw(i)[9]

    def index_of_entry(self, va: int) -> int | None:
        return self._va_index.get(va)

    def index_containing(self, va: int) -> int | None:
        if not (self.entry_va[0] <= va < self.end_va):
            return None
        i = bisect.bisect_right(self.entry_va, va, 0, self.nfunc) - 1
        return i if i >= 0 else None

    def file_by_cu(self, cu_idx: int, val: int) -> str:
        key = cu_idx + val
        f = self._file_cache.get(key)
        if f is None:
            file_off = struct.unpack_from("<I", self.m.mm, self.cu_base + key * 4)[0]
            f = self.cstr(self.filetab_base + file_off)
            self._file_cache[key] = f
        return f

    def func_file(self, i: int) -> str:
        """Source file of the function entry (pcfile at entry)."""
        rng = self.pcvalue_ranges(self.raw(i)[5], self.entry_va[i], limit=1)
        if not rng or rng[0][2] < 0:
            return "<unknown>"
        return self.file_by_cu(self.raw(i)[8], rng[0][2])

    # -- state machines ---------------------------------------------------------------------
    def pcvalue_ranges(self, tab_off: int, entry_va: int, limit: int = 1 << 30) -> list[tuple[int, int, int]]:
        if tab_off == 0:
            return []
        mm = self.m.mm
        pos = self.pctab_base + tab_off
        val = -1
        pc = entry_va
        out: list[tuple[int, int, int]] = []
        first = True
        while len(out) < limit:
            uv, pos = read_uvarint(mm, pos)
            if uv == 0 and not first:
                break
            first = False
            val += (~(uv >> 1)) if (uv & 1) else (uv >> 1)
            pcd, pos = read_uvarint(mm, pos)
            nxt = pc + pcd * self.min_lc
            out.append((pc, nxt, val))
            pc = nxt
        return out

    def line_table(self, i: int) -> "LineTable":
        lt = self._lt_cache.get(i)
        if lt is None:
            lt = LineTable(self, i)
            self._lt_cache[i] = lt
        return lt

    def package(self, i: int) -> str:
        return split_func_name(self.name(i))[0]


class LineTable:
    def __init__(self, pcl: Pclntab, i: int) -> None:
        self.pcl = pcl
        m = pcl.m
        (entry_off, name_off, args, deferret, pcsp, pcfile, pcln, npcdata, cu_idx, start_line) = pcl.raw(i)
        entry = pcl.entry_va[i]
        fpos = pcl._func_pos[i]
        self.entry = entry
        self.start_line = start_line
        self.cu_idx = cu_idx
        self._file_r = pcl.pcvalue_ranges(pcfile, entry)
        self._line_r = pcl.pcvalue_ranges(pcln, entry)
        self._file_starts = [r[0] for r in self._file_r]
        self._line_starts = [r[0] for r in self._line_r]
        # inline tree: funcdata[3] + pcdata[2]
        self._inl_r: list[tuple[int, int, int]] = []
        self._inl_starts: list[int] = []
        self._inl_calls: list[tuple[int, int]] = []  # (parentPc, nameOff)
        try:
            tail = struct.unpack_from("<BBBB", m.mm, fpos + 40)  # funcID, flag, _, nfuncdata
            nfuncdata = tail[3]
            pcdata_pos = fpos + 44
            if npcdata > 2 and nfuncdata > 3 and pcl.gofunc_va:
                pcd_off = struct.unpack_from("<I", m.mm, pcdata_pos + 2 * 4)[0]
                fd_pos = pcdata_pos + npcdata * 4
                fd_off = struct.unpack_from("<I", m.mm, fd_pos + 3 * 4)[0]
                if pcd_off != 0 and fd_off != 0xFFFFFFFF:
                    self._inl_r = pcl.pcvalue_ranges(pcd_off, entry)
                    self._inl_starts = [r[0] for r in self._inl_r]
                    tree_va = pcl.gofunc_va + fd_off
                    max_idx = max((r[2] for r in self._inl_r), default=-1)
                    for k in range(max_idx + 1):
                        b = m.read(tree_va + k * 16, 16)
                        if not b:
                            break
                        _fid, noff, parent_pc, _sl = struct.unpack("<Bxxxiii", b)  # funcID,_[3],nameOff,parentPc,startLine
                        self._inl_calls.append((parent_pc, noff))
        except (struct.error, IndexError):
            self._inl_r = []

    @staticmethod
    def _look(starts: list[int], ranges: list[tuple[int, int, int]], va: int) -> int:
        j = bisect.bisect_right(starts, va) - 1
        if j >= 0 and va < ranges[j][1]:
            return ranges[j][2]
        return -1

    def at(self, va: int) -> tuple[str, int, tuple[str, ...]]:
        """(file, line, inlined-chain innermost-first) for the instruction at *va*."""
        fv = self._look(self._file_starts, self._file_r, va)
        file = self.pcl.file_by_cu(self.cu_idx, fv) if fv >= 0 else "<unknown>"
        line = self._look(self._line_starts, self._line_r, va)
        chain: list[str] = []
        idx = self._look(self._inl_starts, self._inl_r, va) if self._inl_r else -1
        guard = 0
        while 0 <= idx < len(self._inl_calls) and guard < 16:
            parent_pc, noff = self._inl_calls[idx]
            chain.append(self.pcl.cstr(self.pcl.funcname_base + noff))
            idx = self._look(self._inl_starts, self._inl_r, self.entry + parent_pc) if parent_pc >= 0 else -1
            guard += 1
        return file, line, tuple(chain)


# ---------------------------------------------------------------------------------------------
# callgraph
# ---------------------------------------------------------------------------------------------
def build_callgraph(macho: MachO, pcl: Pclntab, verbose: bool = True) -> dict[str, Any]:
    t0 = time.time()
    n = pcl.nfunc
    scope = [is_jetski_name(pcl.name(i)) for i in range(n)]
    text_lo, text_hi = pcl.text_va, pcl.text_va + pcl.text_size
    stubs = macho.sections.get("__TEXT.__stubs")
    mm = macho.mm
    edges: dict[tuple[int, Any], list[int]] = {}
    call_sites = resolved = unresolved = scanned = 0
    unresolved_targets: Counter[int] = Counter()
    for ci in range(n):
        if not scope[ci]:
            continue
        lo, hi = pcl.entry_va[ci], pcl.entry_va[ci + 1]
        if lo < text_lo or hi > text_hi:
            continue
        foff = pcl.text_off + (lo - text_lo)
        scanned += hi - lo
        pc = lo
        for (insn,) in struct.iter_unpack("<I", mm[foff:foff + (hi - lo)]):
            top = insn & 0xFC000000
            if top == 0x94000000 or top == 0x14000000:
                imm = insn & 0x03FFFFFF
                if imm & 0x02000000:
                    imm -= 0x04000000
                target = pc + imm * 4
                is_bl = top == 0x94000000
                ti = pcl._va_index.get(target)
                if is_bl:
                    call_sites += 1
                    if ti is None:
                        if stubs and stubs["va"] <= target < stubs["va"] + stubs["size"]:
                            key: Any = ("stub", target)
                        else:
                            key = ("unres", target)
                            unresolved += 1
                            unresolved_targets[target] += 1
                    else:
                        key = ti
                        resolved += 1
                    rec = edges.setdefault((ci, key), [0, 0])
                    rec[0] += 1
                elif ti is not None and ti != ci:
                    rec = edges.setdefault((ci, ti), [0, 0])
                    rec[1] += 1  # tail-call (unconditional b to another function entry)
            pc += 4

    # node table: callers with edges + all callees
    node_ids: dict[Any, int] = {}
    nodes_out: list[Any] = []
    pkgs: list[str] = []
    pkg_ids: dict[str, int] = {}

    def node_id(key: Any) -> int:
        nid = node_ids.get(key)
        if nid is not None:
            return nid
        if isinstance(key, tuple):
            kind, va = key
            pkg = "<stubs>" if kind == "stub" else "<unresolved>"
            short = f"{va:#x}"
            va_i, size, scope_flag = va, 0, "x"
        else:
            full = pcl.name(key)
            pkg, short = split_func_name(full)
            if not short:
                pkg, short = "", full
            va_i, size = pcl.entry_va[key], pcl.size(key)
            scope_flag = "j" if scope[key] else "x"
        pid = pkg_ids.get(pkg)
        if pid is None:
            pid = len(pkgs)
            pkg_ids[pkg] = pid
            pkgs.append(pkg)
        nid = len(nodes_out)
        node_ids[key] = nid
        nodes_out.append([pid, short, va_i, size, scope_flag])
        return nid

    edge_list: list[list[int]] = []
    for (ci, key), (cnt, tail) in sorted(edges.items(), key=lambda kv: (kv[0][0], str(kv[0][1]))):
        a = node_id(ci)
        b = node_id(key)
        if cnt:
            edge_list.append([a, b, cnt] + ([tail] if tail else []))
        elif tail:
            edge_list.append([a, b, 0, tail])

    fan_out: dict[int, int] = defaultdict(int)
    fan_in: dict[int, int] = defaultdict(int)
    pkg_edges: dict[tuple[int, int], int] = defaultdict(int)
    j2j = j2x = 0
    for e in edge_list:
        a, b, cnt = e[0], e[1], e[2] + (e[3] if len(e) > 3 else 0)
        fan_out[a] += 1
        fan_in[b] += 1
        pkg_edges[(nodes_out[a][0], nodes_out[b][0])] += cnt
        if nodes_out[b][4] == "j":
            j2j += 1
        else:
            j2x += 1

    def full_name(nid: int) -> str:
        pid, short = nodes_out[nid][0], nodes_out[nid][1]
        return f"{pkgs[pid]}.{short}" if pkgs[pid] and short and not pkgs[pid].startswith("<") else f"{pkgs[pid]}{short}"

    def top(d: dict[int, int], k: int = 40) -> list[list[Any]]:
        return [[full_name(nid), c] for nid, c in sorted(d.items(), key=lambda kv: (-kv[1], kv[0]))[:k]]

    pkg_summary: dict[int, dict[str, int]] = {}
    for nid, nd in enumerate(nodes_out):
        s = pkg_summary.setdefault(nd[0], {"nodes": 0, "out_edges": 0, "in_edges": 0})
        s["nodes"] += 1
        s["out_edges"] += fan_out.get(nid, 0)
        s["in_edges"] += fan_in.get(nid, 0)

    stats = {
        "binary_functions_total": n,
        "scope_functions_jetski": sum(scope),
        "scanned_code_bytes": scanned,
        "call_sites_bl": call_sites,
        "call_sites_resolved": resolved,
        "call_sites_unresolved": unresolved,
        "unique_edges": len(edge_list),
        "edges_jetski_to_jetski": j2j,
        "edges_jetski_to_external": j2x,
        "nodes": len(nodes_out),
        "packages": len(pkgs),
        "build_seconds": round(time.time() - t0, 2),
    }
    out = {
        "schema": SCHEMA_VERSION,
        "generator": "cli/scripts/decompile-workbench.py --build-callgraph",
        "binary": {"path": str(macho.path), "size": len(macho.mm), "nfunc": n, "text_va": hex(pcl.text_va)},
        "scope": "callers = functions whose name contains 'jetski' or 'antigravity'; callees = any function (runtime/stdlib/3rd-party included)",
        "format": {
            "packages": "package import path table (index = pkg id)",
            "nodes": "[pkg_id, short_name, va, size, scope('j' jetski | 'x' external)]; full name = packages[pkg_id] + '.' + short_name",
            "edges": "[caller_node, callee_node, bl_call_sites, tail_branches?]",
        },
        "stats": stats,
        "top_fan_out": top(fan_out),
        "top_fan_in": top(fan_in),
        "package_edges": [
            {"from": pkgs[a], "to": pkgs[b], "calls": c}
            for (a, b), c in sorted(pkg_edges.items(), key=lambda kv: (-kv[1], kv[0]))
        ],
        "package_summary": {pkgs[p]: s for p, s in sorted(pkg_summary.items())},
        "unresolved_targets": [[hex(t), c] for t, c in unresolved_targets.most_common(20)],
        "packages": pkgs,
        "nodes": nodes_out,
        "edges": edge_list,
    }
    if verbose:
        print(f"[callgraph] {json.dumps(stats)}", file=sys.stderr)
    return out


def write_callgraph(data: dict[str, Any], path: Path) -> None:
    keys_inline = ("packages", "nodes", "edges")
    with path.open("w", encoding="utf-8") as fh:
        fh.write("{\n")
        items = list(data.items())
        for idx, (k, v) in enumerate(items):
            comma = "," if idx < len(items) - 1 else ""
            if k in keys_inline:
                fh.write(f'"{k}":[\n')
                fh.write(",\n".join(json.dumps(x, separators=(",", ":"), ensure_ascii=False) for x in v))
                fh.write(f"\n]{comma}\n")
            elif k in ("top_fan_out", "top_fan_in", "package_edges", "package_summary", "unresolved_targets", "stats"):
                fh.write(f'"{k}":{json.dumps(v, separators=(",", ":"), ensure_ascii=False)}{comma}\n')
            else:
                fh.write(f'"{k}":{json.dumps(v, ensure_ascii=False)}{comma}\n')
        fh.write("}\n")


class CallGraph:
    """Loaded callgraph with fast va -> node lookups for Callers / Callees reports."""

    def __init__(self, data: dict[str, Any]) -> None:
        self.data = data
        self.packages: list[str] = data["packages"]
        self.nodes: list[list[Any]] = data["nodes"]
        self.callees: dict[int, list[tuple[int, int, int]]] = defaultdict(list)
        self.callers: dict[int, list[tuple[int, int, int]]] = defaultdict(list)
        for e in data["edges"]:
            tail = e[3] if len(e) > 3 else 0
            self.callees[e[0]].append((e[1], e[2], tail))
            self.callers[e[1]].append((e[0], e[2], tail))
        self.va_index = {nd[2]: i for i, nd in enumerate(self.nodes) if nd[4] != "x" or nd[3]}

    def full_name(self, nid: int) -> str:
        nd = self.nodes[nid]
        pkg = self.packages[nd[0]]
        return f"{pkg}.{nd[1]}" if pkg and nd[1] and not pkg.startswith("<") else f"{pkg}{nd[1]}"


def load_or_build_callgraph(macho: MachO, pcl: Pclntab) -> CallGraph:
    if CALLGRAPH_PATH.exists():
        try:
            data = json.loads(CALLGRAPH_PATH.read_text(encoding="utf-8"))
            b = data.get("binary", {})
            if data.get("schema") == SCHEMA_VERSION and b.get("size") == len(macho.mm) and b.get("nfunc") == pcl.nfunc:
                return CallGraph(data)
            print("[workbench] callgraph-summary.json is stale for this binary; rebuilding in memory", file=sys.stderr)
        except (OSError, ValueError, KeyError):
            print("[workbench] callgraph-summary.json unreadable; rebuilding in memory", file=sys.stderr)
    return CallGraph(build_callgraph(macho, pcl, verbose=False))


# ---------------------------------------------------------------------------------------------
# type / data database (abi.Type, abi.Name, itabs, recovered-gotypes.json)
# ---------------------------------------------------------------------------------------------
class TypeDB:
    def __init__(self, macho: MachO, pcl: Pclntab) -> None:
        self.m = macho
        self.pcl = pcl
        self.types_va = pcl.types_va
        self.etypes_va = pcl.etypes_va
        gt = macho.sec("__DATA_CONST.__go_type")
        self.gt_lo = gt["va"]
        self.gt_hi = gt["va"] + gt["size"]
        gf = macho.sections.get("__DATA_CONST.__go_func")
        self.itab_hi = gf["va"] if gf else self.gt_hi
        self._cache: dict[int, dict[str, Any] | None] = {}
        self._json_loaded = False
        self._json_map: dict[str, Any] | None = None
        self._json_types: dict[str, Any] | None = None
        self.json_hits = 0
        self.abi_hits = 0

    # -- recovered-gotypes.json ---------------------------------------------------------------
    def _load_json(self) -> None:
        if self._json_loaded:
            return
        self._json_loaded = True
        if not GOTYPES_PATH.exists():
            return
        try:
            data = json.loads(GOTYPES_PATH.read_text(encoding="utf-8"))
            m = data.get("va_to_type")
            if isinstance(m, dict):
                self._json_map = m
            t = data.get("types")
            self._json_types = t if isinstance(t, dict) else None
        except (OSError, ValueError):
            self._json_map = None

    def _json_lookup(self, va: int) -> Any:
        self._load_json()
        if not self._json_map:
            return None
        key = f"{va:#x}"
        rich = self._json_types.get(key) if self._json_types else None
        compact = self._json_map.get(key)
        if rich is None and compact is None:
            return None
        if isinstance(rich, dict):
            return rich
        return compact

    # -- abi.Name ------------------------------------------------------------------------------
    def read_name(self, va: int) -> tuple[str, str, int] | None:
        head = self.m.read(va, 12)
        if not head:
            return None
        flags = head[0]
        try:
            ln, p = read_uvarint(head, 1)
        except IndexError:
            return None
        if ln == 0 or ln > 400:
            return None
        body = self.m.read(va + p, ln + 12)
        if not body:
            return None
        name = body[:ln].decode("utf-8", "replace")
        tag = ""
        if flags & 0x2:
            tbuf = self.m.read(va + p + ln, 400)
            if tbuf:
                try:
                    tl, tp = read_uvarint(tbuf, 0)
                    tag = tbuf[tp:tp + tl].decode("utf-8", "replace")
                except IndexError:
                    pass
        return name, tag, flags

    def name_off(self, off: int) -> str | None:
        if off <= 0 or off >= self.etypes_va - self.types_va:
            return None
        r = self.read_name(self.types_va + off)
        return r[0] if r else None

    # -- classification ------------------------------------------------------------------------
    def is_type_va(self, va: int) -> bool:
        return self.types_va <= va < self.etypes_va

    def is_itab_va(self, va: int) -> bool:
        return self.etypes_va <= va < self.itab_hi

    def type_info(self, va: int, with_fields: bool = False) -> dict[str, Any] | None:
        key = va if not with_fields else -va
        if key in self._cache:
            return self._cache[key]
        info = self._type_info(va, with_fields)
        self._cache[key] = info
        return info

    def _type_info(self, va: int, with_fields: bool) -> dict[str, Any] | None:
        if not self.is_type_va(va):
            return None
        raw = self.m.read(va, 0x50)
        if not raw:
            return None
        size, ptrbytes, hashv, tflag, align, falign, kind, _eq, _gc, str_off, ptr_to_this = struct.unpack_from("<QQIBBBBQQiI", raw, 0)
        k = kind & 0x1F
        if k not in KIND_NAMES or str_off <= 0:
            return None
        nm = self.name_off(str_off)
        if nm is None or not nm.isprintable():
            return None
        name = nm[1:] if (tflag & 0x2 and nm.startswith("*")) else nm
        info: dict[str, Any] = {"va": va, "name": name, "kind": KIND_NAMES[k], "size": size, "tflag": tflag, "src": "abi"}
        self.abi_hits += 1
        jv = self._json_lookup(va)
        if jv is not None:
            self.json_hits += 1
            info["src"] = "abi+json"
            if isinstance(jv, dict):
                for jk in ("pkg_path", "pkgpath", "package"):
                    if isinstance(jv.get(jk), str) and jv[jk]:
                        info["pkg_path"] = jv[jk]
                        break
                if with_fields and isinstance(jv.get("fields"), list):
                    info["json_fields"] = jv["fields"]
        if k == 25:  # struct
            pk = self.m.u64(va + 0x30)
            if pk and "pkg_path" not in info:
                r = self.read_name(pk)
                if r:
                    info["pkg_path"] = r[0]
            if tflag & 1 and "pkg_path" not in info:
                off = self.m.u32(va + 0x50)
                if off:
                    pn = self.name_off(off)
                    if pn:
                        info["pkg_path"] = pn
            fdata, flen = struct.unpack_from("<QQ", raw, 0x38)
            info["nfields"] = flen
            if with_fields and 0 < flen <= 128:
                fields = []
                for fi in range(flen):
                    fb = self.m.read(fdata + fi * 24, 24)
                    if not fb:
                        break
                    nptr, tptr, off_emb = struct.unpack("<QQQ", fb)
                    nr = self.read_name(nptr)
                    ti = self.type_info(tptr) if self.is_type_va(tptr) else None
                    fields.append({
                        "name": nr[0] if nr else "?",
                        "tag": nr[1] if nr else "",
                        "embedded": bool(nr and nr[2] & 0x8),
                        "type": ti["name"] if ti else f"?{tptr:#x}",
                        "offset": off_emb,
                    })
                info["fields"] = fields
        elif k == 20:  # interface
            pk = self.m.u64(va + 0x30)
            mdata, mlen = struct.unpack_from("<QQ", raw, 0x38)
            info["nmethods"] = mlen
            if with_fields and 0 < mlen <= 64:
                names = []
                for mi in range(mlen):
                    mb = self.m.read(mdata + mi * 8, 8)
                    if not mb:
                        break
                    noff, _toff = struct.unpack("<II", mb)
                    names.append(self.name_off(noff) or "?")
                info["methods"] = names
        elif k in (22, 23, 17, 18):
            elem = self.m.u64(va + 0x30)
            if elem and self.is_type_va(elem):
                ei = self.type_info(elem)
                if ei:
                    info["elem"] = ei["name"]
                    info["elem_va"] = elem
        return info

    def itab_info(self, va: int) -> dict[str, Any] | None:
        head = self.m.read(va, 0x18)
        if not head:
            return None
        inter, typ, _hash = struct.unpack_from("<QQI", head, 0)
        if not (self.is_type_va(inter) and self.is_type_va(typ)):
            return None
        ii = self.type_info(inter, with_fields=True)
        ti = self.type_info(typ)
        if not ii or not ti or ii["kind"] != "interface":
            return None
        return {"iface": ii, "concrete": ti, "va": va}

    def field_at(self, type_va: int, off: int) -> str | None:
        ti = self.type_info(type_va, with_fields=True)
        if not ti or ti["kind"] != "struct":
            return None
        fields = ti.get("fields") or []
        best = None
        for f in fields:
            if f["offset"] <= off:
                best = f
            else:
                break
        if best is None:
            return None
        delta = off - best["offset"]
        return f"{ti['name']}.{best['name']}" + (f"+{delta:#x}" if delta else "") + f" ({best['type']})"

    def format_type(self, ti: dict[str, Any], brief: bool = False) -> str:
        base = f"{ti['kind']} {ti['name']}"
        if ti["kind"] == "struct":
            base += f" size={ti['size']}B fields={ti.get('nfields', '?')}"
            if not brief and ti.get("fields"):
                fl = ", ".join(f"{f['name']} {f['type']}@{f['offset']:#x}" for f in ti["fields"][:12])
                if len(ti["fields"]) > 12:
                    fl += f", …+{len(ti['fields']) - 12}"
                base += f" {{{fl}}}"
            if ti.get("pkg_path"):
                base += f" pkg={ti['pkg_path']}"
        elif ti["kind"] == "interface":
            base += f" methods={ti.get('nmethods', '?')}"
            if not brief and ti.get("methods"):
                base += " {" + ", ".join(ti["methods"][:12]) + ("…" if len(ti["methods"]) > 12 else "") + "}"
        return base


# ---------------------------------------------------------------------------------------------
# disassembly (objdump) + operand helpers
# ---------------------------------------------------------------------------------------------
INSN_RE = re.compile(r"^\s*([0-9a-f]+):\s+([0-9a-f]{8})\s+(\S+)\s*(.*)$")
SYM_SUFFIX_RE = re.compile(r"\s*<[^>]*>")
HEX_RE = re.compile(r"0x([0-9a-f]+)")


class Insn:
    __slots__ = ("va", "raw", "mn", "ops", "text")

    def __init__(self, va: int, raw: str, mn: str, ops: list[str], text: str) -> None:
        self.va = va
        self.raw = raw
        self.mn = mn
        self.ops = ops
        self.text = text


def split_ops(s: str) -> list[str]:
    out: list[str] = []
    depth = 0
    cur = []
    for ch in s:
        if ch == "[":
            depth += 1
        elif ch == "]":
            depth -= 1
        if ch == "," and depth == 0:
            out.append("".join(cur).strip())
            cur = []
        else:
            cur.append(ch)
    tail = "".join(cur).strip()
    if tail:
        out.append(tail)
    return out


def run_objdump(binary: Path, lo: int, hi: int) -> list[Insn]:
    proc = subprocess.run(
        [OBJDUMP, "-d", f"--start-address={lo:#x}", f"--stop-address={hi:#x}", str(binary)],
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, check=False)
    out: list[Insn] = []
    for line in proc.stdout.splitlines():
        m = INSN_RE.match(line)
        if not m:
            continue
        va = int(m.group(1), 16)
        if va < lo or va >= hi:
            continue
        rest = m.group(4)
        comment = ""
        if ";" in rest:
            rest, comment = rest.split(";", 1)
        rest = SYM_SUFFIX_RE.sub("", rest).strip()
        text = rest + (f"   ; {comment.strip()}" if comment.strip() else "")
        out.append(Insn(va, m.group(2), m.group(3), split_ops(rest), text))
    return out


def reg_id(op: str) -> int | None:
    """x0..x30 / w0..w30 -> 0..30 ; sp -> 31 ; xzr/wzr -> 32 ; else None."""
    if len(op) >= 2 and op[0] in "xw" and op[1:].isdigit():
        n = int(op[1:])
        return n if n <= 30 else None
    if op in ("sp", "wsp"):
        return 31
    if op in ("xzr", "wzr"):
        return 32
    return None


def parse_imm(op: str) -> int | None:
    op = op.strip()
    if not op.startswith("#"):
        return None
    try:
        return int(op[1:], 0)
    except ValueError:
        return None


MEM_RE = re.compile(r"^\[(\w+)(?:,\s*#(-?(?:0x[0-9a-f]+|\d+)))?\](!?)$")


def parse_mem(op: str) -> tuple[int, int, bool] | None:
    m = MEM_RE.match(op)
    if not m:
        return None
    base = reg_id(m.group(1))
    if base is None:
        return None
    return base, int(m.group(2), 0) if m.group(2) else 0, bool(m.group(3))


def branch_target(ins: Insn) -> int | None:
    if not ins.ops:
        return None
    m = HEX_RE.fullmatch(ins.ops[-1].strip())
    return int(m.group(1), 16) if m else None


COND_BRANCHES = ("b.eq", "b.ne", "b.hs", "b.cs", "b.lo", "b.cc", "b.mi", "b.pl", "b.vs", "b.vc", "b.hi",
                 "b.ls", "b.ge", "b.lt", "b.gt", "b.le", "b.al", "b.nv")
CMP_OPS = {"eq": "==", "ne": "!=", "hs": ">=u", "cs": ">=u", "lo": "<u", "cc": "<u", "hi": ">u", "ls": "<=u",
           "ge": ">=", "lt": "<", "gt": ">", "le": "<=", "mi": "<0", "pl": ">=0", "vs": "overflow", "vc": "!overflow"}
NO_DEST = {"cmp", "cmn", "tst", "ccmp", "ccmn", "b", "bl", "br", "blr", "ret", "nop", "str", "strb", "strh", "stur",
           "sturb", "sturh", "stp", "stnp", "dmb", "dsb", "isb", "yield", "hint", "brk", "svc", "prfm", "prfum",
           "cbz", "cbnz", "tbz", "tbnz", "stlr", "stlrb", "stlrh", "stxr", "stlxr", "stlxrb", "stlxrh", "casal", "swp"}


# value tuples: ('const', n) ('page', va) ('str', s, va, known_len) ('type', va) ('itab', va) ('func', va)
#               ('data', va, section) ('ptr', type_va) ('sp', off) ('global', va, sect) ('load', base_reg, off)
def fmt_val(v: tuple[Any, ...] | None, tdb: TypeDB, pcl: Pclntab) -> str:
    if v is None:
        return "?"
    k = v[0]
    if k == "const":
        n = v[1]
        return str(n) if -4096 < n < 4096 else f"{n:#x}"
    if k == "str":
        return jstr(v[1]) + ("" if v[3] else "~")
    if k == "type":
        ti = tdb.type_info(v[1])
        return f"type({ti['name']})" if ti else f"type@{v[1]:#x}"
    if k == "itab":
        ii = tdb.itab_info(v[1])
        return f"itab({ii['iface']['name']}←{ii['concrete']['name']})" if ii else f"itab@{v[1]:#x}"
    if k == "func":
        idx = pcl.index_of_entry(v[1])
        return f"&{pcl.name(idx)}" if idx is not None else f"&func@{v[1]:#x}"
    if k == "ptr":
        ti = tdb.type_info(v[1])
        return f"new({ti['name']})" if ti else f"new(type@{v[1]:#x})"
    if k == "sp":
        return f"&stack[{v[1]:#x}]"
    if k == "data":
        return f"&{v[2]}@{v[1]:#x}"
    if k == "global":
        return f"global@{v[1]:#x}"
    if k == "load":
        return f"*(x{v[1]}+{v[2]:#x})"
    if k == "page":
        return f"page@{v[1]:#x}"
    return str(v)


# ---------------------------------------------------------------------------------------------
# decompiler
# ---------------------------------------------------------------------------------------------
class Decompiler:
    def __init__(self, macho: MachO, pcl: Pclntab, cg: CallGraph, tdb: TypeDB) -> None:
        self.m = macho
        self.pcl = pcl
        self.cg = cg
        self.tdb = tdb
        self.rodata = macho.sec("__TEXT.__rodata")
        self.text = macho.sec("__TEXT.__text")
        self._file_cache: dict[int, str] = {}

    # -- address classification -----------------------------------------------------------------
    def classify(self, va: int, insns: list[Insn] | None = None, i: int = 0, dreg: int = 0) -> tuple[Any, ...]:
        sect = self.m.section_of(va)
        ro = self.rodata
        if ro["va"] <= va < ro["va"] + ro["size"]:
            ln = self._find_len(insns, i, dreg) if insns is not None else None
            if ln is not None and 0 <= ln <= (1 << 20):
                raw = self.m.read(va, ln) or b""
                return ("str", raw.decode("utf-8", "replace"), va, True)
            raw = self.m.read(va, 48) or b""
            run = bytearray()
            for b in raw:
                if b < 0x20 or b == 0x7F:
                    break
                run.append(b)
            return ("str", run.decode("utf-8", "replace"), va, False)
        if self.tdb.is_type_va(va):
            if self.tdb.type_info(va):
                return ("type", va)
            return ("data", va, sect or "?")
        if self.tdb.is_itab_va(va) and self.tdb.itab_info(va):
            return ("itab", va)
        ti = self.pcl.index_of_entry(va)
        if ti is not None:
            return ("func", va)
        if sect:
            if sect.endswith(".__go_func") or sect.startswith("__DATA"):
                w0 = self.m.u64(va)
                if w0 and self.pcl.index_of_entry(w0) is not None:
                    return ("data", va, f"funcval→{self.pcl.name(self.pcl.index_of_entry(w0))}")  # type: ignore[arg-type]
            return ("data", va, sect)
        return ("data", va, "?")

    def _find_len(self, insns: list[Insn], i: int, dreg: int) -> int | None:
        want = dreg + 1
        if want > 30:
            return None
        n = len(insns)

        def const_set(ins: Insn) -> tuple[int | None, int | None]:
            if ins.mn == "mov" and len(ins.ops) == 2:
                r, v = reg_id(ins.ops[0]), parse_imm(ins.ops[1])
                if r is not None and v is not None:
                    return r, v
            elif ins.mn == "orr" and len(ins.ops) == 3 and ins.ops[1] in ("xzr", "wzr"):
                r, v = reg_id(ins.ops[0]), parse_imm(ins.ops[2])
                if r is not None and v is not None:
                    return r, v
            elif ins.mn == "movz" and len(ins.ops) >= 2:
                r, v = reg_id(ins.ops[0]), parse_imm(ins.ops[1])
                if r is not None and v is not None and len(ins.ops) == 2:
                    return r, v
            return None, None

        def writes(ins: Insn) -> int | None:
            if ins.mn in NO_DEST or not ins.ops:
                return None
            return reg_id(ins.ops[0])

        def barrier(ins: Insn) -> bool:
            return ins.mn in ("bl", "blr", "b", "br", "ret") or ins.mn in COND_BRANCHES or ins.mn in ("cbz", "cbnz", "tbz", "tbnz")

        for j in range(i + 1, min(i + 9, n)):
            ins = insns[j]
            r, v = const_set(ins)
            if r == want and v is not None:
                return v
            if barrier(ins):
                break
            w = writes(ins)
            if w == dreg or w == want:
                break
        for j in range(i - 1, max(i - 7, -1), -1):
            ins = insns[j]
            if barrier(ins):
                break
            r, v = const_set(ins)
            if r == want and v is not None:
                return v
            w = writes(ins)
            if w == dreg or (w == want):
                break
        return None

    # -- function discovery ---------------------------------------------------------------------
    def find_functions(self, query: str) -> list[int]:
        pcl = self.pcl
        q = query.strip()
        if re.fullmatch(r"0x[0-9a-fA-F]+", q):
            va = int(q, 16)
            idx = pcl.index_containing(va)
            return [idx] if idx is not None else []
        ql = q.lower()
        hits = []
        for i in range(pcl.nfunc):
            nm = pcl.name(i)
            if q in nm:
                hits.append((0, i))
            elif ql in nm.lower():
                hits.append((1, i))

        def rank(h: tuple[int, int]) -> tuple[int, int, int, int]:
            case_rank, i = h
            nm = pcl.name(i)
            exact = 0 if (nm == q or nm.endswith("." + q)) else 1
            return (exact, case_rank, 0 if is_jetski_name(nm) else 1, i)

        hits.sort(key=rank)
        return [i for _, i in hits]

    def disassemble(self, fidx_list: list[int]) -> dict[int, list[Insn]]:
        pcl = self.pcl
        out: dict[int, list[Insn]] = {}
        spans: list[list[Any]] = []
        for i in sorted(fidx_list, key=lambda x: pcl.entry_va[x]):
            lo, hi = pcl.entry_va[i], pcl.entry_va[i + 1]
            if spans and lo - spans[-1][1] <= 0x4000 and hi - spans[-1][0] <= 0x2000000:
                spans[-1][1] = hi
                spans[-1][2].append(i)
            else:
                spans.append([lo, hi, [i]])
        for lo, hi, members in spans:
            insns = run_objdump(self.m.path, lo, hi)
            starts = [x.va for x in insns]
            for i in members:
                a = bisect.bisect_left(starts, pcl.entry_va[i])
                b = bisect.bisect_left(starts, pcl.entry_va[i + 1])
                out[i] = insns[a:b]
        return out

    # -- per function listing ---------------------------------------------------------------------
    def render_function(self, fidx: int, insns: list[Insn], pseudo_only: bool = False) -> str:
        pcl, tdb, cg = self.pcl, self.tdb, self.cg
        name = pcl.name(fidx)
        entry, size = pcl.entry_va[fidx], pcl.size(fidx)
        lt = pcl.line_table(fidx)
        main_file = pcl.func_file(fidx)
        start_line = lt.start_line
        lines: list[str] = []
        w = lines.append

        # ---- basic blocks ----
        lo, hi = entry, entry + size
        leaders = {entry}
        edges_out: dict[int, list[int]] = defaultdict(list)
        for k, ins in enumerate(insns):
            nxt = insns[k + 1].va if k + 1 < len(insns) else None
            tgt = branch_target(ins)
            if ins.mn == "b" or ins.mn in COND_BRANCHES or ins.mn in ("cbz", "cbnz", "tbz", "tbnz"):
                if tgt is not None and lo <= tgt < hi:
                    leaders.add(tgt)
                if nxt is not None and ins.mn != "b":
                    leaders.add(nxt)
                if nxt is not None and ins.mn == "b":
                    leaders.add(nxt)
            elif ins.mn in ("ret", "br") and nxt is not None:
                leaders.add(nxt)
        leader_list = sorted(leaders)
        block_of = {va: bi for bi, va in enumerate(leader_list)}
        preds: dict[int, set[int]] = defaultdict(set)
        block_insns: dict[int, list[Insn]] = defaultdict(list)
        cur = 0
        for ins in insns:
            if ins.va in block_of:
                cur = block_of[ins.va]
            block_insns[cur].append(ins)
        for bi, bl in block_insns.items():
            last = bl[-1]
            tgt = branch_target(last)
            fall = True
            if last.mn == "b":
                fall = False
            if last.mn in ("ret", "br"):
                fall = False
            if last.mn == "b" or last.mn in COND_BRANCHES or last.mn in ("cbz", "cbnz", "tbz", "tbnz"):
                if tgt is not None and tgt in block_of:
                    preds[block_of[tgt]].add(bi)
            if fall and bi + 1 in block_insns:
                preds[bi + 1].add(bi)

        def blabel(va: int) -> str:
            return f"B{block_of[va]}" if va in block_of else f"{va:#x}"

        # ---- header ----
        w("=" * 100)
        w(f"FUNC   {name}")
        w(f"VA     {entry:#x}..{entry + size:#x}   size={size} B   insns={len(insns)}   blocks={len(leader_list)}")
        w(f"SOURCE {main_file}:{start_line}   (func start line)")
        # callers/callees
        nid = cg.va_index.get(entry)
        callers = cg.callers.get(nid, []) if nid is not None else []
        callees = cg.callees.get(nid, []) if nid is not None else []
        if callers:
            names = [f"{cg.full_name(c)}{' (x%d)' % cnt if cnt > 1 else ''}" for c, cnt, _t in callers[:25]]
            w(f"CALLERS ({len(callers)}): " + "; ".join(names) + ("; …" if len(callers) > 25 else ""))
        else:
            w("CALLERS (0 direct jetski bl/b call sites; may be reached via interface/closure/goroutine)")
        if callees:
            names = [f"{cg.full_name(c)}{' (x%d)' % cnt if cnt > 1 else ''}" for c, cnt, _t in callees[:60]]
            w(f"CALLEES ({len(callees)}): " + "; ".join(names) + ("; …" if len(callees) > 60 else ""))
        else:
            w("CALLEES (0)")
        w("=" * 100)

        # ---- walk blocks with abstract interpretation ----
        regs: dict[int, tuple[Any, ...]] = {}
        slots: dict[int, tuple[Any, ...]] = {}
        last_cmp: tuple[str, str, str] | None = None
        strings_seen: list[str] = []
        types_seen: dict[int, str] = {}
        src_mark: tuple[str, int, tuple[str, ...]] | None = None
        idx_of = {ins.va: k for k, ins in enumerate(insns)}

        for bi in sorted(block_insns):
            bl = block_insns[bi]
            first = bl[0]
            pr = sorted(preds.get(bi, set()))
            is_target = bool(pr) and not (len(pr) == 1 and pr[0] == bi - 1 and bi - 1 in block_insns and not (
                branch_target(block_insns[bi - 1][-1]) == first.va))
            if bi == 0 or is_target:
                regs.clear()
                slots.clear()
                last_cmp = None
            w("")
            w(f"B{bi}:  [{first.va:#x}..{bl[-1].va + 4:#x})   preds=[{', '.join('B%d' % p for p in pr) or 'entry'}]")
            for ins in bl:
                k = idx_of[ins.va]
                file, line, chain = lt.at(ins.va)
                mark = (file, line, chain)
                if mark != src_mark:
                    short = file if file != main_file else os.path.basename(file)
                    inl = f"   [inlined: {' ← '.join(chain)}]" if chain else ""
                    w(f"    // {short}:{line}{inl}")
                    src_mark = mark
                notes: list[str] = []
                stmts: list[str] = []
                last_cmp_box = [last_cmp]
                self._step(ins, insns, k, regs, slots, notes, stmts, strings_seen, types_seen, blabel, block_of, lo, hi,
                           last_cmp_box)
                last_cmp = last_cmp_box[0]
                if not pseudo_only:
                    asm = f"{ins.mn:<7}{', '.join(ins.ops)}"
                    note = ("   // " + "; ".join(notes)) if notes else ""
                    w(f"  {ins.va:#x}  {asm:<42}{note}")
                for s in stmts:
                    w(f"      => {s}")
            last = bl[-1]
            if last.mn not in ("b", "ret", "br"):
                if bi + 1 in block_insns:
                    pass
        w("")
        if strings_seen:
            w("STRINGS: " + ", ".join(dict.fromkeys(strings_seen)))
        if types_seen:
            w("TYPES:")
            for va, desc in types_seen.items():
                w(f"  {va:#x}  {desc}")
        w("")
        return "\n".join(lines)

    # -- single instruction semantics ---------------------------------------------------------
    def _step(self, ins: Insn, insns: list[Insn], k: int, regs: dict[int, tuple[Any, ...]], slots: dict[int, tuple[Any, ...]],
              notes: list[str], stmts: list[str], strings_seen: list[str], types_seen: dict[int, str],
              blabel: Any, block_of: dict[int, int], lo: int, hi: int, cmp_box: list[Any]) -> None:
        pcl, tdb = self.pcl, self.tdb
        mn, ops = ins.mn, ins.ops

        def rv(op: str) -> tuple[Any, ...] | None:
            r = reg_id(op)
            if r is None:
                return None
            if r == 32:
                return ("const", 0)
            return regs.get(r)

        def setreg(op: str, v: tuple[Any, ...] | None) -> None:
            r = reg_id(op)
            if r is None or r >= 32:
                return
            if v is None:
                regs.pop(r, None)
            else:
                regs[r] = v

        def describe(v: tuple[Any, ...]) -> None:
            if v[0] == "str":
                if v[1]:
                    strings_seen.append(jstr(v[1], 80))
                notes.append(("string " if v[3] else "string(len unknown, truncated) ") + jstr(v[1], 120))
            elif v[0] == "type":
                ti = tdb.type_info(v[1], with_fields=True)
                if ti:
                    d = tdb.format_type(ti)
                    types_seen[v[1]] = d
                    notes.append(f"[TYPE: {ti['name']}]")
            elif v[0] == "itab":
                ii = tdb.itab_info(v[1])
                if ii:
                    d = f"itab iface={ii['iface']['name']} concrete={ii['concrete']['name']}"
                    types_seen[v[1]] = d
                    notes.append(f"[ITAB: {ii['iface']['name']} <- {ii['concrete']['name']}]")
            elif v[0] == "func":
                notes.append(f"[FUNC: {fmt_val(v, tdb, pcl)}]")
            elif v[0] == "data":
                notes.append(f"[DATA: {v[2]} @{v[1]:#x}]")

        def mem_note(base: int, off: int, is_store: bool, vtxt: str) -> None:
            if base == 28 and off in G_FIELDS:
                notes.append(G_FIELDS[off])
                return
            bv = regs.get(base)
            if bv and bv[0] == "ptr":
                fa = tdb.field_at(bv[1], off)
                if fa:
                    notes.append(f"field {fa}")
                    stmts.append(f"{fa.split(' ')[0]} {'=' if is_store else '→'} {vtxt}")

        if mn == "adrp" and len(ops) == 2:
            m = HEX_RE.search(ops[1])
            if m:
                setreg(ops[0], ("page", int(m.group(1), 16)))
            return
        if mn == "add" and len(ops) == 3:
            imm = parse_imm(ops[2])
            src = rv(ops[1])
            if reg_id(ops[1]) == 31 and imm is not None:
                setreg(ops[0], ("sp", imm))
                return
            if src and src[0] == "page" and imm is not None:
                va = src[1] + imm
                v = self.classify(va, insns, k, reg_id(ops[0]) or 0)
                setreg(ops[0], v)
                describe(v)
                return
            if src and src[0] == "const" and imm is not None:
                setreg(ops[0], ("const", src[1] + imm))
                return
            setreg(ops[0], None)
            return
        if mn == "mov" and len(ops) == 2:
            imm = parse_imm(ops[1])
            if imm is not None:
                setreg(ops[0], ("const", imm if imm >= 0 else imm & 0xFFFFFFFFFFFFFFFF))
            else:
                setreg(ops[0], rv(ops[1]))
            return
        if mn == "movz" and len(ops) >= 2:
            imm = parse_imm(ops[1])
            if imm is not None and len(ops) == 2:
                setreg(ops[0], ("const", imm))
            elif imm is not None and len(ops) == 3 and ops[2].startswith("lsl"):
                sh = parse_imm("#" + ops[2].split("#")[1]) or 0
                setreg(ops[0], ("const", imm << sh))
            else:
                setreg(ops[0], None)
            return
        if mn == "movk" and len(ops) >= 2:
            cur_v = rv(ops[0])
            imm = parse_imm(ops[1])
            sh = 0
            if len(ops) == 3 and "#" in ops[2]:
                sh = parse_imm("#" + ops[2].split("#")[1]) or 0
            if cur_v and cur_v[0] == "const" and imm is not None:
                setreg(ops[0], ("const", (cur_v[1] & ~(0xFFFF << sh)) | (imm << sh)))
            else:
                setreg(ops[0], None)
            return
        if mn == "orr" and len(ops) == 3 and ops[1] in ("xzr", "wzr"):
            imm = parse_imm(ops[2])
            setreg(ops[0], ("const", imm) if imm is not None else None)
            return

        if mn in ("ldr", "ldur", "ldrb", "ldrh", "ldrsw", "ldrsb", "ldrsh", "ldp", "ldar", "ldxr", "ldaxr"):
            ndst = 2 if mn == "ldp" else 1
            mem_idx = ndst
            if len(ops) <= mem_idx:
                return
            mem = parse_mem(ops[mem_idx])
            dests = ops[:ndst]
            post_imm = parse_imm(ops[mem_idx + 1]) if len(ops) > mem_idx + 1 else None
            if mem is None:
                for d in dests:
                    setreg(d, None)
                return
            base, off, pre = mem
            bv = regs.get(base) if base != 31 else ("sp", 0)
            result: list[tuple[Any, ...] | None] = [None] * ndst
            width = 8 if (mn in ("ldr", "ldur", "ldp") and dests[0].startswith("x")) else 4
            if base == 31 and mn in ("ldr", "ldur", "ldp"):
                for j in range(ndst):
                    result[j] = slots.get(off + 8 * j) if width == 8 else None
            elif bv and bv[0] == "page":
                gva = bv[1] + off
                sect = self.m.section_of(gva) or "?"
                notes.append(f"load global {gva:#x} ({sect})")
                if width == 8 and sect.split(".")[0] in ("__TEXT", "__DATA_CONST"):
                    val = self.m.u64(gva)
                    if val:
                        cv = self.classify(val)
                        if cv[0] in ("type", "itab", "func") or (cv[0] == "str"):
                            result[0] = cv if cv[0] != "str" else None
                if result[0] is None:
                    result[0] = ("global", gva, sect)
            elif bv and bv[0] in ("itab", "data", "type") and width == 8:
                bva = bv[1] + off
                sect = self.m.section_of(bva) or ""
                if sect.split(".")[0] in ("__TEXT", "__DATA_CONST"):
                    val = self.m.u64(bva)
                    if val and self.pcl.index_of_entry(val) is not None:
                        result[0] = ("func", val)
                        notes.append(f"[{fmt_val(result[0], tdb, pcl)}]")
            elif bv and bv[0] == "ptr":
                mem_note(base, off, False, "?")
            elif base == 28:
                mem_note(base, off, False, "?")
            if all(r is None for r in result) and off >= 0x18 and base not in (31, 28) and mn in ("ldr", "ldur"):
                result[0] = ("load", base, off)
                if off >= 0x18 and (off - 0x18) % 8 == 0:
                    notes.append(f"maybe itab.fun[{(off - 0x18) // 8}]")
            for d, r in zip(dests, result):
                setreg(d, r)
            if pre or post_imm is not None:
                setreg(f"x{base}", None) if base < 31 else None
            return

        if mn in ("str", "stur", "strb", "strh", "stp"):
            nsrc = 2 if mn == "stp" else 1
            if len(ops) <= nsrc:
                return
            mem = parse_mem(ops[nsrc])
            if mem is None:
                return
            base, off, pre = mem
            width = 8 if (mn in ("str", "stur", "stp") and ops[0].startswith("x")) else 4
            for j in range(nsrc):
                v = rv(ops[j])
                vtxt = fmt_val(v, tdb, pcl) if v else ops[j]
                if base == 31:
                    if v and width == 8:
                        slots[off + 8 * j] = v
                    else:
                        slots.pop(off + 8 * j, None)
                else:
                    mem_note(base, off + 8 * j, True, vtxt)
                    gv = regs.get(base)
                    if gv and gv[0] == "page":
                        notes.append(f"store global {gv[1] + off:#x}")
            return

        if mn in ("cmp", "cmn", "tst") and len(ops) >= 2:
            a = fmt_val(rv(ops[0]), tdb, pcl) if rv(ops[0]) and rv(ops[0])[0] in ("const", "str") else ops[0]  # type: ignore[index]
            b = ops[1]
            bi_ = parse_imm(b)
            if bi_ is not None:
                b = str(bi_) if abs(bi_) < 4096 else hex(bi_)
            elif rv(b) and rv(b)[0] == "const":  # type: ignore[index]
                b = fmt_val(rv(b), tdb, pcl)
            cmp_box[0] = (mn, a, b)
            return

        if mn == "bl" and ops:
            tgt = branch_target(ins)
            fn_name, fn_idx = self._resolve_target(tgt)
            args = []
            for r in range(8):
                v = regs.get(r)
                if v is not None and v[0] != "page" and not fn_name.startswith("runtime.gcWriteBarrier"):
                    args.append(f"x{r}={fmt_val(v, tdb, pcl)}")
            where = ""
            if fn_idx is not None and is_jetski_name(fn_name):
                where = f"   /* {pcl.func_file(fn_idx)}:{pcl.start_line(fn_idx)} */"
            notes.append(f"-> {fn_name}")
            stmts.append(f"call {fn_name}({', '.join(args)}){where}")
            # allocation typing: result in x0
            alloc = fn_name.startswith("runtime.newobject") or fn_name.startswith("runtime.mallocgc")
            tv = None
            if alloc:
                for r in (0, 1, 2):
                    v = regs.get(r)
                    if v and v[0] == "type":
                        tv = v
                        break
            for r in range(0, 28):
                regs.pop(r, None)
            if tv:
                regs[0] = ("ptr", tv[1])
                notes.append(f"x0 = new({fmt_val(tv, tdb, pcl)[5:-1]})")
            return
        if mn == "blr" and ops:
            v = rv(ops[0])
            desc = fmt_val(v, tdb, pcl) if v else ops[0]
            if v and v[0] == "load":
                desc = f"{ops[0]} /* loaded from [x{v[1]}+{v[2]:#x}] — interface method slot {(v[2] - 0x18) // 8 if v[2] >= 0x18 and (v[2] - 0x18) % 8 == 0 else '?'} or func-pointer field */"
            args = []
            for r in range(8):
                av = regs.get(r)
                if av is not None and av[0] != "page":
                    args.append(f"x{r}={fmt_val(av, tdb, pcl)}")
            notes.append("indirect call")
            stmts.append(f"call *{desc}({', '.join(args)})")
            for r in range(0, 28):
                regs.pop(r, None)
            return
        if mn == "ret":
            stmts.append("return")
            return
        if mn == "br" and ops:
            stmts.append(f"goto *{ops[0]}   /* jump table / tail call */")
            return
        if mn == "b":
            tgt = branch_target(ins)
            if tgt is not None and lo <= tgt < hi:
                stmts.append(f"goto {blabel(tgt)}")
            else:
                nm, ti = self._resolve_target(tgt)
                args = [f"x{r}={fmt_val(regs[r], tdb, pcl)}" for r in range(8) if r in regs and regs[r][0] != "page"]
                stmts.append(f"tailcall {nm}({', '.join(args)})")
                notes.append(f"tail -> {nm}")
            return
        if mn in COND_BRANCHES:
            tgt = branch_target(ins)
            cc = mn.split(".")[1]
            cond = self._cond_text(cc, cmp_box[0])
            tl = blabel(tgt) if tgt is not None and tgt in block_of else (f"{tgt:#x}" if tgt else "?")
            stmts.append(f"if {cond} goto {tl}")
            return
        if mn in ("cbz", "cbnz") and len(ops) == 2:
            tgt = branch_target(ins)
            tl = blabel(tgt) if tgt is not None and tgt in block_of else (f"{tgt:#x}" if tgt else "?")
            stmts.append(f"if {ops[0]} {'==' if mn == 'cbz' else '!='} 0 goto {tl}")
            return
        if mn in ("tbz", "tbnz") and len(ops) == 3:
            tgt = branch_target(ins)
            tl = blabel(tgt) if tgt is not None and tgt in block_of else (f"{tgt:#x}" if tgt else "?")
            stmts.append(f"if (bit {ops[1][1:]} of {ops[0]}) {'== 0' if mn == 'tbz' else '!= 0'} goto {tl}")
            return

        # generic: clobber destination register(s)
        if mn not in NO_DEST and ops:
            d = reg_id(ops[0])
            if d is not None and d < 31:
                regs.pop(d, None)

    @staticmethod
    def _cond_text(cc: str, cmp: tuple[str, str, str] | None) -> str:
        if cmp is None:
            return f"flags.{cc}"
        mn, a, b = cmp
        if mn == "tst":
            if cc == "eq":
                return f"({a} & {b}) == 0"
            if cc == "ne":
                return f"({a} & {b}) != 0"
            return f"({a} & {b}) .{cc}"
        if mn == "cmn":
            return f"({a} + {b}) {CMP_OPS.get(cc, '.' + cc)} 0"
        op = CMP_OPS.get(cc, "." + cc)
        if op in ("<0", ">=0"):
            return f"({a} - {b}) {op}"
        return f"{a} {op} {b}"

    def _resolve_target(self, tgt: int | None) -> tuple[str, int | None]:
        if tgt is None:
            return "?", None
        idx = self.pcl.index_of_entry(tgt)
        if idx is not None:
            return self.pcl.name(idx), idx
        idx = self.pcl.index_containing(tgt)
        if idx is not None:
            return f"{self.pcl.name(idx)}+{tgt - self.pcl.entry_va[idx]:#x}", None
        sect = self.m.section_of(tgt) or "?"
        return f"{sect}@{tgt:#x}", None


# ---------------------------------------------------------------------------------------------
# commands
# ---------------------------------------------------------------------------------------------
def cmd_build_callgraph(args: argparse.Namespace, macho: MachO, pcl: Pclntab) -> int:
    data = build_callgraph(macho, pcl, verbose=True)
    out = Path(args.callgraph_out) if args.callgraph_out else CALLGRAPH_PATH
    out.parent.mkdir(parents=True, exist_ok=True)
    write_callgraph(data, out)
    s = data["stats"]
    print(f"wrote {out} ({out.stat().st_size / 1e6:.1f} MB): {s['nodes']} nodes, {s['unique_edges']} edges, "
          f"{s['call_sites_bl']} bl sites ({s['call_sites_resolved']} resolved, {s['call_sites_unresolved']} unresolved), "
          f"{s['scope_functions_jetski']} jetski functions")
    return 0


def make_decompiler(args: argparse.Namespace, macho: MachO, pcl: Pclntab) -> Decompiler:
    cg = load_or_build_callgraph(macho, pcl)
    tdb = TypeDB(macho, pcl)
    return Decompiler(macho, pcl, cg, tdb)


def file_header(macho: MachO, pcl: Pclntab) -> str:
    return (f"# decompile-workbench  binary={macho.path}  funcs={pcl.nfunc}  text={pcl.text_va:#x}  "
            f"types=[{pcl.types_va:#x},{pcl.etypes_va:#x})  gofunc={pcl.gofunc_va:#x}")


def cmd_dump_func(args: argparse.Namespace, macho: MachO, pcl: Pclntab) -> int:
    dec = make_decompiler(args, macho, pcl)
    hits = dec.find_functions(args.dump_func)
    if not hits:
        print(f"no function matches {args.dump_func!r}", file=sys.stderr)
        return 1
    limit = args.limit if args.limit is not None else 3
    print(file_header(macho, pcl))
    print(f"# {len(hits)} function(s) match {args.dump_func!r}; dumping {min(limit, len(hits))}")
    for i in hits[:50]:
        print(f"#   {pcl.entry_va[i]:#x} ({pcl.size(i)} B) {pcl.name(i)}")
    if len(hits) > 50:
        print(f"#   … {len(hits) - 50} more")
    sel = hits[:limit]
    disasm = dec.disassemble(sel)
    for i in sel:
        print(dec.render_function(i, disasm.get(i, []), pseudo_only=args.pseudo_only))
    if dec.tdb.json_hits:
        print(f"# type annotations: {dec.tdb.json_hits} cross-referenced with recovered-gotypes.json")
    return 0


def cmd_dump_package(args: argparse.Namespace, macho: MachO, pcl: Pclntab) -> int:
    if not args.out:
        print("--dump-package requires --out <dir>", file=sys.stderr)
        return 2
    dec = make_decompiler(args, macho, pcl)
    q = args.dump_package
    sel = [i for i in range(pcl.nfunc) if q in pcl.package(i)]
    if not sel:
        sel = [i for i in range(pcl.nfunc) if q in pcl.name(i)]
    if not sel:
        print(f"no functions match package substring {q!r}", file=sys.stderr)
        return 1
    if args.limit is not None:
        sel = sel[:args.limit]
    out_dir = Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)
    disasm = dec.disassemble(sel)
    by_file: dict[str, list[int]] = defaultdict(list)
    for i in sel:
        f = pcl.func_file(i)
        if f.startswith("<"):
            f = "_autogenerated"
        by_file[f].append(i)
    index_rows = []
    for f, items in sorted(by_file.items()):
        items.sort(key=lambda i: (pcl.start_line(i), pcl.entry_va[i]))
        target = out_dir / (f + ".decomp.txt")
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("w", encoding="utf-8") as fh:
            fh.write(file_header(macho, pcl) + "\n")
            fh.write(f"# source file: {f}   functions: {len(items)}   (package filter: {q!r})\n\n")
            for i in items:
                fh.write(dec.render_function(i, disasm.get(i, []), pseudo_only=args.pseudo_only))
                fh.write("\n")
                index_rows.append((f, pcl.start_line(i), pcl.entry_va[i], pcl.size(i), pcl.name(i)))
    with (out_dir / "INDEX.md").open("w", encoding="utf-8") as fh:
        fh.write(f"# decompile-workbench package dump: `{q}`\n\n{len(index_rows)} functions in {len(by_file)} files.\n\n")
        fh.write("| source | line | VA | size | function |\n|---|---:|---|---:|---|\n")
        for f, ln, va, sz, nm in sorted(index_rows):
            fh.write(f"| {f} | {ln} | `{va:#x}` | {sz} | `{nm}` |\n")
    print(f"wrote {len(index_rows)} functions into {len(by_file)} files under {out_dir}")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description="agy ARM64 decompilation workbench")
    ap.add_argument("--binary", type=Path, default=DEFAULT_BINARY)
    mode = ap.add_mutually_exclusive_group(required=True)
    mode.add_argument("--build-callgraph", action="store_true", help="write metadata/callgraph-summary.json")
    mode.add_argument("--dump-func", metavar="SUBSTR", help="decompile functions whose name contains SUBSTR (or 0xVA)")
    mode.add_argument("--dump-package", metavar="PKG_SUBSTR", help="decompile every function of matching package(s)")
    ap.add_argument("--limit", type=int, default=None, help="max functions to dump")
    ap.add_argument("--out", help="output directory for --dump-package")
    ap.add_argument("--pseudo-only", action="store_true", help="omit the raw asm column")
    ap.add_argument("--callgraph-out", help="override callgraph output path")
    args = ap.parse_args()

    if not args.binary.is_file():
        print(f"binary not found: {args.binary}", file=sys.stderr)
        return 2
    macho = MachO(args.binary)
    pcl = Pclntab(macho)
    if args.build_callgraph:
        return cmd_build_callgraph(args, macho, pcl)
    if args.dump_func is not None:
        return cmd_dump_func(args, macho, pcl)
    return cmd_dump_package(args, macho, pcl)


if __name__ == "__main__":
    sys.exit(main())
