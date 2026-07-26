import os
import platform
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import ocr_pdf_articles as ocr


def real_pdf_fixture() -> Path:
    configured = os.environ.get("GUZIYUAN_TEST_PDF", "").strip()
    if configured:
        return Path(configured).expanduser().resolve()
    for parent in Path(__file__).resolve().parents:
        candidate = parent / "testdata" / "guziyuanpdf" / "大成路旁7.21强修复.pdf"
        if candidate.is_file():
            return candidate
    return Path("testdata/guziyuanpdf/大成路旁7.21强修复.pdf").resolve()


class MissingCommandFallbackTest(unittest.TestCase):
    @mock.patch.object(subprocess, "run", side_effect=FileNotFoundError("missing"))
    def test_run_returns_command_not_found_result(self, _run: mock.Mock) -> None:
        result = ocr.run(["pdftotext", "input.pdf", "-"])

        self.assertEqual(127, result.returncode)
        self.assertEqual("", result.stdout)
        self.assertIn("pdftotext", result.stderr)

    @mock.patch.object(subprocess, "run", side_effect=FileNotFoundError("missing"))
    def test_layout_extraction_returns_empty_text(self, _run: mock.Mock) -> None:
        self.assertEqual("", ocr.extract_pdf_text_layout(Path("input.pdf")))


class TextCleanupTest(unittest.TestCase):
    def test_removes_common_misspelling_of_guziyuan_watermark(self) -> None:
        cleaned = ocr.clean_ocr_text(
            "标题 wwwww.guziuan.cn 中段 www.gu2iyuan.cn 正文"
        )

        self.assertEqual("标题中段正文", cleaned)


@unittest.skipUnless(platform.system().lower() == "darwin", "macOS native OCR test")
class MacOSPDFIntegrationTest(unittest.TestCase):
    def test_real_guziyuan_pdf_uses_native_render_and_vision_ocr(self) -> None:
        pdf = real_pdf_fixture()
        if not pdf.is_file():
            self.skipTest(f"real PDF fixture is unavailable: {pdf}")

        with tempfile.TemporaryDirectory(prefix="guziyuan_ocr_test_") as tmp:
            text = ocr.extract_text_from_pdf(
                pdf=pdf,
                work_root=Path(tmp),
                dpi=150,
                chunk_height=4200,
                overlap=220,
                language="zh-Hans-CN",
                sat_threshold=35,
                light_threshold=75,
                source="auto",
                extract_width=2200,
                keep_work=False,
            )

        self.assertGreater(len(ocr.compact_for_compare(text)), 550)
        self.assertIn("7月22日强修复", text)
        self.assertIn("共进股份", text)
        self.assertIn("立新能源", text)
        self.assertNotRegex(text.lower(), r"g[uuv][z2][i1l]y?u?a?n")


if __name__ == "__main__":
    unittest.main()
