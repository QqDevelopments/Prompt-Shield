"""Regenerate with pinned tools into temporary output and compare exact bytes."""

import hashlib
import os
import tempfile
import urllib.request
import zipfile
from pathlib import Path

from verification import ROOT, command

ARCHIVES = {
    "nt": ("win64", "70381b116ab0d71cb6a5177d9b17c7c13415866603a0fd40d513dafe32d56c35"),
    "posix": (
        "linux-x86_64",
        "96553041f1a91ea0efee963cb16f462f5985b4d65365f3907414c360044d8065",
    ),
}


def main():
    platform, expected = ARCHIVES[os.name]
    with tempfile.TemporaryDirectory(prefix="ps-proto-") as temporary:
        base = Path(temporary)
        archive = base / "protoc.zip"
        with urllib.request.urlopen(
            f"https://github.com/protocolbuffers/protobuf/releases/download/v31.1/protoc-31.1-{platform}.zip",
            timeout=60,
        ) as stream:
            archive.write_bytes(stream.read())
        if hashlib.sha256(archive.read_bytes()).hexdigest() != expected:
            raise RuntimeError("protoc archive checksum mismatch")
        with zipfile.ZipFile(archive) as bundle:
            for member in bundle.infolist():
                path = (base / member.filename).resolve()
                if not path.is_relative_to(base.resolve()):
                    raise RuntimeError("unsafe archive path")
            bundle.extractall(base)
        compiler = base / "bin" / ("protoc.exe" if os.name == "nt" else "protoc")
        compiler.chmod(0o755)
        plugins = base / "plugins"
        plugins.mkdir()
        output = base / "generated"
        output.mkdir()
        env = {**os.environ, "GOBIN": str(plugins)}
        for package in (
            "google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10",
            "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0",
        ):
            command(["go", "install", package], env=env)
        env["PATH"] = str(plugins) + os.pathsep + env["PATH"]
        command(
            [
                compiler,
                "--proto_path=" + str(ROOT / "proto"),
                "--go_out=" + str(output),
                "--go_opt=paths=source_relative",
                "--go-grpc_out=" + str(output),
                "--go-grpc_opt=paths=source_relative",
                ROOT / "proto/shield.proto",
            ],
            env=env,
        )
        for name in ("shield.pb.go", "shield_grpc.pb.go"):
            # Git checkout EOL conversion does not change generated content.
            expected_bytes = (
                (ROOT / "services/gateway/gen/shieldv1" / name)
                .read_bytes()
                .replace(b"\r\n", b"\n")
            )
            if (output / name).read_bytes().replace(b"\r\n", b"\n") != expected_bytes:
                raise RuntimeError("protobuf drift: " + name)
        print("both protobuf bindings match pinned generator output")


if __name__ == "__main__":
    main()
