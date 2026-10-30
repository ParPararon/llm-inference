import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from initialize_model import (
    initialize_model,
    is_model_initialized,
    required_env,
)


MODEL_ID = "Qwen/Qwen2.5-1.5B-Instruct"
REVISION = "test-revision"


class ModelInitializerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.model_path = Path(self.directory.name) / "model"

    def write_model(self, path: Path, revision: str) -> None:
        path.mkdir(parents=True, exist_ok=True)
        (path / "config.json").write_text("{}", encoding="utf-8")
        (path / "model.safetensors").write_bytes(b"weights")
        (path / ".initialized.json").write_text(
            json.dumps({"model_id": MODEL_ID, "revision": revision}),
            encoding="utf-8",
        )

    def mock_download(self, **kwargs) -> None:
        model_path = Path(kwargs["local_dir"])
        (model_path / "config.json").write_text("{}", encoding="utf-8")
        (model_path / "model.safetensors").write_bytes(b"new weights")

    def test_downloads_model_and_writes_marker(self) -> None:
        with patch(
            "initialize_model.snapshot_download",
            side_effect=self.mock_download,
        ) as download:
            initialize_model(MODEL_ID, REVISION, self.model_path)

        download.assert_called_once()
        self.assertEqual(download.call_args.kwargs["repo_id"], MODEL_ID)
        self.assertEqual(download.call_args.kwargs["revision"], REVISION)
        self.assertTrue(
            is_model_initialized(self.model_path, MODEL_ID, REVISION)
        )

    def test_skips_download_when_model_is_initialized(self) -> None:
        self.write_model(self.model_path, REVISION)

        with patch("initialize_model.snapshot_download") as download:
            initialize_model(MODEL_ID, REVISION, self.model_path)

        download.assert_not_called()

    def test_failed_download_does_not_mark_model_ready(self) -> None:
        with patch(
            "initialize_model.snapshot_download",
            side_effect=RuntimeError("Download failed"),
        ):
            with self.assertRaisesRegex(RuntimeError, "Download failed"):
                initialize_model(MODEL_ID, REVISION, self.model_path)

        self.assertFalse(self.model_path.exists())

    def test_revision_change_replaces_old_files(self) -> None:
        self.write_model(self.model_path, "old-revision")
        (self.model_path / "old-file.txt").write_text(
            "outdated", encoding="utf-8"
        )

        with patch(
            "initialize_model.snapshot_download",
            side_effect=self.mock_download,
        ):
            initialize_model(MODEL_ID, REVISION, self.model_path)

        self.assertTrue(
            is_model_initialized(self.model_path, MODEL_ID, REVISION)
        )
        self.assertEqual(
            (self.model_path / "model.safetensors").read_bytes(),
            b"new weights",
        )
        self.assertFalse((self.model_path / "old-file.txt").exists())

    def test_failed_update_preserves_old_model(self) -> None:
        self.write_model(self.model_path, "old-revision")

        with patch(
            "initialize_model.snapshot_download",
            side_effect=RuntimeError("Download failed"),
        ):
            with self.assertRaisesRegex(RuntimeError, "Download failed"):
                initialize_model(MODEL_ID, REVISION, self.model_path)

        self.assertTrue(
            is_model_initialized(self.model_path, MODEL_ID, "old-revision")
        )
        self.assertEqual(
            (self.model_path / "model.safetensors").read_bytes(),
            b"weights",
        )

    def test_rejects_incomplete_download(self) -> None:
        def incomplete_download(**kwargs):
            path = Path(kwargs["local_dir"])
            (path / "config.json").write_text("{}", encoding="utf-8")

        with patch(
            "initialize_model.snapshot_download",
            side_effect=incomplete_download,
        ):
            with self.assertRaisesRegex(RuntimeError, "missing safetensors"):
                initialize_model(MODEL_ID, REVISION, self.model_path)

        self.assertFalse(self.model_path.exists())

    def test_recovers_backup_after_interrupted_update(self) -> None:
        backup_path = self.model_path.with_name(".model.backup")
        self.write_model(backup_path, REVISION)

        with patch("initialize_model.snapshot_download") as download:
            initialize_model(MODEL_ID, REVISION, self.model_path)

        download.assert_not_called()
        self.assertTrue(
            is_model_initialized(self.model_path, MODEL_ID, REVISION)
        )
        self.assertFalse(backup_path.exists())

    def test_rejects_empty_environment_variable(self) -> None:
        with patch.dict("os.environ", {"MODEL_ID": "  "}):
            with self.assertRaisesRegex(ValueError, "MODEL_ID must be set"):
                required_env("MODEL_ID")


if __name__ == "__main__":
    unittest.main()
