#!/usr/bin/env python3
"""Copy license texts for FlyTogether's runtime Go and browser dependencies."""
from __future__ import annotations

from collections import Counter
import json
import shutil
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
NOTICE_ROOT = ROOT / "third_party" / "licenses"


def package_name(lock_path: str) -> str:
    return lock_path.removeprefix("node_modules/")


def copy_license_files(source: Path, target: Path) -> list[str]:
    files = sorted(
        path for path in source.iterdir()
        if path.is_file() and path.name.lower().startswith(("license", "copying", "notice"))
    )
    if not files:
        raise RuntimeError(f"No license text found for dependency at {source}")
    target.mkdir(parents=True, exist_ok=True)
    for file in files:
        # Keep upstream notice text intact while avoiding extra blank EOF lines
        # that make git diff --check fail on the generated notice bundle.
        text = file.read_text(errors="replace").rstrip("\n") + "\n"
        (target / file.name).write_text(text)
    return [file.name for file in files]


def main() -> None:
    npm_lock = json.loads((ROOT / "web/package-lock.json").read_text())
    production = {
        path: metadata for path, metadata in npm_lock["packages"].items()
        if path.startswith("node_modules/") and not metadata.get("dev") and not metadata.get("devOptional")
    }
    lock_license_counts: Counter[str] = Counter(
        metadata.get("license", "UNDECLARED")
        for path, metadata in npm_lock["packages"].items()
        if path.startswith("node_modules/")
    )
    if not production:
        raise RuntimeError("No production npm packages found in package-lock.json")
    go_output = subprocess.check_output(
        ["go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}{{end}}", "./cmd/server"],
        cwd=ROOT, text=True,
    )
    go_modules: dict[tuple[str, str], Path] = {}
    for line in go_output.splitlines():
        fields = line.split("\t")
        if len(fields) == 3 and fields[1]:
            go_modules[(fields[0], fields[1])] = Path(fields[2])
    if not go_modules:
        raise RuntimeError("No runtime Go modules found")

    if NOTICE_ROOT.exists():
        shutil.rmtree(NOTICE_ROOT)
    entries = [
        "# Third-party notices",
        "",
        "FlyTogether is distributed under the MIT License in the repository root. The following license texts apply to the pinned runtime dependencies and are copied here to preserve upstream notices in source and binary distributions.",
        "",
        "## Browser runtime dependencies",
        "",
        "| Package | Version | SPDX metadata | Included notice files |",
        "| --- | --- | --- | --- |",
    ]
    for lock_path, metadata in sorted(production.items()):
        name, version = package_name(lock_path), metadata["version"]
        license_id = metadata.get("license", "UNDECLARED")
        source = ROOT / "web" / lock_path
        relative = Path("npm") / name / version
        filenames = copy_license_files(source, NOTICE_ROOT / relative)
        links = ", ".join(f"[`{filename}`](third_party/licenses/{relative.as_posix()}/{filename})" for filename in filenames)
        entries.append(f"| `{name}` | `{version}` | `{license_id}` | {links} |")

    entries.extend([
        "",
        "## Go server runtime dependencies",
        "",
        "| Module | Version | Included notice files |",
        "| --- | --- | --- |",
    ])
    for (module, version), source in sorted(go_modules.items()):
        relative = Path("go") / module / version
        filenames = copy_license_files(source, NOTICE_ROOT / relative)
        links = ", ".join(f"[`{filename}`](third_party/licenses/{relative.as_posix()}/{filename})" for filename in filenames)
        entries.append(f"| `{module}` | `{version}` | {links} |")

    entries.extend([
        "",
        "## Scope",
        "",
        f"The npm lockfile contains {len([p for p in npm_lock['packages'] if p.startswith('node_modules/')])} pinned package records, including build/test and platform-optional packages. This notice copies license texts for the {len(production)} production browser packages that are included in the shipped site; build/test-only packages are not copied into the runtime site. Lockfile SPDX metadata counts: " + ", ".join(f"`{license_id}` {count}" for license_id, count in sorted(lock_license_counts.items())) + ".",
        "",
        "Go notices are collected from the exact modules in `go list -deps ./cmd/server`; nested `LICENSE-*` files are retained because some modules include SQLite or other third-party attributions. Refresh these files after changing runtime dependencies with `python3 scripts/refresh-third-party-notices.py` after `npm ci` and `go mod download`.",
        "",
        "Project-owned browser assets are `web/public/favicon.svg` and `web/public/pcm-capture.js`; the board, layouts, and sound cues are generated by project code. Product and QA screenshots are captured from FlyTogether itself. No third-party photos, recorded voices, or bundled external fonts are included. Phosphor icons are used from the pinned package listed above.",
        "",
    ])
    (ROOT / "THIRD_PARTY_NOTICES.md").write_text("\n".join(entries))
    print(json.dumps({"passed": True, "npm_runtime_packages": len(production), "npm_lock_records": len([p for p in npm_lock['packages'] if p.startswith('node_modules/')]), "go_runtime_modules": len(go_modules), "notice_directory": "third_party/licenses", "report": "THIRD_PARTY_NOTICES.md"}))


if __name__ == "__main__":
    main()
