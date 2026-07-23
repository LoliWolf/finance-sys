import subprocess
import unittest
from pathlib import Path
from unittest import mock

import ocr_pdf_articles as ocr


class MissingPopplerFallbackTest(unittest.TestCase):
    @mock.patch.object(subprocess, "run", side_effect=FileNotFoundError("missing"))
    def test_run_returns_command_not_found_result(self, _run: mock.Mock) -> None:
        result = ocr.run(["pdftotext", "input.pdf", "-"])

        self.assertEqual(127, result.returncode)
        self.assertEqual("", result.stdout)
        self.assertIn("pdftotext", result.stderr)

    @mock.patch.object(subprocess, "run", side_effect=FileNotFoundError("missing"))
    def test_layout_extraction_returns_empty_text(self, _run: mock.Mock) -> None:
        self.assertEqual("", ocr.extract_pdf_text_layout(Path("input.pdf")))


if __name__ == "__main__":
    unittest.main()
