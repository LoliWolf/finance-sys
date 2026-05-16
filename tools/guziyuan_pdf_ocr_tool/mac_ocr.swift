import Foundation
import Vision
import AppKit

func fail(_ message: String, _ code: Int32 = 1) -> Never {
    FileHandle.standardError.write((message + "\n").data(using: .utf8)!)
    exit(code)
}

if CommandLine.arguments.count < 2 {
    fail("Usage: mac_ocr.swift <image-path> [language-tag]")
}

let imagePath = CommandLine.arguments[1]
let language = CommandLine.arguments.count >= 3 ? CommandLine.arguments[2] : "zh-Hans"
let url = URL(fileURLWithPath: imagePath)

guard let nsImage = NSImage(contentsOf: url) else {
    fail("Unable to open image: \(imagePath)")
}

var rect = NSRect(origin: .zero, size: nsImage.size)
guard let cgImage = nsImage.cgImage(forProposedRect: &rect, context: nil, hints: nil) else {
    fail("Unable to convert image to CGImage: \(imagePath)")
}

if #available(macOS 10.15, *) {
    var recognized: [String] = []
    let request = VNRecognizeTextRequest { request, error in
        if let error = error {
            fail("Vision OCR failed: \(error.localizedDescription)")
        }
        guard let observations = request.results as? [VNRecognizedTextObservation] else {
            return
        }
        recognized = observations.compactMap { observation in
            observation.topCandidates(1).first?.string
        }
    }

    request.recognitionLevel = .accurate
    request.usesLanguageCorrection = true
    if #available(macOS 11.0, *) {
        request.recognitionLanguages = [language, "en-US"]
    }

    let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
    do {
        try handler.perform([request])
    } catch {
        fail("Vision request failed: \(error.localizedDescription)")
    }

    print(recognized.joined(separator: "\n"), terminator: "")
} else {
    fail("macOS Vision OCR requires macOS 10.15 or newer.")
}
