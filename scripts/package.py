"""Create a shareable source bundle from non-ignored project files."""
import pathlib
import subprocess
import zipfile

root = pathlib.Path(__file__).resolve().parent.parent
listed = subprocess.check_output(["git", "ls-files", "-c", "-o", "--exclude-standard", "-z"], cwd=root).decode("utf-8").split("\0")
target = root / "output" / "onbo-mvp.zip"
target.parent.mkdir(exist_ok=True)
paths = sorted(set(name for name in listed if name and not name.endswith(".zip") and not name.startswith("output/video/raw/")))
for name in paths:
    if name == ".env" or (name.startswith(".env.") and name not in (".env.example", ".env.sample")) or name.startswith("certs/"):
        raise RuntimeError("A local secret/configuration path would enter the archive")
with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as archive:
    for name in paths:
        file = root / name
        if file.is_file():
            archive.write(file, name)
with zipfile.ZipFile(target) as archive:
    if archive.testzip() is not None:
        raise RuntimeError("Archive integrity check failed")
    required = ["README.md", ".env.example", "go.mod", "docker-compose.yml", "output/pdf/onbo-mvp.pdf", "output/video/onbo-demo.webm"]
    for name in required:
        if name not in archive.namelist():
            raise RuntimeError(f"Missing deliverable: {name}")
print(f"Created output/onbo-mvp.zip: {target.stat().st_size} bytes, {len(paths)} files")
