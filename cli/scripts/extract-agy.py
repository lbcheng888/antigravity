#!/usr/bin/env python3
"""Reproducible lossless extractor and Go metadata analyzer for /Users/mac/.local/bin/agy.

Inspects the Mach-O 64-bit arm64 binary without modifying it, extracts all
embedded payloads (Go embed.FS tables, ZIP bundles, tar.gz runtimes, gzip
fonts, and protobuf descriptors), recovers Go package/file/function maps from
gopclntab (__lrodata_gopcln), and records CLI metadata under cli/.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import json
import os
import re
import shutil
import struct
import subprocess
import tarfile
import zipfile
import zlib
from collections import defaultdict
from pathlib import Path
from typing import Any


DEFAULT_BINARY = Path("/Users/mac/.local/bin/agy")
DEFAULT_CLI_ROOT = Path(__file__).resolve().parent.parent


def sha256_bytes(blob: bytes) -> str:
    return hashlib.sha256(blob).hexdigest()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        while True:
            chunk = f.read(1024 * 1024)
            if not chunk:
                break
            h.update(chunk)
    return h.hexdigest()


def run_cmd(cmd: list[str]) -> tuple[int, str]:
    proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    return proc.returncode, proc.stdout


def parse_macho(data: bytes) -> dict[str, Any]:
    magic, cputype, cpusubtype, filetype, ncmds, sizeofcmds, flags, _ = struct.unpack_from(
        "<IIIIIIII", data, 0
    )
    if magic != 0xFEEDFACF:
        raise ValueError(f"Unexpected Mach-O magic: {hex(magic)}")

    off = 32
    segments: list[dict[str, Any]] = []
    text_section_vmaddr = 0

    for _ in range(ncmds):
        cmd, cmdsize = struct.unpack_from("<II", data, off)
        if cmd == 0x19:  # LC_SEGMENT_64
            segname = data[off + 8 : off + 24].split(b"\x00", 1)[0].decode("ascii", "replace")
            vmaddr, vmsize, fileoff, filesize, maxprot, initprot, nsects, sflags = struct.unpack_from(
                "<QQQQIIII", data, off + 24
            )
            sections: list[dict[str, Any]] = []
            so = off + 72
            for _ in range(nsects):
                sectname = data[so : so + 16].split(b"\x00", 1)[0].decode("ascii", "replace")
                ssegname = data[so + 16 : so + 32].split(b"\x00", 1)[0].decode("ascii", "replace")
                saddr, ssize, soffset, salign, sreloff, snreloc, secflags, r1, r2, r3 = struct.unpack_from(
                    "<QQIIIIIIII", data, so + 32
                )
                if segname == "__TEXT" and sectname == "__text":
                    text_section_vmaddr = saddr
                sections.append(
                    {
                        "sectname": sectname,
                        "segname": ssegname,
                        "vmaddr": hex(saddr),
                        "size": ssize,
                        "fileoff": soffset,
                    }
                )
                so += 80

            segments.append(
                {
                    "segname": segname,
                    "vmaddr": hex(vmaddr),
                    "vmaddr_int": vmaddr,
                    "vmsize": vmsize,
                    "fileoff": fileoff,
                    "filesize": filesize,
                    "sections": sections,
                }
            )
        off += cmdsize

    return {
        "magic": hex(magic),
        "cputype": hex(cputype),
        "filetype": filetype,
        "ncmds": ncmds,
        "flags": hex(flags),
        "text_section_vmaddr": text_section_vmaddr,
        "segments": segments,
    }


def make_vm_mappers(segments: list[dict[str, Any]]):
    spans = [
        (s["vmaddr_int"], s["vmaddr_int"] + s["filesize"], s["fileoff"], s["fileoff"] + s["filesize"])
        for s in segments
        if s["filesize"] > 0
    ]

    def fileoff_to_vm(foff: int) -> int | None:
        for vm_s, _, fo_s, fo_e in spans:
            if fo_s <= foff < fo_e:
                return vm_s + (foff - fo_s)
        return None

    def vm_to_fileoff(va: int) -> int | None:
        for vm_s, vm_e, fo_s, _ in spans:
            if vm_s <= va < vm_e:
                return fo_s + (va - vm_s)
        return None

    return fileoff_to_vm, vm_to_fileoff


def extract_go_buildinfo(data: bytes) -> dict[str, Any]:
    marker = b"\xff Go buildinf:"
    idx = data.find(marker)
    if idx == -1:
        return {}
    chunk = data[idx : idx + 512]
    strings = re.findall(rb"[ -~]{4,}", chunk[16:])
    decoded = [s.decode("ascii", "replace") for s in strings]
    return {
        "offset": idx,
        "raw_strings": decoded[:10],
        "go_version": decoded[0] if decoded else "",
        "build_flags": decoded[1] if len(decoded) > 1 else "",
    }


def find_embed_fs_tables(data: bytes, segments: list[dict[str, Any]]) -> list[dict[str, Any]]:
    fileoff_to_vm, vm_to_fileoff = make_vm_mappers(segments)

    def check_entry(pos: int) -> dict[str, Any] | None:
        nva, nlen, dva, dlen = struct.unpack_from("<QQQQ", data, pos)
        if not (1 <= nlen <= 300) or dlen > 50_000_000:
            return None
        nfo = vm_to_fileoff(nva)
        if nfo is None or nfo + nlen > len(data):
            return None
        nb = data[nfo : nfo + nlen]
        if not all(32 <= b < 127 for b in nb):
            return None
        name = nb.decode("ascii")
        if name.endswith("/"):
            if dlen != 0 or dva != 0:
                return None
            return {"kind": "dir", "name": name, "dfo": 0, "size": 0}
        dfo = 0
        if dlen > 0:
            resolved = vm_to_fileoff(dva)
            if resolved is None or resolved + dlen > len(data):
                return None
            dfo = resolved
        return {"kind": "file", "name": name, "dfo": dfo, "size": dlen}

    tables: list[dict[str, Any]] = []
    for seg in segments:
        if seg["segname"] not in ("__DATA_CONST", "__DATA"):
            continue
        pos = seg["fileoff"]
        seg_end = pos + seg["filesize"]
        while pos + 24 + 48 <= seg_end:
            target_va = fileoff_to_vm(pos + 24)
            ptr, length, cap = struct.unpack_from("<QQQ", data, pos)
            if (
                target_va is not None
                and ptr == target_va
                and length == cap
                and 1 <= length <= 2000
                and pos + 24 + length * 48 <= seg_end
            ):
                entries: list[dict[str, Any]] = []
                ok = True
                for k in range(length):
                    rec = check_entry(pos + 24 + k * 48)
                    if rec is None:
                        ok = False
                        break
                    entries.append(rec)
                if ok:
                    tables.append(
                        {
                            "header_offset": pos,
                            "count": length,
                            "entries": entries,
                        }
                    )
                    pos += 24 + length * 48
                    continue
            pos += 8
    return tables


def safe_join(root: Path, rel: str) -> Path:
    clean_rel = rel.lstrip("/")
    target = (root / clean_rel).resolve()
    root_resolved = root.resolve()
    if target != root_resolved and root_resolved not in target.parents:
        raise ValueError(f"Unsafe path traversal detected: {rel}")
    return target


def write_embed_table(data: bytes, entries: list[dict[str, Any]], target_dir: Path) -> dict[str, Any]:
    if target_dir.exists():
        shutil.rmtree(target_dir)
    target_dir.mkdir(parents=True, exist_ok=True)
    file_count = 0
    dir_count = 0
    total_bytes = 0
    h = hashlib.sha256()

    for ent in entries:
        out_path = safe_join(target_dir, ent["name"])
        if ent["kind"] == "dir":
            out_path.mkdir(parents=True, exist_ok=True)
            dir_count += 1
        else:
            out_path.parent.mkdir(parents=True, exist_ok=True)
            blob = data[ent["dfo"] : ent["dfo"] + ent["size"]] if ent["size"] > 0 else b""
            out_path.write_bytes(blob)
            file_count += 1
            total_bytes += len(blob)
            h.update(ent["name"].encode("utf-8"))
            h.update(blob)

    return {
        "entries": len(entries),
        "files": file_count,
        "dirs": dir_count,
        "total_bytes": total_bytes,
        "tree_sha256": h.hexdigest(),
    }


def extract_zip_at(
    data: bytes, eocd_pos: int, target_dir: Path
) -> dict[str, Any]:
    _, _, _, _, cd_size, cd_off, cmt_len = struct.unpack_from("<HHHHIIH", data, eocd_pos + 4)
    zip_end = eocd_pos + 22 + cmt_len
    zip_start = eocd_pos - cd_size - cd_off
    zip_blob = data[zip_start:zip_end]

    if target_dir.exists():
        shutil.rmtree(target_dir)
    target_dir.mkdir(parents=True, exist_ok=True)

    files = 0
    dirs = 0
    total_bytes = 0
    with zipfile.ZipFile(io.BytesIO(zip_blob)) as zf:
        for info in zf.infolist():
            out_path = safe_join(target_dir, info.filename)
            if info.is_dir():
                out_path.mkdir(parents=True, exist_ok=True)
                dirs += 1
            else:
                out_path.parent.mkdir(parents=True, exist_ok=True)
                payload = zf.read(info.filename)
                out_path.write_bytes(payload)
                files += 1
                total_bytes += len(payload)

    return {
        "offset_start": zip_start,
        "offset_end": zip_end,
        "archive_size": len(zip_blob),
        "archive_sha256": sha256_bytes(zip_blob),
        "files": files,
        "dirs": dirs,
        "extracted_bytes": total_bytes,
    }


def extract_targz_at(data: bytes, gz_off: int, max_scan: int, target_dir: Path) -> dict[str, Any]:
    dec = zlib.decompressobj(16 + zlib.MAX_WBITS)
    chunk = data[gz_off : min(len(data), gz_off + max_scan)]
    raw_tar = dec.decompress(chunk) + dec.flush()
    if not dec.eof:
        raise RuntimeError(f"Gzip stream at {gz_off} did not reach EOF within {max_scan} bytes")
    comp_len = len(chunk) - len(dec.unused_data)
    comp_blob = data[gz_off : gz_off + comp_len]

    if target_dir.exists():
        shutil.rmtree(target_dir)
    target_dir.mkdir(parents=True, exist_ok=True)

    root_resolved = target_dir.resolve()
    files = 0
    dirs = 0
    symlinks = 0
    extracted_bytes = 0

    with tarfile.open(fileobj=io.BytesIO(raw_tar), mode="r:") as tf:
        for member in tf.getmembers():
            out_path = safe_join(target_dir, member.name)
            if member.isdir():
                out_path.mkdir(parents=True, exist_ok=True)
                dirs += 1
            elif member.issym():
                out_path.parent.mkdir(parents=True, exist_ok=True)
                link_target = (out_path.parent / member.linkname).resolve()
                if link_target != root_resolved and root_resolved not in link_target.parents:
                    raise ValueError(f"Unsafe symlink in tar: {member.name} -> {member.linkname}")
                if out_path.exists() or out_path.is_symlink():
                    out_path.unlink()
                os.symlink(member.linkname, out_path)
                symlinks += 1
            elif member.islnk():
                out_path.parent.mkdir(parents=True, exist_ok=True)
                link_src = safe_join(target_dir, member.linkname)
                if out_path.exists() or out_path.is_symlink():
                    out_path.unlink()
                os.link(link_src, out_path)
                files += 1
            elif member.isfile():
                out_path.parent.mkdir(parents=True, exist_ok=True)
                fobj = tf.extractfile(member)
                payload = fobj.read() if fobj is not None else b""
                out_path.write_bytes(payload)
                if member.mode & 0o111:
                    out_path.chmod(member.mode & 0o777)
                files += 1
                extracted_bytes += len(payload)

    return {
        "offset_start": gz_off,
        "compressed_size": comp_len,
        "compressed_sha256": sha256_bytes(comp_blob),
        "tar_size": len(raw_tar),
        "files": files,
        "dirs": dirs,
        "symlinks": symlinks,
        "extracted_bytes": extracted_bytes,
    }


def extract_misc_gzip_blobs(data: bytes, extracted_root: Path) -> dict[str, Any]:
    fonts_dir = extracted_root / "fonts"
    protos_dir = extracted_root / "proto-descriptors"
    for d in (fonts_dir, protos_dir):
        if d.exists():
            shutil.rmtree(d)
        d.mkdir(parents=True, exist_ok=True)

    font_items = []
    proto_items = []

    for m in re.finditer(b"\x1f\x8b\x08", data):
        goff = m.start()
        if goff < 178061312 or goff >= 182900000:
            continue
        dec = zlib.decompressobj(16 + zlib.MAX_WBITS)
        try:
            chunk = data[goff : min(len(data), goff + 3_000_000)]
            out = dec.decompress(chunk) + dec.flush()
            if not dec.eof:
                continue
        except Exception:
            continue

        # Protobuf FileDescriptorProto starts with field 1 (wire type 2 => 0x0a), length, ASCII *.proto name
        if len(out) >= 10 and out[0] == 0x0A:
            name_len = out[1]
            if 2 <= name_len < 120 and 2 + name_len <= len(out):
                pname = out[2 : 2 + name_len].decode("ascii", "replace")
                if pname.endswith(".proto"):
                    pb_rel = pname[:-6] + ".pb"
                    out_path = safe_join(protos_dir, pb_rel)
                    out_path.parent.mkdir(parents=True, exist_ok=True)
                    out_path.write_bytes(out)
                    proto_items.append(
                        {"proto": pname, "file": pb_rel, "offset": goff, "size": len(out), "sha256": sha256_bytes(out)}
                    )
                    continue

        # TrueType/OpenType font (sfnt 0x00010000 or OTTO)
        if out[:4] in (b"\x00\x01\x00\x00", b"OTTO"):
            ext = ".otf" if out[:4] == b"OTTO" else ".ttf"
            fname = f"font_{len(font_items) + 1}_offset_{goff}{ext}"
            out_path = fonts_dir / fname
            out_path.write_bytes(out)
            font_items.append({"file": fname, "offset": goff, "size": len(out), "sha256": sha256_bytes(out)})

    return {"fonts": font_items, "proto_descriptors": proto_items}


def read_svarint(buf: bytes, pos: int) -> tuple[int, int]:
    v = 0
    shift = 0
    while True:
        b = buf[pos]
        pos += 1
        v |= (b & 0x7F) << shift
        if not (b & 0x80):
            break
        shift += 7
    sval = (-(v >> 1) - 1) if (v & 1) else (v >> 1)
    return sval, pos


def parse_gopclntab(data: bytes, text_vmaddr: int) -> dict[str, Any]:
    magic = b"\xf1\xff\xff\xff\x00\x00"
    base = data.find(magic)
    if base == -1:
        raise RuntimeError("Go 1.20+ gopclntab magic not found")

    (
        magic_val,
        pad,
        min_lc,
        ptr_size,
        nfunc,
        nfiles,
        text_start,
        funcname_off,
        cu_off,
        filetab_off,
        pctab_off,
        pcln_off,
    ) = struct.unpack_from("<IHBBQQQQQQQQ", data, base)

    ft_bytes = data[base + filetab_off : base + pctab_off]
    all_files = [s.decode("utf-8", "replace") for s in ft_bytes.split(b"\x00") if s]

    ftab_base = base + pcln_off
    funcname_base = base + funcname_off
    filetab_base = base + filetab_off
    cu_base = base + cu_off
    pctab_base = base + pctab_off

    jetski_funcs: list[dict[str, Any]] = []
    packages: dict[str, dict[str, Any]] = {}

    for i in range(nfunc):
        entry_off, func_off = struct.unpack_from("<II", data, ftab_base + i * 8)
        next_entry_off, _ = struct.unpack_from("<II", data, ftab_base + (i + 1) * 8)
        fstruct = ftab_base + func_off
        (
            f_entry,
            name_off,
            _args,
            _deferret,
            _pcsp,
            pcfile,
            _pcln,
            _npcdata,
            cu_idx,
            start_line,
        ) = struct.unpack_from("<IiiiiIIIIi", data, fstruct)

        fn_pos = funcname_base + name_off
        fn_end = data.find(b"\x00", fn_pos)
        fname = data[fn_pos:fn_end].decode("utf-8", "replace")

        if not ("jetski" in fname or "antigravity" in fname):
            continue

        file_path = "<unknown>"
        if pcfile != 0xFFFFFFFF:
            dv, _ = read_svarint(data, pctab_base + pcfile)
            val = -1 + dv
            if val >= 0:
                file_num = struct.unpack_from("<I", data, cu_base + (cu_idx + val) * 4)[0]
                fp_pos = filetab_base + file_num
                fp_end = data.find(b"\x00", fp_pos)
                file_path = data[fp_pos:fp_end].decode("utf-8", "replace")

        # Derive Go package import path cleanly from function name or file path
        pkg = fname.split(".", 1)[0]
        if "[" in pkg:
            pkg = pkg.split("[", 1)[0]

        code_size = max(0, next_entry_off - entry_off)
        record = {
            "index": i,
            "vmaddr": hex(text_vmaddr + entry_off),
            "text_offset": entry_off,
            "size": code_size,
            "start_line": start_line,
            "file": file_path,
            "package": pkg,
            "name": fname,
        }
        jetski_funcs.append(record)

        pentry = packages.get(pkg)
        if pentry is None:
            pentry = {
                "package": pkg,
                "func_count": 0,
                "code_bytes": 0,
                "files": set(),
                "sample_funcs": [],
            }
            packages[pkg] = pentry
        pentry["func_count"] += 1
        pentry["code_bytes"] += code_size
        if file_path not in ("<autogenerated>", "<unknown>"):
            pentry["files"].add(file_path)
        if len(pentry["sample_funcs"]) < 8 and not fname.endswith(".init") and ".func" not in fname:
            pentry["sample_funcs"].append(fname.split(".", 1)[1] if "." in fname else fname)

    pkg_list = []
    for p in packages.values():
        pkg_list.append(
            {
                "package": p["package"],
                "func_count": p["func_count"],
                "file_count": len(p["files"]),
                "code_bytes": p["code_bytes"],
                "files": sorted(p["files"]),
                "sample_funcs": p["sample_funcs"],
            }
        )
    pkg_list.sort(key=lambda x: (-x["code_bytes"], x["package"]))

    jetski_files = sorted({f for f in all_files if "jetski" in f or "antigravity" in f})
    cli_files = sorted({f for f in jetski_files if "/cli" in f or f.startswith("devtools/jetski/")})

    return {
        "header": {
            "offset": base,
            "magic": hex(magic_val),
            "min_lc": min_lc,
            "ptr_size": ptr_size,
            "nfunc": nfunc,
            "nfiles": nfiles,
        },
        "all_files": all_files,
        "jetski_files": jetski_files,
        "cli_files": cli_files,
        "jetski_funcs": jetski_funcs,
        "packages": pkg_list,
    }


def extract_proto_services(jetski_funcs: list[dict[str, Any]], data: bytes) -> dict[str, Any]:
    """Recover exact RPC service and method names from Go gRPC/Connect/StdioRPC symbols."""
    # Exact proto full service names (up to the trailing '/') avoid rodata string concatenation noise.
    full_names: dict[str, str] = {}
    for m in re.finditer(rb"/([a-z][a-z0-9_.]+\.([A-Za-z0-9_]+Service))/", data):
        full = m.group(1).decode("ascii", "replace")
        short = m.group(2).decode("ascii", "replace")
        full_names[short] = full

    message_types: set[tuple[str, str]] = set()
    candidates: dict[tuple[str, str], dict[str, Any]] = {}

    pat_method = re.compile(
        r"^\(\*?(?:Unimplemented)?([A-Za-z0-9_]+?)(?:Client|Server|Handler)\)\.([A-Z][A-Za-z0-9_]+?)(?:-fm)?$"
    )
    pat_iface = re.compile(r"^([A-Za-z0-9_]+?)Handler\.([A-Z][A-Za-z0-9_]+?)-fm$")

    for rec in jetski_funcs:
        pkg = rec["package"]
        if not (
            "_pb/" in pkg
            or pkg.endswith("_pb")
            or pkg.endswith("_connect")
            or pkg.endswith("_grpc")
            or pkg.endswith("_stdiorpc")
        ):
            continue

        short_fn = rec["name"][len(pkg) + 1 :] if rec["name"].startswith(pkg + ".") else rec["name"]
        m_proto = re.match(r"^\(\*?([A-Za-z0-9_]+)\)\.(?:ProtoMessage|ProtoReflect)$", short_fn)
        if m_proto:
            message_types.add((pkg, m_proto.group(1)))
            continue

        m = pat_method.match(short_fn) or pat_iface.match(short_fn)
        if not m:
            continue
        raw_svc, method = m.group(1), m.group(2)
        if method == "SetMethodHandler" or method.startswith("MustEmbed"):
            continue
        svc = raw_svc[0].upper() + raw_svc[1:]
        key = (pkg, svc)
        entry = candidates.setdefault(
            key,
            {
                "service": svc,
                "full_name": full_names.get(svc, ""),
                "go_package": pkg,
                "methods": set(),
            },
        )
        entry["methods"].add(method)

    # Deduplicate by (service, full_name) while merging go_packages & methods cleanly, excluding protobuf messages
    merged: dict[str, dict[str, Any]] = {}
    for (pkg, svc), info in candidates.items():
        if (pkg, svc) in message_types:
            continue
        # Only retain actual RPC services (in _connect/_grpc/_stdiorpc, known *Service, or verified proto service path)
        if not (
            pkg.endswith(("_connect", "_grpc", "_stdiorpc"))
            or svc.endswith("Service")
            or info["full_name"]
        ):
            continue
        key = info["full_name"] or f"{pkg.rsplit('/', 1)[0]}.{svc}"
        m_entry = merged.setdefault(
            key,
            {
                "service": svc,
                "full_name": info["full_name"] or key,
                "go_packages": set(),
                "methods": set(),
            },
        )
        m_entry["go_packages"].add(pkg)
        m_entry["methods"].update(info["methods"])

    services = [
        {
            "service": v["service"],
            "full_name": v["full_name"],
            "go_packages": sorted(v["go_packages"]),
            "method_count": len(v["methods"]),
            "methods": sorted(v["methods"]),
        }
        for v in merged.values()
    ]
    services.sort(key=lambda x: (-x["method_count"], x["full_name"]))
    return {"service_count": len(services), "services": services}


def capture_cli_commands(binary: Path) -> dict[str, Any]:
    _, ver = run_cmd([str(binary), "--version"])
    _, root_help = run_cmd([str(binary), "--help"])

    subcommands = [
        "acp",
        "agent",
        "changelog",
        "install",
        "mcp",
        "mic-serve",
        "models",
        "plugin",
        "remote-control",
        "update",
    ]
    nested: dict[str, list[str]] = {
        "mcp": ["add", "remove", "list", "enable", "disable"],
        "remote-control": ["start", "status", "stop"],
        "plugin": ["validate"],
    }

    subcommand_helps: dict[str, Any] = {}
    for cmd in subcommands:
        _, out = run_cmd([str(binary), cmd, "--help"])
        entry: dict[str, Any] = {"help": out.strip(), "subcommands": {}}
        for sub in nested.get(cmd, []):
            _, sout = run_cmd([str(binary), cmd, sub, "--help"])
            entry["subcommands"][sub] = sout.strip()
        subcommand_helps[cmd] = entry

    return {
        "version": ver.strip(),
        "root_help": root_help.strip(),
        "subcommands": subcommand_helps,
    }


def classify_embed_table(entries: list[dict[str, Any]]) -> str:
    first = entries[0]["name"]
    if first.startswith("google3/"):
        return "prompts"
    if first.startswith("metaschemas/"):
        return "metaschemas"
    if first.startswith("assets/"):
        return "assets"
    if first.endswith(".xml") and "/" not in first:
        return "chroma-styles"
    if first.startswith("embedded/"):
        return "chroma-lexers"
    return f"embed_{re.sub(r'[^a-zA-Z0-9_-]', '_', first)}"


def main() -> None:
    parser = argparse.ArgumentParser(description="Extract lossless assets and Go metadata from agy")
    parser.add_argument("--input", type=Path, default=DEFAULT_BINARY, help="Path to agy Mach-O binary")
    parser.add_argument("--out", type=Path, default=DEFAULT_CLI_ROOT, help="Target cli/ directory")
    args = parser.parse_args()

    binary: Path = args.input
    cli_root: Path = args.out
    if not binary.is_file():
        raise FileNotFoundError(f"Binary not found: {binary}")

    data = binary.read_bytes()
    bin_sha256 = sha256_bytes(data)

    macho = parse_macho(data)
    buildinfo = extract_go_buildinfo(data)
    _, otool_libs = run_cmd(["otool", "-L", str(binary)])
    linked_libs = [
        line.strip().split(" (")[0]
        for line in otool_libs.splitlines()[1:]
        if line.strip()
    ]

    cli_commands = capture_cli_commands(binary)

    meta_dir = cli_root / "metadata"
    bin_dir = cli_root / "bin"
    extracted_dir = cli_root / "extracted"
    for d in (meta_dir, bin_dir, extracted_dir):
        d.mkdir(parents=True, exist_ok=True)

    # 1. Binary info & Mach-O segments
    agy_info = {
        "binary_path": str(binary),
        "version": cli_commands["version"],
        "size_bytes": len(data),
        "sha256": bin_sha256,
        "format": "Mach-O 64-bit executable arm64",
        "go_buildinfo": buildinfo,
        "linked_libraries": linked_libs,
        "node_sea_present": b"NODE_SEA" in data,
        "app_asar_present": b"app.asar" in data,
    }
    (bin_dir / "agy-info.json").write_text(json.dumps(agy_info, indent=2) + "\n", encoding="utf-8")
    (meta_dir / "macho-segments.json").write_text(json.dumps(macho, indent=2) + "\n", encoding="utf-8")
    (meta_dir / "cli-commands.json").write_text(json.dumps(cli_commands, indent=2) + "\n", encoding="utf-8")

    # 2. Extract Go embed.FS tables
    embed_tables = find_embed_fs_tables(data, macho["segments"])
    embed_manifest = {}
    for tbl in embed_tables:
        label = classify_embed_table(tbl["entries"])
        stats = write_embed_table(data, tbl["entries"], extracted_dir / label)
        embed_manifest[label] = {
            "header_offset": tbl["header_offset"],
            **stats,
        }

    # 3. Extract ZIP archives (EOCDs)
    eocds = [m.start() for m in re.finditer(b"PK\x05\x06", data)]
    zips_manifest = {}
    for epos in eocds:
        _, _, _, _, cd_size, cd_off, cmt_len = struct.unpack_from("<HHHHIIH", data, epos + 4)
        zstart = epos - cd_size - cd_off
        with zipfile.ZipFile(io.BytesIO(data[zstart : epos + 22 + cmt_len])) as zf:
            names = zf.namelist()
        label = "web-bundle" if "index.html" in names else "tzdata"
        zips_manifest[label] = extract_zip_at(data, epos, extracted_dir / label)

    # 4. Extract tar.gz payloads (tsserver & python)
    tars_manifest = {}
    for m in re.finditer(b"\x1f\x8b\x08", data):
        goff = m.start()
        if goff < 182900000:
            continue
        dec = zlib.decompressobj(16 + zlib.MAX_WBITS)
        try:
            head = dec.decompress(data[goff : goff + 4096])
        except Exception:
            continue
        if head.startswith(b"lib/_tsserver.js"):
            tars_manifest["tsserver"] = extract_targz_at(data, goff, 5_000_000, extracted_dir / "tsserver")
        elif head.startswith(b"python/"):
            tars_manifest["python"] = extract_targz_at(data, goff, 25_000_000, extracted_dir / "python")

    # 5. Extract standalone gzipped fonts and FileDescriptorProtos
    misc_manifest = extract_misc_gzip_blobs(data, extracted_dir)

    # 6. Recover Go gopclntab metadata
    pcln = parse_gopclntab(data, macho["text_section_vmaddr"])
    (meta_dir / "go-files.txt").write_text("\n".join(pcln["all_files"]) + "\n", encoding="utf-8")
    (meta_dir / "jetski-files.txt").write_text("\n".join(pcln["jetski_files"]) + "\n", encoding="utf-8")
    (meta_dir / "cli-files.txt").write_text("\n".join(pcln["cli_files"]) + "\n", encoding="utf-8")
    (meta_dir / "jetski-packages.json").write_text(
        json.dumps(
            {
                "header": pcln["header"],
                "total_go_files": len(pcln["all_files"]),
                "jetski_file_count": len(pcln["jetski_files"]),
                "cli_file_count": len(pcln["cli_files"]),
                "jetski_func_count": len(pcln["jetski_funcs"]),
                "package_count": len(pcln["packages"]),
                "packages": pcln["packages"],
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    with (meta_dir / "jetski-functions.jsonl").open("w", encoding="utf-8") as jf:
        for rec in pcln["jetski_funcs"]:
            jf.write(json.dumps(rec) + "\n")

    # 7. Protobuf / ConnectRPC service routes (from gopclntab + exact service prefixes)
    proto_services = extract_proto_services(pcln["jetski_funcs"], data)
    (meta_dir / "proto-services.json").write_text(
        json.dumps(proto_services, indent=2) + "\n", encoding="utf-8"
    )

    # 8. Embedded manifest summary
    manifest = {
        "source_binary": str(binary),
        "source_sha256": bin_sha256,
        "source_size": len(data),
        "embed_fs": embed_manifest,
        "zips": zips_manifest,
        "targz": tars_manifest,
        "fonts": misc_manifest["fonts"],
        "proto_descriptors": misc_manifest["proto_descriptors"],
    }
    (meta_dir / "embedded-manifest.json").write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
    )

    print(json.dumps(
        {
            "version": cli_commands["version"],
            "embed_fs_tables": {k: v["files"] for k, v in embed_manifest.items()},
            "zips": {k: v["files"] for k, v in zips_manifest.items()},
            "targz": {k: v["files"] for k, v in tars_manifest.items()},
            "fonts": len(misc_manifest["fonts"]),
            "proto_descriptors": len(misc_manifest["proto_descriptors"]),
            "total_go_files": len(pcln["all_files"]),
            "jetski_files": len(pcln["jetski_files"]),
            "cli_files": len(pcln["cli_files"]),
            "jetski_funcs": len(pcln["jetski_funcs"]),
            "jetski_packages": len(pcln["packages"]),
            "proto_services": proto_services["service_count"],
        },
        indent=2,
    ))


if __name__ == "__main__":
    main()
