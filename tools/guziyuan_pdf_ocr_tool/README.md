# PDF OCR Tool

用于处理带 PDF 叠加水印、内嵌截图、透明/彩色水印的中文财经文章 PDF。

默认管道模式是：

```text
PDF 二进制输入 -> UTF-8 纯文本输出
```

## 运行环境

- Python 3.9+
- Python 依赖：`pip install -r requirements.txt`
- Windows：Poppler 的 `pdftoppm` 必须在 `PATH` 中；`pdftotext`、`pdftohtml`、`pdfimages` 为可选优化。
- Windows：使用系统 Windows.Media.Ocr，需安装中文 OCR 语言包 `zh-Hans-CN`
- macOS：使用系统 PDFKit 渲染和 Vision OCR，不要求安装 Poppler；需要 macOS 10.15+，并能运行 `swift`

建议把 OCR 依赖放在工具自身的虚拟环境：

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
```

Windows 可使用 `py -3 -m venv .venv` 和 `.venv\Scripts\python.exe -m pip install -r requirements.txt`。wrapper 会优先使用 OCR 工具自身的 `.venv`，然后使用 `$PYTHON`/`%PYTHON%`，最后才查找系统 Python。macOS 缺少 Poppler 时会自动降级到 PDFKit 整页渲染；Windows 仍使用 `pdftoppm` 渲染。

## 二进制输入，纯文本输出

Windows 推荐用 `cmd.exe` 的 `type` 配合 bat，避免 PowerShell 5 的 `type/Get-Content` 把 PDF 当文本流处理：

```bat
type input.pdf | pdf_ocr_tool\ocr_pdf.bat - > output.txt
```

如果你当前在 PowerShell 里，请用 `cmd /c` 包一层：

```powershell
cmd /c "type input.pdf | pdf_ocr_tool\ocr_pdf.bat - > output.txt"
```

或者直接传文件路径，让工具只把正文写到 stdout：

```powershell
pdf_ocr_tool\ocr_pdf.bat input.pdf --stdout > output.txt
```

在 macOS 上推荐使用仓库 wrapper：

```bash
tools/guziyuan_pdf_ocr_tool/ocr_pdf.sh - < input.pdf > output.txt
```

`-` 表示从 `stdin` 读取 PDF bytes。该模式下正文只写到 `stdout`；错误和质量评估信息写到 `stderr`。

## 文件输入，纯文本输出

```bash
tools/guziyuan_pdf_ocr_tool/ocr_pdf.sh input.pdf --stdout > output.txt
```

Windows bat：

```bat
pdf_ocr_tool\ocr_pdf.bat input.pdf --stdout > output.txt
```

## 批量输出文件

```bash
tools/guziyuan_pdf_ocr_tool/ocr_pdf.sh pdf_folder --out-dir ocr_output
```

每个 PDF 会生成一个 UTF-8 文本文件：

```text
<原PDF文件名>_<hash>.txt
```

## 处理链路

默认 `--source auto` 会按以下顺序自动选择最佳链路：

1. PDF XML 文本层，按字体颜色/透明度过滤水印。
2. PDF 内嵌原图，绕过 PDF 叠加文字水印。
3. 整页渲染后调用系统 OCR 兜底；macOS 使用 PDFKit + Vision，Windows 使用 Poppler + Windows.Media.Ocr。

可手动指定：

```bash
--source text
--source extract
--source render
```

## 质量评估

```bash
tools/guziyuan_pdf_ocr_tool/ocr_pdf.sh input.pdf --stdout --reference reference.txt --threshold 0.99 > output.txt
```

返回码：

- `0`：处理成功，且质量评估通过或未提供参考文本。
- `1`：处理完成，但质量评估低于阈值。
- `2`：输入路径下没有 PDF，或 stdout 模式传入了多个 PDF。

## 从其他程序调用

stdin/stdout 最稳的协议：

```text
stdin:  PDF bytes
stdout: UTF-8 text
stderr: diagnostics
exit:   0/1/2
```

示例：

```bash
/path/to/pdf_ocr_tool/ocr_pdf.sh - < input.pdf > output.txt
```

Windows：

```bat
type input.pdf | C:\path\to\pdf_ocr_tool\ocr_pdf.bat - > output.txt
```
