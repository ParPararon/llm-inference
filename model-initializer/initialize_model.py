import json
import os
import shutil
import tempfile
from pathlib import Path

from huggingface_hub import snapshot_download


MARKER_NAME = ".initialized.json"


def required_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise ValueError(f"{name} must be set")
    return value


def is_model_initialized(
    model_path: Path,
    model_id: str,
    revision: str,
) -> bool:
    try:
        metadata = json.loads(
            (model_path / MARKER_NAME).read_text(encoding="utf-8")
        )
    except (OSError, ValueError):
        return False

    return (
        metadata == {"model_id": model_id, "revision": revision}
        and (model_path / "config.json").is_file()
        and any(model_path.glob("*.safetensors"))
    )


def validate_model(model_path: Path) -> None:
    if not (model_path / "config.json").is_file():
        raise RuntimeError("Downloaded model is missing config.json")

    if not any(model_path.glob("*.safetensors")):
        raise RuntimeError("Downloaded model is missing safetensors weights")


def initialize_model(
    model_id: str,
    revision: str,
    model_path: Path,
    token: str | None = None,
) -> None:
    backup_path = model_path.with_name(f".{model_path.name}.backup")

    if backup_path.exists():
        if model_path.exists():
            shutil.rmtree(backup_path)
        else:
            backup_path.rename(model_path)

    if is_model_initialized(model_path, model_id, revision):
        print("Model is already initialized", flush=True)
        return

    model_path.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(
        prefix=f".{model_path.name}-",
        dir=model_path.parent,
    ) as temporary_directory:
        temporary_path = Path(temporary_directory)

        print(
            f"Downloading model {model_id} at revision {revision}",
            flush=True,
        )

        snapshot_download(
            repo_id=model_id,
            revision=revision,
            local_dir=temporary_path,
            token=token,
            ignore_patterns=["*.bin", "*.h5", "*.msgpack", "*.onnx"],
        )

        validate_model(temporary_path)

        metadata = {"model_id": model_id, "revision": revision}
        (temporary_path / MARKER_NAME).write_text(
            json.dumps(metadata),
            encoding="utf-8",
        )

        if model_path.exists():
            model_path.rename(backup_path)

        try:
            temporary_path.rename(model_path)
        except OSError:
            if backup_path.exists():
                backup_path.rename(model_path)
            raise

        if backup_path.exists():
            shutil.rmtree(backup_path)

    print("Model initialized successfully", flush=True)


def main() -> None:
    initialize_model(
        model_id=required_env("MODEL_ID"),
        revision=required_env("MODEL_REVISION"),
        model_path=Path(required_env("MODEL_PATH")),
        token=os.environ.get("HF_TOKEN") or None,
    )


if __name__ == "__main__":
    main()
