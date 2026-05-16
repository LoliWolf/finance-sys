#!/usr/bin/env python
"""Convert image-heavy Chinese article PDFs into cleaned text.

Pipeline:
1. Render PDF pages with Poppler's pdftoppm.
2. Remove high-saturation colored watermarks while preserving dark text.
3. Split very tall pages into Windows-OCR-safe chunks.
4. OCR chunks with Windows.Media.Ocr (offline zh-Hans-CN).
5. Merge overlaps, remove common watermark/UI noise, and optionally score against
   a visually transcribed reference.
"""

from __future__ import annotations

import argparse
import hashlib
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import unicodedata
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from difflib import SequenceMatcher
from pathlib import Path
from typing import Iterable, List, Optional

from PIL import Image, ImageChops, ImageFilter, ImageOps


ROOT = Path(__file__).resolve().parent
WIN_OCR = ROOT / "win_ocr.ps1"
MAC_OCR = ROOT / "mac_ocr.swift"

NOISE_PATTERNS = [
    r"www\.?guziyuan\.cn",
    r"['u/\.\w]{0,20}guz[\.\w]{0,4}yuan\.?cn",
    r"guziyuan\.cn",
    r"以下内容仅[真頁]爱粉可见",
    r"阅读数[:：]?\d+",
    r"[阅闯闻巧]?\W?[读卖]\W?数[:：]?\d+",
    r"\d{1,2}[:：]\d{2}\s*[^\u4e00-\u9fff]{0,6}\s*\d{1,3}",
    r"投[诉訴]",
    r"扌殳[诉訴]",
    r"一手无延迟",
    r"文章[&＆和]?直播合买",
    r"QQ微信\s*410025152\s*华仔",
    r"[+＋]?\s*Q{1,2}\s*微信\s*\d{6,}\s*华仔",
    r"[+＋]?\s*(?:Q{1,2})?微信\s*\d{6,}\s*华仔?",
    r"仅供内部参考[，,、]?(?:不)?构成投资建议",
    r"不构成投资建议",
    r"如涉及侵权[，,、]?请联系删除",
]


@dataclass
class OcrResult:
    pdf: Path
    output: Path
    chars: int
    quality: Optional[float] = None
    passed: Optional[bool] = None


def run(args: List[str], timeout: int = 180) -> subprocess.CompletedProcess:
    return subprocess.run(
        args,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=timeout,
        check=False,
    )


def find_pdfs(input_path: Path) -> List[Path]:
    if input_path.is_file() and input_path.suffix.lower() == ".pdf":
        return [input_path]
    return sorted(p for p in input_path.rglob("*.pdf") if p.is_file())


def slug_for(path: Path) -> str:
    rel = str(path)
    digest = hashlib.sha1(rel.encode("utf-8", "ignore")).hexdigest()[:10]
    safe = re.sub(r'[<>:"/\\|?*\s]+', "_", path.stem).strip("._")
    return f"{safe[:80]}_{digest}"


def work_slug_for(path: Path) -> str:
    digest = hashlib.sha1(str(path).encode("utf-8", "ignore")).hexdigest()[:16]
    return f"pdf_{digest}_{os.getpid()}"


def render_pdf(pdf: Path, render_dir: Path, dpi: int) -> List[Path]:
    prefix = render_dir / "page"
    cmd = [
        "pdftoppm",
        "-png",
        "-r",
        str(dpi),
        str(pdf),
        str(prefix),
    ]
    proc = run(cmd, timeout=600)
    if proc.returncode != 0:
        raise RuntimeError(f"pdftoppm failed for {pdf}\n{proc.stderr.strip()}")
    return sorted(render_dir.glob("page-*.png"))


def extract_pdf_text(pdf: Path) -> str:
    proc = run(["pdftotext", "-nopgbrk", str(pdf), "-"], timeout=120)
    if proc.returncode != 0:
        return ""
    return proc.stdout


def extract_pdf_text_layout(pdf: Path) -> str:
    """Extract PDF text while dropping translucent watermark text boxes."""
    proc = subprocess.run(
        ["pdftohtml", "-xml", "-stdout", str(pdf)],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=120,
        check=False,
    )
    if proc.returncode != 0 or not proc.stdout:
        return ""
    try:
        root = ET.fromstring(proc.stdout.decode("utf-8", "replace"))
    except ET.ParseError:
        return ""

    lines: List[str] = []
    known_fonts = {}
    for page in root.findall("page"):
        known_fonts.update({font.get("id"): font.attrib for font in page.findall("fontspec")})
        items = []
        for text_node in page.findall("text"):
            font = known_fonts.get(text_node.get("font"), {})
            opacity = float(font.get("opacity", "1") or "1")
            color = (font.get("color") or "").lower()
            family = (font.get("family") or "").lower()
            text = "".join(text_node.itertext()).strip()
            if not text:
                continue
            if opacity < 0.85:
                continue
            if "helvetica" in family and "guziyuan" in text.lower():
                continue
            if color and color not in {"#000000", "#111111", "#222222", "#333333"}:
                continue
            items.append((
                int(float(text_node.get("top", "0"))),
                int(float(text_node.get("left", "0"))),
                text,
            ))
        for _, _, text in sorted(items):
            lines.append(text)
    return "\n".join(lines)


def looks_like_article_text(text: str, min_chars: int = 120) -> bool:
    compact = compact_for_compare(text)
    cjk = sum(1 for ch in compact if "\u4e00" <= ch <= "\u9fff")
    return cjk >= min_chars and cjk / max(len(compact), 1) >= 0.45


def extract_pdf_images(pdf: Path, image_dir: Path, min_area: int = 40_000) -> List[Path]:
    """Extract embedded article screenshots, skipping tiny masks/icons."""
    image_dir.mkdir(parents=True, exist_ok=True)
    prefix = image_dir / "img"
    proc = run(["pdfimages", "-png", str(pdf), str(prefix)], timeout=300)
    if proc.returncode != 0:
        return []

    candidates: List[Path] = []
    for path in sorted(image_dir.glob("img-*")):
        if path.suffix.lower() not in {".png", ".jpg", ".jpeg", ".ppm", ".pbm", ".pgm"}:
            continue
        try:
            with Image.open(path) as im:
                width, height = im.size
                mode = im.mode
        except Exception:
            continue
        if width * height >= min_area and mode not in {"1", "L"}:
            candidates.append(path)
    return candidates


def prepare_extracted_image(image: Image.Image, target_width: int) -> Image.Image:
    rgb = image.convert("RGB")
    if target_width > 0 and rgb.width < target_width:
        scale = target_width / rgb.width
        new_size = (target_width, max(1, int(rgb.height * scale)))
        rgb = rgb.resize(new_size, Image.Resampling.LANCZOS)
    return rgb


def remove_colored_watermark(image: Image.Image, sat_threshold: int, light_threshold: int) -> Image.Image:
    rgb = image.convert("RGB")
    r, g, b = rgb.split()
    maxc = ImageChops.lighter(ImageChops.lighter(r, g), b)
    minc = ImageChops.darker(ImageChops.darker(r, g), b)
    sat = ImageChops.subtract(maxc, minc)
    gray = ImageOps.grayscale(rgb)

    sat_mask = sat.point(lambda p: 255 if p >= sat_threshold else 0)
    light_mask = gray.point(lambda p: 255 if p >= light_threshold else 0)
    colored_light_mask = ImageChops.multiply(sat_mask, light_mask)

    cleaned = Image.composite(Image.new("L", gray.size, 255), gray, colored_light_mask)
    cleaned = remove_color_banner_edges(cleaned, sat, min_banner_ratio=0.35)
    cleaned = ImageOps.autocontrast(cleaned, cutoff=1)
    cleaned = cleaned.filter(ImageFilter.SHARPEN)
    return cleaned


def remove_color_banner_edges(gray: Image.Image, saturation: Image.Image, min_banner_ratio: float) -> Image.Image:
    """Blank top/bottom high-saturation bands such as disclaimer banners."""
    width, height = gray.size
    sat_mask = saturation.point(lambda p: 1 if p >= 30 else 0)
    pix = sat_mask.load()
    top_cut = 0
    bottom_cut = height

    def row_ratio(y: int) -> float:
        return sum(pix[x, y] for x in range(width)) / max(width, 1)

    y = 0
    while y < min(height, int(height * 0.12)) and row_ratio(y) >= min_banner_ratio:
        y += 1
    if y > 12:
        top_cut = y

    y = height - 1
    while y >= max(0, int(height * 0.82)) and row_ratio(y) >= min_banner_ratio:
        y -= 1
    if height - 1 - y > 12:
        bottom_cut = y + 1

    if top_cut == 0 and bottom_cut == height:
        return gray
    out = gray.copy()
    draw_top = Image.new("L", (width, top_cut), 255)
    if top_cut:
        out.paste(draw_top, (0, 0))
    if bottom_cut < height:
        out.paste(Image.new("L", (width, height - bottom_cut), 255), (0, bottom_cut))
    return out


def split_image(image: Image.Image, chunk_height: int, overlap: int) -> List[Image.Image]:
    width, height = image.size
    chunks: List[Image.Image] = []
    y = 0
    while y < height:
        bottom = min(height, y + chunk_height)
        chunks.append(image.crop((0, y, width, bottom)))
        if bottom == height:
            break
        y = max(0, bottom - overlap)
    return chunks


def ocr_image(image_path: Path, language: str) -> str:
    system = platform.system().lower()
    if system == "darwin":
        return ocr_image_macos(image_path, language)
    if system == "windows":
        return ocr_image_windows(image_path, language)
    raise RuntimeError(f"Unsupported OCR platform: {platform.system()}. Use Windows or macOS.")


def ocr_image_windows(image_path: Path, language: str) -> str:
    if not WIN_OCR.exists():
        raise FileNotFoundError(f"Missing OCR helper: {WIN_OCR}")
    proc = run(
        [
            "powershell",
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(WIN_OCR),
            "-ImagePath",
            str(image_path),
            "-Language",
            language,
        ],
        timeout=120,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"Windows OCR failed for {image_path}\n{proc.stderr.strip()}")
    return proc.stdout


def macos_language_tag(language: str) -> str:
    mapping = {
        "zh-Hans-CN": "zh-Hans",
        "zh-Hant-TW": "zh-Hant",
        "en-US": "en-US",
    }
    return mapping.get(language, language)


def ocr_image_macos(image_path: Path, language: str) -> str:
    if not MAC_OCR.exists():
        raise FileNotFoundError(f"Missing OCR helper: {MAC_OCR}")
    proc = run(
        [
            "swift",
            str(MAC_OCR),
            str(image_path),
            macos_language_tag(language),
        ],
        timeout=120,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"macOS Vision OCR failed for {image_path}\n{proc.stderr.strip()}")
    return proc.stdout


def normalize_fullwidth(text: str) -> str:
    text = unicodedata.normalize("NFKC", text)
    return text.replace("％", "%").replace("—", "-").replace("－", "-")


def compact_for_compare(text: str) -> str:
    text = normalize_fullwidth(text)
    text = re.sub(r"\s+", "", text)
    text = re.sub(r"[^\w\u4e00-\u9fff.%+-]", "", text)
    return text.lower()


def clean_ocr_text(text: str) -> str:
    text = normalize_fullwidth(text)
    text = re.sub(r"\s+", "", text)
    for pattern in NOISE_PATTERNS:
        text = re.sub(pattern, "", text, flags=re.IGNORECASE)
    text = trim_leading_ui_noise(text)
    text = re.sub(r"[|｜]{2,}", "", text)
    text = re.sub(r"([。！？!?；;])", r"\1\n", text)
    text = re.sub(r"\n{2,}", "\n", text)
    return text.strip()


def trim_leading_ui_noise(text: str) -> str:
    """Drop OCR debris before the first likely body sentence in social screenshots."""
    head = text[:240]
    anchors = ("今天我们", "今天", "今日", "盘面", "市场", "先说", "首先", "简单说", "说说")
    positions = [head.find(anchor) for anchor in anchors if head.find(anchor) > 8]
    if not positions:
        return text
    cut = min(positions)
    prefix = head[:cut]
    noisy_markers = ("阅读", "读数", "卖数", "投诉", "Jacky", "周评", "月评", "甴", "巧")
    if cut < 180 and (any(marker in prefix for marker in noisy_markers) or len(prefix) > 30):
        return text[cut:]
    return text


def merge_overlap(existing: str, new: str, min_overlap: int = 18, max_overlap: int = 500) -> str:
    if not existing:
        return new
    a = compact_for_compare(existing)
    b = compact_for_compare(new)
    limit = min(max_overlap, len(a), len(b))
    for size in range(limit, min_overlap - 1, -1):
        if a[-size:] == b[:size]:
            raw_cut = len(new)
            compact_seen = 0
            for idx, char in enumerate(new):
                if compact_for_compare(char):
                    compact_seen += 1
                if compact_seen >= size:
                    raw_cut = idx + 1
                    break
            return existing + new[raw_cut:]
    return existing + "\n" + new


def similarity(reference: str, hypothesis: str) -> float:
    ref = compact_for_compare(reference)
    hyp = compact_for_compare(hypothesis)
    if not ref:
        return 0.0
    matcher = SequenceMatcher(None, ref, hyp, autojunk=False)
    matched = sum(block.size for block in matcher.get_matching_blocks())
    return matched / len(ref)


def extract_text_from_pdf(
    pdf: Path,
    work_root: Path,
    dpi: int,
    chunk_height: int,
    overlap: int,
    language: str,
    sat_threshold: int,
    light_threshold: int,
    source: str,
    extract_width: int,
    keep_work: bool,
) -> str:
    pdf_work = work_root / work_slug_for(pdf)
    render_dir = pdf_work / "rendered"
    image_dir = pdf_work / "extracted"
    chunk_dir = pdf_work / "chunks"
    render_dir.mkdir(parents=True, exist_ok=True)
    chunk_dir.mkdir(parents=True, exist_ok=True)

    if source in {"auto", "text"}:
        text_layer = clean_ocr_text(extract_pdf_text_layout(pdf) or extract_pdf_text(pdf))
        if looks_like_article_text(text_layer):
            if not keep_work:
                shutil.rmtree(pdf_work, ignore_errors=True)
            return text_layer
        if source == "text":
            raise RuntimeError(f"No substantial clean text layer found in {pdf}")

    raw_parts: List[str] = []
    page_images: List[Path] = []
    if source in {"auto", "extract"}:
        page_images = extract_pdf_images(pdf, image_dir)
        if source == "extract" and not page_images:
            raise RuntimeError(f"No embedded article images found in {pdf}")
    if not page_images:
        page_images = render_pdf(pdf, render_dir, dpi)

    for page_index, page_image in enumerate(page_images, start=1):
        with Image.open(page_image) as im:
            if page_image.parent == image_dir:
                im = prepare_extracted_image(im, extract_width)
            cleaned = remove_colored_watermark(im, sat_threshold, light_threshold)
        for chunk_index, chunk in enumerate(split_image(cleaned, chunk_height, overlap), start=1):
            chunk_path = chunk_dir / f"p{page_index:03d}_{chunk_index:03d}.png"
            chunk.save(chunk_path)
            raw_parts.append(ocr_image(chunk_path, language))

    merged = ""
    for part in raw_parts:
        cleaned_part = clean_ocr_text(part)
        if cleaned_part:
            merged = merge_overlap(merged, cleaned_part)

    if not keep_work:
        shutil.rmtree(pdf_work, ignore_errors=True)
    return merged


def process_pdf(
    pdf: Path,
    out_dir: Path,
    work_root: Path,
    dpi: int,
    chunk_height: int,
    overlap: int,
    language: str,
    sat_threshold: int,
    light_threshold: int,
    source: str,
    extract_width: int,
    reference_text: Optional[str],
    threshold: float,
    keep_work: bool,
) -> OcrResult:
    stem = slug_for(pdf)
    merged = extract_text_from_pdf(
        pdf=pdf,
        work_root=work_root,
        dpi=dpi,
        chunk_height=chunk_height,
        overlap=overlap,
        language=language,
        sat_threshold=sat_threshold,
        light_threshold=light_threshold,
        source=source,
        extract_width=extract_width,
        keep_work=keep_work,
    )
    out_dir.mkdir(parents=True, exist_ok=True)
    output = out_dir / f"{stem}.txt"
    output.write_text(merged + "\n", encoding="utf-8")
    score = similarity(reference_text, merged) if reference_text else None
    return OcrResult(
        pdf=pdf,
        output=output,
        chars=len(compact_for_compare(merged)),
        quality=score,
        passed=(score >= threshold) if score is not None else None,
    )


def load_reference(path: Optional[Path]) -> Optional[str]:
    if not path:
        return None
    return path.read_text(encoding="utf-8")


def write_stdin_pdf(target_dir: Path) -> Path:
    target_dir.mkdir(parents=True, exist_ok=True)
    data = sys.stdin.buffer.read()
    if not data:
        raise RuntimeError("No PDF bytes received on stdin.")
    pdf = target_dir / "stdin.pdf"
    pdf.write_bytes(data)
    return pdf


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(description="OCR image-heavy Chinese PDFs into text.")
    parser.add_argument("input", nargs="?", default="-", help="PDF file, directory, or '-' to read PDF bytes from stdin")
    parser.add_argument("--out-dir", default="ocr_output", help="Where text files are written")
    parser.add_argument("--work-dir", default=".ocr_work", help="Temporary render/chunk directory")
    parser.add_argument("--dpi", type=int, default=150, help="PDF render DPI")
    parser.add_argument("--chunk-height", type=int, default=4200, help="Chunk height under Windows OCR max dimension")
    parser.add_argument("--overlap", type=int, default=220, help="Vertical overlap between chunks")
    parser.add_argument("--language", default="zh-Hans-CN", help="Windows OCR language tag")
    parser.add_argument("--sat-threshold", type=int, default=35, help="Color saturation threshold for watermark removal")
    parser.add_argument("--light-threshold", type=int, default=75, help="Only remove colored pixels lighter than this")
    parser.add_argument("--source", choices=["auto", "text", "extract", "render"], default="auto", help="Use text layer, embedded images, or rendered PDF")
    parser.add_argument("--extract-width", type=int, default=2200, help="Upscale extracted PDF images to this width before OCR")
    parser.add_argument("--reference", type=Path, help="Visual transcription file for quality scoring")
    parser.add_argument("--threshold", type=float, default=0.95, help="Passing quality threshold")
    parser.add_argument("--limit", type=int, help="Limit number of PDFs processed")
    parser.add_argument("--keep-work", action="store_true", help="Keep rendered and chunk images for inspection")
    parser.add_argument("--stdout", action="store_true", help="Write extracted text to stdout instead of a .txt file")
    parser.add_argument("--quiet", action="store_true", help="Suppress progress messages on stderr")
    args = parser.parse_args(argv)

    out_dir = Path(args.out_dir).resolve()
    work_dir = Path(args.work_dir).resolve()
    reference_text = load_reference(args.reference)
    pipe_mode = args.input == "-"
    stdout_mode = args.stdout or pipe_mode

    def log(message: str) -> None:
        if not args.quiet and not stdout_mode:
            print(message, file=sys.stderr)

    if pipe_mode:
        with tempfile.TemporaryDirectory(prefix="pdf_ocr_") as tmp:
            tmp_root = Path(tmp)
            pdf = write_stdin_pdf(tmp_root)
            text = extract_text_from_pdf(
                pdf=pdf,
                work_root=tmp_root / "work",
                dpi=args.dpi,
                chunk_height=args.chunk_height,
                overlap=args.overlap,
                language=args.language,
                sat_threshold=args.sat_threshold,
                light_threshold=args.light_threshold,
                source=args.source,
                extract_width=args.extract_width,
                keep_work=args.keep_work,
            )
            sys.stdout.write(text)
            if text and not text.endswith("\n"):
                sys.stdout.write("\n")
            if reference_text:
                score = similarity(reference_text, text)
                print(f"quality={score:.2%} pass={score >= args.threshold}", file=sys.stderr)
                return 0 if score >= args.threshold else 1
            return 0

    input_path = Path(args.input).resolve()
    pdfs = find_pdfs(input_path)
    if args.limit:
        pdfs = pdfs[: args.limit]
    if not pdfs:
        print(f"No PDFs found under {input_path}", file=sys.stderr)
        return 2
    if stdout_mode and len(pdfs) != 1:
        print("--stdout requires exactly one PDF input.", file=sys.stderr)
        return 2

    work_dir.mkdir(parents=True, exist_ok=True)
    results: List[OcrResult] = []
    for index, pdf in enumerate(pdfs, start=1):
        log(f"[{index}/{len(pdfs)}] OCR {pdf}")
        if stdout_mode:
            text = extract_text_from_pdf(
                pdf=pdf,
                work_root=work_dir,
                dpi=args.dpi,
                chunk_height=args.chunk_height,
                overlap=args.overlap,
                language=args.language,
                sat_threshold=args.sat_threshold,
                light_threshold=args.light_threshold,
                source=args.source,
                extract_width=args.extract_width,
                keep_work=args.keep_work,
            )
            sys.stdout.write(text)
            if text and not text.endswith("\n"):
                sys.stdout.write("\n")
            if reference_text:
                score = similarity(reference_text, text)
                print(f"quality={score:.2%} pass={score >= args.threshold}", file=sys.stderr)
                return 0 if score >= args.threshold else 1
            return 0
        result = process_pdf(
            pdf=pdf,
            out_dir=out_dir,
            work_root=work_dir,
            dpi=args.dpi,
            chunk_height=args.chunk_height,
            overlap=args.overlap,
            language=args.language,
            sat_threshold=args.sat_threshold,
            light_threshold=args.light_threshold,
            source=args.source,
            extract_width=args.extract_width,
            reference_text=reference_text,
            threshold=args.threshold,
            keep_work=args.keep_work,
        )
        results.append(result)
        quality = "" if result.quality is None else f" quality={result.quality:.2%} pass={result.passed}"
        log(f"  -> {result.output} chars={result.chars}{quality}")

    if reference_text and any(r.passed is False for r in results):
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
