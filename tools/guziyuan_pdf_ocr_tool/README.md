# PDF OCR Tool

用于处理带 PDF 叠加水印、内嵌截图、透明/彩色水印的中文财经文章 PDF。

默认管道模式是：

```text
PDF 二进制输入 -> UTF-8 纯文本输出
```

## 运行环境

- Python 3.9+
- Python 依赖：`pip install -r requirements.txt`
- Poppler 命令行工具在 `PATH` 中：`pdftotext`、`pdftohtml`、`pdfimages`、`pdftoppm`
- Windows：使用系统 Windows.Media.Ocr，需安装中文 OCR 语言包 `zh-Hans-CN`
- macOS：使用系统 Vision OCR，通过 `mac_ocr.swift` 调用；需要 macOS 10.15+，并能运行 `swift`

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

跨平台推荐直接调用 Python：

```bash
python pdf_ocr_tool/ocr_pdf_articles.py - < input.pdf > output.txt
```

`-` 表示从 `stdin` 读取 PDF bytes。该模式下正文只写到 `stdout`；错误和质量评估信息写到 `stderr`。

## 文件输入，纯文本输出

```bash
python pdf_ocr_tool/ocr_pdf_articles.py input.pdf --stdout > output.txt
```

Windows bat：

```bat
pdf_ocr_tool\ocr_pdf.bat input.pdf --stdout > output.txt
```

## 批量输出文件

```bash
python pdf_ocr_tool/ocr_pdf_articles.py pdf_folder --out-dir ocr_output
```

每个 PDF 会生成一个 UTF-8 文本文件：

```text
<原PDF文件名>_<hash>.txt
```

## 处理链路

默认 `--source auto` 会按以下顺序自动选择最佳链路：

1. PDF XML 文本层，按字体颜色/透明度过滤水印。
2. PDF 内嵌原图，绕过 PDF 叠加文字水印。
3. 整页渲染后调用系统 OCR 兜底。

可手动指定：

```bash
--source text
--source extract
--source render
```

## 质量评估

```bash
python pdf_ocr_tool/ocr_pdf_articles.py input.pdf --stdout --reference reference.txt --threshold 0.99 > output.txt
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
python /path/to/pdf_ocr_tool/ocr_pdf_articles.py - < input.pdf > output.txt
```

Windows：

```bat
type input.pdf | C:\path\to\pdf_ocr_tool\ocr_pdf.bat - > output.txt
```
