"""Shared process helpers for isolated, reproducible verification runners."""

import ctypes
import hashlib
import json
import os
import platform
import socket
import subprocess
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def command(args, **kwargs):
    return subprocess.run(
        [str(a) for a in args], check=True, cwd=kwargs.pop("cwd", ROOT), **kwargs
    )


def cargo_binary(name, features="evaluation"):
    result = command(
        [
            "cargo",
            "build",
            "--locked",
            "--release",
            "--features",
            features,
            "--bin",
            name,
            "--manifest-path",
            ROOT / "services/engine/Cargo.toml",
            "--message-format=json",
        ],
        capture_output=True,
        text=True,
    )
    for line in result.stdout.splitlines():
        entry = json.loads(line)
        if entry.get("executable") and entry.get("target", {}).get("name") == name:
            return entry["executable"]
    raise RuntimeError("cargo did not return executable")


def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def wait_port(number, process):
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError("verification process exited before readiness")
        try:
            with socket.create_connection(("127.0.0.1", number), timeout=1):
                return
        except OSError:
            time.sleep(0.25)
    raise TimeoutError("verification process startup")


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=15)


def metadata():
    result = {
        "schema_version": 1,
        "os": platform.platform(),
        "cpu": platform.processor(),
        "logical_cpus": os.cpu_count(),
        "commit": subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
        ).strip(),
        "working_changes": subprocess.check_output(
            ["git", "status", "--porcelain"], cwd=ROOT, text=True
        ).splitlines(),
        "versions": {
            name: subprocess.check_output(args, text=True).strip()
            for name, args in {
                "go": ["go", "version"],
                "rust": ["rustc", "--version"],
                "node": ["node", "--version"],
                "docker": ["docker", "version", "--format", "{{.Server.Version}}"],
            }.items()
        },
    }
    if os.name == "nt":
        info = subprocess.check_output(
            [
                "powershell",
                "-NoProfile",
                "-Command",
                "Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors | ConvertTo-Json",
            ],
            text=True,
        )
        result["hardware"] = json.loads(info)
        result["host_memory_bytes"] = int(
            subprocess.check_output(
                [
                    "powershell",
                    "-NoProfile",
                    "-Command",
                    "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory",
                ],
                text=True,
            ).strip()
        )
    else:
        result["hardware"] = subprocess.check_output(["lscpu"], text=True)
        result["host_memory"] = Path("/proc/meminfo").read_text().splitlines()[0]
        result["cgroup_limits"] = {
            name: Path("/sys/fs/cgroup", name).read_text().strip()
            for name in ("cpu.max", "memory.max")
            if Path("/sys/fs/cgroup", name).exists()
        }
    result["model"] = json.loads((ROOT / "models/general-ner.json").read_text())
    names = (
        subprocess.check_output(
            ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
            cwd=ROOT,
        )
        .decode()
        .strip("\0")
        .split("\0")
    )
    fingerprints = {}
    for name in sorted(set(names)):
        relative = Path(name)
        if any(
            part in {".git", "target", "node_modules", "output", "dist"}
            for part in relative.parts
        ):
            continue
        if relative.suffix not in {
            ".go",
            ".rs",
            ".py",
            ".toml",
            ".lock",
            ".json",
            ".proto",
            ".mod",
            ".sum",
        }:
            continue
        path = ROOT / relative
        if path.is_file() and not path.is_symlink():
            fingerprints[name] = hashlib.sha256(path.read_bytes()).hexdigest()
    result["source_sha256"] = fingerprints
    return result


def memory(pid):
    if os.name == "nt":
        from ctypes import wintypes

        class Counters(ctypes.Structure):
            _fields_ = [("cb", wintypes.DWORD), ("PageFaultCount", wintypes.DWORD)] + [
                (name, ctypes.c_size_t)
                for name in (
                    "PeakWorkingSetSize",
                    "WorkingSetSize",
                    "QuotaPeakPagedPoolUsage",
                    "QuotaPagedPoolUsage",
                    "QuotaPeakNonPagedPoolUsage",
                    "QuotaNonPagedPoolUsage",
                    "PagefileUsage",
                    "PeakPagefileUsage",
                )
            ]

        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        psapi = ctypes.WinDLL("psapi", use_last_error=True)
        kernel.OpenProcess.restype = wintypes.HANDLE
        kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        psapi.GetProcessMemoryInfo.argtypes = [
            wintypes.HANDLE,
            ctypes.POINTER(Counters),
            wintypes.DWORD,
        ]
        handle = kernel.OpenProcess(0x1000 | 0x10, False, pid)
        if not handle:
            raise OSError("cannot open process memory counters")
        try:
            counter = Counters()
            counter.cb = ctypes.sizeof(counter)
            if not psapi.GetProcessMemoryInfo(
                handle, ctypes.byref(counter), counter.cb
            ):
                raise OSError("cannot read process memory counters")
            return {
                "rss_bytes": counter.WorkingSetSize,
                "process_lifetime_peak_rss_bytes": counter.PeakWorkingSetSize,
            }
        finally:
            kernel.CloseHandle(handle)
    values = {}
    for line in Path("/proc", str(pid), "status").read_text().splitlines():
        if line.startswith(("VmRSS:", "VmHWM:")):
            key = (
                "rss_bytes"
                if line.startswith("VmRSS:")
                else "process_lifetime_peak_rss_bytes"
            )
            values[key] = int(line.split()[1]) * 1024
    if "rss_bytes" not in values:
        raise RuntimeError("RSS unavailable")
    return values


def fixture_stats(fixture):
    request = urllib.request.Request(
        fixture["url"] + "/__bench/stats",
        headers={"Authorization": "Bearer " + fixture["stats_token"]},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        return json.load(response)
