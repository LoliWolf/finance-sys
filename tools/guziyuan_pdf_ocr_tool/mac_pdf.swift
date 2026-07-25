import AppKit
import CoreGraphics
import Foundation
import PDFKit

func fail(_ message: String, _ code: Int32 = 1) -> Never {
    FileHandle.standardError.write((message + "\n").data(using: .utf8)!)
    exit(code)
}

func openDocument(_ path: String) -> PDFDocument {
    let url = URL(fileURLWithPath: path)
    guard let document = PDFDocument(url: url) else {
        fail("Unable to open PDF: \(path)")
    }
    return document
}

func renderPage(_ page: PDFPage, dpi: CGFloat, outputURL: URL) {
    let bounds = page.bounds(for: .mediaBox)
    let scale = dpi / 72.0
    let width = max(1, Int(ceil(bounds.width * scale)))
    let height = max(1, Int(ceil(bounds.height * scale)))
    let colorSpace = CGColorSpaceCreateDeviceRGB()
    let bitmapInfo = CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedLast.rawValue)

    guard let context = CGContext(
        data: nil,
        width: width,
        height: height,
        bitsPerComponent: 8,
        bytesPerRow: 0,
        space: colorSpace,
        bitmapInfo: bitmapInfo.rawValue
    ) else {
        fail("Unable to create bitmap context for \(outputURL.path)")
    }

    context.setFillColor(NSColor.white.cgColor)
    context.fill(CGRect(x: 0, y: 0, width: width, height: height))
    context.saveGState()
    context.scaleBy(x: scale, y: scale)
    context.translateBy(x: -bounds.minX, y: -bounds.minY)
    page.draw(with: .mediaBox, to: context)
    context.restoreGState()

    guard let image = context.makeImage() else {
        fail("Unable to create rendered image for \(outputURL.path)")
    }
    let bitmap = NSBitmapImageRep(cgImage: image)
    guard let data = bitmap.representation(using: .png, properties: [:]) else {
        fail("Unable to encode PNG: \(outputURL.path)")
    }
    do {
        try data.write(to: outputURL, options: .atomic)
    } catch {
        fail("Unable to write PNG \(outputURL.path): \(error.localizedDescription)")
    }
}

if CommandLine.arguments.count < 3 {
    fail("Usage: mac_pdf.swift <text|render> <pdf-path> [output-dir dpi]")
}

let action = CommandLine.arguments[1]
let pdfPath = CommandLine.arguments[2]
let document = openDocument(pdfPath)

switch action {
case "text":
    print(document.string ?? "", terminator: "")
case "render":
    guard CommandLine.arguments.count >= 5 else {
        fail("Usage: mac_pdf.swift render <pdf-path> <output-dir> <dpi>")
    }
    let outputDirectory = URL(fileURLWithPath: CommandLine.arguments[3], isDirectory: true)
    guard let dpiValue = Double(CommandLine.arguments[4]), dpiValue > 0 else {
        fail("DPI must be a positive number: \(CommandLine.arguments[4])")
    }
    do {
        try FileManager.default.createDirectory(
            at: outputDirectory,
            withIntermediateDirectories: true
        )
    } catch {
        fail("Unable to create render directory: \(error.localizedDescription)")
    }
    for index in 0..<document.pageCount {
        guard let page = document.page(at: index) else {
            fail("Unable to read PDF page \(index + 1)")
        }
        let name = String(format: "page-%03d.png", index + 1)
        renderPage(
            page,
            dpi: CGFloat(dpiValue),
            outputURL: outputDirectory.appendingPathComponent(name)
        )
    }
default:
    fail("Unknown action: \(action)")
}
